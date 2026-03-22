package friend

import "github.com/satori-protocol-go/satori-go/pkg/satori/model/user"

// 好友
type Friend struct {
	User *user.User `json:"user,omitempty"` // 用户对象
	Nick string     `json:"nick,omitempty"` // 好友昵称
}
