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

type spotMap struct {
	Items     []spot `json:"items"`
	Truncated *bool  `json:"truncated"`
}

type page struct {
	Items      []spot `json:"items"`
	NextCursor string `json:"next_cursor"`
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

// near is the base query: Seoul City Hall, where the storetest fixtures are.
func near(kv ...string) url.Values {
	v := url.Values{"lat": {"37.5663"}, "lng": {"126.9779"}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func ids(p page) []string { return spotIDs(p.Items) }

func spotIDs(items []spot) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, strings.TrimPrefix(it.ID, "test:"))
	}
	return out
}

func TestHealthz(t *testing.T) {
	srv := newServer(t)
	var body map[string]string
	res := get(t, srv, "/healthz", nil, &body)
	if res.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v", res.StatusCode, body)
	}
}

func TestListSpotsFilters(t *testing.T) {
	srv := newServer(t)
	cases := []struct {
		name   string
		params url.Values
		want   []string
	}{
		{"default now, by distance, ended hidden", near(),
			[]string{"near-festival-free", "mid-popup", "far-exhibition-unknown"}},
		{"radius", near("radius_m", "1500"), []string{"near-festival-free", "mid-popup"}},
		{"category list", near("category", "popup, exhibition"), []string{"mid-popup", "far-exhibition-unknown"}},
		{"when=week includes upcoming", near("when", "week"),
			[]string{"near-festival-free", "tomorrow", "mid-popup", "far-exhibition-unknown"}},
		{"when=today excludes tomorrow", near("when", "today"),
			[]string{"near-festival-free", "mid-popup", "far-exhibition-unknown"}},
		{"free=true", near("free", "true"), []string{"near-festival-free"}},
		{"free=true with week", near("free", "true", "when", "week"), []string{"near-festival-free", "tomorrow"}},
		{"free=false is no filter", near("free", "false"),
			[]string{"near-festival-free", "mid-popup", "far-exhibition-unknown"}},
		{"far away is empty", url.Values{"lat": {"35.1796"}, "lng": {"129.0756"}}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p page
			res := get(t, srv, "/v1/spots", tc.params, &p)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status %d", res.StatusCode)
			}
			if p.Items == nil {
				t.Fatal("items must be [] not null")
			}
			if got := ids(p); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, it := range p.Items {
				if it.DistanceM == nil {
					t.Errorf("%s: distance_m missing", it.ID)
				}
				if it.ID == "test:ended" {
					t.Error("ended spot returned")
				}
				if it.IsEnded == nil || *it.IsEnded {
					t.Errorf("%s: is_ended = %v, want false", it.ID, it.IsEnded)
				}
			}
			if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age=60") {
				t.Errorf("Cache-Control = %q", cc)
			}
		})
	}
}

func TestListSpotsPagination(t *testing.T) {
	srv := newServer(t)
	var all []string
	params := near("limit", "2", "when", "week")
	for range 5 {
		var p page
		if res := get(t, srv, "/v1/spots", params, &p); res.StatusCode != http.StatusOK {
			t.Fatalf("status %d", res.StatusCode)
		}
		all = append(all, ids(p)...)
		if p.NextCursor == "" {
			break
		}
		params.Set("cursor", p.NextCursor)
	}
	want := []string{"near-festival-free", "tomorrow", "mid-popup", "far-exhibition-unknown"}
	if !slices.Equal(all, want) {
		t.Fatalf("got %v, want %v", all, want)
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

func TestListSpotsBadParams(t *testing.T) {
	srv := newServer(t)
	cases := map[string]url.Values{
		"missing lat":        {"lng": {"126.9779"}},
		"missing lng":        {"lat": {"37.5663"}},
		"lat not number":     near("lat", "abc"),
		"lat out of range":   near("lat", "91"),
		"lng out of range":   near("lng", "-181"),
		"lat NaN":            near("lat", "NaN"),
		"radius too small":   near("radius_m", "99"),
		"radius too big":     near("radius_m", "20001"),
		"radius not int":     near("radius_m", "1.5"),
		"limit zero":         near("limit", "0"),
		"limit too big":      near("limit", "101"),
		"category unknown":   near("category", "festival,bogus"),
		"category free gone": near("category", "free"),
		"when unknown":       near("when", "yesterday"),
		"free not bool":      near("free", "yes"),
		"cursor garbage":     near("cursor", "!!!"),
		"cursor negative":    near("cursor", "bzotMQ"), // base64("o:-1")
		"q empty":            near("q", ""),
		"q only spaces":      near("q", "   "),
		"q too long":         near("q", strings.Repeat("가", 51)),
		"q missing lat":      {"lng": {"126.9779"}, "q": {"서울"}},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			var e apiError
			res := get(t, srv, "/v1/spots", params, &e)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d", res.StatusCode)
			}
			if e.Code != "invalid_parameter" || e.Message == "" {
				t.Fatalf("error body %+v", e)
			}
		})
	}
}

func TestListSpotsKeyword(t *testing.T) {
	srv := newServer(t)
	cases := []struct {
		name   string
		params url.Values
		want   []string
	}{
		{"title case-insensitive", near("q", "Festival"), []string{"near-festival-free"}},
		{"trimmed", near("q", "  서울광장 \t"), []string{"near-festival-free"}},
		{"place name or address, by distance", near("q", "서울"), []string{"near-festival-free", "far-exhibition-unknown"}},
		{"with category", near("q", "서울", "category", "exhibition"), []string{"far-exhibition-unknown"}},
		{"percent literal", near("q", "50%"), []string{"far-exhibition-unknown"}},
		{"50 runes ok", near("q", strings.Repeat("가", 50)), []string{}},
		{"no match", near("q", "빛초롱"), []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p page
			res := get(t, srv, "/v1/spots", tc.params, &p)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status %d", res.StatusCode)
			}
			if p.Items == nil {
				t.Fatal("items must be [] not null")
			}
			if got := ids(p); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListSpotsKeywordPagination(t *testing.T) {
	srv := newServer(t)
	var p1 page
	get(t, srv, "/v1/spots", near("q", "서울", "limit", "1"), &p1)
	if got := ids(p1); !slices.Equal(got, []string{"near-festival-free"}) || p1.NextCursor == "" {
		t.Fatalf("page1 %v cursor %q", got, p1.NextCursor)
	}
	var p2 page
	get(t, srv, "/v1/spots", near("q", "서울", "limit", "1", "cursor", p1.NextCursor), &p2)
	if got := ids(p2); !slices.Equal(got, []string{"far-exhibition-unknown"}) || p2.NextCursor != "" {
		t.Fatalf("page2 %v cursor %q", got, p2.NextCursor)
	}
}

// box is a bbox around the fixtures: just south of City Hall to ~1.6km north.
func box(kv ...string) url.Values {
	v := url.Values{"sw_lat": {"37.5653"}, "sw_lng": {"126.9769"}, "ne_lat": {"37.5813"}, "ne_lng": {"126.9789"}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func TestListSpotsInBBox(t *testing.T) {
	srv := newServer(t)
	cases := []struct {
		name      string
		params    url.Values
		want      []string
		truncated bool
	}{
		{"default now, center first, ended hidden", box(), []string{"mid-popup", "near-festival-free"}, false},
		{"limit truncates", box("limit", "1"), []string{"mid-popup"}, true},
		{"limit equal to count is not truncated", box("limit", "2"), []string{"mid-popup", "near-festival-free"}, false},
		{"free=true", box("free", "true"), []string{"near-festival-free"}, false},
		{"category", box("category", "popup"), []string{"mid-popup"}, false},
		{"when=week", box("when", "week"), []string{"tomorrow", "mid-popup", "near-festival-free"}, false},
		{"empty area", box("sw_lat", "35.0", "ne_lat", "35.1"), []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m spotMap
			res := get(t, srv, "/v1/spots/map", tc.params, &m)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status %d", res.StatusCode)
			}
			if m.Items == nil || m.Truncated == nil {
				t.Fatal("items and truncated are required")
			}
			if got := spotIDs(m.Items); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if *m.Truncated != tc.truncated {
				t.Errorf("truncated = %v, want %v", *m.Truncated, tc.truncated)
			}
			for _, it := range m.Items {
				if it.DistanceM != nil {
					t.Errorf("%s: distance_m must be omitted on map", it.ID)
				}
				if it.ID == "test:ended" {
					t.Error("ended spot returned on map")
				}
				if it.IsEnded == nil || *it.IsEnded {
					t.Errorf("%s: is_ended = %v, want false", it.ID, it.IsEnded)
				}
				if it.FetchedAt == "" {
					t.Errorf("%s: fetched_at missing", it.ID)
				}
			}
			if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age=60") {
				t.Errorf("Cache-Control = %q", cc)
			}
		})
	}
}

func TestListSpotsInBBoxBadParams(t *testing.T) {
	srv := newServer(t)
	cases := map[string]url.Values{
		"missing sw_lat":   {"sw_lng": {"126.9"}, "ne_lat": {"37.6"}, "ne_lng": {"127.0"}},
		"missing ne_lng":   {"sw_lat": {"37.5"}, "sw_lng": {"126.9"}, "ne_lat": {"37.6"}},
		"not a number":     box("sw_lat", "x"),
		"lat out of range": box("ne_lat", "90.1"),
		"lng out of range": box("sw_lng", "-180.5"),
		"sw north of ne":   box("sw_lat", "37.6", "ne_lat", "37.5"),
		"antimeridian":     box("sw_lng", "127.1", "ne_lng", "126.9"),
		"limit zero":       box("limit", "0"),
		"limit too big":    box("limit", "501"),
		"bad category":     box("category", "free"),
		"bad when":         box("when", "later"),
		"bad free":         box("free", "1"),
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			var e apiError
			res := get(t, srv, "/v1/spots/map", params, &e)
			if res.StatusCode != http.StatusBadRequest || e.Code != "invalid_parameter" || e.Message == "" {
				t.Fatalf("got %d %+v", res.StatusCode, e)
			}
		})
	}
}
