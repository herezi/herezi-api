package domain

import "time"

// Region은 앱이 통째로 받아 가는 지역 데이터 파일의 단위다(GET /v1/regions/{region}/spots, 이슈 #74).
// 앱은 사용자 좌표를 서버로 보내지 않고, 받은 지역 데이터로 거리·근처·지도를 기기에서 계산한다
// (SERVICE-PLAN.md §12 Q8).
type Region struct {
	ID   string // 경로 값, 예: "seoul"
	BBox BBox   // 이 지역에 넣을 볼거리 좌표 범위
}

// RegionAheadDays는 지역 파일에 넣는 '앞으로 시작할' 항목의 범위(일).
// 수집 배치의 '90일 넘는 미래 제외' 규칙과 같은 창이다(DATA-PLAN.md §6.6).
const RegionAheadDays = 90

// regions는 지원하는 지역 목록. 서울 bbox는 수집 범위(ingest.InSeoul)와 같은 값이다.
var regions = []Region{
	{ID: "seoul", BBox: BBox{SwLat: 37.413, SwLng: 126.734, NeLat: 37.716, NeLng: 127.270}},
}

// Regions returns all supported regions in spec order (api/openapi.yaml region enum).
func Regions() []Region { return append([]Region(nil), regions...) }

// RegionByID finds a supported region.
func RegionByID(id string) (Region, bool) {
	for _, r := range regions {
		if r.ID == id {
			return r, true
		}
	}
	return Region{}, false
}

// RegionWindow는 지역 파일에 넣을 기간 [from, to]를 돌려준다. 이 구간과 겹치는(Spot.Overlaps) 항목을 넣는다:
//   - 진행 중이거나 to(= now + RegionAheadDays일) 전에 시작하는 항목
//   - 끝났어도 종료가 from(= now - RetainEndedDays일) 이후인 항목 — 저장·상세가 오프라인에서도 보이게
func RegionWindow(now time.Time) (from, to time.Time) {
	return now.AddDate(0, 0, -RetainEndedDays), now.AddDate(0, 0, RegionAheadDays)
}
