# 第 15 章 · 前端与 SSE：最后一公里

## 15.1 技术栈与结构

```
Vue 3.5（<script setup> 组合式 API）+ TypeScript + Vite
Pinia 3（状态）+ vue-router 4（路由）+ TDesign（UI 组件库）
axios（HTTP）+ @microsoft/fetch-event-source（SSE）
marked + katex + mermaid + highlight.js（Markdown/公式/图/代码渲染）
vue-i18n（国际化）
```

目录映射（`frontend/src/`）：

| 目录 | 职责 |
|---|---|
| `router/index.ts` | 路由：`/login`、`/platform/knowledge-bases`、`/chat/:id` 等 |
| `api/` | 与后端域名一一对应：`chat/`、`knowledge-base/`、`model/`、`agent/`、`mcp-service.ts`、`vector-store.ts`… |
| `utils/request.ts` | 共享 axios 实例（baseURL、auth/token、i18n 拦截器） |
| `stores/` | Pinia：`auth`（会话）、`settings`（模型配置）、`knowledge`、`chatResources`… |
| `views/` | 页面：`chat/`、`knowledge/`、`agent/`、`settings/`… |

**分层纪律与后端镜像**：`api/` 每个文件对应后端一组路由；组件不直接写 URL，一律走 `api/` → `request.ts`。前端"三层"= 组件（handler）→ api 模块（service）→ axios 实例（repository）。

## 15.2 SSE 消费：`api/chat/streame.ts`

后端（第 5 章）经 EventBus 推出的事件帧，前端用 `fetchEventSource` 接收：

```ts
import { fetchEventSource } from '@microsoft/fetch-event-source';
// useStream() 组合式函数内：
await fetchEventSource(url, {
  method: 'POST',
  headers: { Authorization: ... },
  onmessage: (ev) => {
    // 按 ev.event / data.type 分发到不同处理
  },
});
```

为什么用 `fetch-event-source` 而不是原生 `EventSource`：原生只支持 GET、不能带 Authorization 头、不能 POST body——聊天接口是 POST + 认证头，必须用 fetch 版。

**事件类型 → UI 现象对照**（第 5 章插件的出口）：

| 后端事件 | 前端现象 |
|---|---|
| 进度事件（retrieval） | "正在检索知识库"转圈 |
| `EventAgentThought`（thinking） | 思考过程折叠卡片逐字出现 |
| `EventAgentFinalAnswer`（answer） | 答案区打字机效果 |
| references | 引用列表（文件名+位置，点击跳转） |
| `Done: true` / complete | 流关闭，输入框解锁 |

一个流两路渲染（思考/答案分离）就是后端 `chat_completion_stream.go` 分流事件的直接映射。

## 15.3 与后端的契约要点

- **SSE 是单向的**：服务器→浏览器。用户"停止生成"走**另一条** HTTP 请求（取消后端 ctx——这正是第 11 章 agent 里 `ctx.Done()` 抢救逻辑的触发源）；
- **引用先于答案到达**（后端特意在开流前发引用事件——5.3 的 bug 疤痕）：前端可以在答案流出前就渲染引用区；
- **done 标志**：最后一条事件带 `Done: true`，前端据此结束打字机状态——不依赖连接关闭（连接可能被代理截断）。

## 15.4 状态管理要点

- `stores/auth.ts`：token、当前租户/组织——所有请求的 Authorization 来源；
- `stores/settings.ts`：系统/模型设置——对话参数（TopK、阈值等）的客户端缓存；
- `stores/chatResources.ts`：当前会话的消息、引用资源缓存；
- `stores/ui.ts` / `menu.ts`：纯 UI 状态。

## 15.5 动手

1. 打开聊天页提问，DevTools Network 里找到那条 SSE 请求，观察 `text/event-stream` 帧的时序——和上表对照；
2. 在 `streame.ts` 的 `onmessage` 加一行 `console.log(ev.event, ev.data)`，重跑一次，把事件序列抄下来——你会看到完整的"进度→思考→答案→引用→完成"剧本；
3. 中途点"停止生成"，观察：一条新的取消请求发出 + SSE 流终止——两条独立 HTTP 的配合。

## 自测

- 15-1 为什么聊天 SSE 必须用 fetch-event-source 而不是原生 EventSource？
- 15-2 "停止生成"是怎么实现的？它和 SSE 流是什么关系？
- 15-3 引用为什么在答案之前到达？这是后端哪个设计决策的结果？
- 15-4 前端 api/ 目录的组织方式与后端什么原则互为镜像？
