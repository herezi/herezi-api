package sources

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/ingest"
)

// CultureInfoBase는 한국문화정보원 한눈에보는문화정보(B553457/cultureinfo).
const CultureInfoBase = "https://apis.data.go.kr/B553457/cultureinfo"

// CultureInfo는 area2(sido=서울, 기간 [오늘, 오늘+Days])로 목록을 받고, 항목마다 detail2(요금·주소·설명·URL)를
// 부른다. 개발계정 10,000회/일. 목록의 period2는 전국이라 쓰지 않는다(area2가 서울 부분집합).
type CultureInfo struct {
	Key         string
	BaseURL     string
	Days        int  // 조회 기간(오늘부터 며칠). 기본 180
	Details     bool // detail2 호출 여부
	Concurrency int  // detail2 동시 호출 수. 기본 3
	MaxDetails  int  // detail2 호출 상한(0 = 제한 없음). 기본 600
	Backoff     time.Duration

	detailErr            error
	detailOK, detailWant int
}

// NewCultureInfo는 기본 설정으로 수집기를 만든다.
func NewCultureInfo(key string) *CultureInfo {
	return &CultureInfo{Key: key, BaseURL: CultureInfoBase, Days: 180, Details: true, Concurrency: 3, MaxDetails: 600}
}

// Name implements ingest.Source.
func (*CultureInfo) Name() string { return "cultureinfo" }

type cultureItem struct {
	Seq         string `xml:"seq"`
	Title       string `xml:"title"`
	StartDate   string `xml:"startDate"`
	EndDate     string `xml:"endDate"`
	Place       string `xml:"place"`
	RealmName   string `xml:"realmName"`
	ServiceName string `xml:"serviceName"`
	Area        string `xml:"area"`
	Sigungu     string `xml:"sigungu"`
	Thumbnail   string `xml:"thumbnail"`
	GpsX        string `xml:"gpsX"` // 경도
	GpsY        string `xml:"gpsY"` // 위도
	// detail2
	Price     string `xml:"price"`
	Contents1 string `xml:"contents1"`
	URL       string `xml:"url"`
	ImgURL    string `xml:"imgUrl"`
	PlaceAddr string `xml:"placeAddr"`
}

type cultureResponse struct {
	Header struct {
		ResultCode string `xml:"resultCode"`
	} `xml:"header"`
	Body struct {
		TotalCount int           `xml:"totalCount"`
		Items      []cultureItem `xml:"items>item"`
	} `xml:"body"`
}

func parseCulture(body []byte) ([]cultureItem, int, error) {
	var r cultureResponse
	if err := xml.Unmarshal(body, &r); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}
	if c := r.Header.ResultCode; c != "00" && c != "0000" {
		return nil, 0, fmt.Errorf("api result %s", c)
	}
	return r.Body.Items, r.Body.TotalCount, nil
}

// cultureCategory: realmName → 카테고리 (2026-11-24 실측 값).
var cultureCategory = map[string]rsvMap{
	"전시":      {domain.CategoryExhibition, true},
	"연극":      {domain.CategoryPerformance, true},
	"뮤지컬/오페라": {domain.CategoryPerformance, true},
	"무용/발레":   {domain.CategoryPerformance, true},
	"음악/콘서트":  {domain.CategoryPerformance, true},
	"국악":      {domain.CategoryPerformance, true},
	"행사/축제":   {domain.CategoryFestival, true},
	"아동/가족":   {domain.CategoryFamily, true},
	"교육/체험":   {domain.CategoryFamily, false},
	"기타":      {domain.CategoryFestival, false},
}

// CultureCategory는 분야(realmName)로 카테고리를 정한다.
func CultureCategory(realm, title string) (domain.Category, bool) {
	m, ok := cultureCategory[strings.TrimSpace(realm)]
	if !ok {
		m = rsvMap{domain.CategoryFestival, false}
	}
	return ingest.KeywordCategory(title, m.c, m.sure)
}

// ParseCultureList는 area2/period2 응답을 Record로 바꾼다. 서울이 아닌 항목은 빼고 notSeoul로 센다.
func ParseCultureList(body []byte, fetchedAt time.Time) (recs []Record, total, notSeoul int, err error) {
	items, total, err := parseCulture(body)
	if err != nil {
		return nil, 0, 0, err
	}
	for _, it := range items {
		if a := strings.TrimSpace(it.Area); a != "" && a != "서울" {
			notSeoul++
			continue
		}
		recs = append(recs, cultureRecord(it, fetchedAt))
	}
	return recs, total, notSeoul, nil
}

func cultureRecord(it cultureItem, fetchedAt time.Time) Record {
	title := ingest.CleanText(it.Title)
	start, end, ok := ingest.ParseRange(it.StartDate, it.EndDate)
	if !ok {
		start = time.Time{}
	}
	lng, _ := strconv.ParseFloat(strings.TrimSpace(it.GpsX), 64)
	lat, _ := strconv.ParseFloat(strings.TrimSpace(it.GpsY), 64)
	cat, sure := CultureCategory(it.RealmName, title)
	return Record{
		Spot: domain.Spot{
			ID: domain.SpotID("cultureinfo", it.Seq), Source: "cultureinfo", SourceID: it.Seq,
			Title: title, Category: cat, Lat: lat, Lng: lng, PlaceName: ingest.CleanText(it.Place),
			StartsAt: start, EndsAt: end, ImageURL: ingest.HTTPS(it.Thumbnail), FetchedAt: fetchedAt,
		},
		CategorySure:   sure,
		SourceCategory: strings.TrimSpace(it.RealmName),
		Gu:             strings.TrimSpace(it.Sigungu),
	}
}

// applyCultureDetail은 detail2 응답(요금·주소·설명·URL·이미지)을 채운다.
func applyCultureDetail(r *Record, d cultureItem) {
	r.FeeText = ingest.CleanText(d.Price)
	r.Spot.IsFree = ingest.DetectFree(r.FeeText)
	r.Description = ingest.Truncate(ingest.CleanText(d.Contents1), 600)
	r.Spot.SourceURL = strings.TrimSpace(d.URL)
	if a := ingest.CleanText(d.PlaceAddr); a != "" {
		r.Spot.Address = a
		r.AddressQuery = a
	}
	if img := ingest.HTTPS(d.ImgURL); img != "" {
		r.Spot.ImageURL = img
	}
	if !r.HasCoords() {
		lng, _ := strconv.ParseFloat(strings.TrimSpace(d.GpsX), 64)
		lat, _ := strconv.ParseFloat(strings.TrimSpace(d.GpsY), 64)
		r.Spot.Lat, r.Spot.Lng = lat, lng
	}
}

func (s *CultureInfo) call(ctx context.Context, op string, q url.Values) ([]cultureItem, int, error) {
	body, err := s.callRaw(ctx, op, q)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := parseCulture(body)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", op, err)
	}
	return items, total, nil
}

// Fetch implements ingest.Source.
func (s *CultureInfo) Fetch(ctx context.Context, now time.Time) ([]Record, int, map[string]int, error) {
	days := s.Days
	if days <= 0 {
		days = 180
	}
	from := now.In(ingest.KST)
	dropped := map[string]int{}
	var recs []Record
	fetched := 0
	const rows = 500
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("PageNo", strconv.Itoa(page))
		q.Set("numOfrows", strconv.Itoa(rows))
		q.Set("from", from.Format("20060102"))
		q.Set("to", from.AddDate(0, 0, days).Format("20060102"))
		q.Set("sido", "서울")
		body, err := s.callRaw(ctx, "area2", q)
		if err != nil {
			return recs, fetched, dropped, err
		}
		got, total, notSeoul, err := ParseCultureList(body, now)
		if err != nil {
			return recs, fetched, dropped, fmt.Errorf("area2: %w", err)
		}
		fetched += len(got) + notSeoul
		dropped[ingest.DropOutOfSeoul] += notSeoul
		recs = append(recs, got...)
		if len(got)+notSeoul == 0 || page*rows >= total {
			break
		}
	}
	if !s.Details {
		return recs, fetched, dropped, nil
	}
	s.fillDetails(ctx, recs, now)
	if s.detailErr != nil {
		// 상세 실패(한도 초과 등)는 목록 정보로 계속 간다. 에러는 리포트에 남긴다.
		return recs, fetched, dropped, fmt.Errorf("%w (상세 %d/%d건만 받음)", s.detailErr, s.detailOK, s.detailWant)
	}
	return recs, fetched, dropped, nil
}

// fillDetails는 끝나지 않은 항목에 detail2를 부른다. 좌표 없는 항목·진행 중 항목을 먼저 부르고,
// MaxDetails를 넘거나 한도 초과(429)가 나면 멈춘다(일일 한도를 다 쓰지 않도록).
func (s *CultureInfo) fillDetails(ctx context.Context, recs []Record, now time.Time) {
	var order []int
	for i := range recs {
		if !recs[i].Spot.Ended(now) {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		pa, pb := detailPriority(recs[a], now), detailPriority(recs[b], now)
		return pb - pa
	})
	if s.MaxDetails > 0 && len(order) > s.MaxDetails {
		order = order[:s.MaxDetails]
	}
	s.detailWant = len(order)
	sem := make(chan struct{}, max(s.Concurrency, 1))
	var wg sync.WaitGroup
	var mu sync.Mutex
	stopped := false
	for _, i := range order {
		mu.Lock()
		stop := stopped
		mu.Unlock()
		if stop || ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(r *Record) {
			defer func() { <-sem; wg.Done() }()
			items, _, err := s.call(ctx, "detail2", url.Values{"seq": {r.Spot.SourceID}})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if s.detailErr == nil {
					s.detailErr = err
				}
				stopped = true
				return
			}
			if len(items) > 0 {
				applyCultureDetail(r, items[0])
				s.detailOK++
			}
		}(&recs[i])
	}
	wg.Wait()
}

func detailPriority(r Record, now time.Time) int {
	p := 0
	if !r.HasCoords() {
		p += 2
	}
	if !r.Spot.StartsAt.After(now) {
		p++
	}
	return p
}

// callRaw는 op를 부른다. data.go.kr이 초당 호출이 많으면 429를 주므로 한 번 물러섰다 다시 한다
// (일일 한도 초과도 429라 오래 재시도하지 않는다).
func (s *CultureInfo) callRaw(ctx context.Context, op string, q url.Values) ([]byte, error) {
	q.Set("serviceKey", s.Key)
	u := strings.TrimRight(s.BaseURL, "/") + "/" + op + "?" + q.Encode()
	backoff := s.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	var err error
	for attempt := range 2 {
		var body []byte
		if body, err = get(ctx, newClient(), u); err == nil {
			return body, nil
		}
		if !strings.Contains(err.Error(), "http 429") {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff << attempt):
		}
	}
	return nil, fmt.Errorf("%s: %w", op, err)
}
