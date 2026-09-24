# satori-go

Satori 协议的 Go 客户端、服务端，以及 QQ / Satori 桥接适配器。

## 架构与运行边界

`model` / `protocol` 负责资源和传输契约；`client` 负责调用、登录快照与事件接收；`server` 负责路由、下游事件编号和临时资源；`adapter/qq` 负责 QQ 与 Satori 的转换。QQ 鉴权、HTTP、Webhook 验签、WebSocket 和媒体上传由 botgo-plus 提供。部署、持久存储、业务幂等及业务重试由上层应用负责。

当前要求 Go 1.25.4 或更高版本，沿用本项目现有工具链基线。依赖固定为 `github.com/WindowsSov8forUs/botgo-plus v0.2.0`，解析到提交 `0decd473af449940151b2145d57f99fabcbc6458`；这是新的原生 SDK 版本，不是历史独立实现的 v1.1.0。仓库不使用绝对路径 `replace`。

## 启动 QQ 适配服务

以下程序从环境变量读取凭证，监听本机 5140 端口。默认使用 QQ Webhook，回调路径是 `/qqbot`；QQ 平台访问此路径时使用自己的签名验证，不使用 Satori Token。实际部署应配置 HTTPS 反向代理和 QQ 平台的回调地址。

```go
package main

import (
    "context"
    "errors"
    "log"
    "os"
    "os/signal"
    "strconv"

    adapterqq "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq"
    "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func run() error {
    appID, err := strconv.ParseUint(os.Getenv("QQBOT_APP_ID"), 10, 64)
    if err != nil || appID == 0 {
        return errors.New("QQBOT_APP_ID must be a positive integer")
    }
    secret, token := os.Getenv("QQBOT_APP_SECRET"), os.Getenv("SATORI_TOKEN")
    if secret == "" || token == "" {
        return errors.New("QQBOT_APP_SECRET and SATORI_TOKEN are required")
    }
    srv, err := server.NewServer(server.Config{Host: "127.0.0.1", Port: 5140, Token: token})
    if err != nil { return err }
    defer srv.Close()
    adapter, err := adapterqq.New(adapterqq.Config{AppID: appID, Secret: secret, Path: "/qqbot"})
    if err != nil { return err }
    if err := srv.Apply(adapter); err != nil { return err }
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()
    return srv.Run(ctx)
}

func main() {
    if err := run(); err != nil { log.Fatal(err) }
}
```

`Config.Apps` 可配置多个 QQ App，每项使用独立凭证、客户端和登录资料。`UseWebSocket: true` 切换到 SDK 网关接入；此时不同时注册 QQ Webhook 路由。默认遍历每个 App 返回的分片；指定 `WSShardCount` 时仅连接指定的 `WSShardID`。登录进入 Online 取决于其配置的分片就绪状态。

`APIBaseURL`、`TokenURL`、`TokenSource`、`HTTPClient` 和 `UploadConfig` 提供显式配置或测试注入。注入的基础 HTTP 客户端应不包含 QQ 鉴权中间件，凭证由 SDK 添加。`QQFeatures` / `QQGuildFeatures` 为 nil 时采用已实现的默认声明；显式提供时保留原数组，可包含平台扩展特性，空数组表示空声明。声明不实现 API，也不替代平台授权。WebSocket 模式已注入 TokenSource 时不额外要求 Secret；自行获取 Token 或启用 Webhook 验签时仍需要 Secret。

## 使用 Satori 客户端

```go
package main

import (
    "context"
    "errors"
    "log"
    "os"
    "os/signal"

    "github.com/satori-protocol-go/satori-go/pkg/satori/client"
    "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

func run() error {
    token := os.Getenv("SATORI_TOKEN")
    if token == "" { return errors.New("SATORI_TOKEN is required") }
    app, err := client.NewApp(client.WebSocketConfig{Host: "127.0.0.1", Port: 5140, Token: token})
    if err != nil { return err }
    app.Register(func(account *client.Account, evt *event.Event) error {
        log.Printf("platform=%s self_id=%s event=%s sn=%d", account.Platform(), account.SelfID(), evt.Type, evt.Sn)
        return nil
    })
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()
    return app.Run(ctx)
}

func main() {
    if err := run(); err != nil { log.Fatal(err) }
}
```

`account.Protocol.Send(ctx, evt, content)` 使用事件中的 channel/referrer 回复。QQ 一次调用会返回实际发送的消息及更新后的 `Message.Referrer`；继续回复同一上下文时，传入上一条返回消息的 referrer。`msg_id` / `event_id` 是回复凭据，`<quote id="..."/>` 是显示引用，不能相互替代。调用方负责多个并发请求的序号协调。分段发送中途失败时，HTTP 错误体保留 `messages` 中已经成功的结果；客户端的请求错误保留响应体，不自动重发已发送分段。

Webhook 接收客户端使用 `client.WebhookConfig`：`ServerHost` / `ServerPort` / `ServerPath` 指向上游 API，`ServerToken` 用于调用上游；`Host` / `Port` / `Path` 是本地接收地址，`Token` 验证反向推送。这两个 Token 可以不同。当前 `Secure` 描述对外 URL 的协议，本地接收器本身是 HTTP，应由反向代理终止 TLS。客户端启动时获取 meta；反向事件地址需显式通过上游配置或 `WebhookCreate` 注册，启动客户端不会自动订阅或删除远端订阅。

## 请求、状态和生命周期

标准 API 使用 POST 和 Satori 身份头。配置 Token 后，RPC、meta 和 meta 的 Webhook 管理都验证 Bearer Token。正式身份头 `Satori-Platform` / `Satori-User-ID` 优先于历史 `X-Platform` / `X-Self-ID`。未配置 Token 表示显式选择不启用这层鉴权，包括监听所有网卡的部署；不宜将这种配置直接暴露给不可信请求。示例要求环境变量 Token 是示例的部署策略，不是协议或构造函数的强制条件。请求缺少平台身份头返回 400；缺少已配置的访问 Token 返回 401。

`Internal` / `RequestInternal` 返回原生 `*http.Response`，保留状态、响应头和原始响应体；调用方必须关闭 Body。原生请求的 method、查询参数和二进制请求体按原生接口处理，标准 API 则使用 Satori JSON 模型。原生接口保留自定义平台请求头与分段到达的流式响应，只替换本地鉴权/身份头并剥离逐跳 HTTP 头。`MessageList` 的 limit=0 表示使用平台默认值，通用层不固定分页数量；QQ 的数量上限只在 QQ 适配器处理。`Download` 返回资源字节。只有指向所配置 Satori API 的请求自动附带其凭证，直接访问第三方资源不携带该 Token。

`event.sn` 是事件流序号，`login.sn` 是来源连接内的登录编号，`platform + user.id` 是 API 账号身份。服务端为不同来源映射本运行期下游登录编号，READY、meta 和该下游事件流使用对应视图；HTTP `login.get` 执行其真实注册处理器，不被通用层替换成缓存查询。协议允许两次连接中的登录编号不同，不能据此给所有 HTTP 调用强制同一编号。不要持久化这些编号后当作永久账号 ID。重新 READY 会替换该来源的完整登录集合，META 只更新代理信息，局部 login 事件按字段存在性更新。读取可变账号状态使用 `SelfInfo()` / `Config()` / `Adapter()`；返回的登录快照可由调用者独立读取。

`App.Run(ctx)` 负责网络退出和清理；运行中的 `App.Close()` 请求同一取消，不在事件回调内等待自己结束。需要等待最终结果时，等待 `Run` 返回或读取 `RunAsync` 的结果。服务端 `Run` / `Shutdown` 返回实际运行或清理错误。网络实例关闭后应新建实例再运行。应用自己的回调也必须能够结束；SDK 不会强行终止阻塞的业务代码。

同一 Satori WebSocket 的事件按接收顺序处理，READY 后才标记可用。客户端每 10 秒发送心跳，未收到上一轮 PONG 时结束当前连接。回调错误会记录，恢复序号代表已经完成一次本地处理尝试，不表示业务成功；不通过重连偷偷替代业务重试。Webhook 成功响应表示本次同步接收路径完成，回调失败返回 503。

服务端默认缓存最近 100 条编码后的事件；历史重放与实时发送串行衔接，事件 SN=0 可以恢复。默认客户端和适配器缓冲区为 128 条，队列满时可取消地等待。QQ Webhook 的成功 ACK 表示事件进入适配器的内存交付队列，不表示已经持久化。一个下游 Webhook 失败会返回/记录实际错误，但不会停止其他推送或终止整个服务；没有自动补发。恢复事件可以携带当前 READY 已不包含的历史登录，客户端提供事件级账号上下文，不将它重新插入当前登录集合。缓存数量、队列容量、心跳容忍和写超时是本实现策略，不是平台配额；不承诺崩溃恢复、跨进程去重或 exactly-once。

## 上传、代理与消息元素

`UploadCreateNamed` / `UploadCreate` 上传临时资源，返回 `internal:<platform>/<self_id>/_tmp/<opaque-name>`。默认请求上限 32 MiB，可通过服务端 `MaxRequestBytes` 调整；临时资源保留 10 分钟，服务关闭时清理。这些是本地资源保护值。上传部件的 filename 可省略，name 必需且唯一，Content-Type 必需。自定义上传处理器使用 `server.UploadCreateParam`（按 name 索引的 `UploadFile`），字段为 Filename、ContentType、Data；不再使用会把无 filename 部件误分为普通值的 `*multipart.Form`。解析字节受同一请求上限约束，当前实现会将上传部件缓存在内存。

包含不透明资源标识的纯读 GET/HEAD 可以用于资源下载，不强制要求 Satori Token；完整链接应作为访问凭据保护。`internal:.../_api/...` 始终需要 Satori 鉴权，资源的写操作同样需要鉴权。内部资源验证登录归属，不允许通过改写用户部分或路径穿越读取其他文件。反向代理的公开静态资源和 QQ 回调是另外的路由边界。

QQ 图片、音频、视频和文件元素可使用 HTTP/HTTPS、data URI 或本服务生成的内部资源。内部资源先取实际字节，再使用 SDK 群/C2C 上传并发送 `file_info`；子频道本地图片使用 SDK 的 multipart。RPC 不允许直接读取服务器任意 `file://` 或裸文件路径，应用应先通过上传接口提供字节。资源读取或上传失败返回错误，不把损坏的内部地址当文字伪装成功。

消息元素保留标签、属性和子元素；真正的 text 节点才作为纯文本编码。陌生平台元素保留为扩展，不引入新的模板语言。QQ 接收的原生文本先转义，避免消息中的标签文字被误当成资源指令；未完整映射的原生内容仍保存在事件 `_data` 中。

## 桥接与 QQ 审核 / 互动

`adapter/satori.New(Config{Host, Port, Token, ...})` 可作为 Adapter 交给同一 `Server.Apply`。它列出上游全部登录，并按请求的 platform/self_id 选择账号；同一目标身份存在歧义时返回 409，而不是任取 map 首项。`PostUpload` 决定上传是否转发到上游；默认由本地服务处理上传。资源代理、原生请求和事件流使用各自现有入口。

QQ 审核使用 App + audit_id 的有限关联，保留早到结果，默认等待和结果保留窗口为 60 秒，最多 1024 个关联项。明确通过且有 message_id 才形成成功消息；拒绝、等待截止和取消可区分。未得到审核结果时不宣称已成功送达。

QQ type=11/12 的互动响应由适配器统一调用 SDK 完成，成功后交付 Satori 互动事件。WebSocket 与 Webhook 使用同一责任点。默认由适配器自动响应；需要应用自行决定响应码时设置 `ManualInteractionResponse: true`，然后通过原生入口调用平台接口。原生 PUT 不再被 SDK 预先禁止；应用应选择一个响应责任人，避免重复响应。本实现没有跨进程去重服务。

## 标准 API 支持矩阵

“实现”表示存在实际转换和调用路径，不代表账号已获平台权限。404 表示平台场景不支持；501 表示平台具备能力但适配器尚未实现；权限错误保留相应状态。QQ 频道列主要指普通子频道，频道私信的消息读取、编辑和本地图片另有限制。桥接保留上游能力，不生成虚假成功结果。

| 标准 API | 客户端 | 服务端 | QQ 群 / 单聊 | QQ 频道 | 桥接 |
| --- | --- | --- | --- | --- | --- |
| `message.create` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `message.update` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `message.get` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `message.delete` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `message.list` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `channel.get` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `channel.list` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `channel.create` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `channel.update` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `channel.delete` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `channel.mute` | 封装 | 可注册 | 404 | 404 | 按上游 |
| `user.channel.create` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `guild.get` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `guild.list` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.approve` | 封装 | 可注册 | 501 | 404 | 按上游 |
| `guild.member.list` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `guild.member.get` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `guild.member.kick` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `guild.member.mute` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `guild.member.approve` | 封装 | 可注册 | 501 | 404 | 按上游 |
| `guild.member.role.set` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.member.role.unset` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.role.list` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.role.create` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.role.update` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.role.delete` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `reaction.create` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `reaction.delete` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `reaction.clear` | 封装 | 可注册 | 404 | 404 | 按上游 |
| `reaction.list` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `login.get` | 封装 | 可注册 | 实现 | 实现 | 按上游 |
| `user.get` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `friend.list` | 封装 | 可注册 | 404 | 404 | 按上游 |
| `friend.delete` | 封装 | 可注册 | 404 | 404 | 按上游 |
| `friend.approve` | 封装 | 可注册 | 404 | 404 | 按上游 |
| `upload.create` | 封装 | 默认实现 | 实现 | 实现 | 按上游 |

默认 `features` 来自已实现能力；显式 QQFeatures / QQGuildFeatures 按调用方声明保留，扩展声明必须由调用方保证与实际平台、路由相符。meta、READY 和 login 更新复用同一份登录资料。`admin/login.list` 为历史客户端扩展，不在标准 API 支持承诺中。


## 当前边界与必要迁移

普通 QQ 群资料与成员管理仍需要平台授权；成员分页使用原生 `next_cursor`。永久移除的黑名单步骤部分失败时返回 502 和原始部分成功内容。频道消息列表支持 before/after 与 asc/desc；around 当前明确返回 501。

频道私信本地 multipart 尚未真实平台验证，当前返回 501；已存在的可下载公网图片 URL 可使用频道私信的 URL 方式。论坛发帖、更多审批/菜单端点、持久事件队列及其他平台适配器不在本轮实现范围。通用路由存在或 features 声明不等于账号具有所有权限。

本轮补充变化：自定义上传参数改为 `server.UploadCreateParam`；消息 content 正确保留省略、null 和空串；泛型参数层不再额外限制 JavaScript 安全整数范围，类型不匹配与 Go 整数溢出仍由解码器报错。

必要接口变化包括：`Message` 的 JSON 时间字段为 `created_at` / `updated_at`；登录可变字段改为快照访问器；`IdentifyBody.Sn` 使用指针表达序号 0 与省略的区别；`Internal` / `RequestInternal` 返回原始响应，由调用者关闭；Provider 的资源代理接收 context；事件桥接与账号同步返回实际错误。QQ 配置使用单一 TokenSource / 原生 Client，不再接收旧 APIV1/APIV2、BotToken 或跳过验签的配置。下游应迁移调用点，不应重新引入失效的兼容外壳。

## 日志注入与记录范围

统一接口为 `logging.Logger.Log(context.Context, logging.Level, ...any)`。应用可将自己的日志系统适配到该接口；传入 `logging.NopLogger{}` 可以关闭对应组件输出。Logger 应支持并发调用，其生命周期和最终刷新由应用管理。

| 组件 | 注入方式 | 作用范围 |
| --- | --- | --- |
| Satori 服务端 | `server.Config.Logger` / `srv.RegisterLogger` | 当前服务器 |
| Satori 客户端 | `app.RegisterLogger` | 当前 App 的网络与账号 HTTP 调用，包含后续登录 |
| 独立 Account | `account.RegisterLogger` | 当前账号 API 调用；CustomWith 继承 |
| Satori 桥接 | `adapter/satori.Config.Logger` / `adapter.RegisterLogger` | 当前桥接的客户端、网络和 API |
| QQ 适配器 | `adapter/qq.Config.Logger` / `adapter.RegisterLogger` | 当前适配器的接入、转换、原生 API 集成 |
| botgo-plus 自身 | `adapter/qq.RegisterSDKLogger` | SDK 现有的进程级 Logger，必须在启动并发 SDK 工作前显式配置 |

创建 QQ 适配器或替换其实例 Logger 不再改写 botgo-plus 的进程级 Logger。SDK 日志桥按 Debug/Info/Warn/Error 原级别转发，底层 Logger 提供 Sync 时一并传递。SDK 本身没有实例级 Logger 接口，这里不伪装成可按 App 隔离的底层能力。

日志记录请求结果和耗时、登录状态、网络就绪/断开、Webhook 鉴权与应答、事件入队、媒体上传阶段和审核关联；流式写入中断记为错误。例行请求详情使用 Debug，生命周期和已接收事件使用 Info，拒绝/异常状态使用 Warn 或 Error。默认日志实现输出所收到的级别，应用可在注入 Logger 内设置过滤。

SDK 新增的 HTTP/事件日志使用状态、类型、App/账号/消息 ID 等元数据，不打印请求体、消息正文、Token、Secret 或资源 URL。应用自行添加的回调错误信息和自定义 Logger 输出由应用负责控制。服务端心跳中的 read_wait_ms 表示读等待时长，不是网络往返延迟。

示例中的外部日志适配器可以这样定义：

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

type ApplicationLogger struct { Output *log.Logger }
func (l ApplicationLogger) Log(_ context.Context, level logging.Level, values ...any) {
    l.Output.Printf("[%s] %s", level, fmt.Sprint(values...))
}
var _ logging.Logger = ApplicationLogger{}
func main() {}
```

将该对象传给上述注入点即可；不需要运行额外的日志服务，也不会新增轮询流程。

## 开发与验证

在仓库根目录执行唯一日常入口：

```sh
go test ./...
```

测试按实际功能包集中维护，使用本机 HTTP/TLS/WebSocket 假服务，覆盖协议资源、权限、登录、重放、关闭、代理、QQ 消息/媒体/审核/互动/群管理。没有嵌套测试模块，也不需要真实 QQ 账号或 Python 服务。

CI 仅有一个 Linux 测试任务，读取 go.mod 的工具链版本并执行同一入口。重要并发修改可在提交前额外执行一次 `go test -race ./...`，不增加第二套常驻流程。部署凭证不应写进仓库、日志或测试夹具。

本地契约测试通过不能代替真实 QQ 权限、频控和媒体联调；也不表示 GlycCat 的旧调用已自动完成迁移。真实联调应另行配置并记录，README 不把 mock 结果写成生产完成。

## 协议与许可证

以 [Satori 协议说明](https://satori.chat/zh-CN/introduction.html) 和 [QQ 官方机器人文档](https://bot.q.qq.com/wiki/develop/api-v2/) 为契约。保留仓库 [LICENSE](LICENSE)；实现选择与协议强制要求分别说明。
