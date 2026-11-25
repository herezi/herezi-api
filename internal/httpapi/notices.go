package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// 공지사항: GET /v1/notices, GET /v1/notices/{id} (이슈 #80).
//
// 웹 공지 페이지(https://herezi.github.io/notices/)가 브라우저에서 이 API를 부른다. 그래서 공지 경로에만
// CORS를 연다(noticeCORS). 작성은 Supabase Table Editor에서 하고 API는 읽기만 한다(docs/ops/notices.md).

const (
	noticePathPrefix   = "/v1/notices"
	noticeCacheControl = "public, max-age=60"
	// noticeCORSMaxAge: 브라우저가 사전 요청(OPTIONS) 결과를 재사용하는 시간(초).
	noticeCORSMaxAge = "600"
)

// WithNotices sets the notice store. nil이면 /v1/notices*는 503 not_configured.
func WithNotices(n store.NoticeReader) Option { return func(s *Server) { s.notices = n } }

// noticeSummaryJSON is components.schemas.NoticeSummary (목록 항목, 본문 없음).
type noticeSummaryJSON struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
}

// noticeListJSON is components.schemas.NoticeList.
type noticeListJSON struct {
	Notices []noticeSummaryJSON `json:"notices"`
}

// noticeJSON is components.schemas.Notice.
type noticeJSON struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

// 시각은 KST 오프셋(+09:00)으로 쓴다(웹 페이지가 그대로 날짜를 보여 줄 수 있게).
func kst(t time.Time) time.Time { return t.In(domain.Seoul) }

func (s *Server) listNotices(w http.ResponseWriter, r *http.Request) {
	if s.notices == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotConfigured, "notices not configured")
		return
	}
	ns, err := s.notices.Notices(r.Context(), s.now(), store.MaxNotices)
	if err != nil {
		s.log.Error("list notices", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	out := noticeListJSON{Notices: make([]noticeSummaryJSON, 0, len(ns))}
	for _, n := range ns {
		out.Notices = append(out.Notices, noticeSummaryJSON{ID: n.ID, Title: n.Title, PublishedAt: kst(n.PublishedAt)})
	}
	w.Header().Set("Cache-Control", noticeCacheControl)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getNotice(w http.ResponseWriter, r *http.Request) {
	if s.notices == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotConfigured, "notices not configured")
		return
	}
	// 숫자가 아닌 id는 있을 수 없는 공지이므로 404로 본다.
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "notice not found")
		return
	}
	n, err := s.notices.NoticeByID(r.Context(), id, s.now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "notice not found")
		return
	}
	if err != nil {
		s.log.Error("get notice", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	w.Header().Set("Cache-Control", noticeCacheControl)
	writeJSON(w, http.StatusOK, noticeJSON{ID: n.ID, Title: n.Title, Body: n.Body, PublishedAt: kst(n.PublishedAt)})
}

// localhostOrigin: 로컬 개발 서버(http://localhost, http://localhost:<port>).
var localhostOrigin = regexp.MustCompile(`^http://localhost(:[0-9]{1,5})?$`)

// noticeOriginAllowed reports whether a browser Origin may read /v1/notices*.
func noticeOriginAllowed(origin string) bool {
	return origin == "https://herezi.github.io" || localhostOrigin.MatchString(origin)
}

// noticeCORS는 공지 경로(/v1/notices, /v1/notices/...)에만 CORS를 붙인다. 다른 경로는 그대로 넘긴다.
//   - 허용 출처: https://herezi.github.io, http://localhost:*  (요청 Origin을 그대로 돌려준다)
//   - 허용 메서드: GET, OPTIONS. 사전 요청(OPTIONS)은 여기서 204로 끝낸다(라우트 표에는 GET만 있다).
//   - 응답이 Origin에 따라 달라지므로 Vary: Origin을 붙인다(Cache-Control: public이라 공유 캐시 대비).
func (s *Server) noticeCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != noticePathPrefix && !strings.HasPrefix(p, noticePathPrefix+"/") {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		allowed := origin != "" && noticeOriginAllowed(origin)
		if allowed {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions {
			h.Set("Allow", "GET, HEAD, OPTIONS")
			if allowed {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Accept, Content-Type")
				h.Set("Access-Control-Max-Age", noticeCORSMaxAge)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
