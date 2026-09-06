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

	cases := []struct {
		utcHour int
		hours   float64
	}{
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

// TestFitLogisticRecencyWeighting checks that down-weighting old observations
// pulls the fit toward the recent signal. Old data says "high urgency, no poop";
// recent data says "high urgency, poop". With old data down-weighted, the
// predicted P(poop) at that point must rise toward the recent behaviour.
func TestFitLogisticRecencyWeighting(t *testing.T) {
	predictAt := func(oldWeight float64) float64 {
		var data []trainRow
		for i := 0; i < 12; i++ { // low-urgency baseline, no poop
			data = append(data, trainRow{localHour: 9, hoursSincePoop: 2, poop: false, weight: 1})
		}
		for i := 0; i < 10; i++ { // old: high urgency, did NOT poop
			data = append(data, trainRow{localHour: 9, hoursSincePoop: 12, poop: false, weight: oldWeight})
		}
		for i := 0; i < 10; i++ { // recent: high urgency, DID poop
			data = append(data, trainRow{localHour: 9, hoursSincePoop: 12, poop: true, weight: 1})
		}
		beta, cov, err := fitLogistic(data)
		if err != nil {
			t.Fatal(err)
		}
		mid, _, _ := (&PoopPredictor{beta: beta, covBeta: cov}).Predict(9, 12)
		return mid
	}

	equalWeight := predictAt(1.0)    // old counts fully
	recencyWeight := predictAt(0.05) // old down-weighted
	if recencyWeight <= equalWeight {
		t.Errorf("recency-weighted P(poop) = %.3f should exceed equal-weight P = %.3f",
			recencyWeight, equalWeight)
	}
}

func TestPredictNilBetaReturnsZero(t *testing.T) {
	pred := &PoopPredictor{} // unfitted
	mid, lo, hi := pred.Predict(9, 5)
	if mid != 0 || lo != 0 || hi != 0 {
		t.Errorf("unfitted predictor returned non-zero: mid=%f lo=%f hi=%f", mid, lo, hi)
	}
}
