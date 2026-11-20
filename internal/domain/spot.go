// Package domain holds Herezi's core types. It must not import any other
// internal package (see ARCHITECTURE.md); everything else depends on it.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Category is the fixed set of spot categories defined in docs/product/PRODUCT.md
// and api/openapi.yaml (components.schemas.Category).
type Category string

// Category values. 무료 여부는 카테고리가 아니라 Spot.IsFree + `free=true` 필터로 다룬다
// (SERVICE-PLAN.md §12 Q3). Keep in sync with api/openapi.yaml — a contract test enforces it.
const (
	CategoryNature      Category = "nature"
	CategoryFestival    Category = "festival"
	CategoryPerformance Category = "performance"
	CategoryExhibition  Category = "exhibition"
	CategoryPopup       Category = "popup"
	CategoryFood        Category = "food"
	CategoryFamily      Category = "family"
	CategoryPet         Category = "pet"
	CategorySports      Category = "sports"
)

// Categories returns all valid categories in spec order.
func Categories() []Category {
	return []Category{
		CategoryNature, CategoryFestival, CategoryPerformance, CategoryExhibition, CategoryPopup,
		CategoryFood, CategoryFamily, CategoryPet, CategorySports,
	}
}

// Valid reports whether c is one of Categories().
func (c Category) Valid() bool {
	for _, v := range Categories() {
		if v == c {
			return true
		}
	}
	return false
}

// ParseCategories parses a comma-separated list such as "festival,popup".
// An empty string yields nil (meaning "all categories").
func ParseCategories(s string) ([]Category, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []Category
	for _, part := range strings.Split(s, ",") {
		c := Category(strings.TrimSpace(part))
		if !c.Valid() {
			return nil, fmt.Errorf("unknown category %q", part)
		}
		out = append(out, c)
	}
	return out, nil
}

// Spot is one "thing to see/do" at a place and time.
// ID is "<source>:<source_id>" so re-ingesting the same item upserts instead of duplicating.
type Spot struct {
	ID        string
	Title     string
	Summary   string
	Category  Category
	Lat       float64
	Lng       float64
	Address   string
	PlaceName string
	StartsAt  time.Time
	EndsAt    *time.Time
	IsFree    *bool // nil = 모름
	ImageURL  string
	Source    string
	SourceID  string
	SourceURL string
	// DistanceM is filled only by nearby queries.
	DistanceM *int
}

// SpotID builds the canonical spot ID.
func SpotID(source, sourceID string) string { return source + ":" + sourceID }

// Overlaps reports whether the spot is active at any point within [from, to].
// A spot without EndsAt is treated as a point event at StartsAt.
func (s Spot) Overlaps(from, to time.Time) bool {
	end := s.StartsAt
	if s.EndsAt != nil {
		end = *s.EndsAt
	}
	return !s.StartsAt.After(to) && !end.Before(from)
}

// Ended reports whether the spot has an explicit end time before now.
// 끝난 행사(ends_at < now)는 어떤 조회에서도 반환하지 않는다 (TECH-PLAN.md §3).
func (s Spot) Ended(now time.Time) bool {
	return s.EndsAt != nil && s.EndsAt.Before(now)
}

// Free reports whether the spot is known to be free. Unknown (nil) counts as not free.
func (s Spot) Free() bool { return s.IsFree != nil && *s.IsFree }

// RawItem is an unnormalized record fetched by a collector. Collectors produce it,
// the normalizer consumes it; it lives in domain so neither depends on the other.
type RawItem struct {
	Source    string
	SourceID  string
	Payload   []byte // original JSON (or other) payload, kept verbatim
	FetchedAt time.Time
}
