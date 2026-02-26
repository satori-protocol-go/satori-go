package element

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
)

type ErrTransformFailed struct {
	Tag string
	Err error
}

func (e *ErrTransformFailed) Error() string {
	return fmt.Sprintf("failed to transform element with tag %q: %v", e.Tag, e.Err)
}

type ownerBinder interface {
	setOwner(owner Element)
}

func bindOwner(element Element) {
	if element == nil {
		return
	}
	if binder, ok := element.(ownerBinder); ok {
		binder.setOwner(element)
	}
}

func attrString(key string, value any) string {
	if value == nil {
		return ""
	}
	key = xhtml.ParamCase(key)
	if value == true {
		return " " + key
	}
	if value == false {
		return " no-" + key
	}
	return " " + key + `="` + xhtml.Escape(fmt.Sprint(value), true) + `"`
}

type Element interface {
	Tag() string
	Alias() []string
	UnmarshalAttrs(attrs map[string]any) error
	Get(key string) (any, bool)
	MarshalXHTML(strip bool) string
	Children() []Element
	AddChild(content ...Element)
	AddChildString(content ...string)
}

type NoAlias struct{}

func (n *NoAlias) Alias() []string {
	return nil
}

type BaseElement struct {
	attrs    map[string]any
	children []Element
	owner    Element
}

func (e *BaseElement) Tag() string {
	return "raw"
}

func (e *BaseElement) setOwner(owner Element) {
	if e == nil {
		return
	}
	e.owner = owner
}

func (e *BaseElement) ownerTag() string {
	if e == nil {
		return ""
	}
	if e.owner != nil {
		return e.owner.Tag()
	}
	return e.Tag()
}

func (e *BaseElement) UnmarshalAttrs(attrs map[string]any) error {
	if attrs == nil {
		attrs = map[string]any{}
	}
	e.attrs = attrs
	return nil
}

func (e *BaseElement) Get(key string) (any, bool) {
	if e == nil || e.attrs == nil {
		return "", false
	}
	value, ok := e.attrs[key]
	if !ok || value == nil {
		return "", false
	}
	return fmt.Sprint(value), true
}

func (e *BaseElement) Children() []Element {
	if e == nil {
		return nil
	}
	return e.children
}

func (e *BaseElement) attributes() string {
	if e == nil || len(e.attrs) == 0 {
		return ""
	}
	var builder strings.Builder
	for k, v := range e.attrs {
		builder.WriteString(attrString(k, v))
	}
	return builder.String()
}

func (e *BaseElement) MarshalXHTML(strip bool) string {
	if e == nil {
		return ""
	}
	tag := e.ownerTag()
	_, hasText := e.attrs["text"]
	if tag == "text" || hasText {
		text, _ := e.Get("text")
		if strip {
			return fmt.Sprint(text)
		}
		return xhtml.Escape(fmt.Sprint(text), false)
	}
	var innerBuilder strings.Builder
	for _, c := range e.children {
		innerBuilder.WriteString(c.MarshalXHTML(strip))
	}
	inner := innerBuilder.String()
	if strip {
		return inner
	}
	attrs := e.attributes()
	if len(e.children) == 0 {
		return "<" + tag + attrs + "/>"
	}
	return "<" + tag + attrs + ">" + inner + "</" + tag + ">"
}

func (e *BaseElement) AddChild(content ...Element) {
	if e == nil {
		return
	}
	for _, c := range content {
		bindOwner(c)
		e.children = append(e.children, c)
	}
}

func (e *BaseElement) AddChildString(content ...string) {
	if e == nil {
		return
	}
	for _, c := range content {
		text, err := New[*Text](map[string]any{"text": c})
		if err != nil {
			continue
		}
		e.children = append(e.children, text)
	}
}

func instantiate[T Element]() (T, error) {
	var zero T
	rt := reflect.TypeOf(zero)
	if rt == nil {
		return zero, fmt.Errorf("cannot instantiate nil element type")
	}

	var raw any
	if rt.Kind() == reflect.Pointer {
		raw = reflect.New(rt.Elem()).Interface()
	} else {
		raw = reflect.New(rt).Elem().Interface()
	}

	element, ok := raw.(T)
	if !ok {
		return zero, fmt.Errorf("type %q does not implement Element", rt.String())
	}
	return element, nil
}

func New[T Element](attrs map[string]any) (T, error) {
	var zero T
	element, err := instantiate[T]()
	if err != nil {
		return zero, err
	}
	bindOwner(element)
	if err := element.UnmarshalAttrs(attrs); err != nil {
		return zero, err
	}
	return element, nil
}

type elementFactory func(attrs map[string]any) (Element, error)

func Factory[T Element]() elementFactory {
	return func(attrs map[string]any) (Element, error) {
		return New[T](attrs)
	}
}

type factoryMap struct {
	m map[string]elementFactory
}

func (f *factoryMap) set(tag string, factory elementFactory) {
	if f.m == nil {
		f.m = make(map[string]elementFactory)
	}
	f.m[tag] = factory
}

func (f *factoryMap) get(tag string) (elementFactory, bool) {
	if f.m == nil {
		return nil, false
	}
	factory, ok := f.m[tag]
	return factory, ok
}

var (
	elementFactoryMap = factoryMap{}
	styleFactoryMap   = factoryMap{}
)

func registerElement(tag string, factory elementFactory) {
	elementFactoryMap.set(tag, factory)
}

func registerStyle(tag string, factory elementFactory) {
	styleFactoryMap.set(tag, factory)
}

func RegisterElement(tag string, factory elementFactory) error {
	if _, exists := elementFactoryMap.get(tag); exists {
		return fmt.Errorf("element with tag %q is already registered", tag)
	}
	registerElement(tag, factory)
	return nil
}

func Transform(elements []*xhtml.Element) ([]Element, error) {
	var message []Element = make([]Element, 0, len(elements))
	for _, elem := range elements {
		tag := elem.Tag()
		if factory, ok := elementFactoryMap.get(tag); ok {
			element, err := factory(elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				element.AddChild(children...)
			}
		} else if slices.Contains([]string{"a", "link"}, tag) {
			link, err := New[*A](elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				link.AddChild(children...)
			}
			message = append(message, link)
		} else if tag == "button" {
			button, err := New[*Button](elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				button.AddChild(children...)
			}
			message = append(message, button)
		} else if factory, ok := styleFactoryMap.get(tag); ok {
			style, err := factory(elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				style.AddChild(children...)
			}
			message = append(message, style)
		} else if slices.Contains([]string{"br", "newline"}, tag) {
			br, err := New[*Br](elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			message = append(message, br)
		} else if tag == "message" {
			msg, err := New[*Message](elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				msg.AddChild(children...)
			}
			message = append(message, msg)
		} else if tag == "quote" {
			quote, err := New[*Quote](elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				quote.AddChild(children...)
			}
			message = append(message, quote)
		} else {
			custom, err := NewExtension(tag, elem.Attrs)
			if err != nil {
				return nil, &ErrTransformFailed{Tag: tag, Err: err}
			}
			if len(elem.Children) > 0 {
				children, err := Transform(elem.Children)
				if err != nil {
					return nil, &ErrTransformFailed{Tag: tag, Err: err}
				}
				custom.AddChild(children...)
			}
			message = append(message, custom)
		}
	}
	return message, nil
}

type selector func(Element) bool

func Select(elements []Element, selector selector) []Element {
	var results []Element
	for _, elem := range elements {
		if selector(elem) {
			results = append(results, elem)
		}
		if len(elem.Children()) > 0 {
			childResults := Select(elem.Children(), selector)
			results = append(results, childResults...)
		}
	}
	return results
}

func TagSelector(tag string) selector {
	return func(elem Element) bool {
		if elem == nil {
			return false
		}
		return elem.Tag() == tag
	}
}

func TypeSelector(dst Element) selector {
	targetType := reflect.TypeOf(dst)
	if targetType == nil {
		return func(Element) bool {
			return false
		}
	}

	return func(elem Element) bool {
		if elem == nil {
			return false
		}
		return reflect.TypeOf(elem) == targetType
	}
}

func init() {
	registerElement("text", Factory[*Text]())
	registerElement("at", Factory[*At]())
	registerElement("sharp", Factory[*Sharp]())
	registerElement("a", Factory[*A]())
	registerElement("link", Factory[*A]())
	registerElement("img", Factory[*Img]())
	registerElement("image", Factory[*Img]())
	registerElement("audio", Factory[*Audio]())
	registerElement("video", Factory[*Video]())
	registerElement("file", Factory[*File]())
	registerElement("author", Factory[*Author]())
	registerElement("button", Factory[*Button]())
	registerElement("message", Factory[*Message]())
	registerElement("quote", Factory[*Quote]())

	registerStyle("b", Factory[*Strong]())
	registerStyle("strong", Factory[*Strong]())
	registerStyle("i", Factory[*Em]())
	registerStyle("em", Factory[*Em]())
	registerStyle("u", Factory[*Ins]())
	registerStyle("ins", Factory[*Ins]())
	registerStyle("s", Factory[*Del]())
	registerStyle("del", Factory[*Del]())
	registerStyle("spl", Factory[*Spl]())
	registerStyle("code", Factory[*Code]())
	registerStyle("sup", Factory[*Sup]())
	registerStyle("sub", Factory[*Sub]())
	registerStyle("p", Factory[*P]())
	registerStyle("br", Factory[*Br]())
}
