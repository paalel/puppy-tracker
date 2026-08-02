package camera

import (
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
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
}

func New(tmpl *template.Template) *Handler {
	return &Handler{
		hub:   newHub(),
		tmpl:  tmpl,
		token: os.Getenv("CAMERA_TOKEN"),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /camera", h.handlePage)
	mux.HandleFunc("POST /camera", h.handleLogin)
	mux.HandleFunc("GET /camera/stream", h.handleStream)
	mux.HandleFunc("PUT /camera/push", h.handlePush)
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
		Path:     "/camera",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   365 * 24 * 60 * 60,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	http.Redirect(w, r, "/camera", http.StatusSeeOther)
}

// handleStream serves a multipart MJPEG stream to the browser.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	if !h.authenticated(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.hub.online() {
		http.Error(w, "camera offline", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx/fly proxy buffering

	ch := h.hub.subscribe()
	defer h.hub.unsubscribe(ch)

	timeout := time.NewTicker(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case frame, ok := <-ch:
			if !ok {
				return
			}
			timeout.Reset(10 * time.Second)
			fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame))
			w.Write(frame)
			fmt.Fprint(w, "\r\n")
			flusher.Flush()
		case <-timeout.C:
			return // no frame for 10 s — close so browser onerror fires and retries
		case <-r.Context().Done():
			return
		}
	}
}

// handlePush receives length-prefixed JPEG frames from the Pi via HTTP PUT.
// The Pi authenticates with "Authorization: Bearer <token>".
func (h *Handler) handlePush(w http.ResponseWriter, r *http.Request) {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !h.validToken(auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	h.hub.piConnected.Add(1)
	log.Println("camera: Pi connected")
	defer func() {
		h.hub.piConnected.Add(-1)
		log.Println("camera: Pi disconnected")
	}()

	var sizeBuf [4]byte
	for {
		if _, err := io.ReadFull(r.Body, sizeBuf[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(sizeBuf[:])
		if n == 0 || n > 10<<20 { // sanity: 0 < frame ≤ 10 MB
			log.Printf("camera: unexpected frame size %d, closing", n)
			return
		}
		frame := make([]byte, n)
		if _, err := io.ReadFull(r.Body, frame); err != nil {
			return
		}
		h.hub.publish(frame)
	}
}
