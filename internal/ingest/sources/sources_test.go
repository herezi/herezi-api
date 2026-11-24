package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/ingest"
)

// 픽스처는 2026-11-24 실제 응답에서 몇 건만 잘라 정리한 것(키 없음).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var fetchedAt = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)

func kst(y int, m time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, m, d, h, mi, s, 0, ingest.KST).UTC()
}

func TestParseSeoulCulture(t *testing.T) {
	recs, total, err := ParseSeoulCulture(fixture(t, "seoul_culture.json"), fetchedAt)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(recs) != 3 {
		t.Fatalf("total=%d len=%d", total, len(recs))
	}
	r := recs[0]
	s := r.Spot
	if s.ID != "seoul_culture:160198" || s.SourceID != "160198" || s.Source != "seoul_culture" {
		t.Errorf("id = %q", s.ID)
	}
	if s.Category != domain.CategoryPerformance || !r.CategorySure {
		t.Errorf("무용 → performance(sure), got %s %v", s.Category, r.CategorySure)
	}
	if s.Lat != 37.4856046634033 || s.Lng != 127.028078834421 {
		t.Errorf("coords = %v,%v (LAT=위도, LOT=경도)", s.Lat, s.Lng)
	}
	if !s.StartsAt.Equal(kst(2027, 2, 12, 0, 0, 0)) || !s.EndsAt.Equal(kst(2027, 2, 28, 23, 59, 59)) {
		t.Errorf("range = %v ~ %v (날짜만 있는 종료일은 23:59:59 KST)", s.StartsAt, s.EndsAt)
	}
	if s.IsFree == nil || *s.IsFree || r.FeeText == "" || s.PlaceName != "한전아트센터" || r.Gu != "서초구" {
		t.Errorf("free/fee/place: %+v", r)
	}
	if !strings.HasPrefix(s.ImageURL, "https://") || !strings.Contains(s.SourceURL, "cultcode=160198") {
		t.Errorf("urls: %q %q", s.ImageURL, s.SourceURL)
	}
	// 어린이 테마 교육/체험 → family(sure)
	if recs[1].Spot.Category != domain.CategoryFamily || !recs[1].CategorySure {
		t.Errorf("kids 교육/체험 = %s %v", recs[1].Spot.Category, recs[1].CategorySure)
	}
	// 깨진 위도("37.47~2")는 앞 숫자만, 엔티티 디코딩 후 <...> 제목 유지
	if recs[2].Spot.Lat != 37.4777188083482 || recs[2].Spot.Title != "2026 송년음악회 <One Voice>" || !recs[2].Spot.Free() {
		t.Errorf("rec2 = %+v", recs[2].Spot)
	}
}

func TestParseSeoulCultureNoData(t *testing.T) {
	recs, total, err := ParseSeoulCulture([]byte(`{"RESULT":{"CODE":"INFO-200","MESSAGE":"해당하는 데이터가 없습니다."}}`), fetchedAt)
	if err != nil || total != 0 || recs != nil {
		t.Fatalf("recs=%v total=%d err=%v", recs, total, err)
	}
	if _, _, err := ParseSeoulCulture([]byte(`{"RESULT":{"CODE":"ERROR-300"}}`), fetchedAt); err == nil {
		t.Fatal("want error for ERROR-300")
	}
}

// Fetch는 list_total_count만큼 페이지를 돌고, 요청 경로에 키가 들어가도 에러 메시지에는 안 나온다.
func TestSeoulCultureFetchPagesAndHidesKey(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write(fixture(t, "seoul_culture.json"))
	}))
	defer srv.Close()
	s := &SeoulCulture{Key: "test-key", BaseURL: srv.URL}
	recs, fetched, _, err := s.Fetch(context.Background(), fetchedAt)
	if err != nil || fetched != 3 || len(recs) != 3 {
		t.Fatalf("fetched=%d len=%d err=%v", fetched, len(recs), err)
	}
	if len(paths) != 1 || paths[0] != "/test-key/json/culturalEventInfo/1/1000/" {
		t.Fatalf("paths = %v", paths)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	s = &SeoulCulture{Key: "secret-key", BaseURL: bad.URL}
	if _, _, _, err := s.Fetch(context.Background(), fetchedAt); err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseSeoulReservation(t *testing.T) {
	recs, total, skipped, err := ParseSeoulReservation(fixture(t, "seoul_reservation_culture.json"), ServiceReservationCulture, fetchedAt)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(recs) != 2 || skipped != 1 {
		t.Fatalf("total=%d len=%d skipped=%d (청년정보는 볼거리가 아님)", total, len(recs), skipped)
	}
	r := recs[0]
	s := r.Spot
	if s.ID != "seoul_reservation:S261020095302613204" || s.Category != domain.CategoryExhibition || !r.CategorySure {
		t.Errorf("id/category = %s %s", s.ID, s.Category)
	}
	if s.Lat != 37.580446301109994 || s.Lng != 126.98409156389061 {
		t.Errorf("coords = %v,%v (Y=위도, X=경도)", s.Lat, s.Lng)
	}
	if !s.Free() || s.PlaceName != "서울역사박물관 백인제가옥" || r.Gu != "종로구" {
		t.Errorf("free/place = %v %q", s.IsFree, s.PlaceName)
	}
	if strings.Contains(r.Description, "필수 준수사항") || !strings.Contains(r.Description, "한옥 전시해설") {
		t.Errorf("description must drop boilerplate: %q", r.Description)
	}
	if !s.EndsAt.Equal(kst(2026, 11, 30, 23, 59, 59)) {
		t.Errorf("ends_at = %v", s.EndsAt)
	}
}

func TestReservationCategory(t *testing.T) {
	for _, tc := range []struct {
		max, min, title string
		want            domain.Category
		sure, skip      bool
	}{
		{"문화체험", "산림여가", "숲 해설", domain.CategoryNature, true, false},
		{"문화체험", "서울형키즈카페", "키즈카페", domain.CategoryFamily, true, false},
		{"교육강좌", "스포츠", "배드민턴 교실", domain.CategorySports, true, false},
		{"문화체험", "교육체험", "반려견 산책 교실", domain.CategoryPet, true, false},
		{"교육강좌", "정보통신", "코딩", "", false, true},
		{"문화체험", "새분류", "모름", domain.CategoryFamily, false, false},
	} {
		c, sure, skip := ReservationCategory(tc.max, tc.min, tc.title)
		if c != tc.want || sure != tc.sure || skip != tc.skip {
			t.Errorf("%s>%s %q = %s %v %v", tc.max, tc.min, tc.title, c, sure, skip)
		}
	}
}

func TestParseTourList(t *testing.T) {
	recs, total, err := ParseTourList(fixture(t, "tourapi_list.json"), fetchedAt)
	if err != nil || total != 3 || len(recs) != 3 {
		t.Fatalf("total=%d len=%d err=%v", total, len(recs), err)
	}
	byID := map[string]Record{}
	for _, r := range recs {
		byID[r.Spot.SourceID] = r
	}
	r := byID["3379778"]
	if r.Spot.ID != "tourapi:3379778" || r.Spot.Lat != 37.4960246566 || r.Spot.Lng != 127.1107532009 {
		t.Errorf("id/coords (mapy=위도, mapx=경도): %+v", r.Spot)
	}
	if r.Spot.Category != domain.CategoryFestival || !r.CategorySure {
		t.Errorf("옥토버페스트(EV01) = %s", r.Spot.Category)
	}
	if r.Spot.Address == "" || r.AddressQuery == "" || r.Spot.PlaceName != "가락몰 3층 하늘공원" {
		t.Errorf("address/place: %+v", r)
	}
	if c := byID["1095732"].Spot.Category; c != domain.CategoryFestival {
		t.Errorf("빛초롱축제 = %s", c)
	}
	if e := byID["4144229"]; e.Spot.Category != domain.CategoryExhibition || e.CategorySure {
		t.Errorf("EV030100 = %s %v (전시, 애매)", e.Spot.Category, e.CategorySure)
	}
}

func TestTourDetail(t *testing.T) {
	recs, _, _ := ParseTourList(fixture(t, "tourapi_list.json"), fetchedAt)
	var r Record
	for _, x := range recs {
		if x.Spot.SourceID == "3379778" {
			r = x
		}
	}
	common, _, err := parseTour(fixture(t, "tourapi_common.json"))
	if err != nil {
		t.Fatal(err)
	}
	intro, _, err := parseTour(fixture(t, "tourapi_intro.json"))
	if err != nil {
		t.Fatal(err)
	}
	applyTourDetail(&r, common, intro)
	if r.Spot.SourceURL != "https://www.garak.co.kr/" || !r.Spot.Free() || r.FeeText != "무료" || r.Description == "" {
		t.Fatalf("detail: url=%q free=%v fee=%q", r.Spot.SourceURL, r.Spot.IsFree, r.FeeText)
	}
	if homepageURL(`<a href="https://ex.com/a" target="_blank">ex</a>`) != "https://ex.com/a" {
		t.Fatal("homepage anchor")
	}
}

func TestParseTourEmptyItems(t *testing.T) {
	items, total, err := parseTour([]byte(`{"response":{"header":{"resultCode":"0000","resultMsg":"OK"},"body":{"items":"","numOfRows":10,"pageNo":1,"totalCount":0}}}`))
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestParseCultureList(t *testing.T) {
	recs, total, notSeoul, err := ParseCultureList(fixture(t, "cultureinfo_area2.xml"), fetchedAt)
	if err != nil || total != 3 || len(recs) != 2 || notSeoul != 1 {
		t.Fatalf("total=%d len=%d notSeoul=%d err=%v", total, len(recs), notSeoul, err)
	}
	s := recs[0].Spot
	if s.ID != "cultureinfo:393376" || s.Title != "[대학로] 대표 코미디 연극 <오백에 삼십>" {
		t.Errorf("id/title (이중 인코딩 엔티티) = %q %q", s.ID, s.Title)
	}
	if s.Category != domain.CategoryPerformance || s.Lat != 37.5766441213197 || s.Lng != 127.00393230387 {
		t.Errorf("category/coords (gpsY=위도): %+v", s)
	}
	if !strings.HasPrefix(s.ImageURL, "https://") {
		t.Errorf("image must be https: %q", s.ImageURL)
	}
}

func TestCultureDetail(t *testing.T) {
	items, _, err := parseCulture(fixture(t, "cultureinfo_detail2.xml"))
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	r := cultureRecord(items[0], fetchedAt)
	if r.HasCoords() || r.Spot.Category != domain.CategoryFamily || r.CategorySure {
		t.Fatalf("교육/체험, 좌표 없음: %+v", r)
	}
	applyCultureDetail(&r, items[0])
	if r.Spot.SourceURL != "https://www.jeijcc.org/education/view.html?id=805" || r.Spot.IsFree != nil {
		t.Fatalf("detail: %+v", r.Spot)
	}
}

func TestSeoulCultureCategory(t *testing.T) {
	for _, tc := range []struct {
		code, theme, title string
		want               domain.Category
		sure               bool
	}{
		{"전시/미술", "기타", "현대미술전", domain.CategoryExhibition, true},
		{"전시/미술", "기타", "음식의 역사 전시", domain.CategoryExhibition, true}, // 확신 있는 전시는 food로 안 바꿈
		{"축제-문화/예술", "기타", "한강 야시장", domain.CategoryFood, true},
		{"기타", "기타", "서울 마라톤", domain.CategorySports, true},
		{"기타", "가족 문화행사", "가족 체험", domain.CategoryFamily, true},
		{"클래식", "기타", "트럼펫 리사이틀", domain.CategoryPerformance, true}, // 트럼'펫'은 pet 아님
		{"기타", "기타", "반려견 운동회", domain.CategoryPet, true},
		{"영화", "기타", "야외 상영", domain.CategoryPerformance, false},
	} {
		c, sure := SeoulCultureCategory(tc.code, tc.theme, tc.title)
		if c != tc.want || sure != tc.sure {
			t.Errorf("%s/%s %q = %s %v, want %s %v", tc.code, tc.theme, tc.title, c, sure, tc.want, tc.sure)
		}
	}
}
