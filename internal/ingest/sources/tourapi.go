package sources

import (
	"context"
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

// TourAPIBase는 한국관광공사 국문 관광정보 서비스_GW(KorService2).
const TourAPIBase = "https://apis.data.go.kr/B551011/KorService2"

// TourAPI는 searchFestival2(서울, lDongRegnCd=11)로 축제·공연·행사 목록을 받고(끝난 지 90일 이내 포함), 끝나지 않은 항목만
// detailCommon2(개요·홈페이지)·detailIntro2(이용요금)를 부른다. 개발계정 1,000회/일이라 호출을 아낀다.
type TourAPI struct {
	Key      string // data.go.kr 일반 인증키(Decoding)
	BaseURL  string
	Lookback int  // 시작일 기준 며칠 전부터 목록을 받을지(진행 중 장기 행사 포함). 기본 400
	Details  bool // 상세(개요·요금) 호출 여부
	// RetainEndedDays: 끝난 지 이 일수 이내인 항목은 유지한다(상세 호출은 진행 중·예정 항목만).
	RetainEndedDays int
}

// NewTourAPI는 기본 설정으로 수집기를 만든다.
func NewTourAPI(key string) *TourAPI {
	return &TourAPI{Key: key, BaseURL: TourAPIBase, Lookback: 400, Details: true, RetainEndedDays: ingest.DefaultRetainEndedDays}
}

// Name implements ingest.Source.
func (*TourAPI) Name() string { return "tourapi" }

type tourItem struct {
	Addr1          string `json:"addr1"`
	Addr2          string `json:"addr2"`
	ContentID      string `json:"contentid"`
	EventStartDate string `json:"eventstartdate"`
	EventEndDate   string `json:"eventenddate"`
	FirstImage     string `json:"firstimage"`
	MapX           string `json:"mapx"` // 경도
	MapY           string `json:"mapy"` // 위도
	Title          string `json:"title"`
	LDongSignguCd  string `json:"lDongSignguCd"`
	Lcls2          string `json:"lclsSystm2"`
	Lcls3          string `json:"lclsSystm3"`
	ProgressType   string `json:"progresstype"`
	// 상세
	Overview        string `json:"overview"`
	Homepage        string `json:"homepage"`
	UseTimeFestival string `json:"usetimefestival"`
	EventPlace      string `json:"eventplace"`
	PlayTime        string `json:"playtime"`
}

// tourItems는 TourAPI의 body.items가 결과 없을 때 ""(문자열)로 오는 것을 흡수한다.
type tourItems []tourItem

func (t *tourItems) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		*t = nil
		return nil
	}
	var w struct {
		Item []tourItem `json:"item"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*t = w.Item
	return nil
}

type tourResponse struct {
	Response struct {
		Header struct {
			ResultCode string `json:"resultCode"`
			ResultMsg  string `json:"resultMsg"`
		} `json:"header"`
		Body struct {
			Items      tourItems `json:"items"`
			TotalCount int       `json:"totalCount"`
		} `json:"body"`
	} `json:"response"`
}

func parseTour(body []byte) ([]tourItem, int, error) {
	var r tourResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err) // 키 오류 등은 XML로 온다
	}
	if c := r.Response.Header.ResultCode; c != "0000" {
		return nil, 0, fmt.Errorf("api result %s", c)
	}
	return r.Response.Body.Items, r.Response.Body.TotalCount, nil
}

// TourCategory: lclsSystm2/3 → 카테고리. EV01 축제, EV02 공연, EV03 행사(전시·박람회·기타).
func TourCategory(lcls2, lcls3, title string) (domain.Category, bool) {
	c, sure := domain.CategoryFestival, false
	switch {
	case lcls2 == "EV01":
		c, sure = domain.CategoryFestival, true
	case lcls2 == "EV02":
		c, sure = domain.CategoryPerformance, true
	case lcls3 == "EV030100":
		c, sure = domain.CategoryExhibition, false
	}
	return ingest.KeywordCategory(title, c, sure)
}

var hrefRe = regexp.MustCompile(`href=["']([^"']+)["']`)

// homepageURL은 detailCommon2 homepage(HTML 앵커일 때가 많다)에서 URL을 뽑는다.
func homepageURL(s string) string {
	if m := hrefRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	s = strings.TrimSpace(ingest.CleanText(s))
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return strings.Fields(s)[0]
	}
	return ""
}

// ParseTourList는 searchFestival2 응답을 Record로 바꾼다(상세 필드는 비어 있다).
func ParseTourList(body []byte, fetchedAt time.Time) ([]Record, int, error) {
	items, total, err := parseTour(body)
	if err != nil {
		return nil, 0, err
	}
	recs := make([]Record, 0, len(items))
	for _, it := range items {
		recs = append(recs, tourRecord(it, fetchedAt))
	}
	return recs, total, nil
}

func tourRecord(it tourItem, fetchedAt time.Time) Record {
	title := ingest.CleanText(it.Title)
	start, end, ok := ingest.ParseRange(it.EventStartDate, it.EventEndDate)
	if !ok {
		start = time.Time{}
	}
	lng, _ := strconv.ParseFloat(strings.TrimSpace(it.MapX), 64)
	lat, _ := strconv.ParseFloat(strings.TrimSpace(it.MapY), 64)
	cat, sure := TourCategory(it.Lcls2, it.Lcls3, title)
	place := ingest.CleanText(it.Addr2)
	if place == "" {
		place = ingest.CleanText(it.EventPlace)
	}
	addr := strings.TrimSpace(ingest.CleanText(it.Addr1))
	return Record{
		Spot: domain.Spot{
			ID: domain.SpotID("tourapi", it.ContentID), Source: "tourapi", SourceID: it.ContentID,
			Title: title, Category: cat, Lat: lat, Lng: lng, Address: addr, PlaceName: place,
			StartsAt: start, EndsAt: end, ImageURL: ingest.HTTPS(it.FirstImage), FetchedAt: fetchedAt,
		},
		CategorySure:   sure,
		SourceCategory: it.Lcls3,
		AddressQuery:   addr,
	}
}

// applyTourDetail은 상세 응답(개요·홈페이지·이용요금)을 Record에 채운다.
func applyTourDetail(r *Record, common, intro []tourItem) {
	if len(common) > 0 {
		r.Description = ingest.Truncate(ingest.CleanText(common[0].Overview), 600)
		r.Spot.SourceURL = homepageURL(common[0].Homepage)
	}
	if len(intro) > 0 {
		in := intro[0]
		r.FeeText = ingest.CleanText(in.UseTimeFestival)
		r.Spot.IsFree = ingest.DetectFree(r.FeeText)
		if r.Spot.PlaceName == "" {
			r.Spot.PlaceName = ingest.CleanText(in.EventPlace)
		}
		if t := ingest.CleanText(in.PlayTime); t != "" && r.Description != "" {
			r.Description = ingest.Truncate("시간: "+t+" / "+r.Description, 600)
		}
	}
}

func (s *TourAPI) call(ctx context.Context, op string, params url.Values) ([]tourItem, int, error) {
	params.Set("serviceKey", s.Key)
	params.Set("MobileOS", "ETC")
	params.Set("MobileApp", "herezi")
	params.Set("_type", "json")
	body, err := get(ctx, newClient(), strings.TrimRight(s.BaseURL, "/")+"/"+op+"?"+params.Encode())
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", op, err)
	}
	items, total, err := parseTour(body)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", op, err)
	}
	return items, total, nil
}

// Fetch implements ingest.Source.
func (s *TourAPI) Fetch(ctx context.Context, now time.Time) ([]Record, int, map[string]int, error) {
	lookback := s.Lookback
	if lookback <= 0 {
		lookback = 400
	}
	dropped := map[string]int{}
	var recs []Record
	fetched := 0
	const rows = 1000
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("eventStartDate", now.In(ingest.KST).AddDate(0, 0, -lookback).Format("20060102"))
		q.Set("lDongRegnCd", "11") // 서울
		q.Set("numOfRows", strconv.Itoa(rows))
		q.Set("pageNo", strconv.Itoa(page))
		q.Set("arrange", "A")
		items, total, err := s.call(ctx, "searchFestival2", q)
		if err != nil {
			return recs, fetched, dropped, err
		}
		fetched += len(items)
		for _, it := range items {
			r := tourRecord(it, now)
			if ingest.Expired(r.Spot, now, s.RetainEndedDays) {
				dropped[ingest.DropEnded]++
				continue
			}
			recs = append(recs, r)
		}
		if len(items) == 0 || page*rows >= total {
			break
		}
	}
	if !s.Details {
		return recs, fetched, dropped, nil
	}
	for i := range recs {
		if ctx.Err() != nil {
			return recs, fetched, dropped, ctx.Err()
		}
		if recs[i].Spot.Ended(now) {
			continue // 끝난 항목(보존 기간 안)은 상세 호출을 아낀다
		}
		id := recs[i].Spot.SourceID
		common, _, err := s.call(ctx, "detailCommon2", url.Values{"contentId": {id}})
		if err != nil {
			return recs, fetched, dropped, err
		}
		intro, _, err := s.call(ctx, "detailIntro2", url.Values{"contentId": {id}, "contentTypeId": {"15"}})
		if err != nil {
			return recs, fetched, dropped, err
		}
		applyTourDetail(&recs[i], common, intro)
	}
	return recs, fetched, dropped, nil
}
