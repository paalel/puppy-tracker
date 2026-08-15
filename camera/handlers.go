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

const cookieName = "camera_auth"

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
	mux.HandleFunc("POST /camera", h.handleLogin)
	mux.HandleFunc("GET /camera/{id}/hls/{file}", h.handleHLSGet)
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

func (h *Handler) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return h.validToken(c.Value)
}

type loginData struct {
	Error bool
}

func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	var (
		name string
		data any
	)
	if h.authenticated(r) {
		name = "camera-page"
	} else {
		name = "camera-login"
		data = &loginData{Error: r.URL.Query().Get("error") == "1"}
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("%s template: %v", name, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.validToken(r.FormValue("token")) {
		http.Redirect(w, r, "/camera?error=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    h.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   365 * 24 * 60 * 60,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	http.Redirect(w, r, "/camera", http.StatusSeeOther)
}

// handleHLSPut receives HLS playlist and segments from the Pi via FFmpeg -method PUT.
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

// handleHLSGet serves HLS playlist and segments to the browser (cookie-authenticated).
func (h *Handler) handleHLSGet(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	file := r.PathValue("file")
	data, ok := h.hub.camera(id).get(file)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasSuffix(file, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Write(data)
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

// handleStatus returns health/status of all known cameras. No auth required
// (data is non-sensitive: online/offline, segment count, presence).
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.hub.status())
}
