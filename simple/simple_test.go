package simple

import (
	"testing"
	"time"
)

func TestComputeOrders(t *testing.T) {
	early := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	late := time.Date(2026, 10, 4, 11, 0, 0, 0, time.UTC)

	// Nothing running → default slots.
	o := computeOrders(nil, nil)
	if o["poop"] != 10 || o["sleep"] != 11 || o["alone"] != 12 {
		t.Errorf("default orders wrong: %v", o)
	}

	// Only a nap running → it pins to the top.
	o = computeOrders(&Nap{Started: early}, nil)
	if o["sleep"] != 1 || o["alone"] != 12 {
		t.Errorf("single running nap: sleep=%d alone=%d", o["sleep"], o["alone"])
	}

	// Both running, nap earlier → nap first, alone second.
	o = computeOrders(&Nap{Started: early}, &AloneSession{Started: late})
	if o["sleep"] != 1 || o["alone"] != 2 {
		t.Errorf("nap earlier: sleep=%d alone=%d", o["sleep"], o["alone"])
	}

	// Both running, alone earlier → alone first.
	o = computeOrders(&Nap{Started: late}, &AloneSession{Started: early})
	if o["alone"] != 1 || o["sleep"] != 2 {
		t.Errorf("alone earlier: alone=%d sleep=%d", o["alone"], o["sleep"])
	}
}

func TestDurStr(t *testing.T) {
	for in, want := range map[int]string{0: "<1m", -5: "<1m", 40: "40m", 90: "1h 30m", 120: "2h"} {
		if got := durStr(in); got != want {
			t.Errorf("durStr(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAloneIsCalm(t *testing.T) {
	if !(AloneSession{Behaviour: "calm"}).IsCalm() {
		t.Error("calm + not destroyed should be calm")
	}
	if (AloneSession{Behaviour: "calm", Destroyed: true}).IsCalm() {
		t.Error("destroyed should not count as calm")
	}
	if (AloneSession{Behaviour: "stressed"}).IsCalm() {
		t.Error("stressed should not be calm")
	}
}
