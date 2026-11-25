package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

type noticeSummaryBody struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
}

type noticeListBody struct {
	Notices []noticeSummaryBody `json:"notices"`
}

type noticeBody struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

// newNoticeServer: storetest.NoticeFixtures를 넣은 메모리 저장소 + 고정 시각(storetest.Now).
func newNoticeServer(t *testing.T) (*httptest.Server, map[string]int64) {
	t.Helper()
	st := memory.New()
	ids := map[string]int64{}
	for _, n := range storetest.NoticeFixtures() {
		id, err := st.InsertNotice(context.Background(), n)
		if err != nil {
			t.Fatal(err)
		}
		ids[n.Title] = id
	}
	return newNoticeServerWith(t, st), ids
}

func newNoticeServerWith(t *testing.T, st store.NoticeReader) *httptest.Server {
	t.Helper()
	opts := []httpapi.Option{httpapi.WithClock(func() time.Time { return storetest.Now })}
	if st != nil {
		opts = append(opts, httpapi.WithNotices(st))
	}
	srv := httptest.NewServer(httpapi.New(memory.New(), slog.New(slog.NewTextHandler(io.Discard, nil)), opts...).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func doNotice(t *testing.T, method, url string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, b
}

func TestListNotices(t *testing.T) {
	srv, ids := newNoticeServer(t)
	res, b := doNotice(t, http.MethodGet, srv.URL+"/v1/notices", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", got)
	}
	if strings.Contains(string(b), `"body"`) {
		t.Errorf("list must not include body: %s", b)
	}
	var body noticeListBody
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range body.Notices {
		titles = append(titles, n.Title)
		if n.ID != ids[n.Title] {
			t.Errorf("%s: id %d, want %d", n.Title, n.ID, ids[n.Title])
		}
		if !strings.HasSuffix(n.PublishedAt, "+09:00") {
			t.Errorf("%s: published_at %q, want KST offset", n.Title, n.PublishedAt)
		}
	}
	// 숨김·게시 전 제외, 게시 시각 최신순(같으면 ID 큰 것 먼저).
	want := []string{"exactly-now", "newest", "same-time-second", "same-time-first", "old"}
	if !slices.Equal(titles, want) {
		t.Fatalf("titles %v, want %v", titles, want)
	}
	if body.Notices[0].PublishedAt != "2026-11-18T15:00:00+09:00" {
		t.Errorf("published_at = %q", body.Notices[0].PublishedAt)
	}
}

func TestListNoticesEmptyIsArray(t *testing.T) {
	srv := newNoticeServerWith(t, memory.New())
	res, b := doNotice(t, http.MethodGet, srv.URL+"/v1/notices", nil)
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != `{"notices":[]}` {
		t.Fatalf("status %d body %s", res.StatusCode, b)
	}
}

func TestListNoticesLimit(t *testing.T) {
	st := memory.New()
	for i := range store.MaxNotices + 5 {
		_, _ = st.InsertNotice(context.Background(), noticeAt(fmt.Sprint(i), storetest.Now.Add(-time.Duration(i)*time.Minute)))
	}
	srv := newNoticeServerWith(t, st)
	_, b := doNotice(t, http.MethodGet, srv.URL+"/v1/notices", nil)
	var body noticeListBody
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Notices) != store.MaxNotices || body.Notices[0].Title != "0" {
		t.Fatalf("got %d notices (first %+v), want %d newest first", len(body.Notices), body.Notices[0], store.MaxNotices)
	}
}

func TestGetNotice(t *testing.T) {
	srv, ids := newNoticeServer(t)
	res, b := doNotice(t, http.MethodGet, fmt.Sprintf("%s/v1/notices/%d", srv.URL, ids["newest"]), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", got)
	}
	var n noticeBody
	if err := json.Unmarshal(b, &n); err != nil {
		t.Fatal(err)
	}
	if n.ID != ids["newest"] || n.Title != "newest" || n.Body != "# newest\n\n본문" || n.PublishedAt != "2026-11-18T14:00:00+09:00" {
		t.Fatalf("notice = %+v", n)
	}
}

func TestGetNoticeNotFound(t *testing.T) {
	srv, ids := newNoticeServer(t)
	for _, id := range []string{
		fmt.Sprint(ids["hidden"]), fmt.Sprint(ids["scheduled"]), "999999", "0", "-1", "abc", "1.5", "99999999999999999999",
	} {
		res, b := doNotice(t, http.MethodGet, srv.URL+"/v1/notices/"+id, nil)
		if res.StatusCode != http.StatusNotFound || !strings.Contains(string(b), `"not_found"`) {
			t.Errorf("id %s: status %d body %s", id, res.StatusCode, b)
		}
		if res.Header.Get("Cache-Control") != "" {
			t.Errorf("id %s: 404 must not be cached, Cache-Control %q", id, res.Header.Get("Cache-Control"))
		}
	}
}

func TestNoticesNotConfigured(t *testing.T) {
	srv := newNoticeServerWith(t, nil)
	for _, p := range []string{"/v1/notices", "/v1/notices/1"} {
		res, b := doNotice(t, http.MethodGet, srv.URL+p, nil)
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d body %s", p, res.StatusCode, b)
		}
	}
}

func TestNoticesCORS(t *testing.T) {
	srv, _ := newNoticeServer(t)
	for _, tc := range []struct {
		origin string
		allow  bool
	}{
		{"https://herezi.github.io", true},
		{"http://localhost:4000", true},
		{"http://localhost", true},
		{"https://evil.example", false},
		{"http://herezi.github.io", false},
		{"https://herezi.github.io.evil.example", false},
		{"http://localhost.evil.example", false},
		{"https://localhost:4000", false},
		{"", false},
	} {
		hdr := map[string]string{}
		if tc.origin != "" {
			hdr["Origin"] = tc.origin
		}
		for _, p := range []string{"/v1/notices", "/v1/notices/1"} {
			res, _ := doNotice(t, http.MethodGet, srv.URL+p, hdr)
			got := res.Header.Get("Access-Control-Allow-Origin")
			if (tc.allow && got != tc.origin) || (!tc.allow && got != "") {
				t.Errorf("GET %s origin %q: Allow-Origin %q", p, tc.origin, got)
			}
			if !slices.Contains(res.Header.Values("Vary"), "Origin") {
				t.Errorf("GET %s: Vary %v, want Origin", p, res.Header.Values("Vary"))
			}
		}

		// 사전 요청(OPTIONS): 라우트 표에는 GET만 있지만 미들웨어가 204로 끝낸다.
		hdr["Access-Control-Request-Method"] = "GET"
		res, _ := doNotice(t, http.MethodOptions, srv.URL+"/v1/notices", hdr)
		if res.StatusCode != http.StatusNoContent {
			t.Errorf("OPTIONS origin %q: status %d", tc.origin, res.StatusCode)
		}
		got := res.Header.Get("Access-Control-Allow-Origin")
		methods := res.Header.Get("Access-Control-Allow-Methods")
		if tc.allow && (got != tc.origin || methods != "GET, OPTIONS") {
			t.Errorf("OPTIONS origin %q: Allow-Origin %q Allow-Methods %q", tc.origin, got, methods)
		}
		if !tc.allow && (got != "" || methods != "") {
			t.Errorf("OPTIONS origin %q: Allow-Origin %q Allow-Methods %q, want none", tc.origin, got, methods)
		}
	}
}

// 다른 경로에는 CORS를 붙이지 않는다(공지 경로에만 연다).
func TestCORSOnlyOnNotices(t *testing.T) {
	srv, _ := newNoticeServer(t)
	hdr := map[string]string{"Origin": "https://herezi.github.io"}
	for _, p := range []string{"/healthz", "/v1/regions/seoul/spots", "/v1/noticesx"} {
		res, _ := doNotice(t, http.MethodGet, srv.URL+p, hdr)
		if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET %s: Allow-Origin %q, want none", p, got)
		}
	}
	hdr["Access-Control-Request-Method"] = "GET"
	res, _ := doNotice(t, http.MethodOptions, srv.URL+"/v1/regions/seoul/spots", hdr)
	if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("OPTIONS /v1/regions/seoul/spots: status %d Allow-Origin %q", res.StatusCode, res.Header.Get("Access-Control-Allow-Origin"))
	}
	// 공지 경로라도 쓰기 메서드는 열지 않는다.
	res, _ = doNotice(t, http.MethodPost, srv.URL+"/v1/notices", map[string]string{"Origin": "https://herezi.github.io"})
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /v1/notices: status %d", res.StatusCode)
	}
}

func noticeAt(title string, at time.Time) domain.Notice {
	return domain.Notice{Title: title, Body: "b", PublishedAt: at}
}
