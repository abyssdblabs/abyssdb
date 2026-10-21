package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/abyssdblabs/abyssdb/internal/engine"
)

// ServeHTTP implements MCP's streamable HTTP transport, answering every POST
// with a single JSON response (no SSE streams: results never take long enough
// to need one).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Reject cross-origin browser requests to prevent DNS rebinding attacks.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.sessions, r.Header.Get("Mcp-Session-Id"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	id := r.Header.Get("Mcp-Session-Id")
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*engine.Session{}
	}
	sess, ok := s.sessions[id]
	if !ok {
		if id != "" {
			s.mu.Unlock()
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		if !strings.Contains(string(body), `"initialize"`) {
			s.mu.Unlock()
			http.Error(w, "missing Mcp-Session-Id", http.StatusBadRequest)
			return
		}
		buf := make([]byte, 16)
		_, _ = rand.Read(buf)
		id = hex.EncodeToString(buf)
		sess = s.Engine.NewSession()
		sess.SetLoginUser("mcp-agent")
		s.sessions[id] = sess
	}
	s.mu.Unlock()

	w.Header().Set("Mcp-Session-Id", id)
	reply := s.HandleMessage(sess, body)
	if reply == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(reply)
}
