package ingest

import (
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// KST는 원천 날짜의 기준 시간대(UTC+9, 서머타임 없음). tzdata 없이 동작하도록 고정 오프셋.
var KST = time.FixedZone("KST", 9*60*60)

var dateLayouts = []string{
	"2006-01-02 15:04:05.0", "2006-01-02 15:04:05", "2006-01-02", "20060102", "2006.01.02", "2006.1.2",
}

// ParseDate는 원천 날짜 문자열(KST)을 읽는다. dateOnly는 시각 정보가 없거나 00:00:00이었는지.
func ParseDate(s string) (t time.Time, dateOnly bool, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false, false
	}
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, KST); err == nil {
			return t, t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0, true
		}
	}
	return time.Time{}, false, false
}

// ParseRange는 시작·종료 문자열을 Spot 기간으로 바꾼다. 날짜만 있는 종료일은 그날 23:59:59 KST
// (DATA-PLAN.md Q1). 종료가 없거나 읽을 수 없으면 nil. 시작을 읽을 수 없으면 ok=false.
func ParseRange(start, end string) (startsAt time.Time, endsAt *time.Time, ok bool) {
	st, _, ok := ParseDate(start)
	if !ok {
		return time.Time{}, nil, false
	}
	if et, dateOnly, ok := ParseDate(end); ok {
		if dateOnly {
			et = et.Add(24*time.Hour - time.Second)
		}
		if et.Before(st) {
			et = st
		}
		return st.UTC(), ptr(et.UTC()), true
	}
	return st.UTC(), nil, true
}

func ptr[T any](v T) *T { return &v }

var (
	// 알려진 HTML 태그만 지운다. 제목의 "<햄릿>", "<One Voice>" 같은 꺾쇠 표기는 남긴다.
	tagRe   = regexp.MustCompile(`(?is)<!--.*?-->|</?(p|br|div|span|a|img|figure|figcaption|strong|b|i|em|u|font|table|tr|td|th|tbody|thead|ul|ol|li|h[1-6]|hr|section|article|style|script|o:p)\b[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// CleanText는 HTML 태그·엔티티를 없애고 공백을 하나로 줄인다. 엔티티가 두 번 인코딩된
// 경우(문화포털 "&amp;lt;")도 풀린다.
func CleanText(s string) string {
	for range 2 {
		s = html.UnescapeString(s)
	}
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, " ", " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// Truncate는 s를 최대 n글자(rune)로 자른다.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Seoul bbox(행정 경계를 살짝 감싸는 사각형). 폴리곤 검사는 M1 이후 과제.
const (
	seoulMinLat, seoulMaxLat = 37.413, 37.716
	seoulMinLng, seoulMaxLng = 126.734, 127.270
)

// InSeoul은 좌표가 서울 범위 안인지 본다.
func InSeoul(lat, lng float64) bool {
	return lat >= seoulMinLat && lat <= seoulMaxLat && lng >= seoulMinLng && lng <= seoulMaxLng
}

// FixCoords는 위도·경도가 뒤바뀐 값(위도 자리에 127.x)을 바로잡는다(DATA-PLAN.md §6.2).
func FixCoords(lat, lng float64) (float64, float64) {
	if lat > 90 && lng < 90 {
		return lng, lat
	}
	return lat, lng
}

// HTTPS는 http:// 이미지 URL을 https로 바꾼다(앱 ATS). 그 외는 그대로.
func HTTPS(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "http://") {
		return "https://" + strings.TrimPrefix(u, "http://")
	}
	return u
}

var (
	wonRe      = regexp.MustCompile(`[1-9][0-9,]*\s*(원|만원|천원)`)
	freeWordRe = regexp.MustCompile(`무료|free|Free|FREE`)
	paidWordRe = regexp.MustCompile(`유료|입장료|관람료|티켓|예매|R석|S석|VIP`)
)

// DetectFree는 요금 텍스트로 무료 여부를 규칙 판정한다. 확신이 없으면 nil(→ GPT 또는 모름).
//   - "무료"만 있고 금액이 없으면 true
//   - 0이 아닌 금액(…원)이 있고 "무료"가 없으면 false
//   - 그 밖(빈 문자열, 섞여 있음, "별도 문의")은 nil
func DetectFree(text string) *bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	free := freeWordRe.MatchString(t)
	won := wonRe.MatchString(t)
	switch {
	case free && !won && !paidWordRe.MatchString(t):
		return ptr(true)
	case won && !free:
		return ptr(false)
	}
	return nil
}

// FreeFromFlag는 원천의 무료/유료 플래그("무료", "유료")를 읽는다. 다른 값이면 nil.
func FreeFromFlag(flag string) *bool {
	switch strings.TrimSpace(flag) {
	case "무료":
		return ptr(true)
	case "유료":
		return ptr(false)
	}
	return nil
}

// 키워드 재분류: 원천 분류에 없는 pet·sports·food를 제목으로 찾는다(DATA-PLAN.md §3.3).
var (
	petRe    = regexp.MustCompile(`반려동물|반려견|반려묘|댕댕|강아지|애견|멍멍|(^|[^가-힣])펫`) // 트럼펫·카펫 제외
	sportsRe = regexp.MustCompile(`마라톤|러닝|런닝|레이스|걷기대회|자전거대회|체육대회|생활체육|(^|[^일])요가|필라테스|트레킹|등반대회|씨름|스포츠|` +
		`클라이밍|축구|풋살|야구|농구|배구|테니스|배드민턴|탁구|골프|게이트볼|볼링|당구|수영|스케이트|스키|라인댄스|밸리댄스|활쏘기|국궁`)
	// 요가: "일요가족" 오분류 방지(이슈 #44).
	// plantRe: "반려식물"은 반려동물이 아니다(이슈 #44 오분류).
	plantRe = regexp.MustCompile(`반려\s*식물`)
	// tourRe: 투어·해설·견학은 공연이 아니다(재판정에서 "도보관광 프로그램" → performance 오분류).
	tourRe = regexp.MustCompile(`투어|해설|도보|견학|탐방|답사|산책`)
	foodRe = regexp.MustCompile(`야시장|먹거리|푸드|미식|맥주|와인|막걸리|디저트|빵축제|김장|음식축제|요리`)
)

// KeywordCategory는 제목 키워드로 pet·sports·food를 판정한다. base가 festival이거나 sure가
// false일 때만 sports·food를 덮어쓴다(전시 "음식의 역사" 같은 오분류 방지). pet은 항상.
func KeywordCategory(title string, base domain.Category, sure bool) (domain.Category, bool) {
	switch {
	case petRe.MatchString(title):
		return domain.CategoryPet, true
	case base != domain.CategoryFestival && sure:
		return base, sure
	case sportsRe.MatchString(title):
		return domain.CategorySports, true
	case foodRe.MatchString(title):
		return domain.CategoryFood, true
	}
	return base, sure
}

// AcceptGPTCategory는 GPT가 고른 카테고리를 쓸지 정한다. popup은 GPT로 정하지 않고(수기 데이터),
// pet은 반려동물 언급이 있을 때만(반려식물 제외), sports는 운동 종목 언급이 있을 때만,
// performance는 투어·해설·견학이 아닐 때만 받는다
// (이슈 #44: "반려식물" → pet, "심폐소생술교육" → sports, "도보관광 프로그램" → performance 오분류).
// 받지 않으면 규칙 카테고리를 그대로 쓴다. 캐시된 이전 판정에도 같은 검사를 한다.
func AcceptGPTCategory(c domain.Category, title, desc string) bool {
	text := title + " " + desc
	switch {
	case !c.Valid() || c == domain.CategoryPopup:
		return false
	case c == domain.CategoryPet:
		return petRe.MatchString(text) && !plantRe.MatchString(title)
	case c == domain.CategorySports:
		return sportsRe.MatchString(text)
	case c == domain.CategoryPerformance:
		return !tourRe.MatchString(title)
	}
	return true
}
