package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

type Webhook struct {
	base *baseNetwork

	host    string
	port    int
	path    string
	token   string
	timeout time.Duration

	mu        sync.Mutex
	server    *http.Server
	client    *http.Client
	runCancel context.CancelFunc
}

func NewWebhook(app AppBridge, options WebhookOptions) *Webhook {
	identity := strings.TrimSpace(options.Identity)
	if identity == "" {
		identity = "default"
	}

	path := strings.TrimSpace(options.Path)
	if path == "" {
		path = "/v1/events"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	network := &Webhook{
		host:    options.Host,
		port:    options.Port,
		path:    path,
		token:   options.Token,
		timeout: normalizedTimeout(options.Timeout),
		client:  &http.Client{Timeout: normalizedTimeout(options.Timeout)},
	}
	network.base = newBaseNetwork(app, options.APIConfig, fmt.Sprintf("satori/net/wh/%s#%p", identity, network), options.Logger)
	return network
}

func (n *Webhook) ID() string {
	return n.base.ID()
}

func (n *Webhook) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	n.mu.Lock()
	if n.runCancel != nil {
		n.mu.Unlock()
		cancel()
		return errors.New("webhook network is already running")
	}
	n.runCancel = cancel
	n.mu.Unlock()
	defer func() { cancel(); n.mu.Lock(); n.runCancel = nil; n.mu.Unlock() }()
	select {
	case <-n.base.CloseSignal():
		return nil
	default:
	}

	mux := http.NewServeMux()
	mux.HandleFunc(n.path, n.handleRequest)

	addr := webhookAddress(n.host, n.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	n.mu.Lock()
	n.server = server
	n.mu.Unlock()
	n.base.MarkAvailable()

	errCh := make(chan error, 1)
	go func() {
		serveErr := server.Serve(listener)
		if serveErr != nil && serveErr != http.ErrServerClosed && serveErr != net.ErrClosed {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	if err := n.fetchMeta(ctx); err != nil {
		_ = n.Close()
		return err
	}

	select {
	case <-ctx.Done():
		_ = n.Close()
		return nil
	case serveErr := <-errCh:
		n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
		return serveErr
	}
}

func (n *Webhook) Close() error {
	n.base.MarkClosed()

	n.mu.Lock()
	server := n.server
	cancelRun := n.runCancel
	n.server = nil
	n.mu.Unlock()
	if cancelRun != nil {
		cancelRun()
	}

	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}

	n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
	return nil
}

func (n *Webhook) Alive() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.server != nil
}

func (n *Webhook) WaitForAvailable(ctx context.Context) error {
	return n.base.WaitAvailable(ctx)
}

func (n *Webhook) SetLogger(logger logging.Logger) {
	if n == nil || n.base == nil {
		return
	}
	n.base.SetLogger(logger)
}

func (n *Webhook) fetchMeta(ctx context.Context) error {
	endpoint := joinURLPath(n.base.Config().APIBase(), "meta")
	body := bytes.NewReader([]byte("{}"))

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.client.Do(request)
	if err != nil {
		return err
	}
	payload, err := readResponseBody(response)
	if err != nil {
		return err
	}
	if err := validateHTTPStatus(response.StatusCode, payload); err != nil {
		return err
	}

	var data meta.Meta
	if err := decodeJSON(payload, &data); err != nil {
		return err
	}

	n.base.SetProxyURLs(data.ProxyUrls)
	n.base.app.SyncLogins(n.ID(), n.base.Config(), data.ProxyUrls, data.Logins)
	return nil
}

func (n *Webhook) handleRequest(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	token, ok := protocol.ParseBearer(request.Header.Get(protocol.HeaderAuthorization))
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if n.token != "" && token != n.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	payload, err := io.ReadAll(request.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	opcode, _ := protocol.ParseOpcode(request.Header.Get(protocol.HeaderOpcode))

	switch operation.Opcode(opcode) {
	case operation.OpcodeMeta:
		var metaPayload operation.MetaBody
		if err := decodeJSON(payload, &metaPayload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.base.SetProxyURLs(metaPayload.ProxyUrls)
		n.base.app.SyncLogins(n.ID(), n.base.Config(), metaPayload.ProxyUrls, nil)
		w.WriteHeader(http.StatusOK)
		return

	case operation.OpcodeEvent:
		var evt event.Event
		if err := decodeJSON(payload, &evt); err != nil {
			n.base.Log(request.Context(), logging.LevelWarn, fmt.Sprintf("failed to parse webhook event network_id=%s error=%v", n.ID(), err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		n.base.SetSequence(evt.Sn)
		// Keep webhook request path fast; callbacks run asynchronously.
		go n.base.app.PostEvent(n.ID(), &evt)
		w.WriteHeader(http.StatusOK)
		return

	default:
		w.WriteHeader(http.StatusAccepted)
		return
	}
}

var _ Runner = (*Webhook)(nil)
var _ Availability = (*Webhook)(nil)
var _ LoggerSetter = (*Webhook)(nil)
