package training

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"puppy/store"
)

type Handler struct {
	db   *sql.DB
	tmpl *template.Template
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{db: db, tmpl: tmpl}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Today-card rep logging.
	mux.HandleFunc("POST /simple/training/skill/{skillID}/log", h.handleLog)
	mux.HandleFunc("DELETE /simple/training/rep/{id}", h.handleDeleteRep)
	mux.HandleFunc("POST /simple/training/undo/{token}", h.handleUndo)
	// Settings config fragment + skill CRUD.
	mux.HandleFunc("GET /api/training/skills", h.handleSkillsFrag)
	mux.HandleFunc("POST /api/training/skill", h.handleSkillCreate)
	mux.HandleFunc("POST /api/training/skill/{id}", h.handleSkillUpdate)
	mux.HandleFunc("POST /api/training/skill/{id}/delete", h.handleSkillDelete)
}

// ── Today card ──────────────────────────────────────────────────────────────

type skillVM struct {
	Skill
	Count int
	Met   bool
	Reps  []Rep
}

type cardVM struct {
	Order      int
	OOB        bool
	Skills     []skillVM
	TotalToday int
	DoneCount  int
	IsToday    bool
	Qualities  []string
}

// cardOrder is the training card's fixed slot in the simplified column — below
// the poop/sleep/alone cards, which can pin to the top while a timer runs. The
// training card itself never moves.
const cardOrder = 20

// BuildCard assembles the training card for a date. Exported so the simple page
// can embed it. Always rendered at its fixed order so re-renders don't reorder.
func (h *Handler) BuildCard(date string) (*cardVM, error) {
	skills, err := listSkills(h.db)
	if err != nil {
		return nil, err
	}
	vm := &cardVM{Order: cardOrder, IsToday: date == store.RolloverDate(), Qualities: Qualities}
	for _, s := range skills {
		count, err := progressCount(h.db, s, date)
		if err != nil {
			return nil, err
		}
		reps, err := repsForSkillDay(h.db, s.ID, date)
		if err != nil {
			return nil, err
		}
		vm.Skills = append(vm.Skills, skillVM{Skill: s, Count: count, Met: count >= s.Target, Reps: reps})
		vm.TotalToday += len(reps)
		if count >= s.Target {
			vm.DoneCount++
		}
	}
	return vm, nil
}

func (h *Handler) renderCard(w http.ResponseWriter, toast *toastVM) {
	vm, err := h.BuildCard(store.RolloverDate())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "card-training", vm); err != nil {
		log.Printf("card-training: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if toast != nil {
		_ = h.tmpl.ExecuteTemplate(&buf, "toast", toast)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

type toastVM struct {
	Msg     string
	Target  string
	UndoURL string
	Token   string // unused here; present so the shared toast template type-checks
}

func (h *Handler) handleLog(w http.ResponseWriter, r *http.Request) {
	skillID, _ := strconv.Atoi(r.PathValue("skillID"))
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	quality := r.FormValue("quality")
	id, err := addRep(h.db, skillID, quality)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderCard(w, &toastVM{
		Msg:     skillName(h.db, skillID) + " · " + quality,
		Target:  "#card-training",
		UndoURL: "/simple/training/undo/del-rep:" + strconv.Itoa(id),
	})
}

func (h *Handler) handleDeleteRep(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if err := deleteRep(h.db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderCard(w, &toastVM{
		Msg:     "Rep removed",
		Target:  "#card-training",
		UndoURL: "/simple/training/undo/undel-rep:" + strconv.Itoa(id),
	})
}

func (h *Handler) handleUndo(w http.ResponseWriter, r *http.Request) {
	verb, id := parseToken(r.PathValue("token"))
	var err error
	switch verb {
	case "del-rep":
		err = deleteRep(h.db, id)
	case "undel-rep":
		err = restoreRep(h.db, id)
	default:
		http.Error(w, "bad token", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderCard(w, nil)
}

func parseToken(token string) (string, int) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", 0
	}
	id, _ := strconv.Atoi(parts[1])
	return parts[0], id
}

// ── Settings config ───────────────────────────────────────────────────────

type sectionVM struct {
	Skills []Skill
}

func (h *Handler) renderSkillsFrag(w http.ResponseWriter) {
	skills, err := listSkills(h.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "training-skills", &sectionVM{Skills: skills}); err != nil {
		log.Printf("training-skills: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (h *Handler) handleSkillsFrag(w http.ResponseWriter, r *http.Request) { h.renderSkillsFrag(w) }

func (h *Handler) handleSkillCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderSkillsFrag(w) // ignore empty add
		return
	}
	target, _ := strconv.Atoi(r.FormValue("target"))
	if err := createSkill(h.db, name, strings.TrimSpace(r.FormValue("instruction")), target, r.FormValue("period")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderSkillsFrag(w)
}

func (h *Handler) handleSkillUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderSkillsFrag(w)
		return
	}
	target, _ := strconv.Atoi(r.FormValue("target"))
	if err := updateSkill(h.db, id, name, strings.TrimSpace(r.FormValue("instruction")), target, r.FormValue("period")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderSkillsFrag(w)
}

func (h *Handler) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if err := deleteSkill(h.db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderSkillsFrag(w)
}
