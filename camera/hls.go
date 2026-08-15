package camera

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// hlsStore holds the latest HLS playlist and segments for one camera.
type hlsStore struct {
	mu          sync.RWMutex
	playlist    []byte
	segments    map[string][]byte
	lastSegment time.Time
}

func newHLSStore() *hlsStore {
	return &hlsStore{segments: make(map[string][]byte)}
}

func (s *hlsStore) put(filename string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasSuffix(filename, ".m3u8") {
		s.playlist = data
	} else if strings.HasSuffix(filename, ".ts") {
		s.segments[filename] = data
		s.lastSegment = time.Now()
		s.prune()
	}
}

func (s *hlsStore) get(filename string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.HasSuffix(filename, ".m3u8") {
		return s.playlist, len(s.playlist) > 0
	}
	data, ok := s.segments[filename]
	return data, ok
}

func (s *hlsStore) online() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.lastSegment.IsZero() && time.Since(s.lastSegment) < 10*time.Second
}

// prune keeps only the most recent 10 segments. Must be called with mu held.
func (s *hlsStore) prune() {
	if len(s.segments) <= 10 {
		return
	}
	names := make([]string, 0, len(s.segments))
	for name := range s.segments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-10] {
		delete(s.segments, name)
	}
}
