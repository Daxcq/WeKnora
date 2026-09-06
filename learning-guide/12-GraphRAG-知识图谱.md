# 第 12 章 · Graph RAG：知识图谱

## 12.1 它解决什么病：向量检索的多跳盲区

问"Docker 和 Kubernetes 是什么关系？"——Docker 的 chunk 讲容器、Kubernetes 的 chunk 讲编排，两段各自相似度都不低，但**没有任何一段同时讲两者的关系**。向量是"逐段打分"，天生不会把分散多处的信息**连起来**。

Graph RAG：在切分向量化之外，**再抽一层实体关系图**（节点=实体，边=关系），关系型问题走图查询。Neo4j 是图数据库。

## 12.2 写侧：`internal/application/service/graph.go`（1039 行）

`graphBuilder.BuildGraph`（356）对每个 chunk 三步：

**① 抽实体**（`extractEntities:99`）：chunk 内容喂 LLM（提示词模板 `config.Conversation.ExtractEntitiesPrompt`），要求返回 JSON 数组（134 `ParseLLMJsonResponse` 解析）。然后**按标题去重合并**（164–185）：

```go
if existEntity, exists := b.entityMapByTitle[entity.Title]; !exists {
    entity.ChunkIDs = []string{chunk.ID}; entity.Frequency = 1   // 新实体
} else {
    existEntity.ChunkIDs = append(existEntity.ChunkIDs, chunk.ID) // 已存在：只记出处
    existEntity.Frequency++
}
```

**为什么合并而不是多节点**（图谱碎裂问题）：Docker 在 chunk-3 和 chunk-42 出现，若建两个节点，"Kubernetes 依赖 Docker"这条边（来自 chunk-17）只挂在其中一个上——另一个成孤岛。查"Docker 的关联"时**看运气**：找到左边知道 Kubernetes、丢了安全性；找到右边反之。**图的价值全在边的连通性，碎裂 = 白建**。合并成单节点后：一个 Docker 节点挂 `ChunkIDs=[3,42]`（出处追踪，查到概念能跳回原文）+ 全部关系边（不丢）。

进阶难题：不同 chunk 可能用不同名字称呼同一实体（"Docker" vs "容器技术"）——按标题合并也解决不了，这叫**实体消解（entity resolution）**，所有 Graph RAG 系统的经典痛点。

**② 抽关系**（`extractRelationships:194`）：实体列表 + 合并原文再喂一次 LLM，判断语义关系（"Kubernetes 依赖 Docker"）。200 行：实体 <2 个直接跳过——一个节点成不了边。

**③ 权重与度**（`calculateWeights:482`、`calculateDegrees:574`）：关系出现越频繁权重越高；实体连接的边越多"度"越大（枢纽概念）。`GetRelationChunks:682` 按权重取相关 chunk；`GetIndirectRelationChunks:747` 用 **DFS 走两跳**找间接关联。

成品写 Neo4j。

## 12.3 触发时机与"整图重建"

挂在写路径异步增强段（`knowledge_post_process.go`）。增量重解析里的钩子（`knowledge_process.go:357–364`）：

```go
if len(removedIDs) > 0 || newCount > 0 {
    s.graphEngine.DelGraph(ctx, []types.NameSpace{namespace})   // 有变化 → 图谱重建
} else {
    logger.Infof(ctx, "[Incremental] No chunks changed, skipping graph deletion")
}
```

**为什么 chunk 一变就整图重建，而不像向量那样单块修补？** 三个理由：

1. **图是互联结构 + 全局统计量**：边的证据来自 chunk（chunk 删了边悬空，节点连带要级联检查）；权重/度是**跨 chunk 的频次统计**，一个 chunk 变了权重分布就变，检索排序跟着变。向量按 chunk **孤立**（只依赖自己内容），所以能单块修补；图不孤立；
2. **LLM 抽取非确定性**：向量化是确定性的（同文字+同模型=逐位相同的向量——你增量缓存的根基）；但同一段文字重抽，实体名可能漂移（这次 "Docker"，下次 "容器技术"）。连"没变的 chunk"和图的关系都可能漂移——**没法定义"只修变化部分"**，增量 patch 会悄悄制造不一致，比不做更糟；
3. **增量维护需要跨版本实体对齐**（旧图的 Docker 节点 = 新抽出来的 Docker 吗？）——又撞实体消解难题，做错比不做更糟。

**整图重建 = 用成本换一致性**（要么全新要么全旧，不存在半新半旧的怪胎）。也是增量缓存方案最大的优化金矿：LLM 抽取比 embedding 贵几个数量级。

## 12.4 读侧：同一图谱，两条消费路径

**路径一：Agent 工具**（`internal/agent/tools/query_knowledge_graph.go`）。图谱作为暴露给 Agent 的一个工具（83 `Execute`：并发查多库、自动去重）。工具描述里还有组合建议（50–53）：

> 关系探索：query_knowledge_graph → list_knowledge_chunks（看细节）
> 主题研究：knowledge_search → query_knowledge_graph（深挖实体关系）

——这是在教模型**组合工具**。

**路径二：流水线实体检索**（`chat_pipeline/search_entity.go` / `extract_entity.go`）：先从问题抽实体名，再查图邻域 chunk 补进检索结果。

## 12.5 三条读路径对比

| | 固定流水线 | Agent | Graph RAG |
|---|---|---|---|
| 谁决定流程 | 代码 | **LLM** | 代码/LLM 皆可 |
| 擅长 | 单轮事实问答 | 多步任务、复合问题 | **多跳关系**问题 |
| 成本 | 中（1 次生成+检索） | 高（多轮 LLM+工具） | 写入贵（每 chunk 2 次 LLM），查询便宜 |
| 失败模式 | 各插件降级 | 死循环/乱用工具→围栏 | 图谱过期→重建策略 |

## 自测

- 12-1 为什么"Docker 和 Kubernetes 的关系"这类问题向量检索答不好？
- 12-2 实体去重为什么按 Title 合并 ChunkIDs 而不是建多节点？用"图谱碎裂"说。
- 12-3 图谱为什么整图重建而向量可以增量？三个理由。
- 12-4 "LLM 抽取非确定性"如何具体破坏增量修补的假设？
- 12-5 图谱的两条读路径分别是什么？各适合什么问题？
