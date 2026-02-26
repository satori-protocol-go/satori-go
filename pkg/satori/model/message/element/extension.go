package element

type Extension struct {
	BaseElement
	NoAlias
	tag string
}

func (e *Extension) Tag() string {
	return e.tag
}

func NewExtension(tag string, attrs map[string]any) (*Extension, error) {
	e := &Extension{
		tag: tag,
	}
	bindOwner(e)
	err := e.UnmarshalAttrs(attrs)
	if err != nil {
		return nil, err
	}
	return e, nil
}
