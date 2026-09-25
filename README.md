# satori-go

Satori 协议的 Go 客户端、服务端与 Satori 桥接适配器。

## 组件边界

`model` 与 `protocol` 定义资源和传输契约；`client` 负责 API 调用、登录快照和事件接收；`server` 负责路由、下游事件编号与临时资源；`adapter/satori` 将上游 Satori 服务作为下游适配器接入。部署、持久存储、业务幂等和业务重试由上层应用负责。

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

## Satori 桥接

`adapter/satori.New(Config{Host, Port, Token, ...})` 可作为 Adapter 交给 `Server.Apply`。桥接列出上游全部登录，并按请求的 platform/self_id 选择账号；同一目标身份存在歧义时返回 409。原生请求、资源响应保留上游状态、响应头和流式响应体。`PostUpload` 决定是否将上传转发上游，默认由本地服务处理。

## 日志与验证

组件通过 `logging.Logger.Log(context.Context, logging.Level, ...any)` 接收日志实现。服务端使用 `server.Config.Logger` 或 `RegisterLogger`，客户端使用 `app.RegisterLogger`，桥接使用 `adapter/satori.Config.Logger`。Logger 应支持并发调用；生命周期和刷新由应用管理。

在仓库根目录执行 `go test ./...`。测试使用本机假服务验证协议、客户端、服务端与桥接行为；通过本地测试不等于生产部署验证。

## 协议与许可证

以 [Satori 协议说明](https://satori.chat/zh-CN/introduction.html) 为契约，许可证见 [LICENSE](LICENSE)。