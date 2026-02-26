package testsuite

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	_ "unsafe"

	xhtml "github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"golang.org/x/net/html"
)

//go:linkname unescape github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.unescape
func unescape(text string) string

//go:linkname uncapitalize github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.uncapitalize
func uncapitalize(source string) string

//go:linkname camelCase github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.camelCase
func camelCase(source string) string

//go:linkname ensureList github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.ensureList
func ensureList(value any) []any

//go:linkname makeElement github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.makeElement
func makeElement(content any) *xhtml.Element

//go:linkname makeElements github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.makeElements
func makeElements(content any) []*xhtml.Element

func elementTag(e *xhtml.Element) string {
	if e == nil {
		return ""
	}
	return e.Tag()
}

//go:linkname evaluate github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.evaluate
func evaluate(expr string, context map[string]any) any

//go:linkname interpolate github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.interpolate
func interpolate(expr string, context map[string]any) any

//go:linkname ensureContext github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.ensureContext
func ensureContext(context map[string]any) map[string]any

//go:linkname lookupValue github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.lookupValue
func lookupValue(value any, part string) (any, bool)

//go:linkname evalExpress github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.evalExpress
func evalExpress(expr string, context map[string]any) (any, bool)

//go:linkname compare github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.compare
func compare(left, right any) (int, bool)

//go:linkname indexValue github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.indexValue
func indexValue(base, index any) (any, bool)

//go:linkname truthy github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.truthy
func truthy(value any) bool

//go:linkname isIterable github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.isIterable
func isIterable(value any) bool

//go:linkname iterate github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.iterate
func iterate(value any) []any

//go:linkname foldToken github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.foldToken
func foldToken(tokens []any) []any

//go:linkname parseTokens github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml.parseTokens
func parseTokens(tokens []any, context map[string]any) []*xhtml.Element

func parseElements(source string, context map[string]any) []*xhtml.Element {
	return xhtml.Parse(source, context)
}

func parseWithContext(source string, context map[string]any) *html.Node {
	return buildDocument(parseElements(source, context))
}

type userProfile struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func extractAttrIDs(elements []*xhtml.Element) []string {
	result := make([]string, 0, len(elements))
	for _, e := range elements {
		if e == nil || e.Attrs == nil {
			continue
		}
		if id, ok := e.Attrs["id"]; ok {
			result = append(result, id.(string))
		}
	}
	return result
}

func joinElementStrings(elements []*xhtml.Element) string {
	var b strings.Builder
	for _, e := range elements {
		b.WriteString(e.String())
	}
	return b.String()
}

func nodeChildren(n *html.Node) []*html.Node {
	var children []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}
	return children
}

func elementToNode(e *xhtml.Element) *html.Node {
	if e == nil {
		return nil
	}
	if e.Type == "text" {
		text := ""
		if value, ok := e.Attrs["text"]; ok && value != nil {
			text = fmt.Sprint(value)
		}
		return &html.Node{Type: html.TextNode, Data: text}
	}

	node := &html.Node{Type: html.ElementNode, Data: e.Tag()}
	keys := make([]string, 0, len(e.Attrs))
	for key := range e.Attrs {
		if e.Type == "component" && key == "is" {
			continue
		}
		if e.Type == "text" && (key == "text" || key == "content") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := e.Attrs[key]
		if value == nil {
			continue
		}
		attrKey := xhtml.ParamCase(key)
		switch v := value.(type) {
		case bool:
			if v {
				node.Attr = append(node.Attr, html.Attribute{Key: attrKey, Val: ""})
			} else {
				node.Attr = append(node.Attr, html.Attribute{Key: attrKey, Val: "false"})
			}
		default:
			node.Attr = append(node.Attr, html.Attribute{Key: attrKey, Val: fmt.Sprint(v)})
		}
	}

	for _, child := range e.Children {
		childNode := elementToNode(child)
		if childNode != nil {
			node.AppendChild(childNode)
		}
	}
	return node
}

func buildDocument(elements []*xhtml.Element) *html.Node {
	root := &html.Node{Type: html.DocumentNode, Data: "body"}
	for _, element := range elements {
		node := elementToNode(element)
		if node != nil {
			root.AppendChild(node)
		}
	}
	return root
}

func TestEscapeAndUnescape(t *testing.T) {
	input := `<a&"b">`
	if got := xhtml.Escape(input, false); got != `&lt;a&amp;"b"&gt;` {
		t.Fatalf("Escape inline=false mismatch: %s", got)
	}
	if got := xhtml.Escape(input, true); got != `&lt;a&amp;&quot;b&quot;&gt;` {
		t.Fatalf("Escape inline=true mismatch: %s", got)
	}

	encoded := `&lt;test&gt;&quot;u&amp;i&quot;&#65;&#x41;&#38;&#x26;&amp;`
	if got := unescape(encoded); got != `<test>"u&i"AA&&&` {
		t.Fatalf("unescape mismatch: %s", got)
	}
}

func TestCaseHelpers(t *testing.T) {
	if got := xhtml.ParamCase("FooBar"); got != "foo-bar" {
		t.Fatalf("ParamCase mismatch: %s", got)
	}
	if got := xhtml.ParamCase("foo_bar"); got != "foo-bar" {
		t.Fatalf("ParamCase underscore mismatch: %s", got)
	}
	if got := xhtml.SnakeCase("FooBar"); got != "foo_bar" {
		t.Fatalf("SnakeCase mismatch: %s", got)
	}
	if got := xhtml.SnakeCase("foo-bar"); got != "foo_bar" {
		t.Fatalf("SnakeCase hyphen mismatch: %s", got)
	}
}

func TestEnsureListAndMakeElements(t *testing.T) {
	if got := ensureList(nil); got != nil {
		t.Fatalf("ensureList(nil) should be nil")
	}

	raw := ensureList([]int{1, 2})
	if len(raw) != 2 || raw[0].(int) != 1 || raw[1].(int) != 2 {
		t.Fatalf("ensureList slice mismatch: %#v", raw)
	}

	single := ensureList("x")
	if len(single) != 1 || single[0].(string) != "x" {
		t.Fatalf("ensureList scalar mismatch: %#v", single)
	}

	elements := makeElements([]any{"a", "", 1, nil})
	if len(elements) != 2 {
		t.Fatalf("makeElements len mismatch: %d", len(elements))
	}
	if elements[0].String() != "a" || elements[1].String() != "1" {
		t.Fatalf("makeElements result mismatch: %s, %s", elements[0].String(), elements[1].String())
	}
}

func TestNewElementAndStringify(t *testing.T) {
	e := xhtml.NewElement("div", map[string]any{
		"title":   "a&b",
		"enabled": true,
		"visible": false,
	}, "hello")

	if got := e.String(); got != `<div enabled title="a&amp;b" no-visible>hello</div>` {
		t.Fatalf("element stringify mismatch: %s", got)
	}

	text := xhtml.NewElement("text", map[string]any{"content": "a<b>"})
	if got := text.String(); got != "a&lt;b&gt;" {
		t.Fatalf("text content remap mismatch: %s", got)
	}

	type widget struct{}
	component := xhtml.NewElement(widget{}, nil)
	if got := elementTag(component); got != "widget" {
		t.Fatalf("component tag mismatch: %s", got)
	}
}

func TestSelect(t *testing.T) {
	source := readFixture(t, "select_source.xhtml")
	tests := []struct {
		query string
		want  []string
	}{
		{query: "a", want: []string{"1", "2", "3"}},
		{query: "b a", want: []string{"2"}},
		{query: "root > a", want: []string{"1", "3"}},
		{query: "c + a", want: []string{"3"}},
		{query: "b ~ a", want: []string{"3"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.query, func(t *testing.T) {
			got := extractAttrIDs(xhtml.Select(source, tc.query))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Select(%q) mismatch: got=%v want=%v", tc.query, got, tc.want)
			}
		})
	}

	if got := xhtml.Select(nil, "a"); got != nil {
		t.Fatalf("Select(nil, ...) should be nil")
	}
	if got := xhtml.Select(source, 123); got != nil {
		t.Fatalf("Select(invalid query) should be nil")
	}
}

func TestEvaluateInterpolateAndHelpers(t *testing.T) {
	ctx := map[string]any{
		"a":   map[string]any{"b": int64(2)},
		"arr": []int{5, 8},
		"obj": userProfile{Name: "neo", Age: 20},
	}

	if got := interpolate("a.b", ctx); got != int64(2) {
		t.Fatalf("interpolate path mismatch: %#v", got)
	}
	if got := interpolate("obj.name", ctx); got != "neo" {
		t.Fatalf("interpolate struct json-tag mismatch: %#v", got)
	}
	if got := interpolate("a.missing", ctx); got != "" {
		t.Fatalf("interpolate missing path should be empty string, got=%#v", got)
	}
	if got := interpolate("arr[0] + a.b", ctx); got != float64(7) {
		t.Fatalf("interpolate expression mismatch: %#v", got)
	}
	if got := evaluate("arr[1] + a.b", ctx); got != float64(10) {
		t.Fatalf("evaluate arithmetic mismatch: %#v", got)
	}
	if got := evaluate("obj.age > 18", ctx); got != true {
		t.Fatalf("evaluate compare mismatch: %#v", got)
	}
	if got := evaluate("bad +", ctx); got != "" {
		t.Fatalf("evaluate invalid expression should be empty string, got=%#v", got)
	}

	if got, ok := lookupValue(map[string]int{"x": 7}, "x"); !ok || got.(int) != 7 {
		t.Fatalf("lookupValue map mismatch: %#v %v", got, ok)
	}
	if got, ok := indexValue([]int{1, 2, 3}, int64(1)); !ok || got.(int) != 2 {
		t.Fatalf("indexValue slice mismatch: %#v %v", got, ok)
	}
	if got, ok := indexValue("你好", int64(1)); !ok || got.(string) != "好" {
		t.Fatalf("indexValue string mismatch: %#v %v", got, ok)
	}
	if cmp, ok := compare(1, 2); !ok || cmp != -1 {
		t.Fatalf("compare mismatch: %d %v", cmp, ok)
	}
	if !truthy([]int{1}) || truthy([]int{}) {
		t.Fatalf("truthy slice mismatch")
	}
}

func TestFoldTokenAndParseTokens(t *testing.T) {
	ifElseTokens := []any{
		&xhtml.Token{Kind: "curly", Name: "if", Position: xhtml.PositionOpen, Extra: "ok"},
		"YES",
		&xhtml.Token{Kind: "curly", Name: "else", Position: xhtml.PositionContinue},
		"NO",
		&xhtml.Token{Kind: "curly", Name: "if", Position: xhtml.PositionClose},
	}

	folded := foldToken(ifElseTokens)
	if got := joinElementStrings(parseTokens(folded, map[string]any{"ok": false})); got != "NO" {
		t.Fatalf("parseTokens if/else mismatch: %s", got)
	}

	eachTokens := []any{
		&xhtml.Token{Kind: "curly", Name: "each", Position: xhtml.PositionOpen, Extra: "items as item"},
		&xhtml.Token{Kind: "curly", Name: "", Position: xhtml.PositionEmpty, Extra: "item"},
		&xhtml.Token{Kind: "curly", Name: "each", Position: xhtml.PositionClose},
	}
	eachFolded := foldToken(eachTokens)
	if got := joinElementStrings(parseTokens(eachFolded, map[string]any{"items": []int{1, 2}})); got != "12" {
		t.Fatalf("parseTokens each mismatch: %s", got)
	}
}

func TestParseElements(t *testing.T) {
	t.Run("without context", func(t *testing.T) {
		source := `<x a="1" b='2' c no-d>&lt;t&gt;</x><!--ignored-->`
		elements := parseElements(source, nil)
		if len(elements) != 1 {
			t.Fatalf("parseElements len mismatch: %d", len(elements))
		}
		if got := elements[0].String(); got != `<x a="1" b="2" c no-d>&lt;t&gt;</x>` {
			t.Fatalf("parseElements result mismatch: %s", got)
		}
	})

	t.Run("with context fixtures", func(t *testing.T) {
		template := readFixture(t, "template_if_each.xhtml")
		expectedActive := readFixture(t, "template_if_each_expected_active.txt")
		expectedInactive := readFixture(t, "template_if_each_expected_inactive.txt")

		activeCtx := map[string]any{
			"user":  map[string]any{"name": "neo", "active": true},
			"items": []int{1, 2},
		}
		inactiveCtx := map[string]any{
			"user":  map[string]any{"name": "neo", "active": false},
			"items": []int{1, 2},
		}

		active := parseElements(template, activeCtx)
		if len(active) != 1 || active[0].String() != expectedActive {
			t.Fatalf("active template mismatch: %s", joinElementStrings(active))
		}

		inactive := parseElements(template, inactiveCtx)
		if len(inactive) != 1 || inactive[0].String() != expectedInactive {
			t.Fatalf("inactive template mismatch: %s", joinElementStrings(inactive))
		}
	})
}

func TestParseAndParseWithContext(t *testing.T) {
	elements := xhtml.Parse(`<a id="1"/>text<b/>`, nil)
	if len(elements) != 3 {
		t.Fatalf("Parse elements len mismatch: %d", len(elements))
	}
	if elements[0] == nil || elements[0].Type != "a" {
		t.Fatalf("first element mismatch: %#v", elements[0])
	}
	if elements[1] == nil || elements[1].Type != "text" {
		t.Fatalf("second element mismatch: %#v", elements[1])
	}
	if text, ok := elements[1].Attrs["text"]; !ok || text != "text" {
		t.Fatalf("second element text mismatch: %#v", elements[1].Attrs)
	}
	if elements[2] == nil || elements[2].Type != "b" {
		t.Fatalf("third element mismatch: %#v", elements[2])
	}

	template := readFixture(t, "template_if_each.xhtml")
	docWithCtx := parseWithContext(template, map[string]any{
		"user":  map[string]any{"name": "neo", "active": true},
		"items": []int{1, 2},
	})
	root := docWithCtx.FirstChild
	if root == nil || root.Type != html.ElementNode || root.Data != "root" {
		t.Fatalf("parseWithContext root mismatch")
	}
	if len(root.Attr) != 1 || root.Attr[0].Key != "title" || root.Attr[0].Val != "neo" {
		t.Fatalf("parseWithContext attrs mismatch: %+v", root.Attr)
	}
}

func TestHelperBranches(t *testing.T) {
	t.Run("uncapitalize and camelCase", func(t *testing.T) {
		if got := uncapitalize(""); got != "" {
			t.Fatalf("uncapitalize empty mismatch: %q", got)
		}
		if got := uncapitalize("Hello"); got != "hello" {
			t.Fatalf("uncapitalize mismatch: %q", got)
		}
		if got := camelCase("a_b-c"); got != "aBC" {
			t.Fatalf("camelCase mismatch: %q", got)
		}
	})

	t.Run("tag function branches", func(t *testing.T) {
		var nilElem *xhtml.Element
		if got := elementTag(nilElem); got != "" {
			t.Fatalf("nil tag mismatch: %q", got)
		}

		componentWithoutIs := &xhtml.Element{Type: "component", Attrs: map[string]any{}}
		if got := elementTag(componentWithoutIs); got != "component" {
			t.Fatalf("component fallback tag mismatch: %q", got)
		}

		demoFn := func(map[string]any, []*xhtml.Element, any) any { return nil }
		componentWithFn := &xhtml.Element{Type: "component", Attrs: map[string]any{"is": demoFn}}
		if got := elementTag(componentWithFn); got != "component" {
			t.Fatalf("function tag should fallback to component, got: %q", got)
		}
	})

	t.Run("Select extra branches", func(t *testing.T) {
		if got := xhtml.Select(123, "a"); got != nil {
			t.Fatalf("Select invalid source should be nil")
		}
	})

	t.Run("ensureContext and lookupValue branches", func(t *testing.T) {
		if got := ensureContext(nil); len(got) != 0 {
			t.Fatalf("ensureContext(nil) mismatch: %+v", got)
		}

		type named struct {
			Value string
		}
		if got, ok := lookupValue(named{Value: "x"}, "Value"); !ok || got.(string) != "x" {
			t.Fatalf("lookupValue struct field mismatch: %#v %v", got, ok)
		}
		if got, ok := lookupValue(map[string]int{"x": 1}, "y"); ok || got != nil {
			t.Fatalf("lookupValue miss mismatch: %#v %v", got, ok)
		}
	})

	t.Run("evaluate branches", func(t *testing.T) {
		ctx := map[string]any{
			"x":   int64(4),
			"y":   int64(2),
			"arr": []int{3, 5},
		}
		cases := []struct {
			expr string
			want any
		}{
			{expr: "3.5", want: float64(3.5)},
			{expr: `"s"`, want: "s"},
			{expr: "true", want: true},
			{expr: "!false", want: true},
			{expr: "-x", want: float64(-4)},
			{expr: "+y", want: float64(2)},
			{expr: "x == y", want: false},
			{expr: "x != y", want: true},
			{expr: "x > y", want: true},
			{expr: "x >= y", want: true},
			{expr: "x < y", want: false},
			{expr: "x <= y", want: false},
			{expr: "x + y", want: float64(6)},
			{expr: "x - y", want: float64(2)},
			{expr: "x * y", want: float64(8)},
			{expr: "x / y", want: float64(2)},
			{expr: "x % y", want: int64(0)},
			{expr: "arr[1]", want: 5},
			{expr: "(x + y) * y", want: float64(12)},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.expr, func(t *testing.T) {
				got := evaluate(tc.expr, ctx)
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("evaluate(%q) mismatch: got=%#v want=%#v", tc.expr, got, tc.want)
				}
			})
		}

		if got := evaluate("x / 0", ctx); got != "" {
			t.Fatalf("evaluate divide-by-zero should be empty string, got=%#v", got)
		}
		if got := evaluate("unknown(1)", ctx); got != "" {
			t.Fatalf("evaluate unsupported expression should be empty string, got=%#v", got)
		}
		if _, ok := evalExpress("bad +", ctx); ok {
			t.Fatalf("evalExpress invalid expression should fail")
		}
	})

	t.Run("asFloat asInt compare truthy iterable iterate", func(t *testing.T) {
		if cmp, ok := compare("a", "b"); !ok || cmp != -1 {
			t.Fatalf("compare string mismatch: %d %v", cmp, ok)
		}
		if cmp, ok := compare(true, false); !ok || cmp != 1 {
			t.Fatalf("compare bool mismatch: %d %v", cmp, ok)
		}

		if !truthy(1) || truthy(0) || !truthy("x") || truthy("") {
			t.Fatalf("truthy basic mismatch")
		}
		if !isIterable("abc") || !isIterable(map[string]int{"k": 1}) || isIterable(1) {
			t.Fatalf("isIterable mismatch")
		}

		iterMap := iterate(map[string]int{"b": 2, "a": 1})
		if len(iterMap) != 2 || iterMap[0].(string) != "a" || iterMap[1].(string) != "b" {
			t.Fatalf("iterate map sort mismatch: %#v", iterMap)
		}
		iterStr := iterate("ab")
		if len(iterStr) != 2 || iterStr[0].(string) != "a" || iterStr[1].(string) != "b" {
			t.Fatalf("iterate string mismatch: %#v", iterStr)
		}
	})

	t.Run("indexValue map and failure branches", func(t *testing.T) {
		if got, ok := indexValue(map[string]int{"x": 9}, "x"); !ok || got.(int) != 9 {
			t.Fatalf("indexValue map mismatch: %#v %v", got, ok)
		}
		if got, ok := indexValue([]int{1}, int64(-1)); ok || got != nil {
			t.Fatalf("indexValue negative should fail: %#v %v", got, ok)
		}
		if got, ok := indexValue(10, 0); ok || got != nil {
			t.Fatalf("indexValue non-indexable should fail: %#v %v", got, ok)
		}
	})
}
