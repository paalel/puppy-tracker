package sessions

import (
	"math"
	"testing"
)

// syntheticTrainData builds rows with a clear urgency signal:
// poop rate is low for < 4h since last poop and high for > 10h.
func syntheticTrainData() []trainRow {
	pairs := [][2]float64{
		{1, 0}, {1.5, 0}, {2, 0}, {2.5, 0}, {3, 0}, {3.5, 0},
		{1, 0}, {2, 0}, {3, 0}, {3.5, 0}, {2, 0}, {1, 0},
		{6, 0}, {7, 0}, {6, 1}, {7, 0}, {6, 0}, {7, 1},
		{6, 0}, {7, 0}, {6, 0}, {7, 1}, {6, 0}, {7, 0},
		{11, 1}, {12, 1}, {13, 0}, {14, 1}, {11, 1}, {12, 0},
		{11, 1}, {12, 1}, {13, 1}, {14, 0}, {11, 1}, {12, 1},
		{15, 1}, {16, 1}, {15, 0}, {16, 1}, {15, 1}, {16, 1},
	}
	data := make([]trainRow, len(pairs))
	for i, p := range pairs {
		data[i] = trainRow{localHour: 9, hoursSincePoop: p[0], poop: p[1] == 1}
	}
	return data
}

func TestFitLogisticReturnsValidCoefficients(t *testing.T) {
	beta, cov, err := fitLogistic(syntheticTrainData())
	if err != nil {
		t.Fatalf("fitLogistic: %v", err)
	}
	if len(beta) != numFeatures {
		t.Fatalf("want %d coefficients, got %d", numFeatures, len(beta))
	}
	for j, b := range beta {
		if math.IsNaN(b) || math.IsInf(b, 0) {
			t.Errorf("beta[%d] = %v", j, b)
		}
	}
	r, c := cov.Dims()
	if r != numFeatures || c != numFeatures {
		t.Fatalf("cov dims %d×%d, want %d×%d", r, c, numFeatures, numFeatures)
	}
	for j := range numFeatures {
		for k := range numFeatures {
			if v := cov.At(j, k); math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("cov[%d][%d] = %v", j, k, v)
			}
		}
	}

	// hours_since_poop coefficient (index 5) must be positive given the signal above
	if beta[5] <= 0 {
		t.Errorf("hours_since_poop coefficient = %f, want > 0", beta[5])
	}
}

func TestPredictOutputsAreValid(t *testing.T) {
	beta, cov, err := fitLogistic(syntheticTrainData())
	if err != nil {
		t.Fatalf("fitLogistic: %v", err)
	}
	pred := &PoopPredictor{beta: beta, covBeta: cov}

	cases := []struct{ utcHour int; hours float64 }{
		{7, 1},
		{9, 4},
		{9, 12},
		{19, 8},
		{0, 16},
		{23, 0.5},
	}
	for _, tc := range cases {
		mid, lo, hi := pred.Predict(tc.utcHour, tc.hours)
		if math.IsNaN(mid) || math.IsNaN(lo) || math.IsNaN(hi) {
			t.Errorf("Predict(%d, %.1f) produced NaN: mid=%v lo=%v hi=%v", tc.utcHour, tc.hours, mid, lo, hi)
			continue
		}
		if mid < 0 || mid > 1 || lo < 0 || hi > 1 {
			t.Errorf("Predict(%d, %.1f) out of [0,1]: mid=%f lo=%f hi=%f", tc.utcHour, tc.hours, mid, lo, hi)
		}
		if lo > mid || mid > hi {
			t.Errorf("Predict(%d, %.1f) CI not ordered: lo=%f mid=%f hi=%f", tc.utcHour, tc.hours, lo, mid, hi)
		}
	}
}

func TestPredictUrgencyIsMonotone(t *testing.T) {
	beta, cov, err := fitLogistic(syntheticTrainData())
	if err != nil {
		t.Fatalf("fitLogistic: %v", err)
	}
	pred := &PoopPredictor{beta: beta, covBeta: cov}

	// At a fixed hour, P(poop) must increase with hours_since_poop
	prev, _, _ := pred.Predict(9, 1)
	for _, h := range []float64{3, 6, 10, 15} {
		mid, _, _ := pred.Predict(9, h)
		if mid < prev {
			t.Errorf("P(poop|%gh) = %f < P(poop|prev) = %f — urgency not monotone", h, mid, prev)
		}
		prev = mid
	}
}

func TestPredictNilBetaReturnsZero(t *testing.T) {
	pred := &PoopPredictor{} // unfitted
	mid, lo, hi := pred.Predict(9, 5)
	if mid != 0 || lo != 0 || hi != 0 {
		t.Errorf("unfitted predictor returned non-zero: mid=%f lo=%f hi=%f", mid, lo, hi)
	}
}
