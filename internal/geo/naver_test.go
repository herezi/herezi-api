package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const naverOK = `{"status":{"code":0,"name":"ok","message":"done"},"results":[
 {"name":"legalcode","region":{"area0":{"name":"kr"},"area1":{"name":"서울특별시"},"area2":{"name":"중구"},"area3":{"name":"태평로1가"},"area4":{"name":""}}},
 {"name":"admcode","region":{"area0":{"name":"kr"},"area1":{"name":"서울특별시"},"area2":{"name":"중구"},"area3":{"name":"명동"},"area4":{"name":""}}}]}`

func fakeNaver(t *testing.T, status int, body string, check func(*http.Request)) *Naver {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	n := NewNaver("test-id", "test-secret")
	n.BaseURL = srv.URL + "/map-reversegeocode/v2/gc"
	return n
}

func TestNaverReverseRequestAndParse(t *testing.T) {
	n := fakeNaver(t, http.StatusOK, naverOK, func(r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/map-reversegeocode/v2/gc" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if got := q.Get("coords"); got != "126.978,37.566" {
			t.Errorf("coords = %q, want lng,lat", got)
		}
		if q.Get("output") != "json" || q.Get("orders") != "legalcode,admcode" {
			t.Errorf("query = %v", q)
		}
		if r.Header.Get("x-ncp-apigw-api-key-id") != "test-id" || r.Header.Get("x-ncp-apigw-api-key") != "test-secret" {
			t.Errorf("auth headers missing")
		}
	})
	r, err := n.Reverse(context.Background(), 37.566, 126.978)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Region{"서울특별시", "중구", "태평로1가"}); r != want {
		t.Fatalf("region = %+v, want %+v (results[0] = legalcode)", r, want)
	}
}

func TestNaverReverseNoResult(t *testing.T) {
	n := fakeNaver(t, http.StatusOK, `{"status":{"code":3,"name":"no results","message":"내용 없음"},"results":[]}`, nil)
	r, err := n.Reverse(context.Background(), 35.0, 129.9)
	if err != nil || r != (Region{}) {
		t.Fatalf("r=%+v err=%v, want empty region and nil", r, err)
	}
}

func TestNaverReverseErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"http 401":    {http.StatusUnauthorized, `{"error":{"errorCode":"200","message":"Authentication Failed"}}`},
		"bad json":    {http.StatusOK, `not json`},
		"status code": {http.StatusOK, `{"status":{"code":100,"name":"invalid request"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			n := fakeNaver(t, tc.status, tc.body, nil)
			if _, err := n.Reverse(context.Background(), 37.5, 127.0); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestNaverReverseTimeout(t *testing.T) {
	n := fakeNaver(t, http.StatusOK, naverOK, func(*http.Request) { time.Sleep(200 * time.Millisecond) })
	n.HTTP.Timeout = 50 * time.Millisecond
	if _, err := n.Reverse(context.Background(), 37.5, 127.0); err == nil {
		t.Fatal("want timeout error")
	}
}

// 에러 메시지에 요청 URL(좌표)이 들어가면 안 된다(로그 규칙).
func TestNaverReverseErrorHasNoCoordinates(t *testing.T) {
	n := fakeNaver(t, http.StatusOK, naverOK, func(*http.Request) { time.Sleep(200 * time.Millisecond) })
	n.HTTP.Timeout = 50 * time.Millisecond
	_, err := n.Reverse(context.Background(), 37.566312, 126.977945)
	if err == nil {
		t.Fatal("want error")
	}
	if msg := err.Error(); strings.Contains(msg, "37.566") || strings.Contains(msg, "126.977") || strings.Contains(msg, "coords") {
		t.Fatalf("error leaks coordinates: %s", msg)
	}
}
