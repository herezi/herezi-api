package memory

import (
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// SeedSeoul returns a few sample spots around central Seoul, relative to now,
// so the API is usable locally without a database. 실제 데이터가 아닌 예시다.
func SeedSeoul(now time.Time) []domain.Spot {
	day := 24 * time.Hour
	ptr := func(t time.Time) *time.Time { return &t }
	yes, no := true, false
	mk := func(id, title, summary string, c domain.Category, lat, lng float64, place string,
		start time.Time, end *time.Time, free *bool) domain.Spot {
		return domain.Spot{
			ID: domain.SpotID("seed", id), Source: "seed", SourceID: id,
			Title: title, Summary: summary, Category: c, Lat: lat, Lng: lng,
			PlaceName: place, StartsAt: start, EndsAt: end, IsFree: free,
		}
	}
	return []domain.Spot{
		mk("1", "서울광장 겨울 축제", "서울광장에서 열리는 예시 축제.", domain.CategoryFestival,
			37.5663, 126.9779, "서울광장", now.Add(-2*day), ptr(now.Add(5*day)), &yes),
		mk("2", "덕수궁 야간 산책", "덕수궁 돌담길 야간 개방 예시.", domain.CategoryNature,
			37.5658, 126.9751, "덕수궁", now.Add(-30*day), ptr(now.Add(30*day)), &no),
		mk("3", "국립현대미술관 기획전", "서울관 기획 전시 예시.", domain.CategoryExhibition,
			37.5789, 126.9800, "국립현대미술관 서울", now.Add(-10*day), ptr(now.Add(40*day)), &no),
		mk("4", "성수동 브랜드 팝업", "성수동 팝업스토어 예시.", domain.CategoryPopup,
			37.5446, 127.0557, "성수동", now.Add(-1*day), ptr(now.Add(3*day)), &yes),
		mk("5", "광화문 버스킹", "광화문광장 무료 공연 예시.", domain.CategoryPerformance,
			37.5720, 126.9769, "광화문광장", now.Add(2*day), ptr(now.Add(2*day+3*time.Hour)), &yes),
		mk("6", "지난 전시 (끝남)", "끝난 행사는 목록·지도에 안 나오고 상세에서만 종료됨으로 보인다.", domain.CategoryExhibition,
			37.5700, 126.9830, "인사동", now.Add(-20*day), ptr(now.Add(-1*day)), &no),
	}
}
