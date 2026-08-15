package camera

import (
	"regexp"
	"sort"
	"strconv"
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
	lastInterval time.Duration
	avgInterval  time.Duration // exponential moving average (α=0.2)
}

func newHLSStore() *hlsStore {
	return &hlsStore{segments: make(map[string][]byte)}
}

// put stores a playlist or segment. For segments it returns the interval since
// the previous segment (zero on the first segment).
func (s *hlsStore) put(filename string, data []byte) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasSuffix(filename, ".m3u8") {
		s.playlist = data
		return 0
	}
	if !strings.HasSuffix(filename, ".ts") {
		return 0
	}
	now := time.Now()
	var interval time.Duration
	if !s.lastSegment.IsZero() {
		interval = now.Sub(s.lastSegment)
		s.lastInterval = interval
		if s.avgInterval == 0 {
			s.avgInterval = interval
		} else {
			s.avgInterval = (s.avgInterval*4 + interval) / 5
		}
	}
	s.segments[filename] = data
	s.lastSegment = now
	s.prune()
	return interval
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

var segNumRe = regexp.MustCompile(`(\d+)\.ts$`)

func segNum(name string) int {
	m := segNumRe.FindStringSubmatch(name)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

type hlsStats struct {
	Online          bool          `json:"online"`
	LastSegment     time.Time     `json:"last_segment"`
	SegmentCount    int           `json:"segments"`
	LastIntervalMs  int64         `json:"last_interval_ms"`
	AvgIntervalMs   int64         `json:"avg_interval_ms"`
}

func (s *hlsStore) stats() hlsStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hlsStats{
		Online:         !s.lastSegment.IsZero() && time.Since(s.lastSegment) < 10*time.Second,
		LastSegment:    s.lastSegment,
		SegmentCount:   len(s.segments),
		LastIntervalMs: s.lastInterval.Milliseconds(),
		AvgIntervalMs:  s.avgInterval.Milliseconds(),
	}
}

// prune keeps only the 15 most-recent segments, ordered by segment number.
// Must be called with mu held (write).
func (s *hlsStore) prune() {
	const keep = 15
	if len(s.segments) <= keep {
		return
	}
	names := make([]string, 0, len(s.segments))
	for name := range s.segments {
		names = append(names, name)
	}
	// Sort by numeric segment ID, not lexicographically.
	// "seg10.ts" > "seg9.ts", not the other way around.
	sort.Slice(names, func(i, j int) bool {
		return segNum(names[i]) < segNum(names[j])
	})
	for _, name := range names[:len(names)-keep] {
		delete(s.segments, name)
	}
}
