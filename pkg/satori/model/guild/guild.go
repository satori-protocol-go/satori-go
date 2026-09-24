package guild

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

// 群组
type Guild struct {
	fields types.FieldPresence
	Id     string `json:"id"`               // 群组 ID
	Name   string `json:"name,omitempty"`   // 群组名称
	Avatar string `json:"avatar,omitempty"` // 群组头像
}

func (u *Guild) UnmarshalJSON(data []byte) error {
	type plain Guild
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*u = Guild(value)
	u.fields = fields
	return nil
}
func (u Guild) MarshalJSON() ([]byte, error) {
	out := map[string]any{"id": u.Id}
	u.fields.Put(out, "name", u.Name, u.Name != "")
	u.fields.Put(out, "avatar", u.Avatar, u.Avatar != "")
	return json.Marshal(out)
}
