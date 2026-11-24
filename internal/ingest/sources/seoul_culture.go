package sources

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/ingest"
)

// SeoulOpenAPIBase는 서울 열린데이터광장 OpenAPI 호스트(키가 경로에 들어간다).
const SeoulOpenAPIBase = "http://openapi.seoul.go.kr:8088"

// seoulPageSize는 열린데이터광장 1회 요청 최대 건수.
const seoulPageSize = 1000

// SeoulCulture는 서울시 문화행사 정보(culturalEventInfo, OA-15486). 원천: 서울문화포털.
type SeoulCulture struct {
	Key     string
	BaseURL string // 테스트에서 httptest 서버로 바꾼다
	// RetainEndedDays: END_DATE가 오늘−이 일수보다 이른 항목은 싣지 않는다(과거 약 2만 건 제외).
	RetainEndedDays int
}

// NewSeoulCulture는 기본 호스트·보존 기간(90일)으로 수집기를 만든다.
func NewSeoulCulture(key string) *SeoulCulture {
	return &SeoulCulture{Key: key, BaseURL: SeoulOpenAPIBase, RetainEndedDays: ingest.DefaultRetainEndedDays}
}

// Name implements ingest.Source.
func (*SeoulCulture) Name() string { return "seoul_culture" }

type seoulCultureRow struct {
	CodeName  string `json:"CODENAME"`
	GuName    string `json:"GUNAME"`
	Title     string `json:"TITLE"`
	Place     string `json:"PLACE"`
	UseTrgt   string `json:"USE_TRGT"`
	UseFee    string `json:"USE_FEE"`
	Player    string `json:"PLAYER"`
	Program   string `json:"PROGRAM"`
	EtcDesc   string `json:"ETC_DESC"`
	OrgLink   string `json:"ORG_LINK"`
	MainImg   string `json:"MAIN_IMG"`
	StrtDate  string `json:"STRTDATE"`
	EndDate   string `json:"END_DATE"`
	ThemeCode string `json:"THEMECODE"`
	Lot       string `json:"LOT"` // 경도
	Lat       string `json:"LAT"` // 위도
	IsFree    string `json:"IS_FREE"`
	HmpgAddr  string `json:"HMPG_ADDR"`
	ProTime   string `json:"PRO_TIME"`
}

// seoulCategory: CODENAME → (카테고리, 확신). DATA-PLAN.md §9 V11의 실측 값(2026-11-24).
var seoulCategory = map[string]struct {
	c    domain.Category
	sure bool
}{
	"전시/미술":    {domain.CategoryExhibition, true},
	"클래식":      {domain.CategoryPerformance, true},
	"콘서트":      {domain.CategoryPerformance, true},
	"연극":       {domain.CategoryPerformance, true},
	"국악":       {domain.CategoryPerformance, true},
	"뮤지컬/오페라":  {domain.CategoryPerformance, true},
	"무용":       {domain.CategoryPerformance, true},
	"독주/독창회":   {domain.CategoryPerformance, true},
	"영화":       {domain.CategoryPerformance, false},
	"교육/체험":    {domain.CategoryFamily, false},
	"축제-문화/예술": {domain.CategoryFestival, true},
	"축제-자연/경관": {domain.CategoryFestival, true},
	"축제-전통/역사": {domain.CategoryFestival, true},
	"축제-시민화합":  {domain.CategoryFestival, true},
	"축제-관광/체육": {domain.CategoryFestival, false},
	"축제-기타":    {domain.CategoryFestival, false},
	"기타":       {domain.CategoryFestival, false},
}

// SeoulCultureCategory는 원천 분류와 테마로 카테고리를 정한다. 어린이·가족 테마의
// 체험·기타 행사는 family로 확신한다.
func SeoulCultureCategory(codeName, theme, title string) (domain.Category, bool) {
	m, ok := seoulCategory[strings.TrimSpace(codeName)]
	if !ok {
		m.c, m.sure = domain.CategoryFestival, false
	}
	if (strings.Contains(theme, "어린이") || strings.Contains(theme, "가족")) &&
		(m.c == domain.CategoryFamily || !m.sure) {
		m.c, m.sure = domain.CategoryFamily, true
	}
	return ingest.KeywordCategory(title, m.c, m.sure)
}

var (
	cultcodeRe = regexp.MustCompile(`cultcode=(\d+)`)
	leadNumRe  = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?`)
)

// parseCoord는 "37.57~2" 같은 깨진 값에서도 앞의 숫자만 읽는다. 없으면 0.
func parseCoord(s string) float64 {
	v, _ := strconv.ParseFloat(leadNumRe.FindString(strings.TrimSpace(s)), 64)
	return v
}

func shortHash(parts ...string) string {
	h := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])[:16]
}

// ParseSeoulCulture는 culturalEventInfo 응답 한 페이지를 읽는다.
func ParseSeoulCulture(body []byte, fetchedAt time.Time) (recs []Record, total int, err error) {
	var resp struct {
		C *struct {
			Total  int `json:"list_total_count"`
			Result struct {
				Code string `json:"CODE"`
			} `json:"RESULT"`
			Rows []seoulCultureRow `json:"row"`
		} `json:"culturalEventInfo"`
		Result *struct {
			Code string `json:"CODE"`
		} `json:"RESULT"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}
	if resp.C == nil {
		code := ""
		if resp.Result != nil {
			code = resp.Result.Code
		}
		if code == "INFO-200" { // 데이터 없음
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("api result %s", code)
	}
	for _, r := range resp.C.Rows {
		recs = append(recs, seoulCultureRecord(r, fetchedAt))
	}
	return recs, resp.C.Total, nil
}

// Record는 ingest.Record의 별칭(테스트·파서 시그니처를 짧게).
type Record = ingest.Record

func seoulCultureRecord(r seoulCultureRow, fetchedAt time.Time) Record {
	id := ""
	if m := cultcodeRe.FindStringSubmatch(r.HmpgAddr); m != nil {
		id = m[1]
	} else {
		id = shortHash(r.Title, r.StrtDate, r.Place)
	}
	title := ingest.CleanText(r.Title)
	start, end, ok := ingest.ParseRange(r.StrtDate, r.EndDate)
	if !ok {
		start = time.Time{}
	}
	cat, sure := SeoulCultureCategory(r.CodeName, r.ThemeCode, title)
	var desc []string
	for _, p := range []struct{ k, v string }{
		{"", r.Program}, {"", r.EtcDesc}, {"대상", r.UseTrgt}, {"출연", r.Player}, {"시간", r.ProTime},
	} {
		if v := ingest.CleanText(p.v); v != "" {
			if p.k != "" {
				v = p.k + ": " + v
			}
			desc = append(desc, v)
		}
	}
	srcURL := strings.TrimSpace(r.HmpgAddr)
	if srcURL == "" {
		srcURL = strings.TrimSpace(r.OrgLink)
	}
	return Record{
		Spot: domain.Spot{
			ID: domain.SpotID("seoul_culture", id), Source: "seoul_culture", SourceID: id,
			Title: title, Category: cat,
			Lat: parseCoord(r.Lat), Lng: parseCoord(r.Lot),
			PlaceName: ingest.CleanText(r.Place), StartsAt: start, EndsAt: end,
			IsFree: ingest.FreeFromFlag(r.IsFree), ImageURL: ingest.HTTPS(r.MainImg),
			SourceURL: srcURL, FetchedAt: fetchedAt,
		},
		CategorySure:   sure,
		FeeText:        ingest.CleanText(r.UseFee),
		SourceCategory: strings.TrimSpace(r.CodeName),
		Description:    strings.Join(desc, " / "),
		Gu:             strings.TrimSpace(r.GuName),
	}
}

// Fetch implements ingest.Source: 전체(종료 포함 약 2만 건)를 1,000건씩 받아 END_DATE가 보존 기간 안인
// 항목만 돌려준다. API에 종료일 조건이 없고 시작일 내림차순이라, 몇 년 전에 시작한 장기 상설 행사가 뒤쪽
// 페이지에 섞여 있어 중간에 멈추지 않고 끝까지 받는다(약 20회 호출).
func (s *SeoulCulture) Fetch(ctx context.Context, now time.Time) ([]Record, int, map[string]int, error) {
	var all []Record
	fetched := 0
	dropped := map[string]int{}
	total := -1
	for start := 1; total < 0 || start <= total; start += seoulPageSize {
		body, err := s.page(ctx, "culturalEventInfo", start, start+seoulPageSize-1)
		if err != nil {
			return all, fetched, dropped, fmt.Errorf("seoul_culture page %d: %w", start, err)
		}
		recs, t, err := ParseSeoulCulture(body, now)
		if err != nil {
			return all, fetched, dropped, fmt.Errorf("seoul_culture page %d: %w", start, err)
		}
		total = t
		fetched += len(recs)
		for _, r := range recs {
			if ingest.Expired(r.Spot, now, s.RetainEndedDays) {
				dropped[ingest.DropEnded]++
				continue
			}
			all = append(all, r)
		}
		if len(recs) == 0 {
			break
		}
	}
	return all, fetched, dropped, nil
}

func (s *SeoulCulture) page(ctx context.Context, service string, start, end int) ([]byte, error) {
	return seoulPage(ctx, s.BaseURL, s.Key, service, start, end)
}

// seoulPage는 {base}/{key}/json/{service}/{start}/{end}/ 를 부른다. 키가 경로에 있으므로
// 에러에 URL을 넣지 않는다(get 참고).
func seoulPage(ctx context.Context, base, key, service string, start, end int) ([]byte, error) {
	u := fmt.Sprintf("%s/%s/json/%s/%d/%d/", strings.TrimRight(base, "/"), url.PathEscape(key), service, start, end)
	return get(ctx, newClient(), u)
}
