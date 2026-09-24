package login

import (
	"encoding/json"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

type LoginStatus uint8

const (
	LoginStatusOffline LoginStatus = iota
	LoginStatusOnline
	LoginStatusConnect
	LoginStatusDisconnect
	LoginStatusReconnect
)

// Login identifies a login within the current source connection. Sn is not a platform ID.
type Login struct {
	fields   types.FieldPresence
	Sn       int64       `json:"sn"`
	Platform string      `json:"platform,omitempty"`
	User     *user.User  `json:"user,omitempty"`
	Status   LoginStatus `json:"status"`
	Adapter  string      `json:"adapter"`
	Features []string    `json:"features,omitempty"`
}

func (l *Login) UnmarshalJSON(data []byte) error {
	type plain Login
	var value plain
	fields, err := types.DecodeFields(data, &value)
	if err != nil {
		return err
	}
	*l = Login(value)
	l.fields = fields
	if !fields.Has("status") {
		l.Status = LoginStatusOnline
	}
	if !fields.Has("adapter") {
		l.Adapter = "satori"
	}
	// Existing, inexpensive input compatibility; output always uses user.id.
	if !fields.Has("user") {
		var legacy struct {
			SelfID string `json:"self_id"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		if legacy.SelfID != "" {
			l.User = &user.User{Id: legacy.SelfID}
		}
	}
	return nil
}

func (l Login) MarshalJSON() ([]byte, error) {
	out := map[string]any{"sn": l.Sn, "status": l.Status, "adapter": l.Adapter}
	l.fields.Put(out, "platform", l.Platform, l.Platform != "")
	l.fields.Put(out, "user", l.User, l.User != nil)
	l.fields.Put(out, "features", l.Features, l.Features != nil)
	return json.Marshal(out)
}

// HasField is used when applying a partial wire login update. Programmatically
// constructed logins always supply Sn/Status and any nonempty optional fields.
func (l *Login) HasField(name string) bool {
	if l == nil {
		return false
	}
	if l.fields != nil {
		return l.fields.Has(name)
	}
	switch name {
	case "sn", "status":
		return true
	case "platform":
		return l.Platform != ""
	case "user":
		return l.User != nil
	case "adapter":
		return l.Adapter != ""
	case "features":
		return l.Features != nil
	}
	return false
}

func (l *Login) Clone() *Login {
	if l == nil {
		return nil
	}
	copy := *l
	if l.User != nil {
		value := *l.User
		copy.User = &value
	}
	if l.Features != nil {
		copy.Features = append([]string{}, l.Features...)
	}
	copy.fields = l.fields.Without()
	if l.fields == nil {
		copy.fields = nil
	}
	return &copy
}

// Merge applies only fields supplied by a partial update, including explicit null.
func (l *Login) Merge(update *Login) *Login {
	if l == nil {
		return update.Clone()
	}
	result := l.Clone()
	if update == nil {
		return result
	}
	if update.HasField("sn") {
		result.Sn = update.Sn
	}
	if update.HasField("status") {
		result.Status = update.Status
	}
	if update.HasField("platform") {
		result.Platform = update.Platform
	}
	if update.HasField("user") {
		result.User = nil
		if update.User != nil {
			copy := *update.User
			result.User = &copy
		}
	}
	if update.HasField("adapter") {
		result.Adapter = update.Adapter
	}
	if update.HasField("features") {
		result.Features = nil
		if update.Features != nil {
			result.Features = append([]string{}, update.Features...)
		}
	}
	if result.fields == nil {
		result.fields = types.FieldPresence{}
	}
	for name, value := range update.fields {
		result.fields[name] = value
	}
	return result
}
