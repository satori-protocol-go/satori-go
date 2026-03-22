package define

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Paginated[T any] struct {
	Data []T    `json:"data"`
	Next string `json:"next,omitempty"`
}

type BidiPaginated[T any] struct {
	Data []T    `json:"data"`
	Prev string `json:"prev,omitempty"`
	Next string `json:"next,omitempty"`
}

type Direction string

const (
	DirectionBefore Direction = "before"
	DirectionAfter  Direction = "after"
	DirectionAround Direction = "around"
)

func (d Direction) Valid() bool {
	switch d {
	case DirectionBefore, DirectionAfter, DirectionAround:
		return true
	default:
		return false
	}
}

func (d *Direction) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	normalized := Direction(strings.ToLower(strings.TrimSpace(value)))
	if !normalized.Valid() {
		return fmt.Errorf("invalid direction")
	}
	*d = normalized
	return nil
}

type Order string

const (
	OrderAsc  Order = "asc"
	OrderDesc Order = "desc"
)

func (o Order) Valid() bool {
	switch o {
	case OrderAsc, OrderDesc:
		return true
	default:
		return false
	}
}

func (o *Order) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	normalized := Order(strings.ToLower(strings.TrimSpace(value)))
	if !normalized.Valid() {
		return fmt.Errorf("invalid order")
	}
	*o = normalized
	return nil
}
