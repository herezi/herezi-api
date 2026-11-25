# 공지사항 쓰는 법 (Supabase Table Editor)

공지사항은 DB `notices` 테이블(migrations/00008, 이슈 #80)에 있고, 웹 공지 페이지(https://herezi.github.io/notices/)가
`GET /v1/notices`·`GET /v1/notices/{id}`로 읽어 보여 준다. 앱 MY 탭 "공지사항"은 이 웹 페이지를 인앱 브라우저로 연다.
**관리자 API는 없다.** 공지 작성·수정·숨김은 Supabase 대시보드에서 행을 직접 고친다. 배포는 필요 없다.

## 새 공지 쓰기

1. Supabase 대시보드 → 프로젝트 `herezi` → **Table Editor** → `notices` 테이블.
2. **Insert → Insert row**.
3. 칸을 채운다.

| 칸 | 넣는 값 |
|---|---|
| `id` | 비워 둔다(자동 번호) |
| `title` | 제목. 목록에 그대로 보인다. 짧게(예: `히어지 테스트를 시작했어요`) |
| `body` | 본문 **마크다운**. 줄바꿈은 그대로 들어간다(문단 사이는 빈 줄 하나). 굵게 `**…**`, 목록 `- …`, 링크 `[글자](https://…)` |
| `published_at` | 게시 시각. 비워 두면 지금. **미래 시각이면 예약 게시** — 그 시각부터 보인다. 시간대를 붙여 쓴다(예: `2026-12-01 09:00:00+09`) |
| `is_hidden` | 보통 `false`. `true`면 목록·상세 모두에서 숨는다 |
| `created_at`, `updated_at` | 비워 둔다(자동). `updated_at`은 행을 고칠 때마다 자동으로 바뀐다 |

4. **Save**. API 응답은 최대 60초 캐시(`Cache-Control: public, max-age=60`)라 1분 안에 웹 페이지에 보인다.

## 고치기·숨기기

- 오타 수정: 해당 행의 `title`·`body`를 고치고 저장.
- 내리기: 행을 지우지 말고 `is_hidden`을 `true`로. 다시 보이게 하려면 `false`.
- 순서: 목록은 `published_at` 최신순(같으면 `id` 큰 것 먼저)이다. 순서를 바꾸려면 `published_at`을 고친다.
- 목록은 최신 50개까지만 돌려준다.

## 확인

```sh
URL=https://api-production-4ebcf3e2.up.railway.app
curl -s $URL/v1/notices                    # 게시된 목록(본문 없음)
curl -s $URL/v1/notices/1                  # 상세(본문 마크다운)
curl -si -H 'Origin: https://herezi.github.io' $URL/v1/notices | grep -i access-control   # CORS
```

## 주의

- 테이블은 RLS가 켜져 있고 `anon`·`authenticated` 권한을 회수했다(00005와 같은 규칙). Supabase Data API(PostgREST)로는
  읽을 수 없고, 공개 읽기는 API 서버만 한다. Table Editor는 소유자 권한이라 그대로 쓸 수 있다.
- 본문에 개인정보·내부 정보(키, 내부 URL)를 쓰지 않는다. 공개된다.
- 스키마(컬럼 추가 등)는 Table Editor에서 바꾸지 않는다. `migrations/`에 새 goose 마이그레이션으로 바꾼다(AGENTS.md 규칙 8).
