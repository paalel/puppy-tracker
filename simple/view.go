package simple

import (
	"sort"
	"time"

	"puppy/config"
	"puppy/store"
)

func today() string { return store.RolloverDate() }

// Fixed tag lists for outings — the stored values are these exact strings.
var placeTags = []string{"Voldsløkka", "Bjølsenparken", "Akerselva", "Home", "Cafe", "Friend's house", "Other"}
var whatTags = []string{"Pee break", "Long walk", "Training", "Play", "Fetch", "Socialising", "Met dogs"}

type pageVM struct {
	Config   *config.Config
	Date     string
	IsToday  bool
	PrevDate string
	NextDate string
	Poop     *poopVM
	Sleep    *sleepVM
	Alone    *aloneVM
	Outings  *outingsVM
}

type poopVM struct {
	Order     int
	OOB       bool
	Poops     []Poop
	Count     int        // poop entries (excludes accidents)
	Accidents int        // accident entries
	Last      *time.Time // latest entry time, any kind
	IsToday   bool
}

type sleepVM struct {
	Order     int
	OOB       bool
	Running   *Nap
	Naps      []Nap // completed, chronological
	Count     int
	TotalMins int
	Woke      *time.Time // last completed nap's end
	IsToday   bool
}

type aloneVM struct {
	Order    int
	OOB      bool
	Running  *AloneSession
	Sessions []AloneSession // completed today, newest first
	Records  AloneRecords
	EditID   int // >0 → that earlier session is in edit mode
	TodayMin int // total alone minutes today
	IsToday  bool
	// Running-only progress target:
	TargetMin   int    // 0 → no record yet, hide progress
	TargetLabel string // "Calm record 2h 15m" / "Overall record 3h 40m"
}

type outingsVM struct {
	Order   int
	OOB     bool
	Outings []Outing
	Last    *Outing
	IsToday bool
}

type outingFormVM struct {
	ID      int
	At      time.Time
	Where   map[string]bool
	What    map[string]bool
	Editing bool
	Places  []string
	Whats   []string
}

// computeOrders assigns CSS order values: running sessions float to the top,
// earliest-started first; everything else keeps its default slot.
func computeOrders(nap *Nap, alone *AloneSession) map[string]int {
	orders := map[string]int{"poop": 10, "sleep": 11, "alone": 12, "outings": 13}
	type run struct {
		card  string
		start time.Time
	}
	var runs []run
	if nap != nil {
		runs = append(runs, run{"sleep", nap.Started})
	}
	if alone != nil {
		runs = append(runs, run{"alone", alone.Started})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].start.Before(runs[j].start) })
	for i, r := range runs {
		orders[r.card] = i + 1
	}
	return orders
}

func (h *Handler) buildPage(date string) (*pageVM, error) {
	cfg, err := config.Get(h.db)
	if err != nil {
		return nil, err
	}
	poop, err := h.buildPoop(date)
	if err != nil {
		return nil, err
	}
	sleep, err := h.buildSleep(date)
	if err != nil {
		return nil, err
	}
	alone, err := h.buildAlone(date, 0)
	if err != nil {
		return nil, err
	}
	outings, err := h.buildOutings(date)
	if err != nil {
		return nil, err
	}
	// Each builder computes its own order (so single-card swaps stay correct).
	return &pageVM{Config: cfg, Date: date, Poop: poop, Sleep: sleep, Alone: alone, Outings: outings}, nil
}

func (h *Handler) buildPoop(date string) (*poopVM, error) {
	poops, err := listPoops(h.db, date)
	if err != nil {
		return nil, err
	}
	vm := &poopVM{Poops: poops, Order: 10, IsToday: date == today()}
	for i := range poops {
		if poops[i].IsAccident() {
			vm.Accidents++
		} else {
			vm.Count++
		}
		t := poops[i].At
		vm.Last = &t
	}
	return vm, nil
}

func (h *Handler) buildSleep(date string) (*sleepVM, error) {
	running, err := runningNap(h.db)
	if err != nil {
		return nil, err
	}
	naps, err := listNaps(h.db, date)
	if err != nil {
		return nil, err
	}
	vm := &sleepVM{Order: 11, IsToday: date == today()}
	// The running nap is shown in its own UI, not the completed list.
	for _, n := range naps {
		if n.Running() {
			continue
		}
		vm.Naps = append(vm.Naps, n)
		vm.Count++
		vm.TotalMins += n.Minutes()
		end := *n.Ended
		vm.Woke = &end
	}
	if vm.IsToday {
		ra, err := runningAlone(h.db)
		if err != nil {
			return nil, err
		}
		vm.Order = computeOrders(running, ra)["sleep"]
	}
	if vm.IsToday {
		vm.Running = running
	}
	return vm, nil
}

func (h *Handler) buildAlone(date string, editID int) (*aloneVM, error) {
	running, err := runningAlone(h.db)
	if err != nil {
		return nil, err
	}
	all, err := aloneForDay(h.db, date)
	if err != nil {
		return nil, err
	}
	runID := 0
	if running != nil && date == today() {
		runID = running.ID
	}
	rec, err := records(h.db, runID)
	if err != nil {
		return nil, err
	}
	vm := &aloneVM{Order: 12, Records: rec, EditID: editID, IsToday: date == today()}
	for _, a := range all {
		if a.ID == runID {
			continue // running one is shown separately
		}
		vm.Sessions = append(vm.Sessions, a)
		vm.TodayMin += a.Minutes()
	}
	if vm.IsToday && running != nil {
		vm.Running = running
		vm.TargetMin, vm.TargetLabel = progressTarget(running, rec)
	}
	if vm.IsToday {
		rn, err := runningNap(h.db)
		if err != nil {
			return nil, err
		}
		vm.Order = computeOrders(rn, running)["alone"]
	}
	return vm, nil
}

// progressTarget picks which record the running session is chasing: the calm
// record while it still qualifies as calm, otherwise the overall record.
func progressTarget(a *AloneSession, rec AloneRecords) (int, string) {
	chasingCalm := (a.Behaviour == "" || a.Behaviour == "calm") && !a.Destroyed
	if chasingCalm && rec.Calm.Set() {
		return rec.Calm.Mins, "Calm record"
	}
	if rec.Overall.Set() {
		return rec.Overall.Mins, "Overall record"
	}
	return 0, ""
}

func (h *Handler) buildOutings(date string) (*outingsVM, error) {
	outings, err := listOutings(h.db, date)
	if err != nil {
		return nil, err
	}
	vm := &outingsVM{Order: 13, Outings: outings, IsToday: date == today()}
	if len(outings) > 0 {
		vm.Last = &outings[len(outings)-1]
	}
	return vm, nil
}

func newOutingForm(id int, at time.Time, where, what []string, editing bool) *outingFormVM {
	wset := map[string]bool{}
	for _, w := range where {
		wset[w] = true
	}
	tset := map[string]bool{}
	for _, t := range what {
		tset[t] = true
	}
	return &outingFormVM{ID: id, At: at, Where: wset, What: tset, Editing: editing, Places: placeTags, Whats: whatTags}
}

// recordHolder reports whether session id holds the calm and/or overall record.
func (vm *aloneVM) IsCalmRecord(id int) bool {
	return vm.Records.Calm.Set() && vm.Records.Calm.ID == id
}
func (vm *aloneVM) IsOverallRecord(id int) bool {
	return vm.Records.Overall.Set() && vm.Records.Overall.ID == id
}
