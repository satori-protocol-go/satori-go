package server

import (
	"encoding/json"
	"sync"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

// cachedEvent owns encoded event bytes. Body is immutable once appended.
type cachedEvent struct {
	sn   int64
	kind event.EventType
	body json.RawMessage
}

type eventDeque struct {
	mu     sync.RWMutex
	maxlen int
	data   []cachedEvent
}

func newEventDeque(maxlen int) eventDeque {
	if maxlen <= 0 {
		maxlen = 1
	}
	return eventDeque{maxlen: maxlen, data: make([]cachedEvent, 0, maxlen)}
}

func (d *eventDeque) Append(item cachedEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.data) == d.maxlen {
		copy(d.data, d.data[1:])
		d.data[len(d.data)-1] = item
		return
	}
	d.data = append(d.data, item)
}

func (d *eventDeque) After(sequence int64) []cachedEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]cachedEvent, 0, len(d.data))
	for _, item := range d.data {
		if item.sn > sequence {
			result = append(result, item)
		}
	}
	return result
}
