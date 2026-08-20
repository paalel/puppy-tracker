package camera

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Handler struct {
	hub   *hub
	tmpl  *template.Template
	token string
	db    *sql.DB
}

func New(db *sql.DB, tmpl *template.Template) *Handler {
	return &Handler{
		hub:   newHub(),
		tmpl:  tmpl,
		token: os.Getenv("CAMERA_TOKEN"),
		db:    db,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /camera", h.handlePage)
	mux.HandleFunc("PUT /camera/{id}/hls/{file}", h.handleHLSPut)
	mux.HandleFunc("POST /camera/{id}/presence", h.handlePresenceUpdate)
	mux.HandleFunc("GET /api/camera/{id}/presence", h.handlePresenceGet)
	mux.HandleFunc("GET /api/camera/status", h.handleStatus)
}

func (h *Handler) validToken(s string) bool {
	if h.token == "" || s == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(s), []byte(h.token)) == 1
}

func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "camera-page", nil); err != nil {
		log.Printf("camera-page template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

// handleHLSPut receives HLS playlist and segments from the Pi.
func (h *Handler) handleHLSPut(w http.ResponseWriter, r *http.Request) {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !h.validToken(auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	file := r.PathValue("file")
	data, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	interval := h.hub.camera(id).put(file, data)
	if strings.HasSuffix(file, ".ts") {
		if interval > 0 {
			log.Printf("hls camera=%s seg=%s interval=%dms size=%dKB", id, file, interval.Milliseconds(), len(data)/1024)
		} else {
			log.Printf("hls camera=%s seg=%s size=%dKB (first)", id, file, len(data)/1024)
		}
	}
	w.WriteHeader(http.StatusCreated)
}

// handlePresenceUpdate receives puppy presence status from the Pi.
func (h *Handler) handlePresenceUpdate(w http.ResponseWriter, r *http.Request) {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !h.validToken(auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	var body struct {
		Present bool `json:"present"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h.hub.setPresence(id, body.Present)
	w.WriteHeader(http.StatusNoContent)
}

// handlePresenceGet returns the latest presence status as JSON.
func (h *Handler) handlePresenceGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s := h.hub.getPresence(id)
	stale := s.UpdatedAt.IsZero() || time.Since(s.UpdatedAt) > 30*time.Second
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"present": s.Present,
		"stale":   stale,
	})
}

// handleStatus returns health/status of all known cameras.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.hub.status())
}
