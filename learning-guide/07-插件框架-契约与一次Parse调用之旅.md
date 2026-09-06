# 第 7 章 · 插件框架：契约与一次 Parse 调用之旅

以 `examples/plugins/plain-text-parser`（docker runtime）为主角，走完一个插件的完整一生。

## 为什么需要插件系统

第 4 章写路径的解析器原本全是 WeKnora 自己写的（内置 Go 转换器、docreader、MinerU、PaddleOCR-VL）——想支持新格式得提 PR 等发版。插件系统把**扩展的权力从核心开发者手里交出去**：第三方不碰核心代码插进新能力。

类比：手机本体不生产所有功能，但留了 App Store 的规矩。

一个插件系统要回答四个问题：

```
1. 接口契约：插件实现哪些方法？  → pkg/pluginapi
2. 描述与发现：插件是谁、能干嘛？ → manifest.json + 扫描加载
3. 隔离与运行：跑在哪、挂了咋办？ → process / docker 两种 runtime
4. 集成点：插进主流程哪里？      → plugin_registry ↔ docparser
```

## 契约层：`pkg/pluginapi/plugin.go`（586 行）

**决策一：JSON over gRPC**（第 1–2 行注释）：

> 插件可以不 import WeKnora 内部包就被实现——Go/Python/Rust 皆可。

gRPC 只当运输壳，货是 JSON：`JSONCodec`（317 行）注册成 gRPC 编解码器，Marshal/Unmarshal 就是 `json.Marshal/Unmarshal`。没有 `.proto` 文件——`Plugin_ServiceDesc`（371 行）是手写的 `grpc.ServiceDesc`，`unaryHandler`（405）把反射式调用翻译成强类型方法。

**决策二：极小的实现面**（135–147）：

```go
type Connector interface {   // 数据源插件：把外部内容搬回来
    Validate / ListResources / ResolveResourceAncestors / FetchAll / FetchIncremental
}
type Parser interface {      // 解析插件：文件 bytes → Markdown
    Parse(...)
}
```

**职责红线**（docs/development/plugin-framework.md:106）：

> 插件只返回源数据/Markdown，**不切分、不调 embedding、不写知识库**。

扩展点必须窄（narrow seam）：接口越窄插件越笨但系统越稳——第三方代码崩溃/作恶/版本腐烂的影响面被窄缝挡住。**宽接口是平台之癌**。

**错误分层**（238–247）：业务错误**装进 `ParseResponse.Error` 字段、gRPC 层返回 nil**；传输错误才走 gRPC error 通道。原则：**传输错误用 error 通道，业务结果用数据结构**。

**示例插件全文 37 行**（main.go），核心 9 行：

```go
func (parser) Parse(_ context.Context, req *pluginapi.ParseRequest) (*pluginapi.ParseResponse, error) {
    // 校验 md/txt...
    return &pluginapi.ParseResponse{MarkdownContent: string(req.FileContent)}, nil
}
func main() {
    manifest := pluginapi.Manifest{ID: "plain-text-parser", ..., Runtime: {Type: "docker", ...}}
    pluginapi.Serve(manifest, parser{})   // 一行起 gRPC 服务器
}
```

## manifest：插件的身份证

四类信息（见 `plugins/plain-text-parser/manifest.json`）：

| 字段 | 例 | 作用 |
|---|---|---|
| 身份 | `id`/`version`/`protocol_version: 1` | 我是谁、说什么协议 |
| 能力 | `extension_types: ["document_parser"]`、`file_types` | 插哪、处理什么格式 |
| 兼容 | `weknora_version: ">=0.6.0 <1.0.0"` | 保证在此主机版本范围工作 |
| 运行+权限 | `runtime.docker.image`、`permissions.allow_network` | 怎么起、申请什么特权 |

校验：`ValidateManifest`（pluginapi:479，逐字段把关）+ `VersionCompatible`（519，手写 semver 区间）。版本区间是生态生死线：主机升级不悄悄弄死插件，插件不未测乱声明。**契约一旦发布就是永久负债**——这也是为什么文档说"先只外化数据源和解析器"。

## 第一幕：出生

`manager.go:43` `LoadDirectory` 扫 `WEKNORA_PLUGIN_DIR` → 每子目录 `findManifest`（277，json/yaml/yml 轮试）→ `NewProcessPlugin`（339）三道安检（340–352）：能读且字段全 / 有主机认识的扩展类型 / **process runtime 不能承诺断网 → 直接拒绝**（347）。

docker 型走 `newDockerCommand`（701），命令里的**七层防御**：

```
docker run --rm -i --name weknora-plugin-<id>-<纳秒时间戳>
  --user 65532:65532                ← 非 root UID
  --read-only                       ← 文件系统全只读
  --cap-drop ALL                    ← 剥掉全部 Linux capabilities
  --security-opt no-new-privileges  ← 禁提权
  --pids-limit 128                  ← fork 炸弹上限
  --network none                    ← 物理断网（allow_network:false 时）
  -e WEKNORA_PLUGIN_STDIO=1         ← 告诉插件用 stdin/stdout 说话
  --mount type=bind,src=...,dst=/weknora/inputs/0,readonly  ← 只读挂载白名单
  weknora-plugin-plain-text-parser:dev
```

细节：容器名带纳秒时间戳（崩溃重启不撞名）；`-i` 保持 stdin 打开（stdio 传输的物理前提）。

## 第二幕：接线——两种传输

**docker 型 → stdio 管**：主机拿容器 StdinPipe/StdoutPipe 包成 `stdioConn`（manager.go:731），然后**传输层注入**（417）：

```go
conn, err = grpc.DialContext(dialCtx, "passthrough:///weknora-plugin-stdio",
    grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
        return transport, nil     // 你要连接？直接给你这根管子
    }),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithBlock(),
    grpc.WithDefaultCallOptions(grpc.ForceCodec(pluginapi.JSONCodec{})))
```

gRPC 向假地址发起"TCP 连接"，拨号函数被劫持塞给它 stdio 管子——框架毫不知情照常收发 JSON。协议层与承载层解耦后，承载层可偷梁换柱。

**process 型 → 本机 TCP**（366–385）——临时端口技巧：

```go
listener, _ := net.Listen("tcp", "127.0.0.1:0")  // ":0" = OS 随机分配空闲端口
address = listener.Addr().String()               // 拿到 "127.0.0.1:54321"
_ = listener.Close()                             // 关掉让位给插件
cmd.Env = append(os.Environ(), "WEKNORA_PLUGIN_ADDR="+address)  // 环境变量告知
```

代价：关闭与插件绑定之间有极小竞争窗口——限定 127.0.0.1 且存活几秒，风险可接受。**没有完美方案，只有算过账的方案**。

插件侧：`Serve`（pluginapi:188）读 `WEKNORA_PLUGIN_STDIO=1` → `newStdioListener`（263——把 stdin/stdout 伪装成 `net.Listener`）；否则读 `WEKNORA_PLUGIN_ADDR` 起 TCP。

## 第三幕：体检——对暗号

（429–444）三连：

```go
remote, _ := connector.client.GetManifest(...)              // ① 你说你是谁？
if remote.Manifest.ID != manifest.ID ||
   remote.Manifest.ProtocolVersion != pluginapi.ProtocolVersion {   // ② 与文件一致吗？
    return nil, fmt.Errorf("plugin manifest handshake mismatch")
}
connector.Health(checkCtx)                                  // ③ 还活着吗？
```

不只信磁盘 manifest（可能被改/写错），让**运行中的进程亲口确认**身份和协议版本。全过后 `RegisterExternalParser`（plugin_registry.go:21）注册——主流程眼里它就是个普通 DocReader。

**补偿事务（Saga）**：`LoadDirectory` 里某扩展类型注册失败时，把前面已注册成功的**逆序撤销**再 Close（105–121 web search 失败→注销 model；133–136 parser 失败→注销 web search 和 model…）。多步注册没有整体事务，手工写"哪步失败回滚哪几步"——**顺序必须与注册严格互逆**。

## 第四幕：一次 Parse 调用的字节旅程

```
 1. ProcessDocument (knowledge_process.go:3147) → convert()
 2. engine_registry 按规则命中插件 "plain-text-parser"
 3. 注册表取出适配器 ProcessConnector → 调它的 Parse (manager.go:585)
 4. 适配器把 types.ReadRequest 翻译成 pluginapi.ParseRequest{FileContent: bytes}
 5. pluginClient.Parse (pluginapi:470)
    → 方法路径 "/weknora.plugin.v1.Plugin/Parse"
    → JSONCodec.Marshal(request) → ClientConn 发送 → 撞上 WithContextDialer
    → 字节写进 stdin 管子
 6. ──── 跨进程边界：容器 stdout/stdin ────
 7. 插件侧 stdioListener → gRPC server 收到 → ForceServerCodec(JSONCodec) 反序列化
    → connectorServer.Parse (pluginapi:238) → 你的 9 行业务代码被调用
    → ParseResponse Marshal 成 JSON 写回 stdout
 8. ──── 回到主机 ────
 9. pluginClient 收到 → Unmarshal → 适配器包成 types.ReadResult 返回 convert()
10. 流水线继续：切分 → 向量化 → 入库（插件的戏份到此结束）
```

十步里**插件业务代码只占第 7 步中间一行**——框架把跨进程协作的全部复杂度收走，插件作者只写领域逻辑。

## 第五幕：退休

WeKnora 收 SIGTERM（第 6 章优雅停机）→ `resourceCleaner` 调 `Manager.Close`（manager.go:255）：**先反注册**（从各注册表摘名字，新请求不再路由过来）→ **再逐个 Close**（关 gRPC 连接、杀进程、`docker rm -f`）。顺序是语义：**先摘路由再关进程**，否则出现"请求正路由过去、进程已死"的窗口。

插件侧收到 stdin 关闭 → `Serve` 里监听 stop 的 goroutine（pluginapi:202）触发 `GracefulStop`——处理完在途 RPC 才退。**主客双方按礼节告别**。

## 设计原则小结

1. 扩展点要窄：插件只报告事实，主机独占决策；
2. 承诺必须由能力兑现（process+断网 → 拒绝）；
3. 传输层注入：协议层与承载层解耦；
4. 握手验证 + 补偿回滚（Saga）；
5. 先摘路由、再关进程。

## 自测

- 7-1 为什么解析插件接口只有 Parse 一个方法，不让它顺便切分？
- 7-2 插件返回业务错误时，gRPC 层为什么返回 nil、错误装进 Response.Error？
- 7-3 externalParsers 注册表存 `interfaces.DocReader` 而非 `pluginapi.PluginClient`——这层"皮"给 ProcessDocument 什么好处？
- 7-4 `--rm` 和容器名的纳秒时间戳分别解决什么问题？
- 7-5 process 型插件的 gRPC 走什么？地址怎么约定？有什么缺陷？
- 7-6 握手比较 ProtocolVersion 防什么？改了主机没改插件会在哪失败？
- 7-7 datasource 注册失败要撤销哪几个已注册类型、什么顺序？
