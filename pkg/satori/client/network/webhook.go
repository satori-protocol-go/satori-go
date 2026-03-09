package network

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

type Webhook struct {
	base *base

	host    string
	port    int
	path    string
	token   string
	timeout time.Duration

	mu     sync.Mutex
	server *http.Server
	client *http.Client
}

func NewWebhook(app AppBridge, options WebhookOptions) *Webhook {
	identity := options.Identity
	if identity == "" {
		identity = fmt.Sprintf("webhook@%p", &options)
	}
	path := strings.TrimSpace(options.Path)
	if path == "" {
		path = "/v1/events"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return &Webhook{
		base:    newBase(app, options.APIConfig, "satori/net/wh/"+identity),
		host:    options.Host,
		port:    options.Port,
		path:    path,
		token:   options.Token,
		timeout: options.Timeout,
		client:  &http.Client{Timeout: normalizedTimeout(options.Timeout)},
	}
}

func (n *Webhook) ID() string {
	return n.base.ID()
}

func (n *Webhook) Run(ctx context.Context) error {
	if err := n.fetchMeta(ctx); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(n.path, n.handleRequest)
	server := &http.Server{
		Addr:    webhookAddress(n.host, n.port),
		Handler: mux,
	}

	n.mu.Lock()
	n.server = server
	n.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
		return nil
	case err := <-errCh:
		n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
		return err
	}
}

func (n *Webhook) Close() error {
	n.base.MarkClosed()
	n.mu.Lock()
	server := n.server
	n.server = nil
	n.mu.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
	return nil
}

func (n *Webhook) fetchMeta(ctx context.Context) error {
	endpoint := stringsJoinPath(n.base.Config().APIBase(), "meta")
	requestBody := bytes.NewReader([]byte("{}"))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("meta request failed with status %d: %s", response.StatusCode, string(payload))
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
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer"))
	if n.token != "" && token != n.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	opcodeRaw := request.Header.Get("Satori-OpCode")
	opcode, _ := strconv.Atoi(opcodeRaw)

	payload, err := io.ReadAll(request.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

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
	case operation.OpcodeEvent:
		var evt event.Event
		if err := decodeJSON(payload, &evt); err != nil {
			log.Printf("[satori-client] failed to parse webhook event: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		n.base.SetSequence(evt.Sn)
		go n.base.app.PostEvent(n.ID(), &evt)
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}
