// Package postgres is the PostGIS-backed store.SpotStore, accessed through GORM (pgx driver).
//
// 스키마의 기준은 migrations/(goose SQL)이다. AutoMigrate는 쓰지 않는다 — geography 컬럼과
// GiST 인덱스를 표현할 수 없다. 공간 조건(ST_DWithin, ST_Distance 등)은 GORM raw 표현식으로 쓴다.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// spotRow is the GORM model for the spots table. geog is never read directly:
// lat/lng/distance_m are computed columns in SELECT (read-only, `->`).
type spotRow struct {
	ID          string     `gorm:"column:id;primaryKey"`
	Source      string     `gorm:"column:source"`
	SourceID    string     `gorm:"column:source_id"`
	Title       string     `gorm:"column:title"`
	Summary     string     `gorm:"column:summary"`
	Category    string     `gorm:"column:category"`
	Lat         float64    `gorm:"column:lat;->"`
	Lng         float64    `gorm:"column:lng;->"`
	Address     string     `gorm:"column:address"`
	PlaceName   string     `gorm:"column:place_name"`
	StartsAt    time.Time  `gorm:"column:starts_at"`
	EndsAt      *time.Time `gorm:"column:ends_at"`
	IsFree      *bool      `gorm:"column:is_free"`
	ImageURL    string     `gorm:"column:image_url"`
	SourceURL   string     `gorm:"column:source_url"`
	FetchedAt   time.Time  `gorm:"column:fetched_at"`
	ContentHash string     `gorm:"column:content_hash"`
	DistanceM   *float64   `gorm:"column:distance_m;->"`
}

func (spotRow) TableName() string { return "spots" }

// spotColumns selects every spotRow column; lat/lng come from the geography point.
const spotColumns = "id, source, source_id, title, summary, category, " +
	"ST_Y(geog::geometry) AS lat, ST_X(geog::geometry) AS lng, " +
	"address, place_name, starts_at, ends_at, is_free, image_url, source_url, fetched_at, content_hash"

// Store implements store.SpotStore on PostgreSQL + PostGIS.
type Store struct {
	db *gorm.DB
}

var _ store.SpotStore = (*Store)(nil)

// New wraps an existing *gorm.DB.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// Open connects to databaseURL. Call Close when done.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := gorm.Open(pgdriver.Open(databaseURL), &gorm.Config{Logger: logger.Discard, SkipDefaultTransaction: true})
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return New(db), nil
}

// Close closes the underlying connection pool.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// point is a geography point expression (PostGIS takes lng, lat order).
func point(lat, lng float64) clause.Expr {
	return gorm.Expr("ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography", lng, lat)
}

// filter applies store.SpotFilter (see its doc for the rules).
func filter(f store.SpotFilter) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(f.Categories) > 0 {
			cats := make([]string, 0, len(f.Categories))
			for _, c := range f.Categories {
				cats = append(cats, string(c))
			}
			db = db.Where("category IN ?", cats)
		}
		if f.FreeOnly {
			db = db.Where("is_free IS TRUE")
		}
		return db.
			Where("(ends_at IS NULL OR ends_at >= ?)", f.Now). // 끝난 행사 숨김
			Where("starts_at <= ?", f.To).
			Where("COALESCE(ends_at, starts_at) >= ?", f.From)
	}
}

// likeEscaper escapes LIKE metacharacters so the keyword matches literally (ESCAPE '\').
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// keyword applies store.NearbyQuery.Keyword: 제목·장소명·주소 ILIKE 부분 일치.
// migrations/00003의 pg_trgm GIN 인덱스를 탄다(3글자 이상일 때 효과적).
func keyword(kw string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if kw == "" {
			return db
		}
		pat := "%" + likeEscaper.Replace(kw) + "%"
		return db.Where(`(title ILIKE ? ESCAPE '\' OR place_name ILIKE ? ESCAPE '\' OR address ILIKE ? ESCAPE '\')`, pat, pat, pat)
	}
}

// NearbySpots implements store.SpotReader.
func (s *Store) NearbySpots(ctx context.Context, q store.NearbyQuery) ([]domain.Spot, error) {
	pt := point(q.Lat, q.Lng)
	tx := s.db.WithContext(ctx).Model(&spotRow{}).
		Select(spotColumns+", ST_Distance(geog, ?) AS distance_m", pt).
		Where("ST_DWithin(geog, ?, ?)", pt, float64(q.RadiusM)).
		Scopes(filter(q.SpotFilter), keyword(q.Keyword)).
		Order("distance_m, id").
		Offset(q.Offset)
	if q.Limit > 0 {
		tx = tx.Limit(q.Limit)
	}
	var rows []spotRow
	if err := tx.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("postgres: nearby spots: %w", err)
	}
	out := make([]domain.Spot, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, nil
}

// SpotsInBBox implements store.SpotReader. 점 데이터라 평면 bbox 겹침(geometry &&)이
// 정확하고, migrations/00002의 (geog::geometry) GiST 인덱스를 탄다.
func (s *Store) SpotsInBBox(ctx context.Context, q store.BBoxQuery) ([]domain.Spot, error) {
	cLat, cLng := q.Center()
	tx := s.db.WithContext(ctx).Model(&spotRow{}).
		Select(spotColumns).
		Where("geog::geometry && ST_MakeEnvelope(?, ?, ?, ?, 4326)", q.SwLng, q.SwLat, q.NeLng, q.NeLat).
		Scopes(filter(q.SpotFilter)).
		Order(clause.OrderBy{Expression: clause.Expr{
			SQL:  "ST_Distance(geog, ?), id",
			Vars: []any{point(cLat, cLng)},
		}})
	if q.Limit > 0 {
		tx = tx.Limit(q.Limit)
	}
	var rows []spotRow
	if err := tx.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("postgres: spots in bbox: %w", err)
	}
	out := make([]domain.Spot, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, nil
}

// SpotByID implements store.SpotReader. 끝난 행사도 반환한다(is_ended는 핸들러가 계산).
func (s *Store) SpotByID(ctx context.Context, id string) (domain.Spot, error) {
	var r spotRow
	err := s.db.WithContext(ctx).Model(&spotRow{}).
		Select(spotColumns).
		Where("id = ?", id).
		Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Spot{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Spot{}, fmt.Errorf("postgres: get spot: %w", err)
	}
	return r.toDomain(), nil
}

// upsertColumns are overwritten when (source, source_id) already exists.
var upsertColumns = []string{
	"title", "summary", "category", "geog", "address", "place_name",
	"starts_at", "ends_at", "is_free", "image_url", "source_url", "fetched_at", "content_hash", "last_seen_at",
}

// UpsertSpot implements store.SpotWriter. A zero FetchedAt becomes now.
func (s *Store) UpsertSpot(ctx context.Context, sp domain.Spot) error {
	if sp.FetchedAt.IsZero() {
		sp.FetchedAt = time.Now()
	}
	values := map[string]any{
		"id": sp.ID, "source": sp.Source, "source_id": sp.SourceID,
		"title": sp.Title, "summary": sp.Summary, "category": string(sp.Category),
		"geog":    point(sp.Lat, sp.Lng),
		"address": sp.Address, "place_name": sp.PlaceName,
		"starts_at": sp.StartsAt, "ends_at": sp.EndsAt, "is_free": sp.IsFree,
		"image_url": sp.ImageURL, "source_url": sp.SourceURL, "fetched_at": sp.FetchedAt,
		"content_hash": sp.ContentHash,
		"last_seen_at": gorm.Expr("now()"), // 수집에서 마지막으로 본 시각(원천에서 빠져도 행은 남긴다)
	}
	err := s.db.WithContext(ctx).Model(&spotRow{}).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source"}, {Name: "source_id"}},
			DoUpdates: append(clause.AssignmentColumns(upsertColumns),
				clause.Assignment{Column: clause.Column{Name: "updated_at"}, Value: gorm.Expr("now()")}),
		}).
		Create(values).Error
	if err != nil {
		return fmt.Errorf("postgres: upsert spot %s: %w", sp.ID, err)
	}
	return nil
}

// PruneEnded implements store.SpotWriter: ends_at < before인 행을 지운다.
func (s *Store) PruneEnded(ctx context.Context, before time.Time) (int, error) {
	res := s.db.WithContext(ctx).Where("ends_at < ?", before).Delete(&spotRow{})
	if res.Error != nil {
		return 0, fmt.Errorf("postgres: prune ended: %w", res.Error)
	}
	return int(res.RowsAffected), nil
}

// DeleteSpots implements store.SpotWriter.
func (s *Store) DeleteSpots(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&spotRow{})
	if res.Error != nil {
		return 0, fmt.Errorf("postgres: delete spots: %w", res.Error)
	}
	return int(res.RowsAffected), nil
}

// EnrichedSpots는 GPT 보강 결과가 있는(content_hash와 요약이 채워진) 항목을 모두 돌려준다.
// 수집 배치가 원문 해시 캐시로 쓴다 — 해시가 같으면 GPT를 다시 부르지 않는다.
func (s *Store) EnrichedSpots(ctx context.Context) ([]domain.Spot, error) {
	var rows []spotRow
	err := s.db.WithContext(ctx).Model(&spotRow{}).
		Select(spotColumns).
		Where("content_hash <> '' AND summary <> ''").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("postgres: enriched spots: %w", err)
	}
	out := make([]domain.Spot, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, nil
}

func (r spotRow) toDomain() domain.Spot {
	sp := domain.Spot{
		ID: r.ID, Source: r.Source, SourceID: r.SourceID, Title: r.Title, Summary: r.Summary,
		Category: domain.Category(r.Category), Lat: r.Lat, Lng: r.Lng, Address: r.Address,
		PlaceName: r.PlaceName, StartsAt: r.StartsAt, EndsAt: r.EndsAt, IsFree: r.IsFree,
		ImageURL: r.ImageURL, SourceURL: r.SourceURL, FetchedAt: r.FetchedAt, ContentHash: r.ContentHash,
	}
	if r.DistanceM != nil {
		d := int(math.Round(*r.DistanceM))
		sp.DistanceM = &d
	}
	return sp
}
