package domain

import (
	"fmt"
	"time"
)

// When is the time filter (KST). 서버 쿼리 파라미터였던 GET /v1/spots는 #74에서 지웠고,
// 지금은 앱이 지역 파일을 기기에서 거를 때 쓰는 공통 값이다(api/openapi.yaml components.schemas.When).
type When string

// When values (api/openapi.yaml components.schemas.When).
const (
	WhenNow     When = "now"
	WhenToday   When = "today"
	WhenWeekend When = "weekend"
	WhenWeek    When = "week"
)

// Seoul is the reference time zone for "today"/"weekend".
var Seoul = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}()

// ParseWhen parses the `when` query value; empty means WhenNow.
func ParseWhen(s string) (When, error) {
	switch When(s) {
	case "":
		return WhenNow, nil
	case WhenNow, WhenToday, WhenWeekend, WhenWeek:
		return When(s), nil
	}
	return "", fmt.Errorf("unknown when %q (want now|today|weekend|week)", s)
}

// Window returns the [from, to] interval a spot must overlap to be shown.
// `from` is never before now, so finished events are never shown (PRODUCT.md 원칙 3).
func (w When) Window(now time.Time) (from, to time.Time) {
	local := now.In(Seoul)
	endOfDay := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 23, 59, 59, 0, Seoul)
	}
	switch w {
	case WhenToday:
		return now, endOfDay(local)
	case WhenWeekend:
		wd := local.Weekday()
		var sat time.Time
		switch wd {
		case time.Saturday:
			sat = local
		case time.Sunday:
			sat = local.AddDate(0, 0, -1)
		default:
			sat = local.AddDate(0, 0, int(time.Saturday-wd))
		}
		y, m, d := sat.Date()
		start := time.Date(y, m, d, 0, 0, 0, 0, Seoul)
		if start.Before(now) {
			start = now
		}
		return start, endOfDay(sat.AddDate(0, 0, 1))
	case WhenWeek:
		return now, now.AddDate(0, 0, 7)
	default: // WhenNow
		return now, now
	}
}
