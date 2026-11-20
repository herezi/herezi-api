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
| `internal/httpapi` | `domain`, `store`(인터페이스) | 구체 저장소, ingest 관련 패키지 |
| `cmd/*` | 모두 | — (조립만 하고 로직을 두지 않는다) |
| `internal/store/storetest` | `domain`, `store` | 구현체 (구현체의 테스트가 이 패키지를 부른다) |

## 핵심 흐름: `GET /v1/spots`
1. `httpapi`가 쿼리 파라미터를 검증·파싱 → 잘못되면 400 + `Error{code, message}`.
2. `when`을 `domain.When.Window(now)`로 [from, to] 구간으로 바꾸고 `store.NearbyQuery`를 만든다.
3. 저장소가 반경·카테고리·무료·기간 겹침·**끝난 행사 제외** 조건으로 거리순 조회.
   - 메모리: 하버사인 거리(`domain.DistanceM`)로 같은 의미를 흉내낸다.
   - postgres: `geography` + `ST_DWithin`/`ST_Distance` (GiST 인덱스).
4. `limit+1`개를 받아 다음 페이지 유무를 판단하고, 불투명 커서(`next_cursor`)를 돌려준다.

## 계약과 테스트
- `api/openapi.yaml`이 단일 기준. `internal/httpapi/contract_test.go`가 카테고리 enum, `when` enum, 라우트 목록, 응답 JSON 필드를 대조한다.
- `internal/store/storetest`: 저장소 공통 스위트. 메모리는 `go test`, postgres는 `-tags integration`(CI의 PostGIS 서비스 컨테이너)에서 같은 스위트를 돈다.
- 핸들러 테스트는 `httptest` + 메모리 저장소 + 고정 시계(`httpapi.WithClock`).

## 데이터베이스
- PostgreSQL + PostGIS. 스키마는 `migrations/`(goose, `migrations.FS`로 embed).
- SQL은 `internal/store/postgres/queries.sql`에만 쓰고, `sqlc generate`로 `sqlcdb/`를 만든다.

## 앞으로 들어올 것
- `cmd/ingest` 아래 수집 파이프라인(수집기 → 정규화 → 지오코딩 → GPT 보강 → 중복 제거). 수집기와 정규화는 `domain.RawItem`으로만 연결해 서로 의존하지 않게 한다. httpapi는 ingest 쪽을 import하지 않는다.
