// Package httpapi implements the public HTTP API described in api/openapi.yaml.
// It may import domain, store (interfaces), geo and auth (interface + types) only — never ingest or normalize
// (see ARCHITECTURE.md). Wiring of concrete stores happens in cmd/api.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
	"github.com/herezi/herezi-api/internal/geo"
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
	spots    store.SpotReader
	geocoder geo.ReverseGeocoder // nil이면 /v1/geo/reverse는 501
	auth     auth.Verifier       // nil이면 /v1/me/*는 503 auth_not_configured
	saved    store.SavedStore    // nil이면 /v1/me/saved*는 503 auth_not_configured
	users    auth.UserDeleter    // nil이면 DELETE /v1/me는 503 auth_not_configured
	apple    auth.AppleRevoker   // nil이면 Apple 사용자의 DELETE /v1/me는 503 auth_not_configured
	notices  store.NoticeReader  // nil이면 /v1/notices*는 503 not_configured
	regions  *regionCache        // 지역 데이터 파일 메모리 캐시(regions.go)
	log      *slog.Logger
	now      func() time.Time
}

// Option configures a Server.
type Option func(*Server)

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// New creates a Server.
func New(spots store.SpotReader, log *slog.Logger, opts ...Option) *Server {
	s := &Server{spots: spots, regions: newRegionCache(), log: log, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Routes is the single route table. Add new endpoints here AND in api/openapi.yaml.
func (s *Server) Routes() []Route {
	return []Route{
		{http.MethodGet, "/healthz", s.healthz},
		// 지역 데이터 파일(좌표를 받지 않음, 이슈 #74). 앱은 이것으로 근처·지도를 기기에서 계산한다.
		{http.MethodGet, "/v1/regions/{region}/spots", s.regionSpots},
		// 폐기 예고(openapi deprecated): 좌표를 받는 조회. 앱 전환 후 제거(#74).
		{http.MethodGet, "/v1/spots", s.listSpots},
		{http.MethodGet, "/v1/spots/map", s.listSpotsInBBox}, // 리터럴 경로라 {id}보다 우선
		{http.MethodGet, "/v1/spots/{id}", s.getSpot},
		{http.MethodGet, "/v1/geo/reverse", s.reverseGeocode},
		// 로그인 사용자 전용(Authorization: Bearer <Supabase JWT>)
		{http.MethodGet, "/v1/me/saved", s.authed(s.listSaved)},
		{http.MethodPost, "/v1/me/saved:import", s.authed(s.importSaved)},
		{http.MethodPut, "/v1/me/saved/{spot_id}", s.authed(s.putSaved)},
		{http.MethodDelete, "/v1/me/saved/{spot_id}", s.authed(s.deleteSaved)},
		{http.MethodDelete, "/v1/me", s.authed(s.deleteMe)},
		// 공지사항(인증 없음, 웹 공지 페이지용 CORS는 noticeCORS, 이슈 #80)
		{http.MethodGet, "/v1/notices", s.listNotices},
		{http.MethodGet, "/v1/notices/{id}", s.getNotice},
	}
}

// Handler returns the root http.Handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.Routes() {
		mux.HandleFunc(r.Method+" "+r.Path, r.Handler)
	}
	return s.recoverer(s.logRequests(s.noticeCORS(mux)))
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
	codeNotConfigured    = "not_configured"
	codeUpstream         = "upstream_error"
	// /v1/me/*
	codeUnauthorized      = "unauthorized"
	codeAuthNotConfigured = "auth_not_configured"
	codeAuthUnavailable   = "auth_unavailable"
	codeSavedLimit        = "saved_limit_exceeded"
	codeAppleReauth       = "apple_reauth_required"
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
		// 원좌표는 남기지 않는다: 좌표 파라미터는 소수 둘째 자리로 줄인다(logQuery).
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "query", logQuery(r.URL.Query()),
			"status", rec.status, "dur", time.Since(start))
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
