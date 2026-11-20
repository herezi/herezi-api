// Package httpapi implements the public HTTP API described in api/openapi.yaml.
// It may import domain and store (interfaces) only — never ingest or normalize
// (see ARCHITECTURE.md). Wiring of concrete stores happens in cmd/api.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/herezi/herezi-api/internal/store"
)

// Route is one registered endpoint. Method+Path must match a path/operation in
// api/openapi.yaml; TestRoutesMatchOpenAPI enforces it.
type Route struct {
	Method  string
	Path    string // net/http ServeMux pattern path, e.g. /v1/spots/{id}
	Handler http.HandlerFunc
}

// Server holds handler dependencies.
type Server struct {
	spots store.SpotReader
	log   *slog.Logger
	now   func() time.Time
}

// Option configures a Server.
type Option func(*Server)

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// New creates a Server.
func New(spots store.SpotReader, log *slog.Logger, opts ...Option) *Server {
	s := &Server{spots: spots, log: log, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Routes is the single route table. Add new endpoints here AND in api/openapi.yaml.
func (s *Server) Routes() []Route {
	return []Route{
		{http.MethodGet, "/healthz", s.healthz},
		{http.MethodGet, "/v1/spots", s.listSpots},
		{http.MethodGet, "/v1/spots/{id}", s.getSpot},
	}
}

// Handler returns the root http.Handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.Routes() {
		mux.HandleFunc(r.Method+" "+r.Path, r.Handler)
	}
	return s.recoverer(s.logRequests(mux))
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// errorBody is components.schemas.Error.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes returned in Error.code.
const (
	codeInvalidParameter = "invalid_parameter"
	codeNotFound         = "not_found"
	codeInternal         = "internal"
)

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Code: code, Message: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "err", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
