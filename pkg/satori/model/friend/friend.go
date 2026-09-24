package friend

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

// 好友
type Friend struct {
	fields types.FieldPresence
	User   *user.User `json:"user,omitempty"` // 用户对象
	Nick   string     `json:"nick,omitempty"` // 好友昵称
}

func (u *Friend) UnmarshalJSON(data []byte) error {
	type plain Friend
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*u = Friend(value)
	u.fields = fields
	return nil
}
func (u Friend) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	u.fields.Put(out, "user", u.User, u.User != nil)
	u.fields.Put(out, "nick", u.Nick, u.Nick != "")
	return json.Marshal(out)
}

func (u Friend) WithoutUser() *Friend { u.User = nil; u.fields = u.fields.Without("user"); return &u }
