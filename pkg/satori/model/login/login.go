package login

import (
	"encoding/json"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

type LoginStatus uint8

const (
	LoginStatusOffline LoginStatus = iota
	LoginStatusOnline
	LoginStatusConnect
	LoginStatusDisconnect
	LoginStatusReconnect
)

// Login is the Satori login payload.
type Login struct {
	Sn       int64       `json:"sn"`
	Platform string      `json:"platform,omitempty"`
	User     *user.User  `json:"user,omitempty"`
	Status   LoginStatus `json:"status"`
	Adapter  string      `json:"adapter"`
	Features []string    `json:"features,omitempty"`
}

func (l *Login) UnmarshalJSON(data []byte) error {
	type loginWire struct {
		Sn       *int64       `json:"sn"`
		Platform string       `json:"platform"`
		User     *user.User   `json:"user"`
		Status   *LoginStatus `json:"status"`
		Adapter  *string      `json:"adapter"`
		Features []string     `json:"features"`
		SelfID   string       `json:"self_id"`
	}

	var wire loginWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	l.Sn = 0
	if wire.Sn != nil {
		l.Sn = *wire.Sn
	}
	l.Platform = wire.Platform
	l.User = wire.User
	if l.User == nil && wire.SelfID != "" {
		l.User = &user.User{Id: wire.SelfID}
	}
	if wire.Status != nil {
		l.Status = *wire.Status
	} else {
		l.Status = LoginStatusOnline
	}
	if wire.Adapter != nil {
		l.Adapter = *wire.Adapter
	} else {
		l.Adapter = "satori"
	}
	l.Features = append([]string(nil), wire.Features...)
	return nil
}
