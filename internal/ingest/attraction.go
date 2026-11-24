package ingest

import (
	"regexp"
	"strings"
)

// 볼거리 여부 제외 사유(리포트 키, 이슈 #44).
const (
	// DropRecruit: 모집·신청·공모 공고(참가자·교육생·서포터즈·자원봉사·패스 신청, 기관·학급 단체 전용).
	DropRecruit = "recruit"
	// DropCourse: 수강 등록이 필요한 다회차 정규 강좌(n주 과정·학기·반 편성).
	DropCourse = "course"
	// DropService: 볼거리가 아닌 교육·서비스(심폐소생술 등 안전·직무 교육, 상담·클리닉).
	DropService = "service"
	// DropGPTNotAttraction: 규칙으로 애매해 GPT가 볼거리 아님으로 판정.
	DropGPTNotAttraction = "gpt_not_attraction"
)

// 볼거리 판정 기준(DATA-PLAN.md §7 Q11). 누구나 그 시간에 가서 보거나 한 번 참여할 수 있으면 볼거리.
//   - 제외: 모집·신청 공고, 다회차 정규 강좌, 단체·기관 전용, 안전·직무 교육, 상담·클리닉 서비스.
//   - 유지(family): 1회성 체험·가족·어린이 교육 프로그램(사전 예약이 필요해도).
//   - 애매(GPT is_attraction): "모집·신청·교실·강좌·레슨·워크숍" 등이 있지만 위 규칙에 안 걸리는 제목.
var (
	recruitRe = regexp.MustCompile(strings.Join([]string{
		`(신청자|참가자|참여자|교육생|수강생|단원|회원|봉사자|작가|강사|운영진|팀|기관|단체)\s*(모집|선발|공모)`,
		`\d+\s*기\s*모집`, `기관\s*모집`, `모집\s*공고`, `공개\s*모집`,
		`서포터즈`, `자원\s*봉사`, `봉사단`, `기자단`, `체험단`, `홍보단`, `오디션`, `채용`,
		`패스\s*신청`, `학급\s*단체`, `학급\s*대상`, `단체\s*(교육|예약|대상)`, `공모전`, `공모\s*(사업|접수|안내|모집)`,
		`접수\s*안내`, `사연.*접수`,
	}, "|"))
	courseRe = regexp.MustCompile(strings.Join([]string{
		`\d+\s*주\s*(과정|코스|\))`, `\(\d+\s*주\)`, `\d+\s*개월\s*과정`, `정규\s*(강좌|과정|교육)`, `(봄|여름|가을|겨울|상반기|하반기|[0-9])\s*학기`,
		`(초급|중급|고급|기초|입문|실습|심화|왕초급|초중급|유아|어린이|초등|중등|고등|여성|성인)[A-Z]?반`,
		`(^|[^A-Za-z])[A-Z]반`, `(^|[^다])양성\s*(교육|과정)`, `자격\s*(과정|취득)`, `교육과정`,
	}, "|"))
	serviceRe = regexp.MustCompile(strings.Join([]string{
		`심폐소생술`, `자동심장충격기`, `응급처치`, `CPR`, `안전\s*교육`, `관리자\s*교육`, `직무\s*연수`, `직무연수`,
		`클리닉`, `상담`, `검진`, `예방접종`, `설명회`, `취업`,
	}, "|"))
	// maybeRe: 볼거리일 수도, 아닐 수도 있는 제목 → GPT is_attraction.
	maybeRe = regexp.MustCompile(`모집|신청|접수|교실|강좌|강의|레슨|아카데미|워크숍|워크샵|특강|연수|과정|학교|교육|수업`)
	// keepRe: maybeRe에 걸려도 볼거리가 분명한 제목(키즈카페·축제·공연·전시).
	keepRe = regexp.MustCompile(`키즈카페|축제|페스티벌|페스타|연주회|연극제|콘서트|리사이틀|기획전|특별전|전시회|개인전`)
	// neutralRe: 규칙 판정 전에 지우는 표현. 공모전시(공모로 뽑은 작품 전시)는 공고가 아니라 전시이고,
	// "예약 상담 필수·단체 상담 환영"은 상담 서비스가 아니라 예약 안내다.
	neutralRe = regexp.MustCompile(`공모\s*전시|공모展|(예약|단체)\s*상담\s*(필수|환영|가능)?|상담\s*(필수|환영|가능)`)
)

// AttractionRule은 제목으로 볼거리 여부를 규칙 판정한다(소스 공통, 이슈 #44).
// reason이 있으면 제외, maybe가 true면 GPT 판정 대상, 둘 다 아니면 볼거리.
func AttractionRule(title string) (reason string, maybe bool) {
	t := neutralRe.ReplaceAllString(title, " ")
	switch {
	case serviceRe.MatchString(t):
		return DropService, false
	case recruitRe.MatchString(t):
		return DropRecruit, false
	case courseRe.MatchString(t):
		return DropCourse, false
	}
	return "", maybeRe.MatchString(t) && !keepRe.MatchString(t)
}
