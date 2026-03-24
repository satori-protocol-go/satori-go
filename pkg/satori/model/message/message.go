package message

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

// Message is the Satori message payload.
type Message struct {
	Id       string                   `json:"id"`
	Content  string                   `json:"content"`
	Channel  *channel.Channel         `json:"channel,omitempty"`
	Guild    *guild.Guild             `json:"guild,omitempty"`
	Member   *guildmember.GuildMember `json:"member,omitempty"`
	User     *user.User               `json:"user,omitempty"`
	CreateAt int64                    `json:"create_at,omitempty"`
	UpdateAt int64                    `json:"update_at,omitempty"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type alias Message

	// Decode regular fields first.
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = Message(decoded)

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
