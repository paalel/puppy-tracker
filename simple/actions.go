package simple

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// toastVM is the out-of-band toast with an optional undo action.
type toastVM struct {
	Msg    string
	Token  string // "verb:id"; empty = no undo button
	Target string // card id the undo swaps, e.g. "#card-poop"
}

// respond writes the primary card for today plus, out of band, the other running
// card (whose order may have shifted) and the toast. The primary card is the
// hx-target swap; everything else is an hx-swap-oob swap.
func (h *Handler) respond(w http.ResponseWriter, primary string, toast *toastVM) {
	var buf bytes.Buffer
	if err := h.renderCard(&buf, primary, 0, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, other := range h.otherRunning(primary) {
		_ = h.renderCard(&buf, other, 0, true)
	}
	if toast != nil {
		_ = h.tmpl.ExecuteTemplate(&buf, "toast", toast)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

// otherRunning returns the running session card other than primary, so its order
// class can be refreshed out of band.
func (h *Handler) otherRunning(primary string) []string {
	switch primary {
	case "sleep":
		if a, _ := runningAlone(h.db); a != nil {
			return []string{"alone"}
		}
	case "alone":
		if n, _ := runningNap(h.db); n != nil {
			return []string{"sleep"}
		}
	}
	return nil
}

func (h *Handler) renderCard(buf *bytes.Buffer, name string, editID int, oob bool) error {
	date := today()
	switch name {
	case "poop":
		vm, err := h.buildPoop(date)
		if err != nil {
			return err
		}
		vm.OOB = oob
		return h.tmpl.ExecuteTemplate(buf, "card-poop", vm)
	case "sleep":
		vm, err := h.buildSleep(date)
		if err != nil {
			return err
		}
		vm.OOB = oob
		return h.tmpl.ExecuteTemplate(buf, "card-sleep", vm)
	case "alone":
		vm, err := h.buildAlone(date, editID)
		if err != nil {
			return err
		}
		vm.OOB = oob
		return h.tmpl.ExecuteTemplate(buf, "card-alone", vm)
	case "outings":
		vm, err := h.buildOutings(date)
		if err != nil {
			return err
		}
		vm.OOB = oob
		return h.tmpl.ExecuteTemplate(buf, "card-outings", vm)
	}
	return fmt.Errorf("unknown card %q", name)
}

func (h *Handler) fail(w http.ResponseWriter, what string, err error) {
	http.Error(w, what+": "+err.Error(), http.StatusInternalServerError)
}

// ── Poop ──────────────────────────────────────────────────────────────────

func (h *Handler) handlePoopAdd(w http.ResponseWriter, r *http.Request) {
	kind := r.FormValue("kind")
	id, err := addPoop(h.db, kind)
	if err != nil {
		h.fail(w, "add poop", err)
		return
	}
	label := "Poop"
	if kind == "accident" {
		label = "Accident"
	}
	h.respond(w, "poop", &toastVM{
		Msg:    fmt.Sprintf("%s logged · %s", label, clockNow()),
		Token:  "del-poop:" + strconv.Itoa(id),
		Target: "#card-poop",
	})
}

func (h *Handler) handlePoopDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	label := "Poop"
	if poopKind(h.db, id) == "accident" {
		label = "Accident"
	}
	if err := softDelete(h.db, "poops", id); err != nil {
		h.fail(w, "delete poop", err)
		return
	}
	h.respond(w, "poop", &toastVM{
		Msg:    label + " removed",
		Token:  "undel-poop:" + strconv.Itoa(id),
		Target: "#card-poop",
	})
}

// ── Sleep ─────────────────────────────────────────────────────────────────

func (h *Handler) handleNapStart(w http.ResponseWriter, r *http.Request) {
	if _, err := startNap(h.db); err != nil {
		h.fail(w, "start nap", err)
		return
	}
	h.respond(w, "sleep", nil)
}

func (h *Handler) handleNapStop(w http.ResponseWriter, r *http.Request) {
	id, mins, err := stopNap(h.db)
	if err != nil {
		h.fail(w, "stop nap", err)
		return
	}
	if id == 0 { // nothing was running
		h.respond(w, "sleep", nil)
		return
	}
	h.respond(w, "sleep", &toastVM{
		Msg:    "Nap logged · " + durStr(mins),
		Token:  "resume-nap:" + strconv.Itoa(id),
		Target: "#card-sleep",
	})
}

func (h *Handler) handleNapDiscard(w http.ResponseWriter, r *http.Request) {
	n, err := runningNap(h.db)
	if err != nil {
		h.fail(w, "discard nap", err)
		return
	}
	if n == nil {
		h.respond(w, "sleep", nil)
		return
	}
	if err := softDelete(h.db, "naps", n.ID); err != nil {
		h.fail(w, "discard nap", err)
		return
	}
	h.respond(w, "sleep", &toastVM{
		Msg:    "Timer discarded",
		Token:  "undel-nap:" + strconv.Itoa(n.ID),
		Target: "#card-sleep",
	})
}

func (h *Handler) handleNapDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := softDelete(h.db, "naps", id); err != nil {
		h.fail(w, "delete nap", err)
		return
	}
	h.respond(w, "sleep", &toastVM{
		Msg:    "Nap removed",
		Token:  "undel-nap:" + strconv.Itoa(id),
		Target: "#card-sleep",
	})
}

// ── Home alone ──────────────────────────────────────────────────────────────

func (h *Handler) handleAloneStart(w http.ResponseWriter, r *http.Request) {
	if _, err := startAlone(h.db); err != nil {
		h.fail(w, "start alone", err)
		return
	}
	h.respond(w, "alone", nil)
}

func (h *Handler) handleAloneStop(w http.ResponseWriter, r *http.Request) {
	id, mins, err := stopAlone(h.db)
	if err != nil {
		h.fail(w, "stop alone", err)
		return
	}
	if id == 0 {
		h.respond(w, "alone", nil)
		return
	}
	h.respond(w, "alone", &toastVM{
		Msg:    "Home alone logged · " + durStr(mins),
		Token:  "resume-alone:" + strconv.Itoa(id),
		Target: "#card-alone",
	})
}

func (h *Handler) handleAloneDiscard(w http.ResponseWriter, r *http.Request) {
	a, err := runningAlone(h.db)
	if err != nil {
		h.fail(w, "discard alone", err)
		return
	}
	if a == nil {
		h.respond(w, "alone", nil)
		return
	}
	if err := deleteAlone(h.db, a.ID); err != nil {
		h.fail(w, "discard alone", err)
		return
	}
	h.respond(w, "alone", &toastVM{
		Msg:    "Home alone discarded",
		Token:  "undel-alone:" + strconv.Itoa(a.ID),
		Target: "#card-alone",
	})
}

func (h *Handler) handleAlonePatch(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch {
	case r.Form.Has("where"):
		_ = setAloneChoice(h.db, id, "alone_location", r.FormValue("where"))
	case r.Form.Has("sleep"):
		_ = setAloneChoice(h.db, id, "alone_slept", r.FormValue("sleep"))
	case r.Form.Has("behaviour"):
		_ = setAloneChoice(h.db, id, "alone_behaviour", r.FormValue("behaviour"))
	case r.Form.Has("destroyed"):
		_ = toggleAloneDestroyed(h.db, id)
	case r.Form.Has("note"):
		_ = setAloneNote(h.db, id, r.FormValue("note"))
		w.WriteHeader(http.StatusNoContent) // hx-swap="none"; nothing to re-render
		return
	default:
		http.Error(w, "no field", http.StatusBadRequest)
		return
	}
	h.respond(w, "alone", nil)
}

func (h *Handler) handleAloneDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := deleteAlone(h.db, id); err != nil {
		h.fail(w, "delete alone", err)
		return
	}
	h.respond(w, "alone", &toastVM{
		Msg:    "Home alone removed",
		Token:  "undel-alone:" + strconv.Itoa(id),
		Target: "#card-alone",
	})
}

func (h *Handler) handleAloneCard(w http.ResponseWriter, r *http.Request) {
	editID, _ := strconv.Atoi(r.URL.Query().Get("edit"))
	var buf bytes.Buffer
	if err := h.renderCard(&buf, "alone", editID, false); err != nil {
		h.fail(w, "alone card", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

// ── Outings ─────────────────────────────────────────────────────────────────

func (h *Handler) handleOutingNew(w http.ResponseWriter, r *http.Request) {
	h.writeForm(w, newOutingForm(0, time.Now(), nil, nil, false))
}

func (h *Handler) handleOutingEdit(w http.ResponseWriter, r *http.Request) {
	o, err := outingByID(h.db, pathID(r))
	if err != nil || o == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.writeForm(w, newOutingForm(o.ID, o.At, o.Where, o.What, true))
}

func (h *Handler) writeForm(w http.ResponseWriter, vm *outingFormVM) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "outing-form", vm); err != nil {
		h.fail(w, "outing form", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

func (h *Handler) handleOutingCreate(w http.ResponseWriter, r *http.Request) {
	where, what := outingForm(r)
	if len(where)+len(what) == 0 {
		http.Error(w, "pick at least one", http.StatusBadRequest)
		return
	}
	if _, err := createOuting(h.db, where, what); err != nil {
		h.fail(w, "create outing", err)
		return
	}
	h.respond(w, "outings", nil)
}

func (h *Handler) handleOutingUpdate(w http.ResponseWriter, r *http.Request) {
	where, what := outingForm(r)
	if len(where)+len(what) == 0 {
		http.Error(w, "pick at least one", http.StatusBadRequest)
		return
	}
	if err := updateOuting(h.db, pathID(r), where, what); err != nil {
		h.fail(w, "update outing", err)
		return
	}
	h.respond(w, "outings", nil)
}

func (h *Handler) handleOutingDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	o, _ := outingByID(h.db, id)
	when := ""
	if o != nil {
		when = " " + clockStr(o.At)
	}
	if err := softDelete(h.db, "outings", id); err != nil {
		h.fail(w, "delete outing", err)
		return
	}
	h.respond(w, "outings", &toastVM{
		Msg:    "Outing" + when + " removed",
		Token:  "undel-outing:" + strconv.Itoa(id),
		Target: "#card-outings",
	})
}

func (h *Handler) handleOutingsCard(w http.ResponseWriter, r *http.Request) {
	h.respond(w, "outings", nil)
}

// ── Undo ────────────────────────────────────────────────────────────────────

func (h *Handler) handleUndo(w http.ResponseWriter, r *http.Request) {
	verb, id := parseToken(r.PathValue("token"))
	var card string
	switch verb {
	case "del-poop":
		_ = softDelete(h.db, "poops", id)
		card = "poop"
	case "undel-poop":
		_ = restore(h.db, "poops", id)
		card = "poop"
	case "resume-nap":
		_ = resumeNap(h.db, id)
		card = "sleep"
	case "undel-nap":
		_ = restore(h.db, "naps", id)
		card = "sleep"
	case "resume-alone":
		_ = resumeAlone(h.db, id)
		card = "alone"
	case "undel-alone":
		_ = restoreAlone(h.db, id)
		card = "alone"
	case "undel-outing":
		_ = restore(h.db, "outings", id)
		card = "outings"
	default:
		http.Error(w, "bad token", http.StatusBadRequest)
		return
	}
	h.respond(w, card, nil)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func pathID(r *http.Request) int {
	id, _ := strconv.Atoi(r.PathValue("id"))
	return id
}

func parseToken(token string) (string, int) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", 0
	}
	id, _ := strconv.Atoi(parts[1])
	return parts[0], id
}

// outingForm reads the multi-select where/what checkboxes, keeping only known
// tags (rejecting anything not in the fixed lists).
func outingForm(r *http.Request) (where, what []string) {
	_ = r.ParseForm()
	return filterTags(r.Form["where"], placeTags), filterTags(r.Form["what"], whatTags)
}

func filterTags(got, allowed []string) []string {
	set := map[string]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	var out []string
	for _, g := range got {
		if set[g] {
			out = append(out, g)
		}
	}
	return out
}

func durStr(m int) string {
	if m <= 0 {
		return "<1m"
	}
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

func clockStr(t time.Time) string { return t.Format("15:04") }
func clockNow() string            { return time.Now().Format("15:04") }
