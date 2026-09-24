# satori-go

Satori 协议的 Go 客户端、服务端和 QQ / Satori 桥接适配器。平台原生通信由 botgo-plus 提供；部署、持久存储和业务重试由上层应用负责。

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
| `guild.get` | 封装 | 可注册 | 501 | 实现 | 按上游 |
| `guild.list` | 封装 | 可注册 | 404 | 实现 | 按上游 |
| `guild.approve` | 封装 | 可注册 | 501 | 404 | 按上游 |
| `guild.member.list` | 封装 | 可注册 | 501 | 实现 | 按上游 |
| `guild.member.get` | 封装 | 可注册 | 501 | 实现 | 按上游 |
| `guild.member.kick` | 封装 | 可注册 | 501 | 实现 | 按上游 |
| `guild.member.mute` | 封装 | 可注册 | 501 | 实现 | 按上游 |
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

`features` 使用已实现的能力集合；QQFeatures / QQGuildFeatures 只能限制默认集合，不能声明缺少实现的能力。meta、READY 和 login 更新复用同一份登录资料。`admin/login.list` 为历史客户端扩展，不在标准 API 支持承诺中。

日常验证：`go test ./...`。测试只调用本地合成服务，不表示已经完成真实 QQ 权限或媒体联调。
