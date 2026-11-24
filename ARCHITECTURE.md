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
| `internal/httpapi` | `domain`, `store`(인터페이스), `geo`(인터페이스·타입) | 구체 저장소, ingest 관련 패키지 |
| `internal/geo` | `domain`, 표준 라이브러리 | `store`, `httpapi`, ingest 관련 패키지 |
| `internal/snapshot` | `domain` | 나머지 internal |
| `internal/config` | 표준 라이브러리만 | 나머지 internal |
| `internal/ingest` | `domain`, `geo` | `store`·`httpapi`, 하위 `sources`·`openai` |
| `internal/ingest/sources`, `internal/ingest/openai` | `domain`, `ingest` | `store`·`httpapi`, 서로 |
| `cmd/*` | 모두 | — (조립만 하고 로직을 두지 않는다) |
| `internal/store/storetest` | `domain`, `store` | 구현체 (구현체의 테스트가 이 패키지를 부른다) |

## 핵심 흐름: `GET /v1/spots`
1. `httpapi`가 쿼리 파라미터를 검증·파싱 → 잘못되면 400 + `Error{code, message}`.
2. `when`을 `domain.When.Window(now)`로 [from, to] 구간으로 바꾸고 `store.NearbyQuery`를 만든다.
3. 저장소가 반경·카테고리·무료·기간 겹침·키워드(`q`: 제목·장소명·주소 ILIKE, pg_trgm 인덱스)·**끝난 행사 제외** 조건으로 거리순 조회.
   - 메모리: 하버사인 거리(`domain.DistanceM`)로 같은 의미를 흉내낸다.
   - postgres: `geography` + `ST_DWithin`/`ST_Distance` (GiST 인덱스).
4. `limit+1`개를 받아 다음 페이지 유무를 판단하고, 불투명 커서(`next_cursor`)를 돌려준다.

`GET /v1/spots/map`도 같은 필터(끝난 행사 제외)를 쓴다. 반면 **상세 `GET /v1/spots/{id}`는 끝난 행사도 반환한다**:
저장소 `SpotByID`는 시각 조건 없이 ID로만 찾고(없으면 `ErrNotFound` → 404), 핸들러가 요청 시계(`s.now()`)로
`is_ended`(= `domain.Spot.Ended(now)`)를 계산해 붙인다. 목록·지도 응답의 `is_ended`는 항상 `false`다.

## 역지오코딩: `GET /v1/geo/reverse`
- 네이버 Reverse Geocoding은 Client Secret이 필요하므로 앱 대신 서버가 부른다.
- `internal/geo`: `ReverseGeocoder` 인터페이스, 네이버 클라이언트(`Naver`, 타임아웃 2초), 메모리 캐시(`Cache`, 좌표 소수 3자리 반올림·TTL 1일, 실패는 캐시 안 함).
- `cmd/api`가 `NCP_MAPS_CLIENT_ID`/`NCP_MAPS_CLIENT_SECRET`이 있을 때만 `Cache(Naver)`를 `httpapi.WithGeocoder`로 꽂는다. 없으면 501.
- 핸들러 테스트는 가짜 `ReverseGeocoder`, 네이버 클라이언트는 `httptest`로 요청·파싱을 검증한다.

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
