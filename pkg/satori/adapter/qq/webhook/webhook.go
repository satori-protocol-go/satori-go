package webhook

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	botgosignature "github.com/WindowsSov8forUs/botgo-plus/interaction/signature"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type Payload struct {
	Op   botgodto.OPCode    `json:"op"`
	Type botgodto.EventType `json:"t,omitempty"`
	Seq  int64              `json:"s,omitempty"`
	ID   string             `json:"id,omitempty"`
	Data json.RawMessage    `json:"d,omitempty"`
}

func ParsePayload(body []byte) (*Payload, error) {
	payload := &Payload{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func VerifySignature(secret string, header http.Header, body []byte) error {
	passed, err := botgosignature.Verify(secret, header, body)
	if err != nil {
		return err
	}
	if !passed {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func BuildValidationResponse(secret string, raw json.RawMessage) (*botgodto.WHValidationResponse, error) {
	data := &botgodto.WHValidationRequest{}
	if err := json.Unmarshal(raw, data); err != nil {
		return nil, err
	}

	privateKey := ed25519.NewKeyFromSeed([]byte(DeriveSeed(secret)))
	message := []byte(data.EventTs + data.PlainToken)
	signature := hex.EncodeToString(ed25519.Sign(privateKey, message))

	return &botgodto.WHValidationResponse{
		PlainToken: data.PlainToken,
		Signature:  signature,
	}, nil
}

func DeriveSeed(secret string) string {
	if secret == "" {
		return strings.Repeat("0", ed25519.SeedSize)
	}
	seed := secret
	for len(seed) < ed25519.SeedSize {
		seed += secret
	}
	return seed[:ed25519.SeedSize]
}

func BuildRootRoutes(paths []string, handler http.HandlerFunc) []satoriserver.RootRoute {
	routes := make([]satoriserver.RootRoute, 0, len(paths))
	for _, path := range paths {
		routes = append(routes, satoriserver.RootRoute{
			Path:    path,
			Methods: []string{http.MethodPost},
			Handler: handler,
		})
	}
	return routes
}
