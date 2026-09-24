package model_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
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
	if evt.Message.Member.User.Id != "u" || evt.Message.Channel.Id != "c" {
		t.Fatal("source message resources changed")
	}
	json.Unmarshal(wire["login"], &body)
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
}

func TestPresenceAndLoginPatch(t *testing.T) {
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
	if base.User.Name != "before" || len(base.Features) != 1 {
		t.Fatal("source login changed")
	}
}
