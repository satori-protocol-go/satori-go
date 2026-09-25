package client_test

import (
	"context"
	"encoding/json"
	"errors"
	clientnetwork "github.com/satori-protocol-go/satori-go/pkg/satori/client/network"
	"sync"
	"testing"
	"time"

	satoriclient "github.com/satori-protocol-go/satori-go/pkg/satori/client"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

type staticAPIConfig struct {
	base    string
	token   string
	timeout time.Duration
}

func (s staticAPIConfig) APIBase() string             { return s.base }
func (s staticAPIConfig) TokenValue() string          { return s.token }
func (s staticAPIConfig) TimeoutValue() time.Duration { return s.timeout }

func TestClientLogins(t *testing.T) {
	app, err := satoriclient.NewApp()
	if err != nil {
		t.Fatal(err)
	}
	config := staticAPIConfig{base: "http://127.0.0.1/v1", token: "fixture"}
	first := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "alpha", Name: "before"}, Status: login.LoginStatusOnline, Adapter: "fixture", Features: []string{"old"}}
	partial := &login.Login{Sn: 1, Status: login.LoginStatusOffline, Adapter: "fixture"}
	second := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "beta"}, Status: login.LoginStatusOnline, Adapter: "fixture"}
	if err := app.SyncLogins("source-a", config, nil, []*login.Login{first, partial}); err != nil {
		t.Fatal(err)
	}
	if err := app.SyncLogins("source-b", config, nil, []*login.Login{second}); err != nil {
		t.Fatal(err)
	}
	if len(app.Accounts()) != 3 {
		t.Fatalf("login count=%d", len(app.Accounts()))
	}
	alpha := app.AccountsBySelfID("alpha")[0]
	var change event.Event
	if err := json.Unmarshal([]byte(`{"sn":1,"type":"login-updated","timestamp":1,"login":{"sn":0,"status":0,"user":{"id":"alpha","name":"after"},"features":["new"]}}`), &change); err != nil {
		t.Fatal(err)
	}
	if err := app.PostEvent("source-a", &change); err != nil {
		t.Fatal(err)
	}
	info := alpha.SelfInfo()
	if alpha.Connected() || info.User.Name != "after" || info.Status != login.LoginStatusOffline || len(info.Features) != 1 || info.Features[0] != "new" {
		t.Fatalf("updated login=%+v", info)
	}
	app.UpdateProxyURLs("source-a", []string{"https://example.invalid/assets/"})
	if len(app.Accounts()) != 3 || len(alpha.ProxyURLs()) != 1 {
		t.Fatalf("META logins=%d proxies=%v", len(app.Accounts()), alpha.ProxyURLs())
	}
	if err := app.SyncLogins("source-a", config, nil, []*login.Login{}); err != nil {
		t.Fatal(err)
	}
	if len(app.Accounts()) != 1 || len(app.AccountsBySelfID("beta")) != 1 {
		t.Fatalf("empty READY login set=%v", app.Accounts())
	}
	if err := json.Unmarshal([]byte(`{"sn":2,"type":"login-removed","timestamp":2,"login":{"sn":0,"status":0}}`), &change); err != nil {
		t.Fatal(err)
	}
	if err := app.PostEvent("source-b", &change); err != nil {
		t.Fatal(err)
	}
	if len(app.Accounts()) != 0 {
		t.Fatalf("removed source login set=%v", app.Accounts())
	}
	recovered := ""
	app.Register(func(account *satoriclient.Account, evt *event.Event) error {
		if evt.Sn == 99 {
			recovered = account.SelfID()
			if evt.Login.Status != login.LoginStatusOnline {
				t.Error("historical event login is not online")
			}
		}
		return nil
	})
	if err := app.PostEvent("source-b", &event.Event{Sn: 99, Type: event.EventTypeMessageCreated, Login: second, Message: &message.Message{Id: "historical"}}); err != nil {
		t.Fatal(err)
	}
	if recovered != "beta" {
		t.Fatalf("recovered account=%s", recovered)
	}
	first.Sn = 7
	if err := app.SyncLogins("source-a", config, nil, []*login.Login{first}); err != nil {
		t.Fatal(err)
	}
	received := ""
	app.Register(func(account *satoriclient.Account, evt *event.Event) error { received = account.SelfID(); return nil })
	if err := app.PostEvent("source-a", &event.Event{Type: event.EventTypeMessageCreated, Login: &login.Login{Sn: 7, Platform: "mock", User: &user.User{Id: "alpha"}}}); err != nil {
		t.Fatal(err)
	}
	if received != "alpha" || app.AccountsBySelfID("alpha")[0].SelfInfo().Sn != 7 {
		t.Fatalf("rebound account=%s", received)
	}

	callbackErr := errors.New("fixture callback failed")
	app.Register(func(*satoriclient.Account, *event.Event) error { return callbackErr })
	err = app.PostEvent("source-a", &event.Event{Sn: 100, Type: event.EventTypeMessageCreated, Login: first, Message: &message.Message{Id: "callback"}})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("callback result=%v", err)
	}
}

type customConfig struct {
	identity string
}

func (c customConfig) APIBase() string {
	return "http://localhost:5140/v1"
}

func (c customConfig) TokenValue() string {
	return ""
}

func (c customConfig) TimeoutValue() time.Duration {
	return 0
}

func (c customConfig) Identity() string {
	return c.identity
}

func (c customConfig) NetworkKind() string {
	return "custom"
}

type lifecycleRunner struct {
	started chan struct{}
	closed  chan struct{}
	mode    string
	failure error
	once    sync.Once
}

func (r *lifecycleRunner) ID() string { return "lifecycle/fixture" }
func (r *lifecycleRunner) Run(ctx context.Context) error {
	close(r.started)
	switch r.mode {
	case "shutdown-failure":
		<-ctx.Done()
		return r.failure
	case "normal":
		return nil
	case "failure":
		return r.failure
	default:
		<-ctx.Done()
		return ctx.Err()
	}
}
func (r *lifecycleRunner) Close() error { r.once.Do(func() { close(r.closed) }); return nil }

func TestApplicationLifecycle(t *testing.T) {
	for _, mode := range []string{"normal", "failure", "close", "cancel", "shutdown-failure"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("fixture runner failure")
			runner := &lifecycleRunner{started: make(chan struct{}), closed: make(chan struct{}), mode: mode, failure: failure}
			app, err := satoriclient.NewApp()
			if err != nil {
				t.Fatal(err)
			}
			if err := app.RegisterNetworkFactory("custom", func(*satoriclient.App, satoriclient.Config) (clientnetwork.Runner, satoriclient.APIConfig, error) {
				return runner, staticAPIConfig{base: "http://127.0.0.1"}, nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := app.Apply(customConfig{identity: mode}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := app.RunAsync(ctx)
			select {
			case <-runner.started:
			case <-time.After(time.Second):
				t.Fatal("runner startup timeout")
			}
			if mode == "close" {
				if err := app.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancel" || mode == "shutdown-failure" {
				cancel()
			}
			select {
			case err := <-done:
				if (mode == "failure" || mode == "shutdown-failure") && !errors.Is(err, failure) {
					t.Fatalf("failure=%v", err)
				}
				if mode != "failure" && mode != "shutdown-failure" && err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run completion timeout")
			}
			select {
			case <-runner.closed:
			case <-time.After(time.Second):
				t.Fatal("runner cleanup timeout")
			}
		})
	}
}
