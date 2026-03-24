package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var ErrEmptyJSONBody = errors.New("empty json body")

func DecodeJSONBytes(data []byte, target any) (bool, error) {
	if target == nil {
		return false, errors.New("json decode target cannot be nil")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func DecodeJSONReader(reader io.Reader, target any) (bool, error) {
	if reader == nil {
		return false, nil
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return false, err
	}
	return DecodeJSONBytes(data, target)
}

func DecodeJSONBytesRequired(data []byte, target any) error {
	decoded, err := DecodeJSONBytes(data, target)
	if err != nil {
		return err
	}
	if !decoded {
		return ErrEmptyJSONBody
	}
	return nil
}

func DecodeJSONReaderRequired(reader io.Reader, target any) error {
	decoded, err := DecodeJSONReader(reader, target)
	if err != nil {
		return err
	}
	if !decoded {
		return ErrEmptyJSONBody
	}
	return nil
}
