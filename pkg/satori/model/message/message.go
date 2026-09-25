package message

import (
	"encoding/json"
	"fmt"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

// Message is the Satori message payload.
type Message struct {
	fields types.FieldPresence
	// Referrer is an interoperability extension for continuing passive replies.
	Referrer map[string]any           `json:"referrer,omitempty"`
	Id       string                   `json:"id"`
	Content  string                   `json:"content,omitempty"`
	Channel  *channel.Channel         `json:"channel,omitempty"`
	Guild    *guild.Guild             `json:"guild,omitempty"`
	Member   *guildmember.GuildMember `json:"member,omitempty"`
	User     *user.User               `json:"user,omitempty"`
	CreateAt int64                    `json:"created_at,omitempty"`
	UpdateAt int64                    `json:"updated_at,omitempty"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type alias Message

	// Decode regular fields first.
	var decoded alias
	fields, err := types.DecodeFields(data, &decoded)
	if err != nil {
		return err
	}
	*m = Message(decoded)
	m.fields = fields

	// Only perform fallback when payload does not provide "content".
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if _, hasContent := raw["content"]; hasContent {
		return nil
	}
	rawElements, ok := raw["elements"]
	if !ok || len(strings.TrimSpace(string(rawElements))) == 0 {
		return nil
	}

	var elements []json.RawMessage
	if err := json.Unmarshal(rawElements, &elements); err != nil {
		return nil
	}

	m.Content = renderElementsToContent(elements)
	return nil
}

func renderElementsToContent(elements []json.RawMessage) string {
	if len(elements) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, raw := range elements {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		node := toXHTMLElement(value)
		if node == nil {
			continue
		}
		builder.WriteString(node.String())
	}
	return builder.String()
}

func toXHTMLElement(value any) *xhtml.Element {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if typed == "" {
			return nil
		}
		return xhtml.NewElement("text", map[string]any{"text": typed})
	case bool, float64:
		return xhtml.NewElement("text", map[string]any{"text": fmt.Sprint(typed)})
	case map[string]any:
		return mapToXHTMLElement(typed)
	default:
		return xhtml.NewElement("text", map[string]any{"text": fmt.Sprint(typed)})
	}
}

func mapToXHTMLElement(fields map[string]any) *xhtml.Element {
	if len(fields) == 0 {
		return nil
	}

	tag := strings.TrimSpace(fmt.Sprint(fields["type"]))
	attrs := map[string]any{}
	if rawAttrs, ok := fields["attrs"].(map[string]any); ok {
		for key, value := range rawAttrs {
			attrs[key] = value
		}
	}
	for key, value := range fields {
		switch key {
		case "type", "attrs", "children":
			continue
		default:
			attrs[key] = value
		}
	}

	children := make([]*xhtml.Element, 0)
	if rawChildren, ok := fields["children"]; ok {
		if childList, ok := rawChildren.([]any); ok {
			for _, child := range childList {
				node := toXHTMLElement(child)
				if node == nil {
					continue
				}
				children = append(children, node)
			}
		}
	}

	// Fallback for malformed elements carrying only text.
	if tag == "" {
		if text, ok := attrs["text"]; ok {
			return xhtml.NewElement("text", map[string]any{"text": fmt.Sprint(text)})
		}
		return nil
	}
	return xhtml.NewElement(tag, attrs, children)
}

func (m Message) MarshalJSON() ([]byte, error) {
	// Promote the member user in API responses without mutating the source model.
	if m.Member != nil {
		if m.User == nil && !m.fields.Has("user") {
			m.User = m.Member.User
		}
		m.Member = m.Member.WithoutUser()
	}
	out := map[string]any{"id": m.Id}
	m.fields.Put(out, "content", m.Content, m.Content != "")
	m.fields.Put(out, "channel", m.Channel, m.Channel != nil)
	m.fields.Put(out, "guild", m.Guild, m.Guild != nil)
	m.fields.Put(out, "member", m.Member, m.Member != nil)
	m.fields.Put(out, "user", m.User, m.User != nil)
	m.fields.Put(out, "created_at", m.CreateAt, m.CreateAt != 0)
	m.fields.Put(out, "updated_at", m.UpdateAt, m.UpdateAt != 0)
	m.fields.Put(out, "referrer", m.Referrer, m.Referrer != nil)
	return json.Marshal(out)
}

// WithoutResources returns the message wire copy after resources are promoted to an event.
func (m Message) WithoutResources() *Message {
	m.Channel = nil
	m.Guild = nil
	m.Member = nil
	m.User = nil
	m.fields = m.fields.Without("channel", "guild", "member", "user")
	return &m
}
