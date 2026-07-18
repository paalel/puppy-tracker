package sessions

import (
	"database/sql"
	"math"
	"sync"

	"gonum.org/v1/gonum/mat"
)

const (
	numFeatures = 6   // intercept, sin_hour, cos_hour, sin2_hour, cos2_hour, log1p(hours_since_poop)
	l2Lambda    = 1.0 // L2 penalty on non-intercept coefficients
	irlsMaxIter = 50
	irlsTol     = 1e-8
)

// PoopPredictor fits logistic regression P(poop | hour_of_day, hours_since_poop)
// using IRLS with L2 regularisation. Refreshed in-memory on each state transition.
type PoopPredictor struct {
	mu      sync.RWMutex
	beta    []float64  // nil until enough training data is available
	covBeta *mat.Dense // Fisher information inverse — used for 80% CI
}

func newPoopPredictor(db *sql.DB) *PoopPredictor {
	p := &PoopPredictor{}
	_ = p.Refresh(db)
	return p
}

func (p *PoopPredictor) Refresh(db *sql.DB) error {
	data, err := loadTrainingData(db)
	if err != nil {
		return err
	}
	if len(data) < numFeatures+1 {
		return nil
	}
	beta, cov, err := fitLogistic(data)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.beta = beta
	p.covBeta = cov
	p.mu.Unlock()
	return nil
}

// Predict returns P(poop | localHour, hoursSincePoop) with 80% credible interval.
// localHour is the local clock hour (0–23) of the session's actual or planned wake time.
func (p *PoopPredictor) Predict(localHour int, hoursSincePoop float64) (mid, lo, hi float64) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.beta == nil {
		return 0, 0, 0
	}

	fv := featureVec(localHour, hoursSincePoop)

	var linPred float64
	for j, v := range fv {
		linPred += v * p.beta[j]
	}
	mid = sigmoid(linPred)

	// CI via delta method: Var(xᵀβ̂) = xᵀ Cov(β̂) x, then push through sigmoid
	var varLP float64
	for j := range numFeatures {
		for k := range numFeatures {
			varLP += fv[j] * p.covBeta.At(j, k) * fv[k]
		}
	}
	std := math.Sqrt(math.Max(varLP, 0))
	lo = sigmoid(linPred - 1.28*std)
	hi = sigmoid(linPred + 1.28*std)
	return
}

func featureVec(localHour int, hoursSincePoop float64) []float64 {
	h := float64(localHour)
	return []float64{
		1,
		math.Sin(2 * math.Pi * h / 24),
		math.Cos(2 * math.Pi * h / 24),
		math.Sin(4 * math.Pi * h / 24),
		math.Cos(4 * math.Pi * h / 24),
		// Shift by 6h: observed poop rate is flat ~12% for 0–8h (digestive transit),
		// then rises sharply. Subtracting 6 gives zero urgency during the refractory
		// period and lets the log grow only after digestion begins.
		math.Log1p(math.Max(0, hoursSincePoop-6)),
	}
}

func sigmoid(z float64) float64 {
	return 1 / (1 + math.Exp(-z))
}

// fitLogistic fits logistic regression by IRLS (Newton-Raphson) with L2 regularisation
// on non-intercept coefficients. Returns β̂ and the Fisher information inverse H⁻¹,
// which is the asymptotic covariance of β̂ used for confidence intervals.
func fitLogistic(data []trainRow) ([]float64, *mat.Dense, error) {
	n, p := len(data), numFeatures

	Xdata := make([]float64, n*p)
	y := make([]float64, n)
	for i, row := range data {
		fv := featureVec(row.localHour, row.hoursSincePoop)
		copy(Xdata[i*p:], fv)
		if row.poop {
			y[i] = 1
		}
	}
	X := mat.NewDense(n, p, Xdata)

	beta := make([]float64, p)
	var lastH *mat.Dense

	for range irlsMaxIter {
		// μ = sigmoid(Xβ)
		mu := make([]float64, n)
		for i := range n {
			var z float64
			for j := range p {
				z += X.At(i, j) * beta[j]
			}
			mu[i] = sigmoid(z)
		}

		// H = XᵀWX + λI (skip λ for intercept), g = Xᵀ(y−μ) − λβ
		H := mat.NewDense(p, p, nil)
		g := make([]float64, p)
		for i := range n {
			wi := math.Max(mu[i]*(1-mu[i]), 1e-10)
			resid := y[i] - mu[i]
			for j := range p {
				g[j] += X.At(i, j) * resid
				for k := range p {
					H.Set(j, k, H.At(j, k)+wi*X.At(i, j)*X.At(i, k))
				}
			}
		}
		for j := 1; j < p; j++ { // intercept (j=0) is unregularised
			H.Set(j, j, H.At(j, j)+l2Lambda)
			g[j] -= l2Lambda * beta[j]
		}
		lastH = H

		// Newton step: solve H·δ = g directly (more stable than inverting H each iteration)
		gMat := mat.NewDense(p, 1, g)
		var delta mat.Dense
		if err := delta.Solve(H, gMat); err != nil {
			return nil, nil, err
		}

		maxStep := 0.0
		for j := range p {
			dj := delta.At(j, 0)
			beta[j] += dj
			if d := math.Abs(dj); d > maxStep {
				maxStep = d
			}
		}
		if maxStep < irlsTol {
			break
		}
	}

	// Invert H once at convergence to get the covariance matrix for the delta method CI.
	var Hinv mat.Dense
	if err := Hinv.Inverse(lastH); err != nil {
		return nil, nil, err
	}
	return beta, &Hinv, nil
}
