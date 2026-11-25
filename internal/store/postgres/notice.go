package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

var _ store.NoticeStore = (*Store)(nil)

// noticeRow is the GORM model for notices (migrations/00008).
// created_at·updated_at은 DB 기본값·트리거가 채우므로 모델에 두지 않는다.
type noticeRow struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Title       string    `gorm:"column:title"`
	Body        string    `gorm:"column:body"`
	PublishedAt time.Time `gorm:"column:published_at"`
	IsHidden    bool      `gorm:"column:is_hidden"`
}

func (noticeRow) TableName() string { return "notices" }

func (r noticeRow) toDomain() domain.Notice {
	return domain.Notice{ID: r.ID, Title: r.Title, Body: r.Body, PublishedAt: r.PublishedAt, IsHidden: r.IsHidden}
}

// publishedNotices narrows to notices visible at now (domain.Notice.Published와 같은 규칙).
func (s *Store) publishedNotices(ctx context.Context, now time.Time) *gorm.DB {
	return s.db.WithContext(ctx).Model(&noticeRow{}).
		Where("published_at <= ? AND NOT is_hidden", now)
}

// Notices implements store.NoticeReader.
func (s *Store) Notices(ctx context.Context, now time.Time, limit int) ([]domain.Notice, error) {
	q := s.publishedNotices(ctx, now).
		Select("id, title, published_at, is_hidden").
		Order("published_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []noticeRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("postgres: list notices: %w", err)
	}
	out := make([]domain.Notice, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toDomain())
	}
	return out, nil
}

// NoticeByID implements store.NoticeReader.
func (s *Store) NoticeByID(ctx context.Context, id int64, now time.Time) (domain.Notice, error) {
	var r noticeRow
	err := s.publishedNotices(ctx, now).Where("id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Notice{}, store.ErrNotFound
	}
	if err != nil {
		return domain.Notice{}, fmt.Errorf("postgres: get notice: %w", err)
	}
	return r.toDomain(), nil
}

// InsertNotice implements store.NoticeStore.
func (s *Store) InsertNotice(ctx context.Context, n domain.Notice) (int64, error) {
	r := noticeRow{Title: n.Title, Body: n.Body, PublishedAt: n.PublishedAt, IsHidden: n.IsHidden}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return 0, fmt.Errorf("postgres: insert notice: %w", err)
	}
	return r.ID, nil
}
