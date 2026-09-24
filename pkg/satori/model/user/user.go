package user

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

// 用户
type User struct {
	fields types.FieldPresence
	Id     string `json:"id"`               // 用户 ID
	Name   string `json:"name,omitempty"`   // 用户名称
	Nick   string `json:"nick,omitempty"`   // 用户昵称
	Avatar string `json:"avatar,omitempty"` // 用户头像
	IsBot  bool   `json:"is_bot,omitempty"` // 是否为机器人
}

func (u *User) UnmarshalJSON(data []byte) error {
	type plain User
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*u = User(value)
	u.fields = fields
	return nil
}
func (u User) MarshalJSON() ([]byte, error) {
	out := map[string]any{"id": u.Id}
	u.fields.Put(out, "name", u.Name, u.Name != "")
	u.fields.Put(out, "nick", u.Nick, u.Nick != "")
	u.fields.Put(out, "avatar", u.Avatar, u.Avatar != "")
	u.fields.Put(out, "is_bot", u.IsBot, u.IsBot)
	return json.Marshal(out)
}
