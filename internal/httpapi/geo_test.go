package httpapi_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/herezi/herezi-api/internal/geo"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store/memory"
)

type fakeGeocoder struct {
	region   geo.Region
	err      error
	lat, lng float64
}

func (f *fakeGeocoder) Reverse(_ context.Context, lat, lng float64) (geo.Region, error) {
	f.lat, f.lng = lat, lng
	return f.region, f.err
}

type geoReverse struct {
	Area1 *string `json:"area1"`
	Area2 *string `json:"area2"`
	Area3 *string `json:"area3"`
	Label *string `json:"label"`
}

func newGeoServer(t *testing.T, opts ...httpapi.Option) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.New(memory.New(), log, opts...).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestReverseGeocode(t *testing.T) {
	fg := &fakeGeocoder{region: geo.Region{Area1: "서울특별시", Area2: "중구", Area3: "태평로1가"}}
	srv := newGeoServer(t, httpapi.WithGeocoder(fg))
	var out geoReverse
	res := get(t, srv, "/v1/geo/reverse", url.Values{"lat": {"37.5663"}, "lng": {"126.9779"}}, &out)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if fg.lat != 37.5663 || fg.lng != 126.9779 {
		t.Errorf("geocoder got %v,%v", fg.lat, fg.lng)
	}
	if out.Label == nil || *out.Label != "중구 태평로1가" || *out.Area1 != "서울특별시" || *out.Area2 != "중구" || *out.Area3 != "태평로1가" {
		t.Fatalf("body = %+v", out)
	}
}

func TestReverseGeocodeEmptyResult(t *testing.T) {
	srv := newGeoServer(t, httpapi.WithGeocoder(&fakeGeocoder{}))
	var out geoReverse
	res := get(t, srv, "/v1/geo/reverse", url.Values{"lat": {"35.0"}, "lng": {"129.9"}}, &out)
	if res.StatusCode != http.StatusOK || out.Label == nil || *out.Label != "" || out.Area1 == nil {
		t.Fatalf("status %d body %+v, want 200 with empty strings", res.StatusCode, out)
	}
}

func TestReverseGeocodeErrors(t *testing.T) {
	ok := url.Values{"lat": {"37.5"}, "lng": {"127.0"}}
	for name, tc := range map[string]struct {
		opts   []httpapi.Option
		params url.Values
		status int
		code   string
	}{
		"missing lat":    {[]httpapi.Option{httpapi.WithGeocoder(&fakeGeocoder{})}, url.Values{"lng": {"127"}}, 400, "invalid_parameter"},
		"lng range":      {[]httpapi.Option{httpapi.WithGeocoder(&fakeGeocoder{})}, url.Values{"lat": {"37"}, "lng": {"181"}}, 400, "invalid_parameter"},
		"not configured": {nil, ok, 501, "not_configured"},
		"upstream fails": {[]httpapi.Option{httpapi.WithGeocoder(&fakeGeocoder{err: errors.New("timeout")})}, ok, 502, "upstream_error"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newGeoServer(t, tc.opts...)
			var e apiError
			res := get(t, srv, "/v1/geo/reverse", tc.params, &e)
			if res.StatusCode != tc.status || e.Code != tc.code {
				t.Fatalf("status %d code %q, want %d %q", res.StatusCode, e.Code, tc.status, tc.code)
			}
		})
	}
}
