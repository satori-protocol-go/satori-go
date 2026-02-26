package element

import "github.com/satori-protocol-go/satori-go/pkg/satori/internal/attr"

// <quote> 元素用于表示对消息引用。
// 它的子元素会被渲染为引用的内容。
// 理论上所有 <message> 元素的特性也可以用于 <quote> 元素，包括子元素 (构造引用消息) 和 forward 属性 (引用合并转发)。
// 然而目前似乎并没有平台提供了这样的支持。
type Quote struct {
	BaseElement
	NoAlias
	Id      string `attr:"id,omitempty"`      // 发 消息的 ID
	Forward bool   `attr:"forward,omitempty"` // 发	是否为转发消息
}

func (q *Quote) Tag() string {
	return "quote"
}

func (q *Quote) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(q, attrs)
	if err != nil {
		return err
	}
	return q.BaseElement.UnmarshalAttrs(attrs)
}

// <author> 元素用于表示消息的作者。它的子元素会被渲染为作者的名字。
type Author struct {
	BaseElement
	NoAlias
	Id     string `attr:"id,omitempty"`     // 发 用户 ID
	Name   string `attr:"name,omitempty"`   // 发 昵称
	Avatar string `attr:"avatar,omitempty"` // 发 头像 URL
}

func (a *Author) Tag() string {
	return "author"
}

func (a *Author) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(a, attrs)
	if err != nil {
		return err
	}
	return a.BaseElement.UnmarshalAttrs(attrs)
}
