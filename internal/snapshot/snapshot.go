// Package snapshot은 수집 결과(domain.Spot 목록)를 JSON 파일로 쓰고 읽는다.
// DATABASE_URL이 없을 때 cmd/ingest가 쓰고(-out), cmd/api가 SNAPSHOT_PATH로 읽어
// 메모리 저장소에 올린다. 로컬에서 앱이 DB 없이 실데이터로 동작하게 하는 용도다.
// domain 외의 internal 패키지를 import하지 않는다.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// Version은 파일 형식 버전. 호환되지 않게 바꾸면 올린다.
const Version = 1

// File은 스냅샷 파일 전체.
type File struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Spots       []Spot    `json:"spots"`
}

// Spot은 domain.Spot의 JSON 표현. API 응답과 달리 content_hash를 포함한다.
type Spot struct {
	ID          string     `json:"id"`
	Source      string     `json:"source"`
	SourceID    string     `json:"source_id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary,omitempty"`
	Category    string     `json:"category"`
	Lat         float64    `json:"lat"`
	Lng         float64    `json:"lng"`
	Address     string     `json:"address,omitempty"`
	PlaceName   string     `json:"place_name,omitempty"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at,omitempty"`
	IsFree      *bool      `json:"is_free,omitempty"`
	ImageURL    string     `json:"image_url,omitempty"`
	SourceURL   string     `json:"source_url,omitempty"`
	FetchedAt   time.Time  `json:"fetched_at"`
	ContentHash string     `json:"content_hash,omitempty"`
}

func fromDomain(s domain.Spot) Spot {
	return Spot{
		ID: s.ID, Source: s.Source, SourceID: s.SourceID, Title: s.Title, Summary: s.Summary,
		Category: string(s.Category), Lat: s.Lat, Lng: s.Lng, Address: s.Address, PlaceName: s.PlaceName,
		StartsAt: s.StartsAt, EndsAt: s.EndsAt, IsFree: s.IsFree, ImageURL: s.ImageURL,
		SourceURL: s.SourceURL, FetchedAt: s.FetchedAt, ContentHash: s.ContentHash,
	}
}

func (s Spot) toDomain() domain.Spot {
	return domain.Spot{
		ID: s.ID, Source: s.Source, SourceID: s.SourceID, Title: s.Title, Summary: s.Summary,
		Category: domain.Category(s.Category), Lat: s.Lat, Lng: s.Lng, Address: s.Address, PlaceName: s.PlaceName,
		StartsAt: s.StartsAt, EndsAt: s.EndsAt, IsFree: s.IsFree, ImageURL: s.ImageURL,
		SourceURL: s.SourceURL, FetchedAt: s.FetchedAt, ContentHash: s.ContentHash,
	}
}

// Write는 spots를 path에 원자적으로(임시 파일 → rename) 쓴다. 상위 디렉터리가 없으면 만든다.
func Write(path string, generatedAt time.Time, spots []domain.Spot) error {
	f := File{Version: Version, GeneratedAt: generatedAt, Spots: make([]Spot, 0, len(spots))}
	for _, s := range spots {
		f.Spots = append(f.Spots, fromDomain(s))
	}
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return fmt.Errorf("snapshot: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("snapshot: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("snapshot: rename: %w", err)
	}
	return nil
}

// Read는 path의 스냅샷을 읽는다. 알 수 없는 카테고리는 에러다.
func Read(path string) ([]domain.Spot, time.Time, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("snapshot: %w", err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, time.Time{}, fmt.Errorf("snapshot: decode: %w", err)
	}
	if f.Version != Version {
		return nil, time.Time{}, fmt.Errorf("snapshot: unsupported version %d", f.Version)
	}
	out := make([]domain.Spot, 0, len(f.Spots))
	for _, s := range f.Spots {
		sp := s.toDomain()
		if !sp.Category.Valid() {
			return nil, time.Time{}, fmt.Errorf("snapshot: spot %s: unknown category %q", sp.ID, sp.Category)
		}
		out = append(out, sp)
	}
	return out, f.GeneratedAt, nil
}
