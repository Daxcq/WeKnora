# 第 1 章 · 初识 WeKnora 与 RAG

## WeKnora 是什么

**腾讯开源的企业级 RAG（检索增强生成）知识库系统**。把文档、网页、Wiki 喂给它，它把资料"消化"存好；之后用自然语言提问，它先从资料里**检索**出相关片段，再把片段作为上下文交给大模型（LLM），让模型**基于你的资料**回答——而不是靠模型自己的记忆瞎编。

RAG 的通俗类比：

> LLM 像一位博览群书但没读过**你公司内部文档**的专家。RAG 就是考前塞给这位专家一份"开卷资料"——先帮你查资料（检索），再让他照着资料答题（生成）。答案有出处、可溯源、能引用。

**重要认知**：本仓库是 WeKnora 的 **Go 语言重写版**（`go.mod` 中模块名 `github.com/Tencent/WeKnora`）。网上旧教程多为早期 Python 版，目录结构完全不同。现在仅剩两个 Python 组件：`docreader/`（文档解析边车）和 `mcp-server/`（MCP 协议服务）。

## 系统组成（docker-compose.yml）

| 服务 | 技术 | 职责 |
|---|---|---|
| `app` | Go + Gin（:8080） | API 中枢，所有业务逻辑 |
| `frontend` | Vue 3 + TypeScript + TDesign | Web 界面（Nginx 托管） |
| `docreader` | Python，gRPC :50051 | 把 PDF/Word/PPT/图片等解析成文本 |
| `postgres`/`mysql`/`sqlite` | — | 业务数据库（元数据、用户、会话） |
| 向量库（可插拔） | pgvector/Qdrant/Milvus/ES 等 11 种 | 存 embedding 向量，相似度检索 |
| `redis` | — | asynq 异步任务队列 + SSE 消息流 |
| `neo4j` | — | 知识图谱（Graph RAG，可选） |
| `mcp`/`sandbox`/`searxng` | — | MCP 工具 / 代码沙箱 / 联网搜索（可选） |

## 三个心智模型

**① 分层清晰的三层架构**

```
internal/handler/            收 HTTP 请求（守门员，薄）
internal/application/service/  业务大脑（约 80 个文件，厚）
internal/application/repository/ 数据库访问（DAO）
```

依赖注入用 `uber.org/dig`，在 `internal/container/container.go` 统一装配。

**② 耗时活全走异步队列**

解析、切分、向量化、摘要等慢操作不卡 HTTP 请求，丢进 Redis 的 asynq 队列由 worker 消费。任务注册表在 `internal/router/task.go`（约 20 个 handler）。

**③ 一切皆可插拔**

向量库（`RETRIEVE_DRIVER` 环境变量）、LLM/Embedding/Rerank 模型（`internal/models/` 每家厂商一个适配器）、对象存储（`internal/application/service/file/`）、文档解析器（内置 + 外部插件）。工厂模式贯穿全库，如 `internal/container/engine_factory.go`。

## 顶层目录速查

```
cmd/server/     API 服务入口        cmd/desktop/   Wails 桌面版
internal/       全部后端业务逻辑     pkg/pluginapi/ 对外插件契约
docreader/      Python 解析边车      mcp-server/    MCP 服务端
frontend/       Vue3 前端           client/        Go SDK
cli/            命令行              config/        YAML 配置
migrations/     SQL 迁移            docs/          设计文档
examples/plugins/  插件示例与模板    plugins/       已安装插件
```

## 动手

按 `README.md` 的 Getting Started 用 docker-compose 启动（轻量单机看 `docs/LITE.md`）。上传一份 PDF、建知识库、提问——**先当用户，建立体感**。

## 自测

- 1-1 用"开卷考试"的类比说清 RAG 里检索和生成各自的职责。
- 1-2 WeKnora 的三个心智模型是什么？为什么"耗时活"要走异步队列？
- 1-3 本仓库的 Go 版和网上旧教程的 Python 版，区别在哪？还剩哪些 Python 组件？
