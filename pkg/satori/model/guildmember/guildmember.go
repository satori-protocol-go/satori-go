package guildmember

import (
	"encoding/json"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

// GuildMember is a Satori member. JoinedAt is a millisecond timestamp.
type GuildMember struct {
	fields   types.FieldPresence
	User     *user.User             `json:"user,omitempty"`
	Nick     string                 `json:"nick,omitempty"`
	Avatar   string                 `json:"avatar,omitempty"`
	JoinedAt int64                  `json:"joined_at,omitempty"`
	Roles    []*guildrole.GuildRole `json:"roles,omitempty"`
}

func (m *GuildMember) UnmarshalJSON(data []byte) error {
	type plain GuildMember
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*m = GuildMember(value)
	m.fields = fields
	return nil
}

func (m GuildMember) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	m.fields.Put(out, "user", m.User, m.User != nil)
	m.fields.Put(out, "nick", m.Nick, m.Nick != "")
	m.fields.Put(out, "avatar", m.Avatar, m.Avatar != "")
	m.fields.Put(out, "joined_at", m.JoinedAt, m.JoinedAt != 0)
	m.fields.Put(out, "roles", m.Roles, m.Roles != nil)
	return json.Marshal(out)
}

// WithoutUser returns the wire copy used when the user is promoted to its parent.
func (m GuildMember) WithoutUser() *GuildMember {
	m.User = nil
	m.fields = m.fields.Without("user")
	return &m
}
