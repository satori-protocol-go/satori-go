package guildrole

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

// 群组角色
type GuildRole struct {
	fields types.FieldPresence
	Id     string `json:"id"`             // 角色 ID
	Name   string `json:"name,omitempty"` // 角色名称
}

func (u *GuildRole) UnmarshalJSON(data []byte) error {
	type plain GuildRole
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*u = GuildRole(value)
	u.fields = fields
	return nil
}
func (u GuildRole) MarshalJSON() ([]byte, error) {
	out := map[string]any{"id": u.Id}
	u.fields.Put(out, "name", u.Name, u.Name != "")
	return json.Marshal(out)
}
