package element

import "github.com/satori-protocol-go/satori-go/pkg/satori/internal/attr"

// <button> 元素用于表示一个按钮。它的子元素会被渲染为按钮的文本。
//
// 按钮目前支持三种不同的类型：
//
// 点击 action 类型的按钮时会触发一个 interaction/button 事件，该事件的 button 资源会包含上述 id。
//
// 点击 link 类型的按钮时会打开一个链接，该链接的地址为上述 href。
//
// 点击 input 类型的按钮时会在用户的输入框中填充上述 text。
type Button struct {
	BaseElement
	NoAlias
	Id    string `attr:"id,omitempty"`    // 发 按钮的 ID
	Type  string `attr:"type,omitempty"`  // 发 按钮的类型
	Href  string `attr:"href,omitempty"`  // 发 按钮的链接
	Text  string `attr:"text,omitempty"`  // 发 待输入文本
	Theme string `attr:"theme,omitempty"` // 发	按钮的样式
}

func (b *Button) Tag() string {
	return "button"
}

func (b *Button) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(b, attrs)
	if err != nil {
		return err
	}
	return b.BaseElement.UnmarshalAttrs(attrs)
}
