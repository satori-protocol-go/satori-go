package network

import (
	"bytes"
	"encoding/json"
)

func decodeJSON(payload []byte, target any) error {
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	return decoder.Decode(target)
}
