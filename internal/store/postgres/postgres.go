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
	ID        string     `gorm:"column:id;primaryKey"`
	Source    string     `gorm:"column:source"`
	SourceID  string     `gorm:"column:source_id"`
	Title     string     `gorm:"column:title"`
	Summary   string     `gorm:"column:summary"`
	Category  string     `gorm:"column:category"`
	Lat       float64    `gorm:"column:lat;->"`
	Lng       float64    `gorm:"column:lng;->"`
	Address   string     `gorm:"column:address"`
	PlaceName string     `gorm:"column:place_name"`
	StartsAt  time.Time  `gorm:"column:starts_at"`
	EndsAt    *time.Time `gorm:"column:ends_at"`
	IsFree    *bool      `gorm:"column:is_free"`
	ImageURL  string     `gorm:"column:image_url"`
	SourceURL string     `gorm:"column:source_url"`
	DistanceM *float64   `gorm:"column:distance_m;->"`
}

func (spotRow) TableName() string { return "spots" }

// spotColumns selects every spotRow column; lat/lng come from the geography point.
const spotColumns = "id, source, source_id, title, summary, category, " +
	"ST_Y(geog::geometry) AS lat, ST_X(geog::geometry) AS lng, " +
	"address, place_name, starts_at, ends_at, is_free, image_url, source_url"

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

// listFilter holds the conditions shared by list queries (see store.NearbyQuery doc).
type listFilter struct {
	Categories    []domain.Category
	FreeOnly      bool
	From, To, Now time.Time
}

// filter applies the shared list conditions.
func filter(f listFilter) func(*gorm.DB) *gorm.DB {
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

// NearbySpots implements store.SpotReader.
func (s *Store) NearbySpots(ctx context.Context, q store.NearbyQuery) ([]domain.Spot, error) {
	pt := point(q.Lat, q.Lng)
	tx := s.db.WithContext(ctx).Model(&spotRow{}).
		Select(spotColumns+", ST_Distance(geog, ?) AS distance_m", pt).
		Where("ST_DWithin(geog, ?, ?)", pt, float64(q.RadiusM)).
		Scopes(filter(listFilter{q.Categories, q.FreeOnly, q.From, q.To, q.Now})).
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

// SpotByID implements store.SpotReader.
func (s *Store) SpotByID(ctx context.Context, id string, now time.Time) (domain.Spot, error) {
	var r spotRow
	err := s.db.WithContext(ctx).Model(&spotRow{}).
		Select(spotColumns).
		Where("id = ?", id).
		Where("(ends_at IS NULL OR ends_at >= ?)", now).
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
	"starts_at", "ends_at", "is_free", "image_url", "source_url",
}

// UpsertSpot implements store.SpotWriter.
func (s *Store) UpsertSpot(ctx context.Context, sp domain.Spot) error {
	values := map[string]any{
		"id": sp.ID, "source": sp.Source, "source_id": sp.SourceID,
		"title": sp.Title, "summary": sp.Summary, "category": string(sp.Category),
		"geog":    point(sp.Lat, sp.Lng),
		"address": sp.Address, "place_name": sp.PlaceName,
		"starts_at": sp.StartsAt, "ends_at": sp.EndsAt, "is_free": sp.IsFree,
		"image_url": sp.ImageURL, "source_url": sp.SourceURL,
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

func (r spotRow) toDomain() domain.Spot {
	sp := domain.Spot{
		ID: r.ID, Source: r.Source, SourceID: r.SourceID, Title: r.Title, Summary: r.Summary,
		Category: domain.Category(r.Category), Lat: r.Lat, Lng: r.Lng, Address: r.Address,
		PlaceName: r.PlaceName, StartsAt: r.StartsAt, EndsAt: r.EndsAt, IsFree: r.IsFree,
		ImageURL: r.ImageURL, SourceURL: r.SourceURL,
	}
	if r.DistanceM != nil {
		d := int(math.Round(*r.DistanceM))
		sp.DistanceM = &d
	}
	return sp
}
