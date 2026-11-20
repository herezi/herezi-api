//go:build integration

// PostGIS 통합 테스트. 실행: HEREZI_TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/store/postgres/
// 매 실행마다 마이그레이션을 0까지 내렸다가 다시 올리므로 버려도 되는 DB만 쓴다.
package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/postgres"
	"github.com/herezi/herezi-api/internal/store/storetest"
	"github.com/herezi/herezi-api/migrations"
)

func TestStore(t *testing.T) {
	url := os.Getenv("HEREZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HEREZI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	storetest.Run(t, func(t *testing.T) store.SpotStore {
		resetSchema(t, url)
		s, pool, err := postgres.Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return s
	})
}

func resetSchema(t *testing.T, url string) {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}
