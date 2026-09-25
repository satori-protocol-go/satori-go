# satori-go

Satori 协议的 Go 客户端、服务端，以及 QQ / Satori 桥接适配器。

## 组件边界

`model` 与 `protocol` 定义资源和传输契约；`client` 负责 API 调用、登录快照和事件接收；`server` 负责路由、下游事件编号与临时资源；`adapter/satori` 将上游 Satori 服务作为下游适配器接入。部署、持久存储、业务幂等和业务重试由上层应用负责。

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

## 客户端接入

```go
app, err := client.NewApp(client.WebSocketConfig{
    Host: "127.0.0.1", Port: 5140, Token: token,
})
if err != nil { return err }
app.Register(func(account *client.Account, evt *event.Event) error {
    // 使用 account.Platform()、account.SelfID() 和 evt.Sn 处理事件。
    return nil
})
err = app.Run(ctx)
```

Webhook 接收端使用 `client.WebhookConfig`：`ServerHost` / `ServerPort` / `ServerPath` 与 `ServerToken` 用于访问上游；`Host` / `Port` / `Path` 与 `Token` 用于本地接收和验证推送。启动时获取 meta，但不会自动注册或删除远端订阅。

`App.Run(ctx)` 管理网络退出和清理；运行中的 `App.Close()` 请求取消。需要等待最终结果时，等待 `Run` 返回或读取 `RunAsync` 的结果。登录的可变资料通过 `SelfInfo()` / `Config()` / `Adapter()` 读取快照。事件按同一 WebSocket 上的接收顺序处理；READY 后才标记账号可用。Webhook 回调失败返回 503。

## 请求与事件

标准 API 使用 POST 和 Satori 身份头。配置 Token 后，RPC、meta 与 Webhook 管理均验证 Bearer Token。正式身份头 `Satori-Platform` / `Satori-User-ID` 优先于历史身份头。未配置 Token 表示不启用这层鉴权。

`Internal` / `RequestInternal` 返回原生 `*http.Response`，保留状态、响应头和响应体；调用方必须关闭 Body。原生请求保留方法、查询参数、二进制请求体及平台自定义请求头。直接访问第三方资源不会附带 Satori API 的凭证。

`event.sn` 是事件流序号，`login.sn` 是来源连接内的登录编号，`platform + user.id` 是 API 账号身份。服务端为不同来源映射本运行期的下游登录编号；READY 替换该来源的登录集合，META 更新代理信息，局部 login 事件按实际出现的字段更新。事件序号 0 可用于恢复。不要将运行期编号持久化为永久账号 ID。

服务端默认缓存最近 100 条事件，历史重放与实时发送串行衔接。队列满时可取消地等待。一个下游 Webhook 失败会返回或记录错误，但不会终止其他推送；没有自动补发或跨进程去重。

## 上传、资源与消息

`UploadCreateNamed` / `UploadCreate` 上传临时资源，返回 `internal:<platform>/<self_id>/_tmp/<opaque-name>`。默认请求上限 32 MiB，可通过 `MaxRequestBytes` 调整；临时资源保留 10 分钟。自定义上传处理器使用 `server.UploadCreateParam`，按 name 索引 `UploadFile`，保留 Filename、ContentType 和 Data。

纯读 GET/HEAD 可以通过不透明资源链接下载，完整链接应作为访问凭据保护；`internal:.../_api/...` 与写操作仍需鉴权。内部资源校验登录归属与路径。资源字段、消息内容保留“缺省”“显式 null”和“显式空值”的区别；消息元素保留标签及属性。

## QQ

QQ 图片、音频、视频和文件元素可使用 HTTP/HTTPS、data URI 或本服务生成的内部资源。内部资源先取实际字节，再使用 SDK 群/C2C 上传并发送 `file_info`；子频道本地图片使用 SDK 的 multipart。RPC 不允许直接读取服务器任意 `file://` 或裸文件路径，应用应先通过上传接口提供字节。资源读取或上传失败返回错误，不把损坏的内部地址当文字伪装成功。

QQ 审核使用 App + audit_id 的有限关联，保留早到结果，默认等待和结果保留窗口为 60 秒，最多 1024 个关联项。明确通过且有 message_id 才形成成功消息；拒绝、等待截止和取消可区分。未得到审核结果时不宣称已成功送达。

QQ type=11/12 的互动响应由适配器统一调用 SDK 完成，成功后交付 Satori 互动事件。WebSocket 与 Webhook 使用同一责任点。默认由适配器自动响应；需要应用自行决定响应码时设置 `ManualInteractionResponse: true`，然后通过原生入口调用平台接口。原生 PUT 不再被 SDK 预先禁止；应用应选择一个响应责任人，避免重复响应。本实现没有跨进程去重服务。

## Satori 桥接

`adapter/satori.New(Config{Host, Port, Token, ...})` 可作为 Adapter 交给 `Server.Apply`。桥接列出上游全部登录，并按请求的 platform/self_id 选择账号；同一目标身份存在歧义时返回 409。原生请求、资源响应保留上游状态、响应头和流式响应体。`PostUpload` 决定是否将上传转发上游，默认由本地服务处理。

## 日志与验证

组件通过 `logging.Logger.Log(context.Context, logging.Level, ...any)` 接收日志实现。服务端使用 `server.Config.Logger` 或 `RegisterLogger`，客户端使用 `app.RegisterLogger`，桥接使用 `adapter/satori.Config.Logger`。Logger 应支持并发调用；生命周期和刷新由应用管理。

在仓库根目录执行 `go test ./...`。测试使用本机假服务验证协议、客户端、服务端与桥接行为；通过本地测试不等于生产部署验证。

## 协议与许可证

以 [Satori 协议说明](https://satori.chat/zh-CN/introduction.html) 为契约，许可证见 [LICENSE](LICENSE)。