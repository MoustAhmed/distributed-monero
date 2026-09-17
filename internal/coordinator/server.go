package coordinator

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/MoustAhmed/distributed-monero/internal/protocol"
)

// maxEnrollBodyBytes bounds the size of an enrollment request body.
const maxEnrollBodyBytes = 64 * 1024

// Server exposes the coordinator's HTTPS enrollment endpoint. It handles
// authenticated worker enrollment only — no heartbeats, telemetry,
// scheduling, or workload endpoints exist here.
type Server struct {
	identity *Identity
	registry *Registry

	httpServer *http.Server
}

// NewServer builds a Server that authenticates enrollment requests against
// identity.JoinToken and records enrolled workers in registry.
func NewServer(identity *Identity, registry *Registry) *Server {
	s := &Server{identity: identity, registry: registry}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/enroll", s.handleEnroll)

	s.httpServer = &http.Server{
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{identity.TLSCert}},
	}
	return s
}

// Handler returns the server's HTTP handler, for use in tests that don't
// need a real TLS listener.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Serve accepts and handles TLS connections on ln until the server is shut
// down. It blocks until Serve fails or Shutdown is called.
func (s *Server) Serve(ln net.Listener) error {
	return s.httpServer.ServeTLS(ln, "", "")
}

// Shutdown gracefully stops the server, waiting for in-flight requests to
// finish or ctx to be done.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	token, ok := bearerToken(r)
	if !ok || !tokensEqual(token, s.identity.JoinToken) {
		writeError(w, http.StatusUnauthorized, "invalid join token")
		return
	}

	var req protocol.EnrollRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxEnrollBodyBytes))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	if req.InstallationID == "" {
		writeError(w, http.StatusBadRequest, "installation_id is required")
		return
	}

	record, _, err := s.registry.Enroll(req.InstallationID, req.Capabilities)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, protocol.EnrollResponse{
		ClusterID:    s.identity.ClusterID,
		WorkerID:     record.WorkerID,
		SessionToken: record.SessionToken,
		Status:       record.Status,
	})
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(h, prefix)
	if token == "" {
		return "", false
	}
	return token, true
}

func tokensEqual(a, b string) bool {
	// Constant-time comparison guards against timing side channels; the
	// length check first is safe because token lengths aren't secret.
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, protocol.ErrorResponse{Error: msg})
}
