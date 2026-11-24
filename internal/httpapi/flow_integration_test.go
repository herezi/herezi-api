//go:build integration

// PostGIS 통합 흐름 테스트(이슈 #56 완료 기준): 저장 → 목록 → 삭제 → 탈퇴를 실제 DB 저장소로 돌린다.
// 실행: HEREZI_TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/httpapi/
// 마이그레이션을 0까지 내렸다가 다시 올리므로 버려도 되는 DB만 쓴다.
package httpapi_test

import (
	"context"
	"database/sql"
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

	"github.com/herezi/herezi-api/internal/auth"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store/postgres"
	"github.com/herezi/herezi-api/internal/store/storetest"
	"github.com/herezi/herezi-api/migrations"
)

func TestSavedFlowPostGIS(t *testing.T) {
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
	_ = db.Close()

	pg, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	for _, sp := range storetest.Fixtures() {
		if err := pg.UpsertSpot(ctx, sp); err != nil {
			t.Fatal(err)
		}
	}
	admin := newFakeAdmin(t)
	srv := httptest.NewServer(httpapi.New(pg, slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpapi.WithClock(func() time.Time { return storetest.Now }),
		httpapi.WithAuth(testVerifier), httpapi.WithSaved(pg),
		httpapi.WithUserDeleter(auth.NewSupabaseAdmin(admin.srv.URL, adminKey, nil))).Handler())
	defer srv.Close()

	list := func(tok string) []string {
		var p savedPage
		if res := call(t, srv, http.MethodGet, "/v1/me/saved", tok, nil, &p); res.StatusCode != http.StatusOK {
			t.Fatalf("list: %d", res.StatusCode)
		}
		return savedIDs(p)
	}
	if res := call(t, srv, http.MethodGet, "/v1/me/saved", "", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	if res := call(t, srv, http.MethodPut, "/v1/me/saved/test:mid-popup", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %d", res.StatusCode)
	}
	var r struct {
		Imported       int `json:"imported"`
		SkippedUnknown int `json:"skipped_unknown"`
	}
	if res := call(t, srv, http.MethodPost, "/v1/me/saved:import", tokA,
		map[string]any{"spot_ids": []string{"test:ended", "test:mid-popup", "test:gone"}}, &r); res.StatusCode != http.StatusOK || r.Imported != 1 || r.SkippedUnknown != 1 {
		t.Fatalf("import: %d %+v", res.StatusCode, r)
	}
	if res := call(t, srv, http.MethodPut, "/v1/me/saved/test:too-far", tokB, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put B: %d", res.StatusCode)
	}
	var page savedPage
	call(t, srv, http.MethodGet, "/v1/me/saved", tokA, nil, &page)
	if got := savedIDs(page); !slices.Equal(got, []string{"ended", "mid-popup"}) {
		t.Fatalf("list A: %v", got)
	}
	for _, it := range page.Items {
		if it.IsEnded == nil || *it.IsEnded != (it.ID == "test:ended") || it.SavedAt == nil {
			t.Fatalf("%s: is_ended=%v saved_at=%v", it.ID, it.IsEnded, it.SavedAt)
		}
	}
	if res := call(t, srv, http.MethodDelete, "/v1/me/saved/test:ended", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if got := list(tokA); !slices.Equal(got, []string{"mid-popup"}) {
		t.Fatalf("after delete: %v", got)
	}
	if res := call(t, srv, http.MethodDelete, "/v1/me", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete me: %d", res.StatusCode)
	}
	if got := list(tokA); len(got) != 0 {
		t.Fatalf("after account deletion: %v", got)
	}
	if got := list(tokB); !slices.Equal(got, []string{"too-far"}) {
		t.Fatalf("user B must be untouched: %v", got)
	}
}
