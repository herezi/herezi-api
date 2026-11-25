package domain

import "time"

// Notice is one 공지사항 (이슈 #80). 작성은 Supabase Table Editor에서 하고, API는 읽기만 한다.
type Notice struct {
	ID          int64
	Title       string
	Body        string // 마크다운 원문
	PublishedAt time.Time
	IsHidden    bool
}

// Published reports whether n is visible at now: 게시 시각이 지났고(같으면 포함) 숨김이 아니다.
func (n Notice) Published(now time.Time) bool {
	return !n.IsHidden && !n.PublishedAt.After(now)
}
