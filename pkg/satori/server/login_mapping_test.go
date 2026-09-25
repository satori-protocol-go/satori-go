package server_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type loginMappingProvider struct{ logins []*login.Login }

func (p *loginMappingProvider) GetLogins(context.Context) ([]*login.Login, error) {
	return p.logins, nil
}
func (p *loginMappingProvider) ProxyUrls() []string { return nil }
func (p *loginMappingProvider) Ensure(platform, selfID string) bool {
	for _, info := range p.logins {
		if info.Platform == platform && info.User != nil && info.User.Id == selfID {
			return true
		}
	}
	return false
}
func (p *loginMappingProvider) HandleInternal(satoriserver.Request[map[string]any], string) (*satoriserver.Response, error) {
	return nil, satoriserver.NotFound("not found")
}
func (p *loginMappingProvider) HandleProxied(string, string) (*satoriserver.Response, error) {
	return nil, satoriserver.NotFound("not found")
}

func TestServerLoginMapping(t *testing.T) {
	srv := makeServer(t, satoriserver.Config{Token: "fixture"})
	alpha := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "alpha"}, Status: login.LoginStatusOnline}
	beta := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "beta"}, Status: login.LoginStatusOnline}
	if err := srv.Apply(&loginMappingProvider{logins: []*login.Login{alpha}}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Apply(&loginMappingProvider{logins: []*login.Login{beta}}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(serverHandler(t, srv))
	defer httpServer.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteJSON(operation.Operation{Op: operation.OpcodeIdentify, Body: operation.IdentifyBody{Token: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	var ready struct {
		Op   operation.Opcode    `json:"op"`
		Body operation.ReadyBody `json:"body"`
	}
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	if ready.Op != operation.OpcodeReady || len(ready.Body.Logins) != 2 {
		t.Fatalf("READY=%+v", ready)
	}
	numbers := map[string]int64{}
	for _, info := range ready.Body.Logins {
		numbers[info.User.Id] = info.Sn
	}
	if numbers["alpha"] == numbers["beta"] {
		t.Fatalf("same downstream login number: %+v", numbers)
	}
	for _, info := range []*login.Login{alpha, beta} {
		evt := &event.Event{Type: event.EventTypeMessageCreated, Timestamp: 1, Login: info}
		if err := srv.Post(evt); err != nil {
			t.Fatal(err)
		}
		var frame struct {
			Op   operation.Opcode `json:"op"`
			Body event.Event      `json:"body"`
		}
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Op != operation.OpcodeEvent || frame.Body.Login == nil || frame.Body.Login.User.Id != info.User.Id || frame.Body.Login.Sn != numbers[info.User.Id] {
			t.Fatalf("event mapping=%+v", frame)
		}
	}
}
