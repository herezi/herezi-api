// Package postgres is the PostGIS-backed store.SpotStore. SQL lives in
// queries.sql; sqlcdb/ is generated from it by sqlc (`make generate`).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/postgres/sqlcdb"
)

// Store implements store.SpotStore on PostgreSQL + PostGIS.
type Store struct {
	q *sqlcdb.Queries
}

var _ store.SpotStore = (*Store)(nil)

// New wraps a pgx pool (or any sqlcdb.DBTX).
func New(db sqlcdb.DBTX) *Store { return &Store{q: sqlcdb.New(db)} }

// Open connects to databaseURL and returns the store and the pool (caller closes it).
func Open(ctx context.Context, databaseURL string) (*Store, *pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return New(pool), pool, nil
}

// NearbySpots implements store.SpotReader.
func (s *Store) NearbySpots(ctx context.Context, q store.NearbyQuery) ([]domain.Spot, error) {
	cats := make([]string, 0, len(q.Categories)) // non-nil: NULL would match nothing
	for _, c := range q.Categories {
		cats = append(cats, string(c))
	}
	limit := q.Limit
	if limit <= 0 {
		limit = math.MaxInt32
	}
	rows, err := s.q.NearbySpots(ctx, sqlcdb.NearbySpotsParams{
		Lat: q.Lat, Lng: q.Lng, RadiusM: float64(q.RadiusM),
		Categories: cats, FreeOnly: q.FreeOnly,
		Now: ts(q.Now), WindowFrom: ts(q.From), WindowTo: ts(q.To),
		RowOffset: int32(q.Offset), RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: nearby spots: %w", err)
	}
	out := make([]domain.Spot, 0, len(rows))
	for _, r := range rows {
		d := int(math.Round(r.DistanceM))
		sp := toSpot(sqlcdb.GetSpotRow{
			ID: r.ID, Source: r.Source, SourceID: r.SourceID, Title: r.Title, Summary: r.Summary,
			Category: r.Category, Lat: r.Lat, Lng: r.Lng, Address: r.Address, PlaceName: r.PlaceName,
			StartsAt: r.StartsAt, EndsAt: r.EndsAt, IsFree: r.IsFree, ImageUrl: r.ImageUrl, SourceUrl: r.SourceUrl,
		})
		sp.DistanceM = &d
		out = append(out, sp)
	}
	return out, nil
}

// SpotByID implements store.SpotReader.
func (s *Store) SpotByID(ctx context.Context, id string, now time.Time) (domain.Spot, error) {
	r, err := s.q.GetSpot(ctx, sqlcdb.GetSpotParams{ID: id, Now: ts(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Spot{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Spot{}, fmt.Errorf("postgres: get spot: %w", err)
	}
	return toSpot(r), nil
}

// UpsertSpot implements store.SpotWriter.
func (s *Store) UpsertSpot(ctx context.Context, sp domain.Spot) error {
	p := sqlcdb.UpsertSpotParams{
		ID: sp.ID, Source: sp.Source, SourceID: sp.SourceID, Title: sp.Title, Summary: sp.Summary,
		Category: string(sp.Category), Lat: sp.Lat, Lng: sp.Lng, Address: sp.Address, PlaceName: sp.PlaceName,
		StartsAt: ts(sp.StartsAt), ImageUrl: sp.ImageURL, SourceUrl: sp.SourceURL,
	}
	if sp.EndsAt != nil {
		p.EndsAt = ts(*sp.EndsAt)
	}
	if sp.IsFree != nil {
		p.IsFree = pgtype.Bool{Bool: *sp.IsFree, Valid: true}
	}
	if err := s.q.UpsertSpot(ctx, p); err != nil {
		return fmt.Errorf("postgres: upsert spot %s: %w", sp.ID, err)
	}
	return nil
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func toSpot(r sqlcdb.GetSpotRow) domain.Spot {
	sp := domain.Spot{
		ID: r.ID, Source: r.Source, SourceID: r.SourceID, Title: r.Title, Summary: r.Summary,
		Category: domain.Category(r.Category), Lat: r.Lat, Lng: r.Lng, Address: r.Address,
		PlaceName: r.PlaceName, StartsAt: r.StartsAt.Time, ImageURL: r.ImageUrl, SourceURL: r.SourceUrl,
	}
	if r.EndsAt.Valid {
		t := r.EndsAt.Time
		sp.EndsAt = &t
	}
	if r.IsFree.Valid {
		b := r.IsFree.Bool
		sp.IsFree = &b
	}
	return sp
}
