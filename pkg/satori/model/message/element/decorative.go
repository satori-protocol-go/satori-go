package element

// 修饰元素
//
// 修饰元素用于修饰其中的内容。
// 如果对应的平台不支持对应的元素，可以忽略这个元素本身，正常渲染其中的子元素。
type Decorative struct {
	BaseElement
}

type decorativeElement interface {
	isDecorative()
}

func (d *Decorative) isDecorative() {}

func (d *Decorative) AddChild(content ...any) {
	if d == nil {
		return
	}
	for _, c := range content {
		switch v := c.(type) {
		case Element:
			bindOwner(v)
			if _, ok := v.(*Text); ok {
				d.children = append(d.children, v)
				continue
			}
			if _, ok := v.(decorativeElement); ok {
				d.children = append(d.children, v)
			}
		case string:
			text, err := New[*Text](map[string]any{"text": v})
			if err != nil {
				continue
			}
			d.children = append(d.children, text)
		default:
			continue
		}
	}
}

// <b> 或 <strong> 元素用于将其中的内容以粗体显示。
type Strong struct {
	Decorative
}

func (s *Strong) Tag() string {
	return "b"
}

func (s *Strong) Alias() []string {
	return []string{"strong"}
}

// <i> 或 <em> 元素用于将其中的内容以斜体显示。
type Em struct {
	Decorative
}

func (e *Em) Tag() string {
	return "i"
}

func (e *Em) Alias() []string {
	return []string{"em"}
}

// <u> 或 <ins> 元素用于为其中的内容附加下划线。
type Ins struct {
	Decorative
}

func (i *Ins) Tag() string {
	return "u"
}

func (i *Ins) Alias() []string {
	return []string{"ins"}
}

// <s> 或 <del> 元素用于为其中的内容附加删除线。
type Del struct {
	Decorative
}

func (d *Del) Tag() string {
	return "s"
}

func (d *Del) Alias() []string {
	return []string{"del"}
}

// <spl> 元素用于将其中的内容标记为剧透 (默认会被隐藏，点击后才显示)。
type Spl struct {
	Decorative
	NoAlias
}

func (s *Spl) Tag() string {
	return "spl"
}

// <code> 元素用于将其中的内容以等宽字体显示 (通常还会有特定的背景色)。
type Code struct {
	Decorative
	NoAlias
}

func (c *Code) Tag() string {
	return "code"
}

// <sup> 元素用于将其中的内容以上标显示。
type Sup struct {
	Decorative
	NoAlias
}

func (s *Sup) Tag() string {
	return "sup"
}

// <sub> 元素用于将其中的内容以下标显示。
type Sub struct {
	Decorative
	NoAlias
}

func (s *Sub) Tag() string {
	return "sub"
}
