# ARCHITECTURE.md — herezi-api

## 계층

```
            cmd/api, cmd/ingest          ← 조립(wiring): 환경 변수, 구체 저장소 선택, 서버 실행
                 │
     ┌───────────┴────────────┐
 internal/httpapi        internal/store/{memory,postgres}
 (HTTP ↔ 도메인)          (도메인 ↔ 저장소 구현)
     │                        │
     └──────► internal/store ◄┘          ← 인터페이스(SpotReader/SpotWriter, NearbyQuery)
                 │
                 ▼
          internal/domain                ← 핵심 타입·규칙. 아무것도 import하지 않음
```

의존은 위에서 아래로만 흐른다: **domain ← store / httpapi ← cmd**.

| 패키지 | import 가능 | import 금지 |
|---|---|---|
| `internal/domain` | 표준 라이브러리만 | 다른 모든 internal 패키지 |
| `internal/store` | `domain` | 구현체(`memory`, `postgres`), `httpapi` |
| `internal/store/memory`, `internal/store/postgres` | `domain`, `store` | `httpapi`, 서로 |
| `internal/httpapi` | `domain`, `store`(인터페이스), `auth`(인터페이스·타입) | 구체 저장소, `geo`, ingest 관련 패키지 |
| `internal/geo` | `domain`, 표준 라이브러리 | `store`, `httpapi`, ingest 관련 패키지 (수집 배치 전용: 네이버 지오코딩·카카오 장소 검색) |
| `internal/auth` | 표준 라이브러리만 | 다른 모든 internal 패키지 |
| `internal/snapshot` | `domain` | 나머지 internal |
| `internal/config` | 표준 라이브러리만 | 나머지 internal |
| `internal/ingest` | `domain`, `geo` | `store`·`httpapi`, 하위 `sources`·`openai` |
| `internal/ingest/sources`, `internal/ingest/openai` | `domain`, `ingest` | `store`·`httpapi`, 서로 |
| `cmd/*` | 모두 | — (조립만 하고 로직을 두지 않는다) |
| `internal/store/storetest` | `domain`, `store` | 구현체 (구현체의 테스트가 이 패키지를 부른다) |

## 상세: `GET /v1/spots/{id}`
**상세는 끝난 행사도 반환한다**: 저장소 `SpotByID`는 시각 조건 없이 ID로만 찾고(없으면 `ErrNotFound` → 404), 핸들러가 요청 시계(`s.now()`)로
`is_ended`(= `domain.Spot.Ended(now)`)를 계산해 붙인다. 근처 목록·지도는 서버가 아니라 앱이 지역 데이터 파일로 기기에서 만든다(아래).

## 지역 데이터 파일: `GET /v1/regions/{region}/spots` (이슈 #74)
앱은 사용자 좌표를 서버로 보내지 않는다. 지역 전체 데이터를 받아 거리·근처·지도·정렬·검색을 기기에서 계산한다.
1. `domain.Region`(현재 `seoul`, 수집 범위와 같은 bbox). 모르는 지역은 404.
2. `domain.RegionWindow(now)` = [now − 90일(`RetainEndedDays`), now + 90일(`RegionAheadDays`)]와 겹치는 항목. 저장소는 `SpotsInBBox`를 쓰되
   `SpotFilter.Now`를 창의 시작으로 당기고 `Limit` 0(제한 없음)으로 불러 **종료 후 90일 안의 끝난 항목도 넣는다**(목록·지도와 다른 점). ID순으로 정렬.
3. `httpapi/regions.go`의 메모리 캐시: 지역별 본문(원본 + gzip BestCompression)·강한 ETag(본문 SHA-256, gzip 표현은 `-gz` 접미사)를
   만들어 두고 **최대 10분마다** 다시 만든다. 항목이 같으면 이전 본문·`generated_at`·ETag를 유지해 `If-None-Match` → 304가 이어진다.
   다시 만들기가 실패하면 이전 본문을 주고 1분 뒤 재시도, 이전 본문이 없으면 500. 수집(cron)은 별도 프로세스라 반영은 최대 10분 늦다.
4. 응답 헤더: `ETag`, `Cache-Control: public, max-age=3600`, `Vary: Accept-Encoding`, gzip이면 `Content-Encoding: gzip`.

## 제거한 좌표 엔드포인트 (#74, 2026-11-25)
좌표를 받던 `GET /v1/spots`(근처)·`GET /v1/spots/map`(bbox)·`GET /v1/geo/reverse`(역지오코딩)는 두 앱의 전환을 확인한 뒤 지웠다.
- 라우트 표(`Routes()`·openapi)에는 없고, `httpapi.GonePaths`가 이 세 경로를 **410 Gone** + `Error{code: "gone"}`으로 답한다(쿼리를 읽지 않음).
  옛 빌드(TestFlight 1–2, Play ≤4)가 404 대신 분명한 오류를 받게 하려는 것이다. `/v1/spots/map`은 리터럴 경로라 `/v1/spots/{id}`보다 우선한다.
- 서버 역지오코딩 클라이언트(네이버 Reverse Geocoding·캐시)도 지웠다. `internal/geo`에는 수집 배치용 지오코딩(주소 → 좌표)·장소 검색만 남는다.
- 접속 로그의 좌표 축약(`logredact.go`)은 **방어용으로 남긴다** — 옛 앱 등이 좌표를 붙여 보내도 원좌표가 로그에 남지 않게.
- 계약 테스트 `TestNoCoordinateEndpoints`: openapi에 좌표 파라미터·deprecated 연산·지운 경로가 없어야 한다.
- 저장소의 `NearbySpots`(+ `pg_trgm` 키워드 조건)는 HTTP에서 더 쓰지 않는다. 정리는 후속 작업.

## 인증·저장: `/v1/me/*` (이슈 #56, TECH-PLAN §4.1)
- 앱은 카카오·Apple → Supabase Auth 로그인 후 받은 access token을 `Authorization: Bearer <JWT>`로 보낸다. 조회 API는 인증 없이 그대로.
- `internal/auth`: `Verifier` 인터페이스와 Supabase 구현(표준 라이브러리 crypto만). 프로젝트 JWKS(`<SUPABASE_URL>/auth/v1/.well-known/jwks.json`, ES256/RS256)를
  10분 캐시하고, 모르는 `kid`는 1분 간격으로 다시 받는다(받기 실패 시 이전 키 사용). `SUPABASE_JWT_SECRET`이 있으면 레거시 HS256도 받는다.
  `iss`=`<SUPABASE_URL>/auth/v1`, `aud`∋`authenticated`, `exp`(30초 오차 허용), `sub`=UUID(사용자 ID), 익명 사용자 거절.
- `httpapi.authed` 미들웨어: 설정 없음 → 503 `auth_not_configured`, 토큰 문제 → 401 `unauthorized`, JWKS 못 받음 → 503 `auth_unavailable`.
  `/v1/me/*` 응답은 `Cache-Control: no-store`. 토큰·사용자 ID는 로그에 남기지 않는다.
- 저장소: `store.SavedStore`(메모리·postgres, `storetest.RunSaved` 공통 스위트). 테이블 `saved_spots`(migrations/00007, PK(user_id, spot_id),
  `spot_id` → `spots(id) ON DELETE CASCADE`). 수집 배치가 종료 후 90일 지난 볼거리를 정리하면 저장 행도 사라진다. 목록은 정리 전이라도
  보존 기간(`domain.RetainEndedDays`)이 지난 항목을 뺀다. 사용자당 500개(`store.MaxSavedPerUser`) — 넘으면 409.
- 회원 탈퇴 `DELETE /v1/me`: ① `auth.SupabaseAdmin`(`SUPABASE_SERVICE_ROLE_KEY`)으로 `DELETE /auth/v1/admin/users/{id}` → ② 성공하면 `DeleteUserSaved` → 204.
  ①이 실패하면 502이고 저장은 남는다. ①이 404(이미 없음)면 성공으로 본다 — ②가 실패해 500을 받은 앱이 같은 토큰으로 재시도하면 정리가 끝난다.
- `cmd/api`가 `SUPABASE_URL`이 있을 때만 `httpapi.WithAuth`를, `SUPABASE_SERVICE_ROLE_KEY`도 있을 때만 `httpapi.WithUserDeleter`를 꽂는다. 핸들러 테스트는 가짜 `Verifier`, 검증기 테스트는 테스트용 키쌍 + 가짜 JWKS 서버.

## 계약과 테스트
- `api/openapi.yaml`이 단일 기준. `internal/httpapi/contract_test.go`가 카테고리 enum, `when` enum, 라우트 목록, 응답 JSON 필드를 대조한다.
- `internal/store/storetest`: 저장소 공통 스위트. 메모리는 `go test`, postgres는 `-tags integration`(CI의 PostGIS 서비스 컨테이너)에서 같은 스위트를 돈다.
- 핸들러 테스트는 `httptest` + 메모리 저장소 + 고정 시계(`httpapi.WithClock`).

## 데이터베이스
- PostgreSQL + PostGIS. 스키마는 `migrations/`(goose, `migrations.FS`로 embed).
- DB 접근은 GORM(pgx 드라이버). 모델(`spotRow`)은 `internal/store/postgres` 안에만 두고 `domain.Spot`과 변환한다. `domain`은 gorm을 import하지 않는다.
- 스키마의 기준은 goose 마이그레이션이다. `AutoMigrate`는 쓰지 않는다(geography 컬럼·GiST 인덱스를 표현할 수 없음).
- 공간 조건(`ST_DWithin`, `ST_Distance`, `ST_MakePoint` 등)은 GORM raw 표현식(`Where`, `Select`, `gorm.Expr`)으로 쓴다. 사용자 입력은 항상 `?` 바인딩으로 넘긴다.

## 수집 배치: `cmd/ingest` (이슈 #42)
```
sources.*.Fetch ─▶ ingest.Record ─▶ Pipeline: 필터(종료 후 90일 지남·서울 밖·90일 넘는 미래) ─▶ 좌표 보강(장소명 색인 → 네이버 Geocoding → 카카오 키워드)
   ─▶ Dedup(정규화 제목 + 기간 겹침 + 100m, 이미지·원본 좌표 있는 쪽이 대표) ─▶ GPT 보강(content_hash 캐시, 요청 수 상한, 배치 JSON)
   ─▶ Sink: DATABASE_URL 있으면 postgres.UpsertSpot + DeleteSpots(볼거리 아님) + PruneEnded(종료 후 90일 지남),
            없으면 MergeRetained(이전 스냅샷 중 안 보인 항목 유지) → snapshot.Write(-out)
```
- `internal/ingest`: `Source` 인터페이스, `Record`(정규화 Spot + 원천 분류·설명·요금 텍스트), 정규화 도우미(날짜·HTML·무료 규칙·키워드 분류), `Dedup`, `Pipeline`, `Enricher` 인터페이스.
- `internal/ingest/sources`: 소스별 수집기(서울 문화행사, 공공서비스예약, TourAPI, 문화포털)와 원천 분류 → 9개 카테고리 매핑 표. 파서는 `testdata/` 실제 응답 픽스처로 테스트.
- `internal/ingest/openai`: Chat Completions 배치 클라이언트(net/http). GPT는 요약(60자)·애매한 분류·애매한 무료만. 날짜·좌표·가격은 원천 값만 쓴다.
- GPT 캐시: `domain.Spot.ContentHash`(원문 해시). postgres는 `content_hash` 컬럼(`EnrichedSpots`), 스냅샷은 이전 `-out` 파일을 읽어 해시가 같으면 재호출하지 않는다.
- `cmd/api`는 `DATABASE_URL`이 없고 `SNAPSHOT_PATH`가 있으면 스냅샷을 메모리 저장소에 올린다. httpapi는 ingest 쪽을 import하지 않는다.
- 좌표 보강 순서: ① 같은 구·같은 장소명의 다른 항목 좌표 ② 네이버 Geocoding(주소) ③ 카카오 키워드 검색(`geo.KakaoPlaces`, `KAKAO_REST_API_KEY`, "장소명 구", 서울 rect, 괄호·세부 공간을 줄인 이름 최대 3개). 카카오 후보는 서울 안 + 이름 유사도(`ingest.SimilarPlaceName`: 포함 또는 bigram Dice ≥ 0.5)를 통과한 첫 건만 쓴다. 질의 결과는 실행 동안 캐시하고, 찾은 좌표는 장소명 색인에 넣어 재사용한다.
- **끝난 항목 보존(이슈 #51)**: 종료 후 `-retain-ended-days`(기본 90일) 동안 저장해 상세(`is_ended: true`)·저장 목록에서 보여 준다. 목록·지도·검색은 계속 제외. 원천 목록에서 빠져도 지우지 않고(DB는 `last_seen_at` 갱신만 멈춤, 스냅샷은 이전 스냅샷에서 유지), 보존 기간이 지난 행만 정리한다. 끝난 항목에는 GPT를 쓰지 않는다. 서울 문화행사는 END_DATE가 보존 기간 안인 항목만 싣는다.
- 외부 호출 에러에는 URL(키·주소)을 넣지 않는다(`url.Error`를 풀고 HTTP 상태만).
