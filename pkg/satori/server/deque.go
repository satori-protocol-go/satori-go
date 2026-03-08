package server

import (
	"sync"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

type eventDeque struct {
	mu     sync.RWMutex
	maxlen int
	offset int64
	data   []*event.Event
}

func newEventDeque(maxlen int) eventDeque {
	if maxlen <= 0 {
		maxlen = 1
	}
	return eventDeque{
		maxlen: maxlen,
		data:   make([]*event.Event, 0, maxlen),
	}
}

func (d *eventDeque) Append(item *event.Event) {
	if item == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.data) == d.maxlen {
		d.offset++
		copy(d.data, d.data[1:])
		d.data[len(d.data)-1] = item
		return
	}
	d.data = append(d.data, item)
}

func (d *eventDeque) After(sequence int64) []*event.Event {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.data) == 0 {
		return nil
	}
	if sequence < d.offset {
		sequence = d.offset - 1
	}

	start := sequence + 1 - d.offset
	if start < 0 {
		start = 0
	}
	if start >= int64(len(d.data)) {
		return nil
	}

	result := make([]*event.Event, len(d.data)-int(start))
	copy(result, d.data[start:])
	return result
}
