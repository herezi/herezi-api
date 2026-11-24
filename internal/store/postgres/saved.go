package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/herezi/herezi-api/internal/store"
)

var _ store.SavedStore = (*Store)(nil)

// savedRow is the GORM model for saved_spots (migrations/00007).
type savedRow struct {
	UserID  string    `gorm:"column:user_id;primaryKey"`
	SpotID  string    `gorm:"column:spot_id;primaryKey"`
	SavedAt time.Time `gorm:"column:saved_at"`
}

func (savedRow) TableName() string { return "saved_spots" }

// savedSpotRow is a spots row joined with saved_at.
type savedSpotRow struct {
	spotRow `gorm:"embedded"`
	SavedAt time.Time `gorm:"column:saved_at;->"`
}

// pgForeignKeyViolation is SQLSTATE 23503 (spot_id가 spots에 없음).
const pgForeignKeyViolation = "23503"

func isFKViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == pgForeignKeyViolation
}

// SaveSpot implements store.SavedStore.
// 한도 확인과 삽입을 한 문장으로 해서 동시에 여러 요청이 와도 한도를 크게 넘지 않게 한다.
func (s *Store) SaveSpot(ctx context.Context, userID, spotID string, at time.Time) error {
	db := s.db.WithContext(ctx)
	res := db.Exec(`INSERT INTO saved_spots (user_id, spot_id, saved_at)
		SELECT ?::uuid, ?, ?::timestamptz
		WHERE EXISTS (SELECT 1 FROM spots WHERE id = ?)
		  AND (SELECT count(*) FROM saved_spots WHERE user_id = ?) < ?
		ON CONFLICT (user_id, spot_id) DO NOTHING`,
		userID, spotID, at, spotID, userID, store.MaxSavedPerUser)
	if res.Error != nil {
		if isFKViolation(res.Error) { // 확인과 삽입 사이에 spot이 지워진 경우
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres: save spot: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		return nil
	}
	// 0행: 이미 저장됨(멱등) / spot 없음 / 한도 초과 중 무엇인지 가린다.
	var st struct {
		Saved bool
		Spot  bool
	}
	err := db.Raw(`SELECT
		EXISTS (SELECT 1 FROM saved_spots WHERE user_id = ? AND spot_id = ?) AS saved,
		EXISTS (SELECT 1 FROM spots WHERE id = ?) AS spot`, userID, spotID, spotID).Scan(&st).Error
	if err != nil {
		return fmt.Errorf("postgres: save spot: %w", err)
	}
	switch {
	case st.Saved:
		return nil
	case !st.Spot:
		return store.ErrNotFound
	}
	return store.ErrSavedLimit
}

// UnsaveSpot implements store.SavedStore.
func (s *Store) UnsaveSpot(ctx context.Context, userID, spotID string) error {
	err := s.db.WithContext(ctx).Where("user_id = ? AND spot_id = ?", userID, spotID).Delete(&savedRow{}).Error
	if err != nil {
		return fmt.Errorf("postgres: unsave spot: %w", err)
	}
	return nil
}

// SavedSpots implements store.SavedStore.
func (s *Store) SavedSpots(ctx context.Context, q store.SavedQuery) ([]store.SavedSpot, error) {
	tx := s.db.WithContext(ctx).Table("saved_spots AS ss").
		Select("ss.saved_at, "+qualifiedSpotColumns).
		Joins("JOIN spots ON spots.id = ss.spot_id").
		Where("ss.user_id = ?", q.UserID).
		Order("ss.saved_at DESC, ss.spot_id").
		Offset(q.Offset)
	if !q.HideEndedBefore.IsZero() {
		tx = tx.Where("(spots.ends_at IS NULL OR spots.ends_at >= ?)", q.HideEndedBefore)
	}
	if q.Limit > 0 {
		tx = tx.Limit(q.Limit)
	}
	var rows []savedSpotRow
	if err := tx.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("postgres: saved spots: %w", err)
	}
	out := make([]store.SavedSpot, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.SavedSpot{Spot: r.toDomain(), SavedAt: r.SavedAt})
	}
	return out, nil
}

// qualifiedSpotColumns is spotColumns with the spots. prefix (조인용).
const qualifiedSpotColumns = "spots.id, spots.source, spots.source_id, spots.title, spots.summary, spots.category, " +
	"ST_Y(spots.geog::geometry) AS lat, ST_X(spots.geog::geometry) AS lng, " +
	"spots.address, spots.place_name, spots.starts_at, spots.ends_at, spots.is_free, spots.image_url, " +
	"spots.source_url, spots.fetched_at, spots.content_hash"

// ImportSaved implements store.SavedStore. 한 트랜잭션에서 사용자 행을 잠그듯 순서대로 처리한다.
func (s *Store) ImportSaved(ctx context.Context, userID string, spotIDs []string, at time.Time) (store.ImportResult, error) {
	ids := slices.Compact(slices.Sorted(slices.Values(spotIDs)))
	var res store.ImportResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 같은 사용자의 동시 이전·저장이 한도를 넘지 않게 사용자 단위로 직렬화한다(트랜잭션 끝에 풀림).
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "saved:"+userID).Error; err != nil {
			return err
		}
		var known []string
		if len(ids) > 0 {
			if err := tx.Table("spots").Where("id IN ?", ids).Pluck("id", &known).Error; err != nil {
				return err
			}
		}
		res.SkippedUnknown = len(ids) - len(known)
		var count int64
		if err := tx.Model(&savedRow{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
			return err
		}
		var already int64
		if len(known) > 0 {
			if err := tx.Model(&savedRow{}).Where("user_id = ? AND spot_id IN ?", userID, known).Count(&already).Error; err != nil {
				return err
			}
		}
		if int(count)+len(known)-int(already) > store.MaxSavedPerUser {
			return store.ErrSavedLimit
		}
		if len(known) == 0 {
			return nil
		}
		rows := make([]savedRow, 0, len(known))
		for _, id := range known {
			rows = append(rows, savedRow{UserID: userID, SpotID: id, SavedAt: at})
		}
		r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows)
		if r.Error != nil {
			return r.Error
		}
		res.Imported = int(r.RowsAffected)
		return nil
	})
	if errors.Is(err, store.ErrSavedLimit) {
		return store.ImportResult{}, err
	}
	if err != nil {
		return store.ImportResult{}, fmt.Errorf("postgres: import saved: %w", err)
	}
	return res, nil
}

// DeleteUserSaved implements store.SavedStore.
func (s *Store) DeleteUserSaved(ctx context.Context, userID string) (int, error) {
	res := s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&savedRow{})
	if res.Error != nil {
		return 0, fmt.Errorf("postgres: delete user saved: %w", res.Error)
	}
	return int(res.RowsAffected), nil
}
