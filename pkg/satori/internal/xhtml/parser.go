package xhtml

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	tagPat1 = regexp.MustCompile(`(<!--[\s\S]*?-->)|(<(/?)([^!\s>/]*)([^>]*?)\s*(/?)>)`)
	tagPat2 = regexp.MustCompile(`(<!--[\s\S]*?-->)|(<(/?)([^!\s>/]*)([^>]*?)\s*(/?)>)|(\{([@:/#][^\s\}]*)?[\s\S]*?\})`)

	attrPat1 = regexp.MustCompile(`([^\s=]+)(?:="([^"]*)"|='([^']*)')?`)
	attrPat2 = regexp.MustCompile(`([^\s=]+)(?:="([^"]*)"|='([^']*)'|=\{([^\}]+)\})?`)

	trimStartPat = regexp.MustCompile(`(?m)^\s*\n\s*`)
	trimEndPat   = regexp.MustCompile(`(?m)\s*\n\s*$`)

	combPat       = regexp.MustCompile(` *([ >+~]) *`)
	identifierPat = regexp.MustCompile(`^[\w.]+$`)
	eachSplitPat  = regexp.MustCompile(`\s+as\s+`)
	camelCasePat  = regexp.MustCompile(`[_-][a-z]`)
	paramCasePat  = regexp.MustCompile(`.[A-Z]+`)
	snakeCasePat  = regexp.MustCompile(`.[A-Z]`)
)

type Position int

const (
	PositionOpen Position = iota
	PositionClose
	PositionEmpty
	PositionContinue
)

type tokenKind string

const (
	tokenKindAngle tokenKind = "angle"
	tokenKindCurly tokenKind = "curly"
)

type Token struct {
	Kind     tokenKind
	Name     string
	Position Position
	Source   string
	Extra    string
	Children map[string][]any
}

type stackItem struct {
	Token *Token
	Slot  string
}

type Element struct {
	Type     string
	Attrs    map[string]any
	Children []*Element
	Source   *string
}

type selector struct {
	Type       string
	Combinator string
}

func Escape(text string, inLine bool) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	if inLine {
		text = strings.ReplaceAll(text, "\"", "&quot;")
	}
	return text
}

func unescape(text string) string {
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")

	re := regexp.MustCompile(`&#(\d+);`)
	text = re.ReplaceAllStringFunc(text, func(s string) string {
		matches := re.FindStringSubmatch(s)
		if matches[1] == "38" {
			return s
		}
		i, _ := strconv.Atoi(matches[1])
		return string(rune(i))
	})

	re = regexp.MustCompile("&#x([0-9a-f]+);")
	text = re.ReplaceAllStringFunc(text, func(s string) string {
		matches := re.FindStringSubmatch(s)
		if matches[1] == "26" {
			return s
		}
		i, _ := strconv.ParseInt(matches[1], 16, 32)
		return string(rune(i))
	})

	re = regexp.MustCompile("&(amp|#38|#x26);")
	text = re.ReplaceAllString(text, "&")

	return text
}

func uncapitalize(source string) string {
	if source == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(source)
	if r == utf8.RuneError && size == 0 {
		return ""
	}
	return string(unicode.ToLower(r)) + source[size:]
}

func camelCase(source string) string {
	return camelCasePat.ReplaceAllStringFunc(source, func(mat string) string {
		if len(mat) < 2 {
			return mat
		}
		return strings.ToUpper(mat[1:])
	})
}

func ParamCase(source string) string {
	source = strings.ReplaceAll(uncapitalize(source), "_", "-")
	return paramCasePat.ReplaceAllStringFunc(source, func(mat string) string {
		if len(mat) < 2 {
			return mat
		}
		return mat[:1] + "-" + strings.ToLower(mat[1:])
	})
}

func SnakeCase(source string) string {
	source = strings.ReplaceAll(uncapitalize(source), "-", "_")
	return snakeCasePat.ReplaceAllStringFunc(source, func(mat string) string {
		if len(mat) < 2 {
			return mat
		}
		return mat[:1] + "_" + strings.ToLower(mat[1:])
	})
}

func ensureList(value any) []any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		result := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			result = append(result, rv.Index(i).Interface())
		}
		return result
	}
	return []any{value}
}

func makeElement(content any) *Element {
	switch value := content.(type) {
	case nil:
		return nil
	case *Element:
		return value
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return NewElement("text", map[string]any{"text": fmt.Sprint(value)})
	case string:
		if value != "" {
			return NewElement("text", map[string]any{"text": value})
		}
	}
	return nil
}

func makeElements(content any) []*Element {
	if content == nil {
		return nil
	}

	rawElements := ensureList(content)
	if len(rawElements) == 0 {
		return nil
	}

	result := make([]*Element, 0, len(rawElements))
	for _, raw := range rawElements {
		if e := makeElement(raw); e != nil {
			result = append(result, e)
		}
	}
	return result
}

func NewElement(typ any, attrs map[string]any, children ...any) *Element {
	e := &Element{
		Type:     "",
		Attrs:    map[string]any{},
		Children: make([]*Element, 0),
	}

	for k, v := range attrs {
		if v == nil {
			continue
		}
		if k == "children" {
			e.Children = append(e.Children, makeElements(v)...)
			continue
		}
		e.Attrs[camelCase(k)] = v
	}

	for _, child := range children {
		e.Children = append(e.Children, makeElements(child)...)
	}

	if s, ok := typ.(string); ok {
		e.Type = s
	} else {
		e.Type = "component"
		e.Attrs["is"] = typ
	}

	if e.Tag() == "text" {
		if content, ok := e.Attrs["content"]; ok {
			delete(e.Attrs, "content")
			e.Attrs["text"] = content
		} else if len(e.Attrs) == 0 {
			e.Attrs["text"] = ""
		}
	}

	return e
}

func (e *Element) Tag() string {
	if e == nil {
		return ""
	}
	if e.Type != "component" {
		return e.Type
	}

	isValue, ok := e.Attrs["is"]
	if !ok || isValue == nil {
		return "component"
	}

	rt := reflect.TypeOf(isValue)
	if rt != nil {
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Name() != "" {
			return rt.Name()
		}
	}

	return "component"
}

func (e *Element) attributes() string {
	if e == nil || len(e.Attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(e.Attrs))
	for key := range e.Attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		value := e.Attrs[key]
		if value == nil {
			continue
		}
		paramKey := ParamCase(key)
		switch v := value.(type) {
		case bool:
			if v {
				b.WriteString(" ")
				b.WriteString(paramKey)
			} else {
				b.WriteString(" no-")
				b.WriteString(paramKey)
			}
		default:
			b.WriteString(" ")
			b.WriteString(paramKey)
			b.WriteString(`="`)
			b.WriteString(Escape(fmt.Sprint(v), true))
			b.WriteString(`"`)
		}
	}
	return b.String()
}

func (e *Element) dumps(strip bool) string {
	if e == nil {
		return ""
	}
	if e.Type == "text" {
		text := ""
		if value, ok := e.Attrs["text"]; ok && value != nil {
			text = fmt.Sprint(value)
		}
		if strip {
			return text
		}
		return Escape(text, false)
	}

	var inner strings.Builder
	for _, child := range e.Children {
		inner.WriteString(child.dumps(strip))
	}
	innerString := inner.String()
	if strip {
		return innerString
	}

	attrs := e.attributes()
	tag := e.Tag()
	if len(e.Children) == 0 {
		return "<" + tag + attrs + "/>"
	}
	return "<" + tag + attrs + ">" + innerString + "</" + tag + ">"
}

func (e *Element) String() string {
	return e.dumps(false)
}

func parseSelector(input string) [][]selector {
	parseQuery := func(query string) []selector {
		selectors := make([]selector, 0)
		combinator := " "
		for {
			loc := combPat.FindStringSubmatchIndex(query)
			if loc == nil {
				break
			}
			match := combPat.FindStringSubmatch(query)
			selectors = append(selectors, selector{Type: query[:loc[0]], Combinator: combinator})
			combinator = match[1]
			query = query[loc[1]:]
		}
		selectors = append(selectors, selector{Type: query, Combinator: combinator})
		return selectors
	}

	parts := strings.Split(input, ",")
	result := make([][]selector, 0, len(parts))
	for _, part := range parts {
		result = append(result, parseQuery(part))
	}
	return result
}

func selectElements(source []*Element, query [][]selector) []*Element {
	if len(source) == 0 || len(query) == 0 {
		return nil
	}

	adjacent := make([][]selector, 0)
	results := make([]*Element, 0)
	baseQuery := make([][]selector, len(query))
	copy(baseQuery, query)

	for index, elem := range source {
		inner := make([][]selector, 0)
		local := make([][]selector, 0, len(baseQuery)+len(adjacent))
		local = append(local, baseQuery...)
		local = append(local, adjacent...)
		adjacent = make([][]selector, 0)
		matched := false

		for _, group := range local {
			if len(group) == 0 {
				continue
			}
			typ := group[0].Type
			combinator := group[0].Combinator
			if typ == elem.Type || typ == "*" {
				if len(group) == 1 {
					matched = true
				} else {
					next := group[1:]
					switch group[1].Combinator {
					case " ", ">":
						inner = append(inner, next)
					case "+":
						adjacent = append(adjacent, next)
					default:
						baseQuery = append(baseQuery, next)
					}
				}
			}
			if combinator == " " {
				inner = append(inner, group)
			}
		}

		if matched {
			results = append(results, source[index])
		}
		results = append(results, selectElements(elem.Children, inner)...)
	}
	return results
}

func Select(source any, query any) []*Element {
	if source == nil || query == nil {
		return nil
	}

	var sourceElements []*Element
	switch typedSource := source.(type) {
	case string:
		sourceElements = Parse(typedSource, nil)
	case []*Element:
		sourceElements = typedSource
	case []Element:
		sourceElements = make([]*Element, 0, len(typedSource))
		for i := range typedSource {
			elem := typedSource[i]
			sourceElements = append(sourceElements, &elem)
		}
	default:
		return nil
	}

	var selectors [][]selector
	switch typedQuery := query.(type) {
	case string:
		selectors = parseSelector(typedQuery)
	case [][]selector:
		selectors = typedQuery
	default:
		return nil
	}

	if len(selectors) == 0 {
		return nil
	}
	return selectElements(sourceElements, selectors)
}

func evaluate(expr string, context map[string]any) any {
	value, ok := evalExpress(expr, context)
	if !ok || value == nil {
		return ""
	}
	return value
}

func interpolate(expr string, context map[string]any) any {
	expr = strings.TrimSpace(expr)
	if !identifierPat.MatchString(expr) {
		return evaluate(expr, context)
	}
	value := any(ensureContext(context))
	for part := range strings.SplitSeq(expr, ".") {
		next, ok := lookupValue(value, part)
		if !ok || next == nil {
			return ""
		}
		value = next
	}
	if value == nil {
		return ""
	}
	return value
}

func ensureContext(context map[string]any) map[string]any {
	if context == nil {
		return map[string]any{}
	}
	return context
}

func cloneContext(context map[string]any) map[string]any {
	clone := make(map[string]any, len(context))
	for key, value := range context {
		clone[key] = value
	}
	return clone
}

func lookupValue(value any, part string) (any, bool) {
	if value == nil {
		return nil, false
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Map:
		keyType := rv.Type().Key()
		key := reflect.ValueOf(part)
		if !key.Type().AssignableTo(keyType) {
			if key.Type().ConvertibleTo(keyType) {
				key = key.Convert(keyType)
			} else {
				return nil, false
			}
		}
		mapped := rv.MapIndex(key)
		if !mapped.IsValid() {
			return nil, false
		}
		return mapped.Interface(), true
	case reflect.Struct:
		field := rv.FieldByName(part)
		if field.IsValid() && field.CanInterface() {
			return field.Interface(), true
		}
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == part {
				fv := rv.Field(i)
				if fv.IsValid() && fv.CanInterface() {
					return fv.Interface(), true
				}
			}
		}
	}

	return nil, false
}

func evalExpress(expr string, context map[string]any) (any, bool) {
	node, err := goparser.ParseExpr(expr)
	if err != nil {
		return nil, false
	}
	value, err := evalAST(node, ensureContext(context))
	if err != nil {
		return nil, false
	}
	return value, true
}

func evalAST(node ast.Expr, context map[string]any) (any, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		switch n.Kind {
		case token.INT:
			v, err := strconv.ParseInt(n.Value, 0, 64)
			if err != nil {
				return nil, err
			}
			return v, nil
		case token.FLOAT:
			v, err := strconv.ParseFloat(n.Value, 64)
			if err != nil {
				return nil, err
			}
			return v, nil
		case token.STRING, token.CHAR:
			v, err := strconv.Unquote(n.Value)
			if err != nil {
				return nil, err
			}
			return v, nil
		}
	case *ast.Ident:
		switch n.Name {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "nil", "null":
			return nil, nil
		default:
			if value, ok := context[n.Name]; ok {
				return value, nil
			}
		}
		return nil, fmt.Errorf("unknown identifier: %s", n.Name)
	case *ast.ParenExpr:
		return evalAST(n.X, context)
	case *ast.UnaryExpr:
		value, err := evalAST(n.X, context)
		if err != nil {
			return nil, err
		}
		switch n.Op {
		case token.NOT:
			return !truthy(value), nil
		case token.SUB:
			if f, ok := asFloat(value); ok {
				return -f, nil
			}
		case token.ADD:
			if f, ok := asFloat(value); ok {
				return f, nil
			}
		}
		return nil, fmt.Errorf("unsupported unary op")
	case *ast.BinaryExpr:
		left, err := evalAST(n.X, context)
		if err != nil {
			return nil, err
		}
		right, err := evalAST(n.Y, context)
		if err != nil {
			return nil, err
		}
		value, ok := applyBinary(n.Op, left, right)
		if !ok {
			return nil, fmt.Errorf("unsupported binary op")
		}
		return value, nil
	case *ast.SelectorExpr:
		base, err := evalAST(n.X, context)
		if err != nil {
			return nil, err
		}
		value, ok := lookupValue(base, n.Sel.Name)
		if !ok {
			return nil, fmt.Errorf("unknown selector")
		}
		return value, nil
	case *ast.IndexExpr:
		base, err := evalAST(n.X, context)
		if err != nil {
			return nil, err
		}
		index, err := evalAST(n.Index, context)
		if err != nil {
			return nil, err
		}
		value, ok := indexValue(base, index)
		if !ok {
			return nil, fmt.Errorf("invalid index")
		}
		return value, nil
	}

	return nil, fmt.Errorf("unsupported expression")
}

func applyBinary(op token.Token, left, right any) (any, bool) {
	switch op {
	case token.LAND:
		return truthy(left) && truthy(right), true
	case token.LOR:
		return truthy(left) || truthy(right), true
	case token.EQL:
		return reflect.DeepEqual(left, right), true
	case token.NEQ:
		return !reflect.DeepEqual(left, right), true
	case token.GTR, token.GEQ, token.LSS, token.LEQ:
		cmp, ok := compare(left, right)
		if !ok {
			return nil, false
		}
		switch op {
		case token.GTR:
			return cmp > 0, true
		case token.GEQ:
			return cmp >= 0, true
		case token.LSS:
			return cmp < 0, true
		case token.LEQ:
			return cmp <= 0, true
		}
	case token.ADD:
		if ls, ok := left.(string); ok {
			return ls + fmt.Sprint(right), true
		}
		if rs, ok := right.(string); ok {
			return fmt.Sprint(left) + rs, true
		}
		lf, lok := asFloat(left)
		rf, rok := asFloat(right)
		if lok && rok {
			return lf + rf, true
		}
	case token.SUB, token.MUL, token.QUO:
		lf, lok := asFloat(left)
		rf, rok := asFloat(right)
		if !lok || !rok {
			return nil, false
		}
		switch op {
		case token.SUB:
			return lf - rf, true
		case token.MUL:
			return lf * rf, true
		case token.QUO:
			if rf == 0 {
				return nil, false
			}
			return lf / rf, true
		}
	case token.REM:
		li, lok := asInt(left)
		ri, rok := asInt(right)
		if !lok || !rok || ri == 0 {
			return nil, false
		}
		return li % ri, true
	}

	return nil, false
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func asInt(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return i, true
		}
	}
	return 0, false
}

func compare(left, right any) (int, bool) {
	if lf, lok := asFloat(left); lok {
		rf, rok := asFloat(right)
		if !rok {
			return 0, false
		}
		switch {
		case lf < rf:
			return -1, true
		case lf > rf:
			return 1, true
		default:
			return 0, true
		}
	}

	ls, lok := left.(string)
	rs, rok := right.(string)
	if lok && rok {
		switch {
		case ls < rs:
			return -1, true
		case ls > rs:
			return 1, true
		default:
			return 0, true
		}
	}

	lb, lok := left.(bool)
	rb, rok := right.(bool)
	if lok && rok {
		li := 0
		if lb {
			li = 1
		}
		ri := 0
		if rb {
			ri = 1
		}
		switch {
		case li < ri:
			return -1, true
		case li > ri:
			return 1, true
		default:
			return 0, true
		}
	}

	return 0, false
}

func indexValue(base, index any) (any, bool) {
	rv := reflect.ValueOf(base)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		i, ok := asInt(index)
		if !ok || i < 0 || int(i) >= rv.Len() {
			return nil, false
		}
		return rv.Index(int(i)).Interface(), true
	case reflect.String:
		i, ok := asInt(index)
		if !ok || i < 0 {
			return nil, false
		}
		runes := []rune(rv.String())
		if int(i) >= len(runes) {
			return nil, false
		}
		return string(runes[i]), true
	case reflect.Map:
		key := reflect.ValueOf(index)
		if !key.IsValid() {
			return nil, false
		}
		keyType := rv.Type().Key()
		if !key.Type().AssignableTo(keyType) {
			if key.Type().ConvertibleTo(keyType) {
				key = key.Convert(keyType)
			} else {
				return nil, false
			}
		}
		mapped := rv.MapIndex(key)
		if !mapped.IsValid() {
			return nil, false
		}
		return mapped.Interface(), true
	}

	return nil, false
}

func truthy(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint8:
		return v != 0
	case uint16:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	case float32:
		return v != 0
	case float64:
		return v != 0
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
		return rv.Len() > 0
	}
	return true
}

func isIterable(value any) bool {
	if value == nil {
		return false
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
		return true
	default:
		return false
	}
}

func iterate(value any) []any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	result := make([]any, 0)
	switch rv.Kind() {
	case reflect.Array, reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			result = append(result, rv.Index(i).Interface())
		}
	case reflect.Map:
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
		})
		for _, key := range keys {
			result = append(result, key.Interface())
		}
	case reflect.String:
		for _, r := range []rune(rv.String()) {
			result = append(result, string(r))
		}
	}
	return result
}

func foldToken(tokens []any) []any {
	root := &Token{
		Kind:     tokenKindAngle,
		Name:     "template",
		Position: PositionOpen,
		Children: map[string][]any{"default": make([]any, 0)},
	}
	stack := []stackItem{{Token: root, Slot: "default"}}

	pushToken := func(values ...any) {
		top := &stack[len(stack)-1]
		top.Token.Children[top.Slot] = append(top.Token.Children[top.Slot], values...)
	}

	for _, raw := range tokens {
		if text, ok := raw.(string); ok {
			pushToken(text)
			continue
		}

		token, ok := raw.(*Token)
		if !ok || token == nil {
			continue
		}

		switch token.Position {
		case PositionClose:
			if len(stack) > 1 && stack[len(stack)-1].Token.Name == token.Name {
				stack = stack[:len(stack)-1]
			}
		case PositionContinue:
			top := &stack[len(stack)-1]
			if top.Token.Children == nil {
				top.Token.Children = make(map[string][]any)
			}
			top.Token.Children[token.Name] = make([]any, 0)
			top.Slot = token.Name
		case PositionOpen:
			pushToken(token)
			token.Children = map[string][]any{"default": make([]any, 0)}
			stack = append(stack, stackItem{Token: token, Slot: "default"})
		default:
			pushToken(token)
		}
	}

	return root.Children["default"]
}

func parseTokens(tokens []any, context map[string]any) []*Element {
	result := make([]*Element, 0)
	for _, raw := range tokens {
		if text, ok := raw.(string); ok {
			result = append(result, NewElement("text", map[string]any{"text": text}))
			continue
		}

		token, ok := raw.(*Token)
		if !ok || token == nil {
			continue
		}

		if token.Kind == tokenKindAngle {
			attrs := make(map[string]any)
			attrPat := attrPat1
			if context != nil {
				attrPat = attrPat2
			}
			extra := token.Extra
			for {
				loc := attrPat.FindStringSubmatchIndex(extra)
				if loc == nil {
					break
				}

				current := extra
				extra = extra[loc[1]:]
				key := current[loc[2]:loc[3]]

				value1Start, value1End := -1, -1
				value2Start, value2End := -1, -1
				curlyStart, curlyEnd := -1, -1
				if len(loc) >= 6 {
					value1Start, value1End = loc[4], loc[5]
				}
				if len(loc) >= 8 {
					value2Start, value2End = loc[6], loc[7]
				}
				if len(loc) >= 10 {
					curlyStart, curlyEnd = loc[8], loc[9]
				}

				if context != nil && curlyStart != -1 {
					curly := current[curlyStart:curlyEnd]
					if curly != "" {
						attrs[key] = interpolate(curly, context)
						continue
					}
				}
				if value2Start != -1 {
					attrs[key] = unescape(current[value2Start:value2End])
					continue
				}
				if value1Start != -1 {
					attrs[key] = unescape(current[value1Start:value1End])
					continue
				}
				if strings.HasPrefix(key, "no-") {
					attrs[key[3:]] = false
				} else {
					attrs[key] = true
				}
			}

			children := make([]*Element, 0)
			if token.Children != nil {
				children = parseTokens(token.Children["default"], context)
			}
			result = append(result, NewElement(token.Name, attrs, children))
			continue
		}

		ctx := ensureContext(context)
		switch token.Name {
		case "":
			result = append(result, makeElements(interpolate(token.Extra, ctx))...)
		case "if":
			if truthy(evaluate(token.Extra, ctx)) {
				result = append(result, parseTokens(token.Children["default"], ctx)...)
			} else {
				result = append(result, parseTokens(token.Children["else"], ctx)...)
			}
		case "each":
			parts := eachSplitPat.Split(token.Extra, 2)
			if len(parts) != 2 {
				continue
			}
			expr := strings.TrimSpace(parts[0])
			ident := strings.TrimSpace(parts[1])
			items := interpolate(expr, ctx)
			if !isIterable(items) {
				continue
			}
			for _, item := range iterate(items) {
				nextCtx := cloneContext(ctx)
				nextCtx[ident] = item
				result = append(result, parseTokens(token.Children["default"], nextCtx)...)
			}
		}
	}
	return result
}

func Parse(source string, context map[string]any) []*Element {
	tokens := make([]any, 0)

	pushText := func(text string) {
		if text != "" {
			tokens = append(tokens, text)
		}
	}

	parseContent := func(content string, stripStart, stripEnd bool) {
		content = unescape(content)
		if stripStart {
			content = trimStartPat.ReplaceAllString(content, "")
		}
		if stripEnd {
			content = trimEndPat.ReplaceAllString(content, "")
		}
		pushText(content)
	}

	tagPat := tagPat1
	if context != nil {
		tagPat = tagPat2
	}
	stripStart := true

	for {
		tagLoc := tagPat.FindStringSubmatchIndex(source)
		if tagLoc == nil {
			break
		}

		matches := tagPat.FindStringSubmatch(source)
		hasCurly := len(matches) > 7 && matches[7] != ""
		stripEnd := !hasCurly
		parseContent(source[:tagLoc[0]], stripStart, stripEnd)
		stripStart = stripEnd
		source = source[tagLoc[1]:]

		if matches[1] != "" {
			continue
		}

		if hasCurly {
			curly := matches[7]
			derivative := ""
			if len(matches) > 8 {
				derivative = matches[8]
			}

			name := ""
			position := PositionEmpty
			if derivative != "" {
				name = derivative[1:]
				switch derivative[0] {
				case '@':
					position = PositionEmpty
				case '#':
					position = PositionOpen
				case '/':
					position = PositionClose
				case ':':
					position = PositionContinue
				}
			}

			extraStart := 1 + len(derivative)
			extraEnd := len(curly) - 1
			extra := ""
			if extraEnd >= extraStart {
				extra = curly[extraStart:extraEnd]
			}

			tokens = append(tokens, &Token{
				Kind:     tokenKindCurly,
				Name:     name,
				Position: position,
				Source:   curly,
				Extra:    extra,
			})
			continue
		}

		closeTag, typeName, extra, selfClose := matches[3], matches[4], matches[5], matches[6]
		if typeName == "" {
			typeName = "template"
		}
		position := PositionOpen
		if closeTag != "" {
			position = PositionClose
		} else if selfClose != "" {
			position = PositionEmpty
		}

		tokens = append(tokens, &Token{
			Kind:     tokenKindAngle,
			Name:     typeName,
			Position: position,
			Source:   matches[0],
			Extra:    extra,
		})
	}

	parseContent(source, stripStart, true)
	return parseTokens(foldToken(tokens), context)
}
