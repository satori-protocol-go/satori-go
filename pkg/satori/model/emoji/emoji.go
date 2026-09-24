package emoji

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

// 表情
type Emoji struct {
	fields types.FieldPresence
	Id     string `json:"id"`             // 表情 ID
	Name   string `json:"name,omitempty"` // 表情名称
}

func (u *Emoji) UnmarshalJSON(data []byte) error {
	type plain Emoji
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*u = Emoji(value)
	u.fields = fields
	return nil
}
func (u Emoji) MarshalJSON() ([]byte, error) {
	out := map[string]any{"id": u.Id}
	u.fields.Put(out, "name", u.Name, u.Name != "")
	return json.Marshal(out)
}
