package httpapi_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// 원좌표(소수 6자리)로 요청해도 로그에는 소수 둘째 자리만 남아야 한다.
func TestAccessLogCoarsensCoordinates(t *testing.T) {
	upstreamErr := &url.Error{Op: "Get", URL: "https://maps.example/gc?coords=126.977945,37.566312", Err: errors.New("timeout")}
	for _, tc := range []struct {
		name    string
		path    string
		params  url.Values
		geo     *fakeGeocoder
		want    []string
		forbids []string
	}{
		{
			name:    "spots",
			path:    "/v1/spots",
			params:  url.Values{"lat": {"37.566312"}, "lng": {"126.977945"}, "q": {"축제"}, "api_key": {"s3cr3t"}},
			want:    []string{"lat=37.57", "lng=126.98", "q=", "api_key=REDACTED"},
			forbids: []string{"s3cr3t"},
		},
		{
			name: "map",
			path: "/v1/spots/map",
			params: url.Values{"sw_lat": {"37.551234"}, "sw_lng": {"126.961234"}, "ne_lat": {"37.581234"},
				"ne_lng": {"126.991234"}, "limit": {"50"}},
			want: []string{"sw_lat=37.55", "sw_lng=126.96", "ne_lat=37.58", "ne_lng=126.99", "limit=50"},
		},
		{
			name:   "geo reverse",
			path:   "/v1/geo/reverse",
			params: url.Values{"lat": {"37.566312"}, "lng": {"126.977945"}},
			geo:    &fakeGeocoder{},
			want:   []string{"lat=37.57", "lng=126.98", "status=200"},
		},
		{
			name:   "geo reverse upstream error",
			path:   "/v1/geo/reverse",
			params: url.Values{"lat": {"37.566312"}, "lng": {"126.977945"}},
			geo:    &fakeGeocoder{err: upstreamErr},
			want:   []string{"lat=37.57", "status=502", "reverse geocode failed", "timeout"},
		},
		{
			name:   "invalid coordinate",
			path:   "/v1/spots",
			params: url.Values{"lat": {"37.566312abc"}, "lng": {"126.977945"}},
			want:   []string{"lat=REDACTED", "lng=126.98", "status=400"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf syncBuf
			log := slog.New(slog.NewTextHandler(&buf, nil))
			opts := []httpapi.Option{httpapi.WithClock(func() time.Time { return storetest.Now })}
			if tc.geo != nil {
				opts = append(opts, httpapi.WithGeocoder(tc.geo))
			}
			srv := httptest.NewServer(httpapi.New(memory.New(storetest.Fixtures()...), log, opts...).Handler())
			defer srv.Close()

			res, err := http.Get(srv.URL + tc.path + "?" + tc.params.Encode())
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			srv.Close() // 로그가 다 쓰이도록 핸들러 종료를 기다린다

			out := buf.String()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("log missing %q:\n%s", w, out)
				}
			}
			// 원좌표의 소수 셋째 자리 이후가 어디에도 남으면 안 된다.
			for _, f := range append(tc.forbids, "37.566", "126.977", "37.551", "126.961", "37.581", "126.991", "37.567", "126.978") {
				if strings.Contains(out, f) {
					t.Errorf("log contains %q:\n%s", f, out)
				}
			}
		})
	}
}
