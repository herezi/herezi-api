package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

type spot struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	IsFree    *bool  `json:"is_free"`
	DistanceM *int   `json:"distance_m"`
	FetchedAt string `json:"fetched_at"`
	IsEnded   *bool  `json:"is_ended"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := memory.New(storetest.Fixtures()...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := httpapi.New(st, log, httpapi.WithClock(func() time.Time { return storetest.Now })).Handler()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string, params url.Values, out any) *http.Response {
	t.Helper()
	u := srv.URL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("%s: content-type %q", u, ct)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("%s: decode: %v", u, err)
		}
	}
	return res
}

func TestHealthz(t *testing.T) {
	srv := newServer(t)
	var body map[string]string
	res := get(t, srv, "/healthz", nil, &body)
	if res.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v", res.StatusCode, body)
	}
}
func TestGetSpot(t *testing.T) {
	srv := newServer(t)
	var s spot
	res := get(t, srv, "/v1/spots/test:mid-popup", nil, &s)
	if res.StatusCode != http.StatusOK || s.ID != "test:mid-popup" || s.Category != "popup" {
		t.Fatalf("got %d %+v", res.StatusCode, s)
	}
	if s.DistanceM != nil {
		t.Error("distance_m must be omitted on detail")
	}
	if ft, err := time.Parse(time.RFC3339, s.FetchedAt); err != nil || !ft.Equal(storetest.FetchedAt) {
		t.Errorf("fetched_at = %q, want %v", s.FetchedAt, storetest.FetchedAt)
	}
	if s.IsEnded == nil || *s.IsEnded {
		t.Errorf("is_ended = %v, want false", s.IsEnded)
	}
	var e apiError
	res = get(t, srv, "/v1/spots/test:nope", nil, &e)
	if res.StatusCode != http.StatusNotFound || e.Code != "not_found" || e.Message == "" {
		t.Errorf("missing: got %d %+v", res.StatusCode, e)
	}
}

// 끝난 행사: 목록·지도에선 숨기지만 상세는 200 + is_ended=true (PRODUCT.md 원칙 3).
func TestGetSpotEnded(t *testing.T) {
	srv := newServer(t)
	var s spot
	res := get(t, srv, "/v1/spots/test:ended", nil, &s)
	if res.StatusCode != http.StatusOK || s.ID != "test:ended" {
		t.Fatalf("got %d %+v", res.StatusCode, s)
	}
	if s.IsEnded == nil || !*s.IsEnded {
		t.Errorf("is_ended = %v, want true", s.IsEnded)
	}
}

// is_ended는 저장값이 아니라 요청 시계 기준으로 계산된다.
func TestGetSpotIsEndedUsesClock(t *testing.T) {
	st := memory.New(storetest.Fixtures()...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"before end", storetest.Now.Add(-2 * time.Hour), false},
		{"exactly at end", storetest.Now.Add(-time.Hour), false},
		{"after end", storetest.Now, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := httpapi.New(st, log, httpapi.WithClock(func() time.Time { return tc.now })).Handler()
			srv := httptest.NewServer(h)
			defer srv.Close()
			var s spot
			res := get(t, srv, "/v1/spots/test:ended", nil, &s)
			if res.StatusCode != http.StatusOK || s.IsEnded == nil || *s.IsEnded != tc.want {
				t.Fatalf("got %d is_ended=%v, want %v", res.StatusCode, s.IsEnded, tc.want)
			}
		})
	}
}

// TestRemovedCoordinateEndpointsGone: #74에서 지운 좌표 엔드포인트는 410 Gone + Error JSON(code=gone)을 돌려준다.
// 쿼리(좌표)를 읽지 않으므로 파라미터가 잘못돼도 400이 아니라 410이다. 상세(/v1/spots/{id})는 그대로다.
func TestRemovedCoordinateEndpointsGone(t *testing.T) {
	srv := newServer(t)
	for _, tc := range []struct {
		path   string
		params url.Values
	}{
		{"/v1/spots", url.Values{"lat": {"37.5663"}, "lng": {"126.9779"}}},
		{"/v1/spots", nil},
		{"/v1/spots/map", url.Values{"sw_lat": {"37.55"}, "sw_lng": {"126.96"}, "ne_lat": {"37.58"}, "ne_lng": {"126.99"}}},
		{"/v1/spots/map", url.Values{"sw_lat": {"bad"}}},
		{"/v1/geo/reverse", url.Values{"lat": {"37.5663"}, "lng": {"126.9779"}}},
	} {
		var e apiError
		res := get(t, srv, tc.path, tc.params, &e)
		if res.StatusCode != http.StatusGone || e.Code != "gone" || !strings.Contains(e.Message, "#74") {
			t.Errorf("%s?%s: status %d code %q message %q", tc.path, tc.params.Encode(), res.StatusCode, e.Code, e.Message)
		}
		if cc := res.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("%s: Cache-Control %q on 410", tc.path, cc)
		}
	}
	if got := httpapi.GonePaths; !slices.Equal(got, []string{"/v1/spots", "/v1/spots/map", "/v1/geo/reverse"}) {
		t.Fatalf("GonePaths = %v", got)
	}
	// "map"이라는 id의 상세가 아니라 410이어야 하고, 다른 id의 상세는 그대로 동작한다.
	var s spot
	if res := get(t, srv, "/v1/spots/test:mid-popup", nil, &s); res.StatusCode != http.StatusOK || s.ID != "test:mid-popup" {
		t.Fatalf("detail: status %d id %q", res.StatusCode, s.ID)
	}
}
