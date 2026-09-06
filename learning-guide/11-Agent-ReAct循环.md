# 第 11 章 · Agent：ReAct 循环

## 11.1 和固定流水线的本质区别

读路径（第 5 章）是**流水线**：改写→检索→精排→生成，顺序写死，每次提问走全套。但有的问题不需要全套，有的问题需要**多轮**检索（先查 A，发现缺 B，再查 B）——固定流水线做不到。

Agent 模式把**控制流从代码移交给 LLM**：

```
固定流水线：代码决定"查→排→拼→答"，LLM 只负责答题
Agent：     LLM 决定每步干什么（调哪个工具、传什么参数），代码只负责执行和兜底
```

这就是 **ReAct**（Reasoning + Acting）：思考（我需要什么信息？）→ 行动（调工具）→ 观察（结果如何？）→ 再思考……直到给出最终答案。

**哲学追问**："LLM 也是代码组成的，区别到底在哪？"——对，LLM 推理也是代码（权重+矩阵乘法）。区别在三个维度：

1. **决策书写时间**：传统代码的控制流在开发期由程序员逐条枚举写死（每个 if 都是人预想过的情形）；LLM 的控制流在运行期现场生成，分支规则压缩在权重里——**学出来的，不是写出来的**；
2. **决策空间可否枚举**：开放式任务的路径组合爆炸，程序员只能枚举"典型路径"；学习出的函数能对**没见过的输入**给出合理分支。实践原则：**人类枚举得动的空间用代码，枚举不动的交给学出来的函数**；
3. **代价是可验证性**：枚举代码可穷举测试；学习函数无法穷举。所以 Agent 必然是混合体——

> **代码拥有不变量，LLM 拥有不变量之内的选择权。**
> 硬围栏（MaxIterations、工具白名单、schema 校验、卡死检测）是代码的领地；围栏内选哪个工具、传什么参数、何时收手，是 LLM 的领地。

控制流光谱（WeKnora 三种形态全有）：

| 形态 | 控制流谁定 | 仓库实例 |
|---|---|---|
| 固定流水线 | 全代码 | chat_pipeline 十个插件按序跑 |
| 路由器（半 Agent） | LLM 在枚举好的选项里选 | query_understand 判意图；AddIf 裁剪 |
| 完全 Agent | LLM 自由组合 | agent/engine.go 的 ReAct 循环 |

选型原则：**任务越开放越往右，越需要可预测越往左**。

## 11.2 主循环：`internal/agent/engine.go`

`Execute`（188）初始化 `AgentState`（记录每轮步骤）→ `executeLoop`（344）→ 每轮 `runReActIteration`（450）。骨架：

```go
for state.CurrentRound < e.config.MaxIterations {
    select {
    case <-ctx.Done():            // 用户点"停止"或超时
        // 已有工具结果就"抢救"出一份答案
    default:
    }
    outcome := e.runReActIteration(...)   // think → act → observe 一轮
    switch outcome {
    case iterOutcomeContinue: continue loop  // 空响应重试，不计数
    case iterOutcomeBreak:    break loop     // 最终答案 / 卡死
    case iterOutcomeNext:     state.CurrentRound++
    }
}
```

四个文件对应四阶段：`think.go`（带工具列表流式调 LLM）、`act.go`（执行工具调用）、`observe.go`（结果塞回消息历史喂下一轮）、`finalize.go`（最终答案）。**文件名即架构**。

## 11.3 四个生产级防御

1. **空转与卡死检测**（375–377 的 `emptyRetries`/`consecutiveSameContent`）：连续输出一模一样 = 死循环，强制 break。`MaxIterations` 是硬围栏，"连续相同输出"是软检测——双保险。**LLM 会抽风，代码必须设围栏**；
2. **取消时抢救**（385–391）：用户中途停止，已有 3 轮工具结果时用它们合成答案再退，不直接丢弃；
3. **取消后跳过兜底生成**（421–423 注释）：循环跑满上限时补生成答案，但 ctx 已取消时跳过——否则兜底调用失败，把通用"抱歉，无法生成"泄漏给用户主动放弃的对话；
4. **完成事件恰好一次**（365–373）：`completionEmitted` 标志 + `defer` 保证每条退出路径（正常/取消/出错）恰好发一次完成事件（触发前端落库步骤历史）。

## 11.4 工具系统：`internal/agent/tools/`（约 30 个）

| 工具 | 干什么 |
|---|---|
| `knowledge_search.go` | 语义检索 |
| `grep_chunks.go` | 关键词/正则精确搜索 |
| `query_knowledge_graph.go` | 查知识图谱（第 12 章） |
| `data_analysis.go` | DuckDB 跑 SQL，沙箱执行 |
| `mcp_tool.go` | 调外部 MCP 服务的工具 |
| `web_search` / `web_fetch` | 联网 |

## 11.5 工具描述工程（教学瑰宝）

`knowledge_search.go:22-78` 的描述文本不只是描述功能，是在**教模型什么时候不该用它**：

> "Does NOT perform exact keyword matching… Should NOT be used to locate specific strings or error codes. For literal/keyword/entity search, another tool should be used."

还规定输入形态：1–5 条"语义化短问句"，**禁止**贴用户原文整段。三个精确职能：

1. **防错选工具**：模型拿语义搜索找 `ERR-1042` → 召回废物 → 整轮 ReAct 作废甚至循环到上限。描述在选错**之前**拦截；
2. **参数整形**：垃圾输入→垃圾检索→浪费一轮。描述教模型**怎么喂**；
3. **划清边界防内耗**：knowledge_search 和 grep_chunks 职责有重叠区，不写清"谁管语义、谁管精确"，模型来回乱试。

**工具描述的质量直接决定 ReAct 循环的轮数**——它是"给 AI 读的 API 文档"。写 function calling 工具照此标准。

## 11.6 体验与安全的细节

- `act.go:117` `toolDisplayNames`：内部工具名翻成中文展示（"关键词搜索""查询数据"）——前端体验；
- `toolHintSensitiveArgs`（136）：`database_query` 的参数（原始 SQL）**不在界面提示里显示**——防泄漏实现细节。前端体验里也有安全账；
- 并行工具（178–209）：模型一次要求 2+ 个工具且配置允许 → errgroup 并发执行、按原序回填（errgroup 自带"一个出错全体取消"）。

## 自测

- 11-1 Agent 和固定流水线的本质区别？"代码定围栏、LLM 做选择"各举三例。
- 11-2 为什么工具描述花大篇幅写"不要用我做什么"？
- 11-3 `consecutiveSameContent` 检测什么？为什么需要软检测+硬上限双保险？
- 11-4 用户中途点停止后，系统对已有工具结果做什么？为什么取消后跳过兜底生成？
