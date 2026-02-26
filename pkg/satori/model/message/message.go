package message

import (
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

// 消息
type Message struct {
	Id       string                   `json:"id"`                  // 消息 ID
	Content  string                   `json:"content"`             // 消息内容
	Channel  *channel.Channel         `json:"channel,omitempty"`   // 频道对象
	Guild    *guild.Guild             `json:"guild,omitempty"`     // 群组对象
	Member   *guildmember.GuildMember `json:"member,omitempty"`    // 群组成员对象
	User     *user.User               `json:"user,omitempty"`      // 用户对象
	CreateAt int64                    `json:"create_at,omitempty"` // 消息发送的时间戳
	UpdateAt int64                    `json:"update_at,omitempty"` // 消息修改的时间戳
}
