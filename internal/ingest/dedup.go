package ingest

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/herezi/herezi-api/internal/domain"
)

// DedupRadiusM는 같은 행사로 볼 최대 거리(이슈 #42: 100m).
const DedupRadiusM = 100.0

var (
	bracketRe = regexp.MustCompile(`[\[(<〈《「『【][^\])>〉》」』】]*[\])>〉》」』】]`)
	yearRe    = regexp.MustCompile(`(19|20)\d{2}(년)?`)
)

// NormalizeTitle은 중복 판정용 제목 키: 괄호 안 내용·연도·공백·문장부호를 없애고 소문자로.
// 괄호를 지우면 아무것도 안 남는 제목("[세 개의 심장]")은 괄호 안 글자를 남긴다.
func NormalizeTitle(t string) string {
	t = strings.ToLower(CleanText(t))
	if k := keep(yearRe.ReplaceAllString(bracketRe.ReplaceAllString(t, " "), " ")); len([]rune(k)) >= 2 {
		return k
	}
	return keep(yearRe.ReplaceAllString(t, " "))
}

func keep(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sameTitle: 정규화 제목이 같거나, 4글자 이상인 한쪽이 다른 쪽에 포함되면 같은 행사 후보.
func sameTitle(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if len([]rune(a)) > len([]rune(b)) {
		a, b = b, a
	}
	return len([]rune(a)) >= 4 && strings.Contains(b, a)
}

func overlaps(a, b domain.Spot) bool {
	aEnd, bEnd := a.StartsAt, b.StartsAt
	if a.EndsAt != nil {
		aEnd = *a.EndsAt
	}
	if b.EndsAt != nil {
		bEnd = *b.EndsAt
	}
	return !a.StartsAt.After(bEnd) && !b.StartsAt.After(aEnd)
}

// sourceRank는 동점일 때 대표로 남길 소스 순서(원천 성격이 가까운 쪽, DATA-PLAN.md §6.3).
var sourceRank = map[string]int{
	"seoul_culture": 4, "tourapi": 3, "seoul_reservation": 2, "cultureinfo": 1,
}

// score: 이미지·원본 좌표가 있는 쪽을 대표로(이슈 #42), 그다음 요금 정보·소스 순서.
func score(r Record, geocoded bool) int {
	s := sourceRank[r.Spot.Source]
	if r.Spot.ImageURL != "" {
		s += 100
	}
	if !geocoded {
		s += 50
	}
	if r.Spot.IsFree != nil {
		s += 10
	}
	return s
}

// Dedup은 정규화 제목 + 기간 겹침 + 거리 ≤ 100m인 항목을 하나로 합친다. 같은 공연장의
// 다른 공연은 제목이 달라 합쳐지지 않는다(DATA-PLAN.md §6.3 5). geocoded[i]는 recs[i]의
// 좌표가 지오코딩 결과인지. 남은 항목과, 합쳐져 빠진 항목의 소스별 수를 돌려준다.
func Dedup(recs []Record, geocoded []bool) (kept []Record, merged map[string]int) {
	merged = map[string]int{}
	n := len(recs)
	keys := make([]string, n)
	parent := make([]int, n)
	for i := range recs {
		keys[i] = NormalizeTitle(recs[i].Spot.Title)
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// 약 220m 격자로 나눠 이웃 칸끼리만 비교한다.
	const cell = 0.002
	type ck struct{ x, y int }
	grid := map[ck][]int{}
	cellOf := func(r Record) ck {
		return ck{int(math.Floor(r.Spot.Lat / cell)), int(math.Floor(r.Spot.Lng / cell))}
	}
	for i, r := range recs {
		c := cellOf(r)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for _, j := range grid[ck{c.x + dx, c.y + dy}] {
					a, b := recs[i].Spot, recs[j].Spot
					if sameTitle(keys[i], keys[j]) && overlaps(a, b) &&
						domain.DistanceM(a.Lat, a.Lng, b.Lat, b.Lng) <= DedupRadiusM {
						parent[find(i)] = find(j)
					}
				}
			}
		}
		grid[c] = append(grid[c], i)
	}
	best := map[int]int{} // root → 대표 index
	for i := range recs {
		root := find(i)
		j, ok := best[root]
		if !ok || score(recs[i], geocoded[i]) > score(recs[j], geocoded[j]) {
			best[root] = i
		}
	}
	// 대표에 없는 무료 여부·이미지·설명은 같은 그룹의 다른 항목에서 채운다.
	for i := range recs {
		j := best[find(i)]
		if i == j {
			continue
		}
		merged[recs[i].Spot.Source]++
		rep, o := &recs[j], recs[i]
		if rep.Spot.IsFree == nil && o.Spot.IsFree != nil {
			rep.Spot.IsFree = o.Spot.IsFree
		}
		if rep.FeeText == "" {
			rep.FeeText = o.FeeText
		}
		if rep.Spot.ImageURL == "" {
			rep.Spot.ImageURL = o.Spot.ImageURL
		}
		if len(o.Description) > len(rep.Description) {
			rep.Description = o.Description
		}
	}
	for i := range recs {
		if best[find(i)] == i {
			kept = append(kept, recs[i])
		}
	}
	return kept, merged
}
