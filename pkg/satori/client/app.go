package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	clientnetwork "github.com/satori-protocol-go/satori-go/pkg/satori/client/network"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

type EventCallback func(account *Account, evt *event.Event) error

type LifecycleCallback func(account *Account, status login.LoginStatus) error

type NetworkFactory func(app *App, cfg Config) (clientnetwork.Runner, APIConfig, error)

type networkState struct {
	config    APIConfig
	proxyURLs []string
	logins    map[int64]*Account
}

type App struct {
	mu        sync.RWMutex
	runCancel context.CancelFunc

	accounts      map[string]*Account
	networks      []clientnetwork.Runner
	networkStates map[string]*networkState

	eventCallbacks     []EventCallback
	lifecycleCallbacks []LifecycleCallback

	defaultProtocolFactory ProtocolFactory
	networkFactories       map[string]NetworkFactory
	logger                 logging.Logger
}

var defaultApp atomic.Pointer[App]

func NewApp(configs ...Config) (*App, error) {
	app := &App{
		accounts:         map[string]*Account{},
		networkStates:    map[string]*networkState{},
		networkFactories: map[string]NetworkFactory{},
		logger:           logging.NewStdLogger(),
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

func (a *App) RegisterLogger(logger logging.Logger) {
	if logger == nil {
		logger = logging.NopLogger{}
	}
	a.mu.Lock()
	a.logger = logger
	for _, account := range a.accounts {
		account.RegisterLogger(logger)
	}
	networks := append([]clientnetwork.Runner(nil), a.networks...)
	a.mu.Unlock()
	for _, runner := range networks {
		if setter, ok := runner.(clientnetwork.LoggerSetter); ok {
			setter.SetLogger(logger)
		}
	}
}

func (a *App) Logger() logging.Logger {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.logger == nil {
		return logging.NopLogger{}
	}
	return a.logger
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

// RegisterConfig keeps naming parity with satori-python register_config.
func (a *App) RegisterConfig(kind string, factory NetworkFactory) error {
	return a.RegisterNetworkFactory(kind, factory)
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
	if a.runCancel != nil {
		a.mu.Unlock()
		_ = runner.Close()
		return errors.New("configure networks before Run")
	}
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

// On keeps decorator-style registration parity with satori-python register_on.
func (a *App) On(eventType event.EventType) func(EventCallback) EventCallback {
	return a.OnType(string(eventType))
}

// OnType keeps decorator-style registration parity with satori-python register_on.
func (a *App) OnType(eventType string) func(EventCallback) EventCallback {
	eventType = strings.TrimSpace(eventType)
	return func(callback EventCallback) EventCallback {
		a.RegisterOnType(eventType, callback)
		return callback
	}
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

func (a *App) Connections() []clientnetwork.Runner {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]clientnetwork.Runner(nil), a.networks...)
}

func (a *App) WaitForAvailable(ctx context.Context, networkIDs ...string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	required := map[string]struct{}{}
	for _, id := range networkIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		required[id] = struct{}{}
	}

	networks := a.Connections()
	found := map[string]struct{}{}
	for _, runner := range networks {
		id := runner.ID()
		if len(required) > 0 {
			if _, ok := required[id]; !ok {
				continue
			}
			found[id] = struct{}{}
		}

		available, ok := runner.(clientnetwork.Availability)
		if !ok {
			continue
		}
		if err := available.WaitForAvailable(ctx); err != nil {
			return err
		}
	}

	if len(required) == 0 {
		return nil
	}
	for id := range required {
		if _, ok := found[id]; !ok {
			return fmt.Errorf("network %q not found", id)
		}
	}
	return nil
}

func (a *App) RunAsync(ctx context.Context) <-chan error {
	result := make(chan error, 1)
	go func() {
		defer close(result)
		result <- a.Run(ctx)
	}()
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

func (a *App) SyncLogins(networkID string, cfg clientnetwork.APIConfig, proxyURLs []string, logins []*login.Login) error {
	seen := make(map[int64]bool, len(logins))
	for _, info := range logins {
		if info == nil || !info.HasField("sn") || info.Sn < 0 {
			return errors.New("invalid login sequence in snapshot")
		}
		if seen[info.Sn] {
			return fmt.Errorf("duplicate login sequence %d", info.Sn)
		}
		seen[info.Sn] = true
	}
	a.mu.Lock()
	state := a.ensureNetworkStateLocked(networkID)
	config := state.config
	if cfg != nil {
		config = cfg
	}
	factory := a.defaultProtocolFactory
	old := make(map[int64]*Account, len(state.logins))
	for sn, account := range state.logins {
		old[sn] = account
	}
	a.mu.Unlock()

	// Reuse a live handle only by a unique platform account, never by a sequence
	// from the previous connection. Factories run outside the registry lock.
	targets := map[string][]*Account{}
	for _, account := range old {
		if target := loginTarget(account.SelfInfo()); target != "" {
			targets[target] = append(targets[target], account)
		}
	}
	next := make(map[int64]*Account, len(logins))
	used := map[*Account]bool{}
	for _, info := range logins {
		var account *Account
		matches := targets[loginTarget(info)]
		if len(matches) == 1 && !used[matches[0]] {
			account = matches[0]
		}
		if account == nil {
			account = NewAccount(info, config, proxyURLs, factory)
		}
		used[account] = true
		next[info.Sn] = account
	}
	removed := []*Account{}
	a.mu.Lock()
	state = a.ensureNetworkStateLocked(networkID)
	for sn, account := range state.logins {
		delete(a.accounts, accountKey(networkID, sn))
		if !used[account] {
			account.SetConnected(false)
			removed = append(removed, account)
		}
	}
	state.config = config
	state.proxyURLs = append([]string(nil), proxyURLs...)
	state.logins = next
	for _, info := range logins {
		account := next[info.Sn]
		account.RegisterLogger(a.logger)
		account.apply(info, config, proxyURLs)
		a.accounts[accountKey(networkID, info.Sn)] = account
	}
	a.mu.Unlock()
	for _, account := range removed {
		a.accountUpdate(account, login.LoginStatusOffline)
	}
	for _, info := range logins {
		a.accountUpdate(next[info.Sn], info.Status)
	}
	return nil
}

func (a *App) UpdateProxyURLs(networkID string, proxyURLs []string) {
	a.mu.Lock()
	state := a.ensureNetworkStateLocked(networkID)
	state.proxyURLs = append([]string(nil), proxyURLs...)
	for _, account := range state.logins {
		account.apply(nil, nil, proxyURLs)
	}
	a.mu.Unlock()
}

// These keys are connection-local registry keys, not public platform identifiers.
func accountKey(networkID string, sn int64) string { return fmt.Sprintf("%s#%d", networkID, sn) }
func loginTarget(info *login.Login) string {
	if info == nil || info.User == nil || info.Platform == "" || info.User.Id == "" {
		return ""
	}
	return info.Platform + "\x00" + info.User.Id
}

// PostEvent applies a source-local login transition before invoking callbacks.
func (a *App) PostEvent(networkID string, evt *event.Event) error {
	if evt == nil || evt.Login == nil {
		return errors.New("event has no login")
	}
	sn := evt.Login.Sn
	if sn < 0 {
		return errors.New("invalid event login sequence")
	}
	added := evt.Type == event.EventTypeLoginAdded
	updated := evt.Type == event.EventTypeLoginUpdated
	removed := evt.Type == event.EventTypeLoginRemoved

	a.mu.Lock()
	state := a.ensureNetworkStateLocked(networkID)
	account := state.logins[sn]
	config := state.config
	proxies := append([]string(nil), state.proxyURLs...)
	factory := a.defaultProtocolFactory
	a.mu.Unlock()
	if !added && !updated && !removed {
		// A recovered event may refer to a login absent from the new READY.
		// Its complete event identity is sufficient for an event-scoped account.
		target := loginTarget(evt.Login)
		if target != "" && (account == nil || loginTarget(account.SelfInfo()) != target) {
			info := evt.Login.Clone()
			info.Status = login.LoginStatusOnline
			temporary := NewAccount(info, config, proxies, factory)
			temporary.RegisterLogger(a.Logger())
			temporary.SetConnected(true)
			value := *evt
			value.Login = info
			return a.dispatchEvent(temporary, &value)
		}
	}
	// A partial offline login is still a login; it simply cannot issue account APIs.
	var candidate *Account
	if account == nil && (added || updated) {
		candidate = NewAccount(evt.Login, config, proxies, factory)
	}

	a.mu.Lock()
	state = a.ensureNetworkStateLocked(networkID)
	account = state.logins[sn]
	if account == nil {
		account = candidate
	}
	if account == nil {
		a.mu.Unlock()
		return fmt.Errorf("unknown source login %s/%d", networkID, sn)
	}
	account.RegisterLogger(a.logger)
	info := account.SelfInfo()
	if added {
		info = evt.Login.Clone()
	} else if updated || removed {
		info = info.Merge(evt.Login)
	}
	if removed {
		info.Status = login.LoginStatusOffline
	}
	if !added && !updated && !removed {
		expected, actual := loginTarget(info), loginTarget(evt.Login)
		if expected != "" && actual != "" && expected != actual {
			a.mu.Unlock()
			return errors.New("event login identity does not match its source sequence")
		}
	}
	if added || updated || removed {
		account.apply(info, state.config, state.proxyURLs)
	}
	if removed {
		delete(state.logins, sn)
		delete(a.accounts, accountKey(networkID, sn))
	} else {
		state.logins[sn] = account
		a.accounts[accountKey(networkID, sn)] = account
	}
	a.mu.Unlock()
	// Keep the caller's decoded object intact; the callback gets resolved login context.
	value := *evt
	value.Login = info.Clone()
	if !added && !updated && !removed {
		value.Login.Status = login.LoginStatusOnline
	}
	if added || updated || removed {
		a.accountUpdate(account, info.Status)
	}
	return a.dispatchEvent(account, &value)
}

func (a *App) MarkNetworkStatus(networkID string, status login.LoginStatus, remove bool) {
	a.mu.Lock()
	state, ok := a.networkStates[networkID]
	if !ok {
		a.mu.Unlock()
		return
	}
	accounts := make([]*Account, 0, len(state.logins))
	for sn, account := range state.logins {
		info := account.SelfInfo()
		info.Status = status
		account.apply(info, state.config, state.proxyURLs)
		accounts = append(accounts, account)
		if remove {
			delete(a.accounts, accountKey(networkID, sn))
		}
	}
	if remove {
		state.logins = map[int64]*Account{}
	}
	a.mu.Unlock()
	for _, account := range accounts {
		a.accountUpdate(account, status)
	}
}

func (a *App) ensureNetworkStateLocked(networkID string) *networkState {
	if state, ok := a.networkStates[networkID]; ok {
		return state
	}
	state := &networkState{config: APIInfo{}, proxyURLs: []string{}, logins: map[int64]*Account{}}
	a.networkStates[networkID] = state
	return state
}

func (a *App) log(ctx context.Context, level logging.Level, v ...any) {
	logger := a.Logger()
	if logger == nil {
		return
	}
	logger.Log(ctx, level, v...)
}

func (a *App) dispatchEvent(account *Account, evt *event.Event) error {
	a.mu.RLock()
	callbacks := append([]EventCallback(nil), a.eventCallbacks...)
	a.mu.RUnlock()

	errCh := make(chan error, len(callbacks))
	var wg sync.WaitGroup
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		wg.Add(1)
		go func(fn EventCallback) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					errCh <- fmt.Errorf("event callback panic: %v", recovered)
				}
			}()
			if err := fn(account, evt); err != nil {
				errCh <- err
			}
		}(callback)
	}
	wg.Wait()
	close(errCh)

	var result error
	for err := range errCh {
		result = errors.Join(result, err)
	}
	return result
}

func (a *App) accountUpdate(account *Account, status login.LoginStatus) {
	a.log(context.Background(), logging.LevelInfo, fmt.Sprintf("Satori login status platform=%q self_id=%q status=%d", account.Platform(), account.SelfID(), status))
	a.mu.RLock()
	callbacks := append([]LifecycleCallback(nil), a.lifecycleCallbacks...)
	a.mu.RUnlock()

	errCh := make(chan error, len(callbacks))
	var wg sync.WaitGroup
	for _, callback := range callbacks {
		if callback == nil {
			continue
		}
		wg.Add(1)
		go func(fn LifecycleCallback) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					errCh <- fmt.Errorf("lifecycle callback panic: %v", recovered)
				}
			}()
			if err := fn(account, status); err != nil {
				errCh <- err
			}
		}(callback)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		a.log(context.Background(), logging.LevelError, fmt.Sprintf("lifecycle callback error error=%v", err))
	}
}

func (a *App) cleanupAccounts() {
	a.mu.RLock()
	sources := make([]string, 0, len(a.networkStates))
	for id := range a.networkStates {
		sources = append(sources, id)
	}
	a.mu.RUnlock()
	for _, id := range sources {
		a.MarkNetworkStatus(id, login.LoginStatusOffline, true)
	}
}

func wsNetworkFactory(app *App, cfg Config) (clientnetwork.Runner, APIConfig, error) {
	switch typed := cfg.(type) {
	case WebSocketConfig:
		normalized := typed
		normalized.normalize()
		handshakeTimeout := normalized.HandshakeTimeout
		if handshakeTimeout <= 0 {
			handshakeTimeout = normalized.Timeout
		}
		return clientnetwork.NewWS(app, clientnetwork.WebSocketOptions{
			Identity:         normalized.Identity(),
			WSBase:           normalized.WSBase(),
			Token:            normalized.Token,
			APIConfig:        normalized,
			HandshakeTimeout: handshakeTimeout,
			Logger:           app.Logger(),
		}), normalized, nil
	case *WebSocketConfig:
		if typed == nil {
			return nil, nil, errors.New("websocket config cannot be nil")
		}
		normalized := *typed
		normalized.normalize()
		handshakeTimeout := normalized.HandshakeTimeout
		if handshakeTimeout <= 0 {
			handshakeTimeout = normalized.Timeout
		}
		return clientnetwork.NewWS(app, clientnetwork.WebSocketOptions{
			Identity:         normalized.Identity(),
			WSBase:           normalized.WSBase(),
			Token:            normalized.Token,
			APIConfig:        normalized,
			HandshakeTimeout: handshakeTimeout,
			Logger:           app.Logger(),
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
			Logger:    app.Logger(),
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
			Logger:    app.Logger(),
		}), normalized, nil
	default:
		return nil, nil, fmt.Errorf("webhook factory does not support config type %T", cfg)
	}
}

var _ clientnetwork.AppBridge = (*App)(nil)
