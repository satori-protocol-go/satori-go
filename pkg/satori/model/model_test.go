package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message/element"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/paginated"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
	"strings"
	"testing"
)

func object(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestResourceWire(t *testing.T) {
	for _, tc := range []struct{ raw, content string }{
		{`{"id":"m"}`, ""},
		{`{"id":"m","content":null}`, "null"},
		{`{"id":"m","content":""}`, `""`},
		{`{"id":"m","content":"hello"}`, `"hello"`},
	} {
		var value message.Message
		if err := json.Unmarshal([]byte(tc.raw), &value); err != nil {
			t.Fatal(err)
		}
		if got := string(object(t, value)["content"]); got != tc.content {
			t.Fatalf("content wire %s: %s", tc.raw, got)
		}
	}
	var msg message.Message
	if err := json.Unmarshal([]byte(`{"id":"m","content":"text","created_at":0,"updated_at":null,"referrer":{"large":9007199254740993}}`), &msg); err != nil {
		t.Fatal(err)
	}
	wire := object(t, msg)
	if string(wire["created_at"]) != "0" || string(wire["updated_at"]) != "null" || !bytes.Contains(wire["referrer"], []byte("9007199254740993")) {
		t.Fatalf("message wire=%s", wire)
	}
	var member guildmember.GuildMember
	if err := json.Unmarshal([]byte(`{"user":{"id":"u","name":null,"is_bot":false},"joined_at":0,"roles":[{"id":"admin","name":"Admin"}]}`), &member); err != nil {
		t.Fatal(err)
	}
	wire = object(t, member)
	if len(member.Roles) != 1 || member.Roles[0].Id != "admin" || string(wire["joined_at"]) != "0" {
		t.Fatalf("member wire=%s", wire)
	}
	usr := object(t, member.User)
	if string(usr["name"]) != "null" || string(usr["is_bot"]) != "false" {
		t.Fatalf("user wire=%s", usr)
	}
	var evt event.Event
	if err := json.Unmarshal([]byte(`{"sn":0,"type":"message-created","timestamp":1,"login":{"sn":3,"platform":"p","user":{"id":"bot"}},"emoji":{"id":"e"},"friend":{"nick":"friend"},"button":null,"message":{"id":"m","content":"text","channel":{"id":"c","type":0},"member":{"user":{"id":"u"},"roles":[{"id":"r"}]}},"_data":{"raw_id":9007199254740993}}`), &evt); err != nil {
		t.Fatal(err)
	}
	wire = object(t, evt)
	if evt.Emoji.Id != "e" || evt.Friend.Nick != "friend" || string(wire["button"]) != "null" || !bytes.Contains(wire["_data"], []byte("9007199254740993")) {
		t.Fatalf("event wire=%s", wire)
	}
	var body map[string]json.RawMessage
	json.Unmarshal(wire["message"], &body)
	if len(body) != 2 || !bytes.Contains(wire["channel"], []byte(`"c"`)) || !bytes.Contains(wire["user"], []byte(`"u"`)) {
		t.Fatalf("promoted wire=%s", wire)
	}
	// Decode into a fresh map so its keys describe the current object only.
	var loginBody map[string]json.RawMessage
	json.Unmarshal(wire["login"], &loginBody)
	if len(loginBody) != 3 {
		t.Fatalf("event login=%s", wire["login"])
	}
	evt.Type = event.EventTypeLoginUpdated
	loginBody = nil
	json.Unmarshal(object(t, evt)["login"], &loginBody)
	if string(loginBody["status"]) != "1" || string(loginBody["adapter"]) != `"satori"` {
		t.Fatalf("login update=%s", loginBody)
	}

	t.Run("optional-login-fields", func(t *testing.T) {
		zero := int64(0)
		if string(object(t, operation.IdentifyBody{Sn: &zero})["sn"]) != "0" {
			t.Fatal("zero recovery position")
		}
		for _, raw := range []string{"false", "null"} {
			var value types.Option[bool]
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(value)
			if err != nil || string(encoded) != raw || value.IsNone() {
				t.Fatalf("optional %s -> %s: %v", raw, encoded, err)
			}
		}
		base := &login.Login{Sn: 7, Platform: "p", User: &user.User{Id: "u", Name: "before"}, Status: login.LoginStatusOnline, Adapter: "fixture", Features: []string{"message.create"}}
		var patch login.Login
		if err := json.Unmarshal([]byte(`{"sn":7,"status":0,"user":null,"features":[]}`), &patch); err != nil {
			t.Fatal(err)
		}
		result := base.Merge(&patch)
		if result.Sn != 7 || result.Status != login.LoginStatusOffline || result.Platform != "p" || result.User != nil || len(result.Features) != 0 {
			t.Fatalf("merged=%+v", result)
		}
		if string(object(t, result)["user"]) != "null" || string(object(t, result)["features"]) != "[]" {
			t.Fatalf("merged wire=%s", object(t, result))
		}

	})
}

func TestMessageElementWire(t *testing.T) {
	for _, source := range []string{
		`<button id="b" type="input" text="hello">Send</button>`,
		`<qq:custom text="literal">child &amp; &#65;</qq:custom>`,
		`<message><quote id="q"/><image src="https://example.invalid/p.png"/>text</message>`,
	} {
		parsed := xhtml.Parse(source, nil)
		values, err := element.Transform(parsed)
		if err != nil || len(values) != 1 {
			t.Fatalf("parse %s: %v", source, err)
		}
		out := values[0].MarshalXHTML(false)
		reparsed := xhtml.Parse(out, nil)
		if len(reparsed) != 1 || reparsed[0].Tag() != parsed[0].Tag() && !(parsed[0].Tag() == "image" && reparsed[0].Tag() == "img") {
			t.Fatalf("element %s -> %s", source, out)
		}
		if parsed[0].Tag() == "button" && (reparsed[0].Attrs["text"] != "hello" || len(reparsed[0].Children) != 1 || reparsed[0].Children[0].String() != "Send") {
			t.Fatalf("button=%s", out)
		}
		if parsed[0].Tag() == "qq:custom" && (reparsed[0].Attrs["text"] != "literal" || reparsed[0].Children[0].String() != "child &amp; A") {
			t.Fatalf("extension=%s", out)
		}
	}

	t.Run("resource-attributes", func(t *testing.T) {
		image := &element.Img{}
		if err := image.UnmarshalAttrs(map[string]any{"src": "image.png", "width": "32", "height": 24, "cache": true, "timeout": "500"}); err != nil {
			t.Fatal(err)
		}
		if image.Src != "image.png" || image.Width != 32 || image.Height != 24 || !image.Cache || image.Timeout != 500 {
			t.Fatalf("image attributes=%+v", image)
		}
		audio := &element.Audio{}
		if err := audio.UnmarshalAttrs(map[string]any{"src": "voice.silk", "duration": "1.5"}); err != nil {
			t.Fatal(err)
		}
		if audio.Src != "voice.silk" || audio.Duration != 1.5 {
			t.Fatalf("audio attributes=%+v", audio)
		}
	})
	t.Run("parse-and-select", func(t *testing.T) {
		source := `<root><a id="1"/><b><a id="2"/></b><c/><a id="3"/></root>`
		for _, tc := range []struct{ query, ids string }{
			{"a", "1,2,3"}, {"b a", "2"}, {"root > a", "1,3"}, {"c + a", "3"}, {"b ~ a", "3"},
		} {
			var ids []string
			for _, item := range xhtml.Select(source, tc.query) {
				ids = append(ids, item.Attrs["id"].(string))
			}
			if got := strings.Join(ids, ","); got != tc.ids {
				t.Fatalf("selector %q=%s, want %s", tc.query, got, tc.ids)
			}
		}
		parsed := xhtml.Parse(`<x a="1" b='2' c no-d>&lt;t&gt;</x><!--comment-->`, nil)
		if len(parsed) != 1 || parsed[0].String() != `<x a="1" b="2" c no-d>&lt;t&gt;</x>` {
			t.Fatalf("parsed elements=%v", parsed)
		}
		text := xhtml.Parse(`&lt;test&gt;&quot;u&amp;i&quot;&#65;&#x41;&#38;&#x26;&amp;`, nil)
		if len(text) != 1 || text[0].Attrs["text"] != `<test>"u&i"AA&&&` {
			t.Fatalf("decoded text=%v", text)
		}
		rendered := xhtml.NewElement("div", map[string]any{"title": "a&b", "enabled": true, "visible": false}, "hello").String()
		if rendered != `<div enabled title="a&amp;b" no-visible>hello</div>` {
			t.Fatalf("rendered=%s", rendered)
		}
	})
	t.Run("template-output", func(t *testing.T) {
		template := `<root title={user.name}>{#if user.active}<p>{user.name}</p>{:else}<p>guest</p>{/if}<ul>{#each items as item}<li>{item}</li>{/each}</ul></root>`
		for _, tc := range []struct {
			active bool
			name   string
		}{{true, "neo"}, {false, "guest"}} {
			values := xhtml.Parse(template, map[string]any{"user": map[string]any{"name": "neo", "active": tc.active}, "items": []int{1, 2}})
			want := `<root title="neo"><p>` + tc.name + `</p><ul><li>1</li><li>2</li></ul></root>`
			if len(values) != 1 || values[0].String() != want {
				t.Fatalf("template active=%t: %v", tc.active, values)
			}
		}
	})
}

func TestOpaquePagination(t *testing.T) {
	var cursor types.Option[string]
	if err := json.Unmarshal([]byte(`" token+/== "`), &cursor); err != nil {
		t.Fatal(err)
	}
	value, ok := cursor.Get()
	if !ok || value != " token+/== " {
		t.Fatalf("cursor=%q", value)
	}
	pages := paginated.NewPaginatedSeq(context.Background(), value, func(ctx context.Context, next string) (*paginated.Paginated[int], error) {
		switch next {
		case " token+/== ":
			return &paginated.Paginated[int]{Data: []int{1}, Next: " next+/== "}, nil
		case " next+/== ":
			return &paginated.Paginated[int]{Data: []int{2}}, nil
		default:
			t.Fatalf("altered cursor=%q", next)
			return nil, nil
		}
	})
	var items []int
	for item, err := range pages.Iter() {
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if len(items) != 2 || items[0] != 1 || items[1] != 2 || pages.NextToken() != "" {
		t.Fatalf("page values=%v next=%q", items, pages.NextToken())
	}
}
