package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	clientnetwork "github.com/satori-protocol-go/satori-go/pkg/satori/client/network"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

type EventCallback func(account *Account, evt *event.Event) error

type LifecycleCallback func(account *Account, status login.LoginStatus) error

type NetworkFactory func(app *App, cfg Config) (clientnetwork.Runner, APIConfig, error)

type networkState struct {
	config    APIConfig
	proxyURLs []string
	accountID map[string]struct{}
}

type App struct {
	mu sync.RWMutex

	accounts      map[string]*Account
	networks      []clientnetwork.Runner
	networkStates map[string]*networkState

	eventCallbacks     []EventCallback
	lifecycleCallbacks []LifecycleCallback

	defaultProtocolFactory ProtocolFactory
	networkFactories       map[string]NetworkFactory
}

var defaultApp atomic.Pointer[App]

func NewApp(configs ...Config) (*App, error) {
	app := &App{
		accounts:         map[string]*Account{},
		networkStates:    map[string]*networkState{},
		networkFactories: map[string]NetworkFactory{},
		defaultProtocolFactory: func(account *Account) *APIProtocol {
			return NewAPIProtocol(account, nil)
		},
	}
	app.registerNetworkFactoryLocked("ws", wsNetworkFactory)
	app.registerNetworkFactoryLocked("webhook", webhookNetworkFactory)
	for _, cfg := range configs {
		if err := app.Apply(cfg); err != nil {
			return nil, err
		}
	}
	defaultApp.Store(app)
	return app, nil
}

func GetApp() (*App, error) {
	app := defaultApp.Load()
	if app == nil {
		return nil, errors.New("app is not initialized")
	}
	return app, nil
}

func GetAccounts() map[string]*Account {
	app := defaultApp.Load()
	if app == nil {
		return map[string]*Account{}
	}
	return app.Accounts()
}

func GetAccountsBySelfID(selfID string) []*Account {
	app := defaultApp.Load()
	if app == nil {
		return []*Account{}
	}
	return app.AccountsBySelfID(selfID)
}

func (a *App) SetDefaultProtocolFactory(factory ProtocolFactory) {
	if factory == nil {
		return
	}
	a.mu.Lock()
	a.defaultProtocolFactory = factory
	a.mu.Unlock()
}

func (a *App) RegisterNetworkFactory(kind string, factory NetworkFactory) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return errors.New("network kind cannot be empty")
	}
	if factory == nil {
		return errors.New("network factory cannot be nil")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.registerNetworkFactoryLocked(kind, factory)
	return nil
}

func (a *App) registerNetworkFactoryLocked(kind string, factory NetworkFactory) {
	if a.networkFactories == nil {
		a.networkFactories = map[string]NetworkFactory{}
	}
	a.networkFactories[kind] = factory
}

func (a *App) Apply(cfg Config) error {
	if cfg == nil {
		return errors.New("config cannot be nil")
	}

	kind := strings.TrimSpace(cfg.NetworkKind())
	if kind == "" {
		return errors.New("config network kind cannot be empty")
	}

	a.mu.RLock()
	factory, ok := a.networkFactories[kind]
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown network kind: %s", kind)
	}

	runner, apiCfg, err := factory(a, cfg)
	if err != nil {
		return err
	}
	if runner == nil {
		return fmt.Errorf("network factory %q returned nil runner", kind)
	}
	if apiCfg == nil {
		apiCfg = cfg
	}

	networkIDRef := runner.ID()
	a.mu.Lock()
	a.networks = append(a.networks, runner)
	state := a.ensureNetworkStateLocked(networkIDRef)
	if apiCfg != nil {
		state.config = apiCfg
	}
	a.mu.Unlock()
	return nil
}

func (a *App) Register(callback EventCallback) {
	if callback == nil {
		return
	}
	a.mu.Lock()
	a.eventCallbacks = append(a.eventCallbacks, callback)
	a.mu.Unlock()
}

func (a *App) RegisterOn(eventType event.EventType, callback EventCallback) {
	a.RegisterOnType(string(eventType), callback)
}

func (a *App) RegisterOnType(eventType string, callback EventCallback) {
	if callback == nil {
		return
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return
	}
	a.Register(func(account *Account, evt *event.Event) error {
		if evt != nil && string(evt.Type) == eventType {
			return callback(account, evt)
		}
		return nil
	})
}

func (a *App) Lifecycle(callback LifecycleCallback) {
	if callback == nil {
		return
	}
	a.mu.Lock()
	a.lifecycleCallbacks = append(a.lifecycleCallbacks, callback)
	a.mu.Unlock()
}

func (a *App) Accounts() map[string]*Account {
	a.mu.RLock()
	defer a.mu.RUnlock()
	copied := make(map[string]*Account, len(a.accounts))
	for key, account := range a.accounts {
		copied[key] = account
	}
	return copied
}

func (a *App) AccountsBySelfID(selfID string) []*Account {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]*Account, 0)
	for _, account := range a.accounts {
		if account.SelfID() == selfID {
			result = append(result, account)
		}
	}
	return result
}

func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.RLock()
	networks := append([]clientnetwork.Runner(nil), a.networks...)
	a.mu.RUnlock()
	if len(networks) == 0 {
		return errors.New("no network configured")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(networks))
	var wg sync.WaitGroup

	for _, runner := range networks {
		wg.Add(1)
		go func(r clientnetwork.Runner) {
			defer wg.Done()
			if err := r.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- err
			}
		}(runner)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		runErr = err
		cancel()
	}

	for _, runner := range networks {
		_ = runner.Close()
	}
	wg.Wait()

	a.cleanupAccounts()
	return runErr
}

func (a *App) Close() error {
	a.mu.RLock()
	networks := append([]clientnetwork.Runner(nil), a.networks...)
	a.mu.RUnlock()
	for _, runner := range networks {
		_ = runner.Close()
	}
	a.cleanupAccounts()
	return nil
}

func (a *App) SyncLogins(networkID string, cfg clientnetwork.APIConfig, proxyURLs []string, logins []*login.Login) {
	var localCfg APIConfig
	if cfg != nil {
		localCfg = cfg
	}

	a.mu.Lock()
	state := a.ensureNetworkStateLocked(networkID)
	if localCfg != nil {
		state.config = localCfg
	}
	state.proxyURLs = append([]string(nil), proxyURLs...)

	existing := make([]*Account, 0, len(state.accountID))
	for identity := range state.accountID {
		if account, ok := a.accounts[identity]; ok {
			existing = append(existing, account)
		}
	}
	config := state.config
	proxy := append([]string(nil), state.proxyURLs...)
	a.mu.Unlock()

	for _, account := range existing {
		account.Config = config
		account.SetProxyURLs(proxy)
	}

	for _, item := range logins {
		_, account, ok := a.ensureAccount(item, networkID)
		if !ok {
			continue
		}
		connected := item.Status == login.LoginStatusOnline || item.Status == login.LoginStatusConnect
		account.SetConnected(connected)
		a.accountUpdate(account, item.Status)
	}
}

func (a *App) PostEvent(networkID string, evt *event.Event) {
	a.post(evt, networkID)
}

func (a *App) MarkNetworkStatus(networkID string, status login.LoginStatus, remove bool) {
	a.markNetworkStatus(networkID, status, remove)
}

func (a *App) post(evt *event.Event, networkID string) {
	if evt == nil {
		return
	}

	var (
		identity string
		account  *Account
		ok       bool
	)

	switch evt.Type {
	case event.EventTypeLoginAdded:
		identity, account, ok = a.ensureAccount(evt.Login, networkID)
		if !ok {
			return
		}
		account.SetConnected(evt.Login.Status == login.LoginStatusOnline)
		a.accountUpdate(account, evt.Login.Status)
	case event.EventTypeLoginUpdated:
		identity = a.accountIdentity(evt.Login, networkID)
		if identity == "" {
			return
		}

		a.mu.RLock()
		account, ok = a.accounts[identity]
		a.mu.RUnlock()
		if !ok {
			if evt.Login == nil || evt.Login.Status != login.LoginStatusOnline {
				log.Printf("[satori-client] received login update for unknown account: %+v", evt)
				return
			}
			_, account, ok = a.ensureAccount(evt.Login, networkID)
			if !ok {
				return
			}
		}
		connected := evt.Login.Status == login.LoginStatusOnline || evt.Login.Status == login.LoginStatusConnect
		account.SetConnected(connected)
		a.accountUpdate(account, evt.Login.Status)
	case event.EventTypeLoginRemoved:
		identity = a.accountIdentity(evt.Login, networkID)
		if identity == "" {
			return
		}
		a.mu.RLock()
		account, ok = a.accounts[identity]
		a.mu.RUnlock()
		if !ok {
			log.Printf("[satori-client] received login removed for unknown account: %+v", evt)
			return
		}
	default:
		identity = a.accountIdentity(evt.Login, networkID)
		if identity == "" {
			return
		}
		a.mu.RLock()
		account, ok = a.accounts[identity]
		a.mu.RUnlock()
		if !ok {
			log.Printf("[satori-client] received event for unknown account: %+v", evt)
			return
		}
	}

	a.dispatchEvent(account, evt)

	if evt.Type == event.EventTypeLoginRemoved {
		account.SetConnected(false)
		a.accountUpdate(account, login.LoginStatusOffline)

		a.mu.Lock()
		delete(a.accounts, identity)
		if state, exists := a.networkStates[networkID]; exists {
			delete(state.accountID, identity)
		}
		a.mu.Unlock()
	}
}

func (a *App) markNetworkStatus(networkID string, status login.LoginStatus, remove bool) {
	a.mu.Lock()
	state, ok := a.networkStates[networkID]
	if !ok {
		a.mu.Unlock()
		return
	}

	identities := make([]string, 0, len(state.accountID))
	accounts := make([]*Account, 0, len(state.accountID))
	for identity := range state.accountID {
		account, exists := a.accounts[identity]
		if !exists {
			continue
		}
		identities = append(identities, identity)
		accounts = append(accounts, account)
	}

	if remove {
		for _, identity := range identities {
			delete(a.accounts, identity)
			delete(state.accountID, identity)
		}
	}
	a.mu.Unlock()

	for _, account := range accounts {
		account.SetConnected(false)
		a.accountUpdate(account, status)
	}
}

func (a *App) ensureAccount(info *login.Login, networkID string) (string, *Account, bool) {
	identity := a.accountIdentity(info, networkID)
	if identity == "" {
		return "", nil, false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	state := a.ensureNetworkStateLocked(networkID)
	if state.config == nil {
		state.config = APIInfo{}
	}
	if account, ok := a.accounts[identity]; ok {
		account.SelfInfo = info
		account.Adapter = info.Adapter
		account.Config = state.config
		account.SetProxyURLs(state.proxyURLs)
		state.accountID[identity] = struct{}{}
		return identity, account, true
	}

	account := NewAccount(info, state.config, state.proxyURLs, a.defaultProtocolFactory)
	a.accounts[identity] = account
	state.accountID[identity] = struct{}{}
	return identity, account, true
}

func (a *App) accountIdentity(info *login.Login, networkID string) string {
	if info == nil || info.User == nil {
		return ""
	}
	platform := info.Platform
	if platform == "" {
		platform = "satori"
	}
	return fmt.Sprintf("%s_%s@%s", platform, info.User.Id, networkID)
}

func (a *App) ensureNetworkStateLocked(networkID string) *networkState {
	state, ok := a.networkStates[networkID]
	if ok {
		return state
	}
	state = &networkState{
		config:    APIInfo{},
		proxyURLs: []string{},
		accountID: map[string]struct{}{},
	}
	a.networkStates[networkID] = state
	return state
}

func (a *App) dispatchEvent(account *Account, evt *event.Event) {
	a.mu.RLock()
	callbacks := append([]EventCallback(nil), a.eventCallbacks...)
	a.mu.RUnlock()
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		if err := callback(account, evt); err != nil {
			log.Printf("[satori-client] event callback error: %v", err)
		}
	}
}

func (a *App) accountUpdate(account *Account, status login.LoginStatus) {
	a.mu.RLock()
	callbacks := append([]LifecycleCallback(nil), a.lifecycleCallbacks...)
	a.mu.RUnlock()
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		if err := callback(account, status); err != nil {
			log.Printf("[satori-client] lifecycle callback error: %v", err)
		}
	}
}

func (a *App) cleanupAccounts() {
	a.mu.Lock()
	accounts := make([]*Account, 0, len(a.accounts))
	for _, account := range a.accounts {
		accounts = append(accounts, account)
	}
	a.accounts = map[string]*Account{}
	for _, state := range a.networkStates {
		state.accountID = map[string]struct{}{}
	}
	a.mu.Unlock()

	for _, account := range accounts {
		account.SetConnected(false)
		a.accountUpdate(account, login.LoginStatusOffline)
	}
}

func wsNetworkFactory(app *App, cfg Config) (clientnetwork.Runner, APIConfig, error) {
	switch typed := cfg.(type) {
	case WebSocketConfig:
		normalized := typed
		normalized.normalize()
		return clientnetwork.NewWS(app, clientnetwork.WebSocketOptions{
			Identity:  normalized.Identity(),
			WSBase:    normalized.WSBase(),
			Token:     normalized.Token,
			APIConfig: normalized,
		}), normalized, nil
	case *WebSocketConfig:
		if typed == nil {
			return nil, nil, errors.New("websocket config cannot be nil")
		}
		normalized := *typed
		normalized.normalize()
		return clientnetwork.NewWS(app, clientnetwork.WebSocketOptions{
			Identity:  normalized.Identity(),
			WSBase:    normalized.WSBase(),
			Token:     normalized.Token,
			APIConfig: normalized,
		}), normalized, nil
	default:
		return nil, nil, fmt.Errorf("ws factory does not support config type %T", cfg)
	}
}

func webhookNetworkFactory(app *App, cfg Config) (clientnetwork.Runner, APIConfig, error) {
	switch typed := cfg.(type) {
	case WebhookConfig:
		normalized := typed
		normalized.normalize()
		return clientnetwork.NewWebhook(app, clientnetwork.WebhookOptions{
			Identity:  normalized.Identity(),
			Host:      normalized.Host,
			Port:      normalized.Port,
			Path:      normalized.Path,
			Token:     normalized.Token,
			APIConfig: normalized,
			Timeout:   normalized.Timeout,
		}), normalized, nil
	case *WebhookConfig:
		if typed == nil {
			return nil, nil, errors.New("webhook config cannot be nil")
		}
		normalized := *typed
		normalized.normalize()
		return clientnetwork.NewWebhook(app, clientnetwork.WebhookOptions{
			Identity:  normalized.Identity(),
			Host:      normalized.Host,
			Port:      normalized.Port,
			Path:      normalized.Path,
			Token:     normalized.Token,
			APIConfig: normalized,
			Timeout:   normalized.Timeout,
		}), normalized, nil
	default:
		return nil, nil, fmt.Errorf("webhook factory does not support config type %T", cfg)
	}
}

var _ clientnetwork.AppBridge = (*App)(nil)
