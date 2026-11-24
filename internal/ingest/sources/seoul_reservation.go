package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/ingest"
)

// 서울시 공공서비스예약 데이터셋(열린데이터광장 서비스명, 2026-11-24 샘플 호출로 확인).
const (
	ServiceReservationCulture   = "ListPublicReservationCulture"   // 문화행사 OA-2269
	ServiceReservationEducation = "ListPublicReservationEducation" // 교육강좌 OA-2268
)

// DropNotLeisure는 볼거리가 아닌 교육강좌(취업·IT 강의 등) 제외 사유.
const DropNotLeisure = "not_leisure"

// SeoulReservation은 서울시 공공서비스예약 문화행사·교육강좌 두 데이터셋을 하나의 소스로 수집한다.
type SeoulReservation struct {
	Key     string
	BaseURL string
}

// NewSeoulReservation은 기본 호스트로 수집기를 만든다.
func NewSeoulReservation(key string) *SeoulReservation {
	return &SeoulReservation{Key: key, BaseURL: SeoulOpenAPIBase}
}

// Name implements ingest.Source.
func (*SeoulReservation) Name() string { return "seoul_reservation" }

type seoulRsvRow struct {
	SvcID      string `json:"SVCID"`
	MaxClass   string `json:"MAXCLASSNM"`
	MinClass   string `json:"MINCLASSNM"`
	SvcStat    string `json:"SVCSTATNM"`
	SvcNm      string `json:"SVCNM"`
	PayAt      string `json:"PAYATNM"`
	PlaceNm    string `json:"PLACENM"`
	UseTgtInfo string `json:"USETGTINFO"`
	SvcURL     string `json:"SVCURL"`
	X          string `json:"X"` // 경도
	Y          string `json:"Y"` // 위도
	OpenBegin  string `json:"SVCOPNBGNDT"`
	OpenEnd    string `json:"SVCOPNENDDT"`
	AreaNm     string `json:"AREANM"`
	ImgURL     string `json:"IMGURL"`
	DtlCont    string `json:"DTLCONT"`
}

type rsvMap struct {
	c    domain.Category
	sure bool
}

// reservationCategory: MAXCLASSNM>MINCLASSNM → 카테고리 (2026-11-24 실측 값).
// 볼거리가 아닌 교육강좌는 skip.
var reservationCategory = map[string]rsvMap{
	"문화체험>교육체험":    {domain.CategoryFamily, false},
	"문화체험>산림여가":    {domain.CategoryNature, true},
	"문화체험>서울형키즈카페": {domain.CategoryFamily, true},
	"문화체험>공원탐방":    {domain.CategoryNature, true},
	"문화체험>전시/관람":   {domain.CategoryExhibition, true},
	"문화체험>문화행사":    {domain.CategoryFestival, false},
	"문화체험>농장체험":    {domain.CategoryFamily, true},
	"교육강좌>자연/과학":   {domain.CategoryFamily, false},
	"교육강좌>역사":      {domain.CategoryFamily, false},
	"교육강좌>스포츠":     {domain.CategorySports, true},
	"교육강좌>공예/취미":   {domain.CategoryFamily, false},
	"교육강좌>도시농업":    {domain.CategoryFamily, false},
	"교육강좌>미술제작":    {domain.CategoryFamily, false},
	"교육강좌>기타":      {domain.CategoryFamily, false},
}

// 볼거리가 아닌 교육강좌(취업 정보·IT·어학 강의)는 수집하지 않는다.
var reservationSkip = map[string]bool{
	"교육강좌>청년정보": true, "교육강좌>정보통신": true, "교육강좌>교양/어학": true,
}

// ReservationCategory는 대·소분류로 카테고리를 정한다. skip이면 볼거리가 아니다.
func ReservationCategory(maxClass, minClass, title string) (c domain.Category, sure, skip bool) {
	key := strings.TrimSpace(maxClass) + ">" + strings.TrimSpace(minClass)
	if reservationSkip[key] {
		return "", false, true
	}
	m, ok := reservationCategory[key]
	if !ok {
		m = rsvMap{domain.CategoryFamily, false}
	}
	c, sure = ingest.KeywordCategory(title, m.c, m.sure)
	return c, sure, false
}

// reservationBoilerplate: DTLCONT 앞의 공통 이용 안내("1. 공공시설 예약서비스 … 3. 상세내용")를 자른다.
func reservationDescription(dtl string) string {
	t := ingest.CleanText(dtl)
	if i := strings.Index(t, "3. 상세내용"); i >= 0 {
		t = strings.TrimSpace(t[i+len("3. 상세내용"):])
	}
	return ingest.Truncate(t, 600)
}

// ParseSeoulReservation은 공공서비스예약 응답 한 페이지를 읽는다(service는 데이터셋 이름).
func ParseSeoulReservation(body []byte, service string, fetchedAt time.Time) (recs []Record, total, skipped int, err error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, 0, 0, fmt.Errorf("decode: %w", err)
	}
	payload, ok := raw[service]
	if !ok {
		var res struct {
			Result struct {
				Code string `json:"CODE"`
			} `json:"RESULT"`
		}
		_ = json.Unmarshal(body, &res)
		if res.Result.Code == "INFO-200" {
			return nil, 0, 0, nil
		}
		return nil, 0, 0, fmt.Errorf("api result %s", res.Result.Code)
	}
	var page struct {
		Total int           `json:"list_total_count"`
		Rows  []seoulRsvRow `json:"row"`
	}
	if err := json.Unmarshal(payload, &page); err != nil {
		return nil, 0, 0, fmt.Errorf("decode rows: %w", err)
	}
	for _, r := range page.Rows {
		title := ingest.CleanText(r.SvcNm)
		cat, sure, skip := ReservationCategory(r.MaxClass, r.MinClass, title)
		if skip {
			skipped++
			continue
		}
		start, end, ok := ingest.ParseRange(r.OpenBegin, r.OpenEnd)
		if !ok {
			start = time.Time{}
		}
		free := ingest.FreeFromFlag(r.PayAt)
		if free == nil && strings.HasPrefix(strings.TrimSpace(r.PayAt), "유료") { // "유료(요금안내문의)"
			f := false
			free = &f
		}
		desc := reservationDescription(r.DtlCont)
		if t := ingest.CleanText(r.UseTgtInfo); t != "" {
			desc = "대상: " + t + " / " + desc
		}
		recs = append(recs, Record{
			Spot: domain.Spot{
				ID: domain.SpotID("seoul_reservation", r.SvcID), Source: "seoul_reservation", SourceID: r.SvcID,
				Title: title, Category: cat, Lat: parseCoord(r.Y), Lng: parseCoord(r.X),
				PlaceName: strings.ReplaceAll(ingest.CleanText(r.PlaceNm), ">", " "),
				StartsAt:  start, EndsAt: end, IsFree: free, ImageURL: ingest.HTTPS(r.ImgURL),
				SourceURL: strings.TrimSpace(r.SvcURL), FetchedAt: fetchedAt,
			},
			CategorySure:   sure,
			SourceCategory: strings.TrimSpace(r.MaxClass) + ">" + strings.TrimSpace(r.MinClass),
			Description:    desc,
			Gu:             strings.TrimSpace(r.AreaNm),
		})
	}
	return recs, page.Total, skipped, nil
}

// Fetch implements ingest.Source: 문화행사·교육강좌를 모두 받는다.
func (s *SeoulReservation) Fetch(ctx context.Context, now time.Time) ([]Record, int, map[string]int, error) {
	var all []Record
	fetched, skipped := 0, 0
	for _, svc := range []string{ServiceReservationCulture, ServiceReservationEducation} {
		total := -1
		for start := 1; total < 0 || start <= total; start += seoulPageSize {
			body, err := seoulPage(ctx, s.BaseURL, s.Key, svc, start, start+seoulPageSize-1)
			if err != nil {
				return all, fetched, map[string]int{DropNotLeisure: skipped}, fmt.Errorf("%s page %d: %w", svc, start, err)
			}
			recs, t, sk, err := ParseSeoulReservation(body, svc, now)
			if err != nil {
				return all, fetched, map[string]int{DropNotLeisure: skipped}, fmt.Errorf("%s page %d: %w", svc, start, err)
			}
			total = t
			fetched += len(recs) + sk
			skipped += sk
			all = append(all, recs...)
			if len(recs)+sk == 0 {
				break
			}
		}
	}
	return all, fetched, map[string]int{DropNotLeisure: skipped}, nil
}
