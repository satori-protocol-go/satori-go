package login

import "github.com/satori-protocol-go/satori-go/pkg/satori/model/user"

type LoginStatus uint8

const (
	LoginStatusOffline    LoginStatus = iota // 离线
	LoginStatusOnline                        // 在线
	LoginStatusConnect                       // 正在连接
	LoginStatusDisconnect                    // 正在断开连接
	LoginStatusReconnect                     // 正在重新连接
)

// 登录信息
type Login struct {
	Sn       int64       `json:"sn"`                 // 序列号
	Platform string      `json:"platform,omitempty"` // 平台名称
	User     *user.User  `json:"user,omitempty"`     // 用户对象
	Status   LoginStatus `json:"status"`             // 登录状态
	Adapter  string      `json:"adapter"`            // 适配器名称
	Features []string    `json:"features,omitempty"` // 平台特性 列表
}
