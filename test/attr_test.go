package testsuite

import (
	"testing"

	attrbind "github.com/satori-protocol-go/satori-go/pkg/satori/internal/attr"
)

type bindNested struct {
	Name string `attr:"name"`
}

type bindTarget struct {
	ID      int               `attr:"id"`
	Title   string            `json:"title"`
	Enabled bool              `attr:"enabled"`
	Rate    float64           `attr:"rate"`
	Count   *int              `attr:"count"`
	Tags    []string          `attr:"tags"`
	Meta    map[string]string `attr:"meta"`
	Nested  bindNested        `attr:"nested"`
	Default string
}

type requiredTarget struct {
	Name string `attr:"name"`
}

type optionalTarget struct {
	Name string `attr:"name,omitempty"`
}

type fallbackTarget struct {
	Value string `attr:"alias,omitempty"`
}

func TestBindAttrs(t *testing.T) {
	dst := &bindTarget{}
	err := attrbind.BindAttrs(dst, map[string]any{
		"id":      "12",
		"title":   123,
		"enabled": "true",
		"rate":    "1.5",
		"count":   7,
		"tags":    []any{"a", "b"},
		"meta": map[any]any{
			"x": "1",
			"y": 2,
		},
		"nested": map[string]any{
			"name": "neo",
		},
		"default": "ok",
	})
	if err != nil {
		t.Fatalf("BindAttrs failed: %v", err)
	}

	if dst.ID != 12 {
		t.Fatalf("unexpected ID: %d", dst.ID)
	}
	if dst.Title != "123" {
		t.Fatalf("unexpected Title: %s", dst.Title)
	}
	if !dst.Enabled {
		t.Fatalf("unexpected Enabled: %v", dst.Enabled)
	}
	if dst.Rate != 1.5 {
		t.Fatalf("unexpected Rate: %f", dst.Rate)
	}
	if dst.Count == nil || *dst.Count != 7 {
		t.Fatalf("unexpected Count: %#v", dst.Count)
	}
	if len(dst.Tags) != 2 || dst.Tags[0] != "a" || dst.Tags[1] != "b" {
		t.Fatalf("unexpected Tags: %#v", dst.Tags)
	}
	if dst.Meta["x"] != "1" || dst.Meta["y"] != "2" {
		t.Fatalf("unexpected Meta: %#v", dst.Meta)
	}
	if dst.Nested.Name != "neo" {
		t.Fatalf("unexpected Nested.Name: %s", dst.Nested.Name)
	}
	if dst.Default != "ok" {
		t.Fatalf("unexpected Default: %s", dst.Default)
	}
}

func TestBindAttrsErrors(t *testing.T) {
	var nilPtr *bindTarget
	if err := attrbind.BindAttrs(nilPtr, map[string]any{"id": 1}); err == nil {
		t.Fatalf("expected nil pointer error")
	}
	if err := attrbind.BindAttrs(bindTarget{}, map[string]any{"id": 1}); err == nil {
		t.Fatalf("expected non-pointer error")
	}
	if err := attrbind.BindAttrs(&bindTarget{}, map[string]any{"id": "bad-int"}); err == nil {
		t.Fatalf("expected conversion error")
	}
	if err := attrbind.BindAttrs(&requiredTarget{}, map[string]any{}); err == nil {
		t.Fatalf("expected missing required error")
	}
}

func TestBindAttrsOptionalAndFallback(t *testing.T) {
	optional := &optionalTarget{}
	if err := attrbind.BindAttrs(optional, map[string]any{}); err != nil {
		t.Fatalf("optional field should not error: %v", err)
	}

	fallback := &fallbackTarget{}
	if err := attrbind.BindAttrs(fallback, map[string]any{"value": "ok"}); err != nil {
		t.Fatalf("fallback field key should bind: %v", err)
	}
	if fallback.Value != "ok" {
		t.Fatalf("fallback bind mismatch: %q", fallback.Value)
	}
}
