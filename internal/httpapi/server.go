// Package httpapi implements the public HTTP API described in api/openapi.yaml.
// It may import domain, store (interfaces) and auth (interface + types) only — never ingest or normalize
// (see ARCHITECTURE.md). Wiring of concrete stores happens in cmd/api.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
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
	spots   store.SpotReader
	auth    auth.Verifier      // nil이면 /v1/me/*는 503 auth_not_configured
	saved   store.SavedStore   // nil이면 /v1/me/saved*는 503 auth_not_configured
	users   auth.UserDeleter   // nil이면 DELETE /v1/me는 503 auth_not_configured
	apple   auth.AppleRevoker  // nil이면 Apple 사용자의 DELETE /v1/me는 503 auth_not_configured
	notices store.NoticeReader // nil이면 /v1/notices*는 503 not_configured
	regions *regionCache       // 지역 데이터 파일 메모리 캐시(regions.go)
	log     *slog.Logger
	now     func() time.Time
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
		{http.MethodGet, "/v1/spots/{id}", s.getSpot},
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

// GonePaths are endpoints removed in #74 (좌표를 받던 조회). openapi에는 없고, 옛 앱 빌드
// (TestFlight 1–2, Play ≤4)가 조용히 404를 받는 대신 분명히 실패하도록 410 Gone + Error JSON을 돌려준다.
// 쿼리(좌표 포함)는 읽지 않는다. 접속 로그의 좌표 축약(logredact.go)은 방어용으로 남긴다.
var GonePaths = []string{"/v1/spots", "/v1/spots/map", "/v1/geo/reverse"}

// Handler returns the root http.Handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.Routes() {
		mux.HandleFunc(r.Method+" "+r.Path, r.Handler)
	}
	for _, p := range GonePaths {
		// "/v1/spots/map"은 리터럴 경로라 "/v1/spots/{id}"보다 우선한다.
		mux.HandleFunc(http.MethodGet+" "+p, gone)
	}
	return s.recoverer(s.logRequests(s.noticeCORS(mux)))
}

// gone answers removed endpoints (GonePaths) with 410.
func gone(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusGone, codeGone,
		"this endpoint was removed (issue #74): the app no longer sends location; update the app (it uses GET /v1/regions/{region}/spots)")
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
	codeGone             = "gone" // GonePaths(#74에서 제거한 좌표 엔드포인트)
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
