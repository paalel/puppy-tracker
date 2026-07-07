package sessions

import (
	"database/sql"
	"math"
	"sync"

	"gonum.org/v1/gonum/stat/distuv"
)

// PoopPredictor estimates poop likelihood per routine session using a Bayesian
// model combining session-level base rates with elapsed-session urgency.
// Posteriors use Beta(k+1, n-k+1) — Laplace-smoothed from observed (k, n).
// Refreshed in-memory on each state transition; reads never block.
type PoopPredictor struct {
	mu           sync.RWMutex
	sessionRates map[int]distuv.Beta // routine session position → Beta posterior
	urgencyRates []distuv.Beta       // index i = sessions_since_poop of (i+1)
	overallMean  float64
}

func newPoopPredictor(db *sql.DB) *PoopPredictor {
	p := &PoopPredictor{}
	_ = p.Refresh(db)
	return p
}

func (p *PoopPredictor) Refresh(db *sql.DB) error {
	sessionData, err := loadSessionRates(db)
	if err != nil {
		return err
	}
	urgencyData, err := loadUrgencyRates(db)
	if err != nil {
		return err
	}

	var totalK, totalN int
	for _, kn := range sessionData {
		totalK += kn[0]
		totalN += kn[1]
	}

	sessionRates := make(map[int]distuv.Beta, len(sessionData))
	for pos, kn := range sessionData {
		sessionRates[pos] = distuv.Beta{
			Alpha: float64(kn[0]) + 1,
			Beta:  float64(kn[1]-kn[0]) + 1,
		}
	}

	urgencyRates := make([]distuv.Beta, len(urgencyData))
	for i, kn := range urgencyData {
		urgencyRates[i] = distuv.Beta{
			Alpha: float64(kn[0]) + 1,
			Beta:  float64(kn[1]-kn[0]) + 1,
		}
	}

	overallMean := float64(totalK+1) / float64(totalN+2)

	p.mu.Lock()
	p.sessionRates = sessionRates
	p.urgencyRates = urgencyRates
	p.overallMean = overallMean
	p.mu.Unlock()
	return nil
}

// Score returns estimated P(poop | session position, sessions since last poop)
// via naive Bayes: P(poop|pos) × P(poop|urgency) / P(poop_overall), clamped to [0, 1].
func (p *PoopPredictor) Score(position, sessionsSincePoop int) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.overallMean == 0 || len(p.sessionRates) == 0 {
		return 0
	}

	sessionBeta, ok := p.sessionRates[position]
	if !ok {
		return 0
	}

	urgencyMean := p.overallMean
	if len(p.urgencyRates) > 0 {
		idx := sessionsSincePoop - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(p.urgencyRates) {
			idx = len(p.urgencyRates) - 1
		}
		urgencyMean = p.urgencyRates[idx].Mean()
	}

	return math.Min(sessionBeta.Mean()*urgencyMean/p.overallMean, 1.0)
}
