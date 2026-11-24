package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

func TestRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	end := now.Add(48 * time.Hour)
	yes := true
	in := []domain.Spot{{
		ID: "seoul_culture:1", Source: "seoul_culture", SourceID: "1", Title: "제목", Summary: "요약",
		Category: domain.CategoryExhibition, Lat: 37.5, Lng: 127, StartsAt: now, EndsAt: &end, IsFree: &yes,
		FetchedAt: now, ContentHash: "abc",
	}}
	path := filepath.Join(t.TempDir(), "sub", "snap.json")
	if err := Write(path, now, in); err != nil {
		t.Fatal(err)
	}
	got, gen, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !gen.Equal(now) || len(got) != 1 {
		t.Fatalf("gen=%v len=%d", gen, len(got))
	}
	g := got[0]
	if g.ID != in[0].ID || g.ContentHash != "abc" || !g.EndsAt.Equal(end) || !g.Free() || g.Category != domain.CategoryExhibition {
		t.Fatalf("round trip mismatch: %+v", g)
	}
}

func TestReadRejectsBadCategory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"spots":[{"id":"x","category":"bogus"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(path); err == nil {
		t.Fatal("expected error")
	}
}
