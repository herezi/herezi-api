//go:build integration

// PostGIS 통합 테스트. 실행: HEREZI_TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/store/postgres/
// 매 실행마다 마이그레이션을 0까지 내렸다가 다시 올리므로 버려도 되는 DB만 쓴다.
package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/herezi/herezi-api/internal/domain"
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
		s, err := postgres.Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

// 저장 목록(saved_spots, migrations/00007): 메모리와 같은 스위트. spots 정리·삭제 시 CASCADE도 확인한다.
func TestSaved(t *testing.T) {
	url := os.Getenv("HEREZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HEREZI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	storetest.RunSaved(t, func(t *testing.T) storetest.SavedSpotStore {
		resetSchema(t, url)
		s, err := postgres.Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
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

// content_hash는 upsert로 저장되고, EnrichedSpots는 해시·요약이 있는 항목만 돌려준다(수집 해시 캐시).
func TestEnrichedSpots(t *testing.T) {
	url := os.Getenv("HEREZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HEREZI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	resetSchema(t, url)
	s, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := domain.Spot{
		Source: "seoul_culture", Title: "t", Category: domain.CategoryFestival,
		Lat: 37.5663, Lng: 126.9779, StartsAt: storetest.Now,
	}
	a, b, c := base, base, base
	a.ID, a.SourceID, a.Summary, a.ContentHash = "seoul_culture:a", "a", "요약", "h1"
	b.ID, b.SourceID, b.ContentHash = "seoul_culture:b", "b", "h2" // 요약 없음 → 캐시 아님
	c.ID, c.SourceID, c.Summary = "seoul_culture:c", "c", "요약"     // 해시 없음
	for _, sp := range []domain.Spot{a, b, c} {
		if err := s.UpsertSpot(ctx, sp); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.EnrichedSpots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != a.ID || got[0].ContentHash != "h1" {
		t.Fatalf("got = %+v", got)
	}
	one, err := s.SpotByID(ctx, b.ID)
	if err != nil || one.ContentHash != "h2" {
		t.Fatalf("SpotByID content_hash = %q, %v", one.ContentHash, err)
	}
}

// 공지사항(notices, migrations/00008): 메모리와 같은 스위트. 마이그레이션이 넣은 첫 공지는 지우고 시작한다.
func TestNotices(t *testing.T) {
	url := os.Getenv("HEREZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HEREZI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	storetest.RunNotices(t, func(t *testing.T) store.NoticeStore {
		resetSchema(t, url)
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.ExecContext(ctx, "DELETE FROM notices"); err != nil {
			t.Fatal(err)
		}
		s, err := postgres.Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

// 마이그레이션 00008의 첫 공지가 게시 상태로 들어가고, 고치면 updated_at이 바뀌며, RLS가 켜져 있다.
func TestNoticesMigrationSeed(t *testing.T) {
	url := os.Getenv("HEREZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HEREZI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	resetSchema(t, url)
	s, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, domain.Seoul)
	list, err := s.Notices(ctx, now, store.MaxNotices)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "히어지 테스트를 시작했어요" ||
		!list[0].PublishedAt.Equal(time.Date(2026, 11, 25, 0, 0, 0, 0, domain.Seoul)) {
		t.Fatalf("seed notices = %+v", list)
	}
	n, err := s.NoticeByID(ctx, list[0].ID, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"내부 테스트", "공개된 자료", "정보가 달라요"} {
		if !strings.Contains(n.Body, want) {
			t.Errorf("seed body missing %q: %q", want, n.Body)
		}
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rls bool
	if err := db.QueryRowContext(ctx, "SELECT relrowsecurity FROM pg_class WHERE oid = 'public.notices'::regclass").Scan(&rls); err != nil {
		t.Fatal(err)
	}
	if !rls {
		t.Error("notices: row level security is off")
	}
	var before, after time.Time
	if err := db.QueryRowContext(ctx, "SELECT updated_at FROM notices WHERE id = $1", n.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// 트랜잭션 안의 now()는 고정이므로 별도 문장으로 고친다.
	if err := db.QueryRowContext(ctx, "UPDATE notices SET is_hidden = true WHERE id = $1 RETURNING updated_at", n.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Errorf("updated_at not bumped: before %v after %v", before, after)
	}
	if _, err := s.NoticeByID(ctx, n.ID, now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("hidden seed: err = %v, want ErrNotFound", err)
	}
}
