//go:build integration

// 공지사항 PostGIS 통합 테스트(이슈 #80): 실제 DB 저장소로 목록·상세·404·CORS를 확인한다.
// 실행: HEREZI_TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/httpapi/
// 마이그레이션을 0까지 내렸다가 다시 올리므로 버려도 되는 DB만 쓴다.
package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store/postgres"
	"github.com/herezi/herezi-api/internal/store/storetest"
	"github.com/herezi/herezi-api/migrations"
)

func TestNoticesPostGIS(t *testing.T) {
	url := os.Getenv("HEREZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HEREZI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	// 마이그레이션이 넣은 첫 공지(2026-11-25)는 storetest.Now(2026-11-18)에는 아직 게시 전이라 목록에 없다.
	_ = db.Close()

	pg, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	ids := map[string]int64{}
	for _, n := range storetest.NoticeFixtures() {
		id, err := pg.InsertNotice(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		ids[n.Title] = id
	}
	srv := httptest.NewServer(httpapi.New(pg, slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpapi.WithClock(func() time.Time { return storetest.Now }), httpapi.WithNotices(pg)).Handler())
	defer srv.Close()

	origin := map[string]string{"Origin": "https://herezi.github.io"}
	res, b := doNotice(t, http.MethodGet, srv.URL+"/v1/notices", origin)
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "public, max-age=60" ||
		res.Header.Get("Access-Control-Allow-Origin") != "https://herezi.github.io" {
		t.Fatalf("list: %d %v %s", res.StatusCode, res.Header, b)
	}
	var list noticeListBody
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range list.Notices {
		titles = append(titles, n.Title)
	}
	if want := []string{"exactly-now", "newest", "same-time-second", "same-time-first", "old"}; !slices.Equal(titles, want) {
		t.Fatalf("titles %v, want %v", titles, want)
	}

	res, b = doNotice(t, http.MethodGet, fmt.Sprintf("%s/v1/notices/%d", srv.URL, ids["newest"]), origin)
	var n noticeBody
	if err := json.Unmarshal(b, &n); err != nil || res.StatusCode != http.StatusOK || n.Body != "# newest\n\n본문" ||
		n.PublishedAt != "2026-11-18T14:00:00+09:00" {
		t.Fatalf("get: %d %s", res.StatusCode, b)
	}
	for _, id := range []int64{ids["hidden"], ids["scheduled"], 999999} {
		if res, b := doNotice(t, http.MethodGet, fmt.Sprintf("%s/v1/notices/%d", srv.URL, id), nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("id %d: %d %s", id, res.StatusCode, b)
		}
	}
	res, _ = doNotice(t, http.MethodOptions, srv.URL+"/v1/notices", map[string]string{
		"Origin": "http://localhost:5173", "Access-Control-Request-Method": "GET",
	})
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
	}
}
