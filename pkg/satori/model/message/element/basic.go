package element

import (
	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/attr"
	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
)

// 一段纯文本。
type Text struct {
	BaseElement
	NoAlias
	Text string `attr:"text"` // 文本内容
}

func (t *Text) Tag() string {
	return "text"
}

func (t *Text) Children() []Element {
	return nil
}

func (t *Text) AddChild(content ...Element) {
	// Text 元素不接受子元素，直接忽略
}

func (t *Text) AddChildString(content ...string) {
	// Text 元素不接受子元素，直接忽略
}

func (t *Text) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(t, attrs)
	if err != nil {
		return err
	}
	return t.BaseElement.UnmarshalAttrs(attrs)
}

func (t *Text) MarshalXHTML(strip bool) string {
	if strip {
		return t.Text
	}
	return xhtml.Escape(t.Text, false)
}

// <at> 元素用于提及某个或某些用户。
type At struct {
	BaseElement
	NoAlias
	Id   string `attr:"id,omitempty"`   // 收发 目标用户的 ID
	Name string `attr:"name,omitempty"` // 收发	目标用户的名称
	Role string `attr:"role,omitempty"` // 收发	目标角色
	Type string `attr:"type,omitempty"` // 收发	特殊操作，例如 all 表示 @全体成员，here 表示 @在线成员
}

func (a *At) Tag() string {
	return "at"
}

func (a *At) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(a, attrs)
	if err != nil {
		return err
	}
	return a.BaseElement.UnmarshalAttrs(attrs)
}

// <sharp> 元素用于提及某个频道。
type Sharp struct {
	BaseElement
	NoAlias
	Id   string `attr:"id"`             // 收发 目标频道的 ID
	Name string `attr:"name,omitempty"` // 收发 目标频道的名称
}

func (s *Sharp) Tag() string {
	return "sharp"
}

func (s *Sharp) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(s, attrs)
	if err != nil {
		return err
	}
	return s.BaseElement.UnmarshalAttrs(attrs)
}

// <a> 元素用于显示一个链接。
type A struct {
	BaseElement
	Href string `attr:"href"` // 收发	链接的 URL
}

func (a *A) Tag() string {
	return "a"
}

func (a *A) Alias() []string {
	return []string{"link"}
}

func (a *A) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(a, attrs)
	if err != nil {
		return err
	}
	return a.BaseElement.UnmarshalAttrs(attrs)
}
