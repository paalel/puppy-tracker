package camera

import (
	"sync"
	"time"
)

type presenceStatus struct {
	Present   bool
	UpdatedAt time.Time
}

type hub struct {
	camerasMu  sync.Mutex
	cameras    map[string]*hlsStore
	presenceMu sync.RWMutex
	presence   map[string]presenceStatus
}

func newHub() *hub {
	return &hub{
		cameras:  make(map[string]*hlsStore),
		presence: make(map[string]presenceStatus),
	}
}

func (h *hub) camera(id string) *hlsStore {
	h.camerasMu.Lock()
	defer h.camerasMu.Unlock()
	if _, ok := h.cameras[id]; !ok {
		h.cameras[id] = newHLSStore()
	}
	return h.cameras[id]
}

func (h *hub) setPresence(id string, present bool) {
	h.presenceMu.Lock()
	h.presence[id] = presenceStatus{Present: present, UpdatedAt: time.Now()}
	h.presenceMu.Unlock()
}

func (h *hub) getPresence(id string) presenceStatus {
	h.presenceMu.RLock()
	defer h.presenceMu.RUnlock()
	return h.presence[id]
}
