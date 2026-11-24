package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeGeocoder(t *testing.T, status int, body string, check func(*http.Request)) *NaverGeocoder {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	g := NewNaverGeocoder("test-id", "test-secret")
	g.BaseURL = srv.URL + "/map-geocode/v2/geocode"
	return g
}

func TestNaverGeocode(t *testing.T) {
	g := fakeGeocoder(t, http.StatusOK,
		`{"status":"OK","meta":{"totalCount":1},"addresses":[{"roadAddress":"서울특별시 중구 세종대로 110","x":"126.9783882","y":"37.5666103"}],"errorMessage":""}`,
		func(r *http.Request) {
			if r.URL.Query().Get("query") != "서울특별시 중구 세종대로 110" {
				t.Errorf("query = %q", r.URL.Query().Get("query"))
			}
			if r.Header.Get("x-ncp-apigw-api-key-id") != "test-id" || r.Header.Get("x-ncp-apigw-api-key") != "test-secret" {
				t.Errorf("auth headers missing")
			}
		})
	p, err := g.Geocode(context.Background(), "서울특별시 중구 세종대로 110")
	if err != nil || !p.OK || p.Lat != 37.5666103 || p.Lng != 126.9783882 {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

func TestNaverGeocodeNoResult(t *testing.T) {
	g := fakeGeocoder(t, http.StatusOK, `{"status":"OK","meta":{"totalCount":0},"addresses":[],"errorMessage":""}`, nil)
	p, err := g.Geocode(context.Background(), "국립국악원")
	if err != nil || p.OK {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

func TestNaverGeocodeErrorHasNoQuery(t *testing.T) {
	g := fakeGeocoder(t, http.StatusUnauthorized, `{}`, nil)
	_, err := g.Geocode(context.Background(), "비밀주소 1")
	if err == nil || strings.Contains(err.Error(), "비밀주소") {
		t.Fatalf("err = %v", err)
	}
}
