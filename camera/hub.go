package camera

import (
	"sync"
	"sync/atomic"
)

// hub broadcasts JPEG frames from the Pi to all active browser streams.
type hub struct {
	mu          sync.RWMutex
	subs        map[chan []byte]struct{}
	piConnected atomic.Int32
}

func newHub() *hub {
	return &hub{subs: make(map[chan []byte]struct{})}
}

func (h *hub) online() bool { return h.piConnected.Load() > 0 }

func (h *hub) publish(frame []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- frame:
		default: // slow subscriber — drop frame rather than block
		}
	}
}

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, 2)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
	close(ch)
}
