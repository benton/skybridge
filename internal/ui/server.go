package ui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/mounts"
)

//go:embed static
var static embed.FS

// MountInfo is a profile as the panel's setup pane lists it.
type MountInfo struct {
	Name   string `json:"name"`
	Baud   int    `json:"baud"`
	Probe  bool   `json:"probe"`
	Covers string `json:"covers"`
	Notes  string `json:"notes,omitempty"`
}

// Handler serves the panel and its API for c. host is the address the
// server listens on; requests whose Host header names another machine are
// refused, which blocks DNS rebinding.
func Handler(c *Controller, host string) http.Handler {
	s := &server{c: c, listenHost: host}
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	files := http.FileServer(http.FS(sub))
	mux.Handle("GET /static/", http.StripPrefix("/static/", files))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, sub, "index.html")
	})
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/ports", s.ports)
	mux.HandleFunc("GET /api/mounts", s.mounts)
	mux.HandleFunc("POST /api/start", s.start)
	mux.HandleFunc("POST /api/stop", s.stop)
	mux.HandleFunc("POST /api/settings", s.settings)
	mux.HandleFunc("POST /api/probe", s.probe)
	return s.guard(mux)
}

type server struct {
	c          *Controller
	listenHost string
}

// guard refuses requests from other origins. A Host header must name this
// machine, and a mutating request must carry a JSON body, which a cross-site
// form cannot send and a cross-site fetch cannot send without a preflight
// this server never grants.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r.Host) {
			http.Error(w, "request for another host", http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				jsonError(w, http.StatusUnsupportedMediaType, "send application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) hostOK(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" || net.ParseIP(h) != nil {
		return true
	}
	lh, _, err := net.SplitHostPort(s.listenHost)
	if err != nil {
		lh = s.listenHost
	}
	return lh != "" && strings.EqualFold(h, lh)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *server) state(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.c.State())
}

func (s *server) ports(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	ports, err := s.c.Ports(ctx)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ports)
}

func (s *server) mounts(w http.ResponseWriter, _ *http.Request) {
	all := mounts.All()
	out := make([]MountInfo, 0, len(all))
	for _, p := range all {
		out = append(out, MountInfo{p.Name, p.Baud, p.Probe != nil, p.Covers, p.Notes})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) start(w http.ResponseWriter, _ *http.Request) {
	if err := s.c.Start(); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.c.State())
}

func (s *server) stop(w http.ResponseWriter, _ *http.Request) {
	s.c.Stop()
	writeJSON(w, http.StatusOK, s.c.State())
}

func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "settings are not valid JSON: "+err.Error())
		return
	}
	in.Port = strings.TrimSpace(in.Port)
	in.Mount = strings.TrimSpace(in.Mount)
	in.Listen = strings.TrimSpace(in.Listen)
	if in.Listen == "" {
		jsonError(w, http.StatusBadRequest, "listen address is required")
		return
	}
	if err := s.c.Apply(in); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.c.State())
}

func (s *server) probe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := s.c.Probe(ctx)
	if errors.Is(err, ErrRunning) {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	// A probe that found nothing is still an answer; the result carries the error.
	writeJSON(w, http.StatusOK, res)
}

// events streams the state and log as server-sent events. The first events
// replay the current state and the buffered log so a fresh page is complete.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := s.c.Subscribe()
	defer unsubscribe()

	send := func(name string, data []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	}
	stateJSON, _ := json.Marshal(s.c.State())
	send("state", stateJSON)
	logsJSON, _ := json.Marshal(s.c.Logs())
	send("logs", logsJSON)
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.c.Closing():
			// The server is shutting down; end the stream so Shutdown can finish.
			return
		case ev := <-ch:
			send(ev.Name, ev.Data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
