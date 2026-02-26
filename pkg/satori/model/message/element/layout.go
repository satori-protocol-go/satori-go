package element

import "github.com/satori-protocol-go/satori-go/pkg/satori/internal/attr"

// <br> 元素表示一个独立的换行。
type Br struct {
	BaseElement
	NoAlias
}

func (b *Br) Tag() string {
	return "br"
}

func (b *Br) Children() []Element {
	return nil
}

func (b *Br) AddChild(content ...any) {
	// Br 元素不接受子元素，直接忽略
}

// <p> 元素表示一个段落。在渲染时，它与相邻的元素之间会确保有一个换行。
type P struct {
	BaseElement
	NoAlias
}

func (p *P) Tag() string {
	return "p"
}

// <message> 元素的基本用法是表示一条消息。子元素对应于消息的内容。如果其没有子元素，则消息不会被发送。
type Message struct {
	BaseElement
	NoAlias
	Id      string `attr:"id,omitempty"`      // 发 消息的 ID
	Forward bool   `attr:"forward,omitempty"` // 发	是否为转发消息
}

func (m *Message) Tag() string {
	return "message"
}

func (m *Message) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(m, attrs)
	if err != nil {
		return err
	}
	return m.BaseElement.UnmarshalAttrs(attrs)
}
