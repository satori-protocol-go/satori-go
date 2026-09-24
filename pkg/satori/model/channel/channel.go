package channel

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

type ChannelType uint8 // 频道类型

const (
	ChannelTypeText     ChannelType = iota // 文本频道
	ChannelTypeDirect                      // 私聊频道
	ChannelTypeCategory                    // 分类频道
	ChannelTypeVoice                       // 语音频道
)

// 频道
type Channel struct {
	fields   types.FieldPresence
	Id       string      `json:"id"`                  // 频道 ID
	Type     ChannelType `json:"type"`                // 频道类型
	Name     string      `json:"name,omitempty"`      // 频道名称
	ParentId string      `json:"parent_id,omitempty"` // 父频道 ID
}

func (u *Channel) UnmarshalJSON(data []byte) error {
	type plain Channel
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*u = Channel(value)
	u.fields = fields
	return nil
}
func (u Channel) MarshalJSON() ([]byte, error) {
	out := map[string]any{"id": u.Id, "type": u.Type}
	u.fields.Put(out, "name", u.Name, u.Name != "")
	u.fields.Put(out, "parent_id", u.ParentId, u.ParentId != "")
	return json.Marshal(out)
}
