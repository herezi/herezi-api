// Package domain holds Herezi's core types. It must not import any other
// internal package (see ARCHITECTURE.md); everything else depends on it.
package domain

import (
	"fmt"
	"math"
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

// BBox is a map viewport given by its south-west and north-east corners.
type BBox struct {
	SwLat, SwLng, NeLat, NeLng float64
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
	// FetchedAt is when the item was last fetched from its source (앱의 "갱신 시각").
	FetchedAt time.Time
	// ContentHash는 GPT 보강 입력(원문)의 해시. 같으면 이전 보강 결과(요약·분류·무료)를
	// 재사용하고 GPT를 다시 부르지 않는다(TECH-PLAN.md §2). API 응답에는 나가지 않는다.
	ContentHash string
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
// 끝난 행사(ends_at < now)는 목록·지도에서 숨기고, 상세에서는 is_ended=true로 반환한다 (TECH-PLAN.md §3).
// 날짜만 있는 종료일은 수집 시 그날 23:59:59 KST로 저장되므로 여기선 시각 비교만 한다 (DATA-PLAN.md Q1).
func (s Spot) Ended(now time.Time) bool {
	return s.EndsAt != nil && s.EndsAt.Before(now)
}

// MatchesKeyword reports whether kw appears (case-insensitive substring) in the
// title, place name or address. 빈 kw는 모두 일치. 호출자가 공백을 잘라서 넘긴다.
// postgres 저장소의 ILIKE '%kw%'와 같은 의미다.
func (s Spot) MatchesKeyword(kw string) bool {
	if kw == "" {
		return true
	}
	kw = strings.ToLower(kw)
	for _, f := range []string{s.Title, s.PlaceName, s.Address} {
		if strings.Contains(strings.ToLower(f), kw) {
			return true
		}
	}
	return false
}

// Free reports whether the spot is known to be free. Unknown (nil) counts as not free.
func (s Spot) Free() bool { return s.IsFree != nil && *s.IsFree }

// Center returns the midpoint of the bbox (no antimeridian support).
func (b BBox) Center() (lat, lng float64) {
	return (b.SwLat + b.NeLat) / 2, (b.SwLng + b.NeLng) / 2
}

// Contains reports whether (lat, lng) is inside the bbox, edges included.
func (b BBox) Contains(lat, lng float64) bool {
	return lat >= b.SwLat && lat <= b.NeLat && lng >= b.SwLng && lng <= b.NeLng
}

// Validate checks ranges and that sw is south-west of ne.
// 날짜변경선을 넘는 영역(SwLng > NeLng)은 지원하지 않는다.
func (b BBox) Validate() error {
	for _, v := range []struct {
		name string
		val  float64
		max  float64
	}{{"sw_lat", b.SwLat, 90}, {"ne_lat", b.NeLat, 90}, {"sw_lng", b.SwLng, 180}, {"ne_lng", b.NeLng, 180}} {
		if math.IsNaN(v.val) || math.Abs(v.val) > v.max {
			return fmt.Errorf("%s must be a number in [-%g, %g]", v.name, v.max, v.max)
		}
	}
	if b.SwLat > b.NeLat {
		return fmt.Errorf("sw_lat must be <= ne_lat")
	}
	if b.SwLng > b.NeLng {
		return fmt.Errorf("sw_lng must be <= ne_lng (antimeridian-crossing bbox is not supported)")
	}
	return nil
}

// RawItem is an unnormalized record fetched by a collector. Collectors produce it,
// the normalizer consumes it; it lives in domain so neither depends on the other.
type RawItem struct {
	Source    string
	SourceID  string
	Payload   []byte // original JSON (or other) payload, kept verbatim
	FetchedAt time.Time
}
