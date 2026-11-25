// Package store defines persistence interfaces for spots.
// Implementations: store/memory (tests, local fake) and store/postgres (PostGIS + GORM).
// store may import only internal/domain (see ARCHITECTURE.md).
// 모든 구현체는 store/storetest 스위트를 통과해야 한다.
package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// ErrNotFound is returned by SpotByID when no spot exists.
var ErrNotFound = errors.New("store: not found")

// SpotFilter is shared by list queries (목록·지도). 상세(SpotByID)에는 적용하지 않는다.
//
// 구현체는 반드시 다음을 지킨다:
//   - ends_at < Now 인 항목은 반환하지 않는다 (끝난 행사 숨김).
//   - 기간 [From, To]와 겹치는 항목만 (domain.Spot.Overlaps).
//   - Categories가 비어 있으면 전체, 아니면 그중 하나.
//   - FreeOnly면 IsFree가 true인 항목만 (nil=모름은 제외).
type SpotFilter struct {
	Categories []domain.Category // empty = all
	FreeOnly   bool
	From, To   time.Time
	Now        time.Time
}

// Match reports whether sp passes the filter (reference semantics for every store).
func (f SpotFilter) Match(sp domain.Spot) bool {
	if sp.Ended(f.Now) || !sp.Overlaps(f.From, f.To) {
		return false
	}
	if len(f.Categories) > 0 && !slices.Contains(f.Categories, sp.Category) {
		return false
	}
	return !f.FreeOnly || sp.Free()
}

// NearbyQuery selects spots within RadiusM of (Lat, Lng) that match the filter,
// ordered by distance then ID. Offset/Limit page through the ordered result.
//
// Keyword가 비어 있지 않으면 제목·장소명·주소 중 하나에 대소문자 무시 부분 일치하는
// 항목만 반환한다(domain.Spot.MatchesKeyword). 호출자가 공백을 잘라서 넘긴다.
// 와일드카드(%, _)는 글자 그대로 취급한다.
type NearbyQuery struct {
	SpotFilter
	Keyword  string
	Lat, Lng float64
	RadiusM  int
	Offset   int
	Limit    int
}

// BBoxQuery selects spots inside BBox that match the filter, ordered by distance
// from the bbox center then ID, at most Limit.
type BBoxQuery struct {
	SpotFilter
	domain.BBox
	Limit int
}

// SpotReader is what the HTTP API needs.
// NearbySpots는 GET /v1/spots(근처)를 #74에서 지운 뒤 HTTP에서 쓰지 않는다(저장소 정리는 후속 작업).
// SpotsInBBox는 지역 데이터 파일(/v1/regions/{region}/spots)이 쓴다.
type SpotReader interface {
	NearbySpots(ctx context.Context, q NearbyQuery) ([]domain.Spot, error)
	SpotsInBBox(ctx context.Context, q BBoxQuery) ([]domain.Spot, error)
	// SpotByID returns ErrNotFound when the spot does not exist.
	// 끝난 행사(ends_at < now)도 반환한다 — 상세는 is_ended로 표시한다(TECH-PLAN.md §3).
	SpotByID(ctx context.Context, id string) (domain.Spot, error)
}

// SpotWriter is what the ingest pipeline needs.
// FetchedAt가 비어 있으면 구현체가 현재 시각으로 채운다.
// 원천 목록에서 빠졌다는 이유로는 지우지 않는다. 끝난 지 오래된 항목만 PruneEnded로 정리한다(이슈 #51).
type SpotWriter interface {
	UpsertSpot(ctx context.Context, s domain.Spot) error
	// PruneEnded는 ends_at < before인 항목을 지우고 지운 수를 돌려준다(종료 후 보존 기간이 지난 항목).
	PruneEnded(ctx context.Context, before time.Time) (int, error)
	// DeleteSpots는 ID로 지운다(수집 규칙·GPT가 볼거리가 아니라고 판정한 항목). 없는 ID는 무시한다.
	DeleteSpots(ctx context.Context, ids []string) (int, error)
}

// SpotStore is the full store.
type SpotStore interface {
	SpotReader
	SpotWriter
}

// 저장(찜) — 로그인한 사용자별 저장 목록(TECH-PLAN.md §4.1, 이슈 #56).

// MaxSavedPerUser는 사용자 한 명이 저장할 수 있는 최대 개수.
const MaxSavedPerUser = 500

// ErrSavedLimit는 저장하면 MaxSavedPerUser를 넘을 때.
var ErrSavedLimit = errors.New("store: saved limit exceeded")

// SavedSpot은 저장 목록의 한 항목(스팟 스냅샷 + 저장 시각).
type SavedSpot struct {
	Spot    domain.Spot
	SavedAt time.Time
}

// SavedQuery는 사용자의 저장 목록을 저장 시각 최신순(같으면 spot ID순)으로 고른다.
//
// HideEndedBefore가 0이 아니면 ends_at < HideEndedBefore인 항목(종료 후 보존 기간이 지남)은 뺀다.
// 보통은 수집 배치의 PruneEnded가 spots 행을 지울 때 저장 행도 함께 지워지지만(FK ON DELETE CASCADE),
// 정리가 늦어도 목록에 나오지 않게 한다.
type SavedQuery struct {
	UserID          string
	HideEndedBefore time.Time
	Offset          int
	Limit           int
}

// ImportResult는 ImportSaved 결과.
type ImportResult struct {
	Imported       int // 새로 저장한 수(이미 저장돼 있던 항목은 세지 않음)
	SkippedUnknown int // 서버에 없는(정리됐거나 잘못된) spot ID 수
}

// SavedStore는 저장 목록 저장소. userID는 Supabase 사용자 ID(UUID 소문자)다.
type SavedStore interface {
	// SaveSpot은 멱등이다: 이미 저장돼 있으면 saved_at을 바꾸지 않고 nil.
	// spot이 없으면 ErrNotFound, 개수 한도를 넘으면 ErrSavedLimit.
	SaveSpot(ctx context.Context, userID, spotID string, at time.Time) error
	// UnsaveSpot은 멱등이다: 저장돼 있지 않아도 nil.
	UnsaveSpot(ctx context.Context, userID, spotID string) error
	SavedSpots(ctx context.Context, q SavedQuery) ([]SavedSpot, error)
	// ImportSaved는 여러 spot을 한 번에 저장한다(로그인 전 기기 로컬 저장분 이전).
	// 없는 spot은 건너뛰고 센다. 한도를 넘으면 아무것도 저장하지 않고 ErrSavedLimit.
	ImportSaved(ctx context.Context, userID string, spotIDs []string, at time.Time) (ImportResult, error)
	// DeleteUserSaved는 사용자의 저장 행을 모두 지운다(회원 탈퇴).
	DeleteUserSaved(ctx context.Context, userID string) (int, error)
}

// MaxNotices는 공지 목록(GET /v1/notices)이 돌려주는 최대 개수.
const MaxNotices = 50

// NoticeReader는 공지사항 읽기 저장소(이슈 #80). 게시된 공지(domain.Notice.Published)만 돌려준다.
type NoticeReader interface {
	// Notices는 now 기준 게시된 공지를 게시 시각 최신순(같으면 ID 큰 것 먼저)으로 최대 limit개 돌려준다.
	// 목록용이라 Body는 비어 있다.
	Notices(ctx context.Context, now time.Time, limit int) ([]domain.Notice, error)
	// NoticeByID는 게시된 공지 하나를 본문과 함께 돌려준다. 없거나 숨김·게시 전이면 ErrNotFound.
	NoticeByID(ctx context.Context, id int64, now time.Time) (domain.Notice, error)
}

// NoticeStore는 NoticeReader + 쓰기(테스트·예시 데이터용. 운영 작성은 Supabase Table Editor).
type NoticeStore interface {
	NoticeReader
	// InsertNotice는 공지를 추가하고 새 ID를 돌려준다(n.ID는 무시).
	InsertNotice(ctx context.Context, n domain.Notice) (int64, error)
}
