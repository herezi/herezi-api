package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeKakao(t *testing.T, status int, body string, check func(*http.Request)) *KakaoPlaces {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	k := NewKakaoPlaces("test-key")
	k.BaseURL = srv.URL + "/v2/local/search/keyword.json"
	return k
}

// 카카오 응답 형식(필드 일부). 실제 응답에서 좌표·이름만 남김.
const kakaoFixture = `{"documents":[
 {"place_name":"국립국악원 예악당","address_name":"서울 서초구 서초동 700","road_address_name":"서울 서초구 남부순환로 2364","x":"127.00866","y":"37.47799","category_name":"문화,예술 > 공연장"},
 {"place_name":"국립국악원","address_name":"서울 서초구 서초동 700","road_address_name":"","x":"127.00880","y":"37.47810"},
 {"place_name":"좌표 깨짐","x":"bad","y":"37.1"}
],"meta":{"total_count":3,"pageable_count":3,"is_end":true}}`

func TestKakaoSearchPlace(t *testing.T) {
	k := fakeKakao(t, http.StatusOK, kakaoFixture, func(r *http.Request) {
		q := r.URL.Query()
		if q.Get("query") != "국립국악원 예악당 서초구" {
			t.Errorf("query = %q", q.Get("query"))
		}
		if q.Get("rect") != SeoulRect || q.Get("size") != "5" {
			t.Errorf("rect=%q size=%q", q.Get("rect"), q.Get("size"))
		}
		if r.Header.Get("Authorization") != "KakaoAK test-key" {
			t.Errorf("auth header missing")
		}
	})
	got, err := k.SearchPlace(context.Background(), "국립국악원 예악당 서초구")
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got[0].Name != "국립국악원 예악당" || got[0].Lat != 37.47799 || got[0].Lng != 127.00866 || got[0].Address != "서울 서초구 남부순환로 2364" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Address != "서울 서초구 서초동 700" {
		t.Errorf("fallback address = %q", got[1].Address)
	}
}

func TestKakaoSearchPlaceEmpty(t *testing.T) {
	k := fakeKakao(t, http.StatusOK, `{"documents":[],"meta":{"total_count":0}}`, nil)
	got, err := k.SearchPlace(context.Background(), "없는곳")
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got, err := k.SearchPlace(context.Background(), "  "); err != nil || got != nil {
		t.Fatalf("blank query: got=%+v err=%v", got, err)
	}
}

func TestKakaoErrorHasNoQueryOrKey(t *testing.T) {
	k := fakeKakao(t, http.StatusUnauthorized, `{"errorType":"AccessDeniedError"}`, nil)
	_, err := k.SearchPlace(context.Background(), "비밀장소")
	if err == nil || strings.Contains(err.Error(), "비밀장소") || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("err = %v", err)
	}
	k.BaseURL = "http://127.0.0.1:1/v2/local/search/keyword.json"
	_, err = k.SearchPlace(context.Background(), "비밀장소")
	if err == nil || strings.Contains(err.Error(), "query=") {
		t.Fatalf("transport err = %v", err)
	}
}
