# Herezi 기술 기획서

| 항목 | 내용 |
|---|---|
| 상태 | **승인됨** (2026-11-20, 위임 승인) |
| 버전 | v1.4 (2026-11-24) |
| 근거 | [SERVICE-PLAN.md](SERVICE-PLAN.md) v1.0, [DATA-PLAN.md](DATA-PLAN.md) |
| 출시 목표 | 2027년 3월 16일, 서울, iOS · Android 동시 |

> 에이전트 규칙: 기술 선택을 바꾸려면 이 문서를 먼저 고치고 PR로 남긴다. 큰 결정은 `docs/adr/`에 ADR로 기록한다.

---

## 1. 전체 구조

```
 공공데이터 API ─┐
 (TourAPI, KOPIS, │   ┌────────────┐   ┌──────────────────┐   ┌──────────┐
  서울 열린데이터…) ├──▶│ ingest 작업 │──▶│ PostgreSQL+PostGIS │◀──│ api 서버  │◀── iOS / Android 앱
 OpenAI GPT ──────┘   │ (주기 실행) │   └──────────────────┘   │ (Go)     │
 네이버 지오코딩 ─────▶└────────────┘                          └──────────┘
```

- **api**: REST. 볼거리 조회는 인증 없이, **저장 목록(`/v1/me/saved`)만 로그인 사용자 토큰 필요**.
- **ingest**: 소스별 수집기 → 정규화 → 지오코딩 → GPT 보강(요약·분류) → 중복 제거 → 저장. 주기 실행 배치.
- **계약**: `api/openapi.yaml`이 단일 기준. 앱 레포는 `make sync-api`로 복사해서 사용.

## 2. 레포와 스택

| 레포 | 스택 | 비고 |
|---|---|---|
| `herezi-api` | Go 1.25, 표준 `net/http`, GORM(pgx 드라이버), goose, PostGIS | `cmd/api`, `cmd/ingest` 두 실행 파일 |
| `herezi-ios` | Swift 6, SwiftUI, iOS 17+, XcodeGen, 네이버 지도 SDK(SPM) | `.xcodeproj`는 생성물, 커밋 안 함 |
| `herezi-android` | Kotlin, Jetpack Compose, 멀티모듈, Hilt, Retrofit + kotlinx.serialization, 네이버 지도 SDK | minSdk 26 |

### 결정과 이유
| 결정 | 이유 |
|---|---|
| Go 표준 라이브러리 위주 | 엔드포인트가 적고 단순. 의존성 최소화가 에이전트 개발에 유리 |
| PostGIS `geography` + GiST 인덱스 | 반경 검색·거리순 정렬을 DB에서 정확하게 처리 |
| GORM | 사용자 결정(2026-11-20). PostGIS 공간 쿼리는 GORM raw 표현식으로 작성, 스키마는 goose SQL 마이그레이션이 기준 |
| 네이버 지도 (양 앱) | 국내 지도 품질, 길찾기를 네이버지도 앱으로 연결(기획 P0) |
| 지오코딩: 네이버 클라우드 Geocoding + 카카오 키워드 검색 | 좌표 없는 소스 항목 보완. 네이버(주소) 실패 시 카카오 키워드(장소명, 서울 범위·이름 유사도 확인) |
| LLM: OpenAI GPT | 요약·카테고리 분류·무료 여부 추출. 결과는 DB에 캐시, 원문 해시가 같으면 재호출 안 함 |
| 앱 아키텍처: 기능별 모듈 + 도메인 포트 | 가짜 서비스(Fake)로 서버 없이 화면 개발·테스트 가능 |

## 3. 데이터 모델 (요약)

`spots` 테이블 (`migrations/`):
- `id = "<source>:<source_id>"`, `source`, `source_id`
- `title`, `summary`, `category`(9종, `free` 제외), `is_free`
- `geog geography(Point,4326)`, `address`, `place_name`
- `starts_at`, `ends_at`, `image_url`, `source_url`
- `dedup_key`: 정규화 제목 + 기간 + 반경 100m 묶음으로 소스 간 중복 제거
- `content_hash`: GPT 재호출 방지(원문 해시, migrations/00004, 이슈 #42). `enriched_at`은 필요해지면 추가
- `fetched_at`: 앱에 "갱신 시각"으로 표시

조회 규칙: 목록(`/v1/spots`)·지도(`/v1/spots/map`)는 `ends_at < now()`인 항목을 반환하지 않는다. 상세(`/v1/spots/{id}`)는 끝난 항목도 반환하고 `is_ended: true`를 붙인다.

## 4. API (M1)

| 메서드 | 경로 | 설명 |
|---|---|---|
| GET | `/v1/spots` | `lat`, `lng`, `radius_m`, `category`, `when`, `free`, `q`(키워드: 제목·장소명·주소 부분 일치, 1~50자), `cursor`, `limit` → 거리순 |
| GET | `/v1/spots/{id}` | 상세 |
| GET | `/v1/spots/map` | 지도 영역(bbox: `sw_lat`, `sw_lng`, `ne_lat`, `ne_lng`) + `category`, `when`, `free`, 최대 `limit`(기본 200, 최대 500)개, 영역 중심에서 가까운 순. 넘치면 `truncated: true`. 클러스터는 앱에서 처리 |
| GET | `/healthz` | 헬스체크 |

- `Spot.fetched_at`(원본에서 가져온 시각, 필수) — 상세 화면의 "갱신 시각".

- 인증 없음. 대신 앱별 API 키 헤더 + IP 기준 속도 제한.
- 응답 캐시: `Cache-Control: max-age=60`.

## 4.1 인증·저장 (v1.3)

| 항목 | 결정 |
|---|---|
| 로그인 | 카카오, Apple (필수 둘 다). 앱이 각 SDK로 로그인 → **Supabase Auth**에 ID 토큰으로 로그인(`signInWithIdToken`, provider kakao/apple) → Supabase 세션(JWT) |
| API 인증 | `Authorization: Bearer <Supabase JWT>`. API는 Supabase JWKS로 서명·만료 검증, `sub`=user_id. 조회 API는 인증 불필요 |
| 저장 테이블 | `saved_spots(user_id uuid, spot_id text, saved_at timestamptz, PK(user_id, spot_id))` + RLS(본인 행만). API는 서비스 역할로 접근 |
| 엔드포인트 | `GET /v1/me/saved`(스팟 스냅샷 포함, 끝난 항목 `is_ended`), `PUT /v1/me/saved/{spot_id}`, `DELETE /v1/me/saved/{spot_id}`, `DELETE /v1/me`(회원 탈퇴: 저장 삭제 + Supabase 사용자 삭제) |
| 기기 로컬 저장 | 로그인 전 저장 시도는 로그인 시트를 띄움. 기존 로컬 저장분은 첫 로그인 때 계정으로 옮기고 로컬 삭제 |
| 끝난 볼거리 | 종료 후 90일 보존(상세·저장 목록에서 '종료됨'), 이후 정리. 저장 목록에서도 사라짐 |
| 앱스토어 | 앱 안 회원 탈퇴 필수, Apple 로그인 필수(소셜 로그인 제공 시). Apple Developer 계정 필요 |
| 개인정보 | 수집 항목 추가(카카오/Apple 계정 식별자, 이메일은 받지 않음 기본) → 처리방침 갱신 필요 |

## 5. 인프라

| 항목 | 선택 | 이유 |
|---|---|---|
| 서버 | **Railway** 프로젝트 `herezi` / 서비스 `api` (리전 Southeast Asia — Singapore `asia-southeast1`) | 기존 Hobby 계정(언제볼까 프로젝트와 같은 결제) 재사용. Dockerfile 배포, GitHub `develop` 연결, `/healthz` 헬스체크, `$PORT` 리슨 |
| 배치 | Railway 서비스 `ingest` (cron `0 20 * * *` UTC = 매일 05:00 KST) | `cmd/ingest -max-gpt 100` 하루 1회 실행 후 종료. 매일 DB에 쓰므로 Supabase 비활성 일시정지도 막는다 |
| DB | **Supabase 무료** — 조직 `herezi`, 프로젝트 `herezi`, `ap-southeast-1`(싱가포르), PostGIS·pg_trgm | 관리형 Postgres. API와 같은 리전이라 지연이 짧다. 접속은 **session pooler**(IPv4, 5432) |
| DB 노출 차단 | public 테이블 RLS + `anon`·`authenticated` 권한 회수 (마이그레이션 00005) | 앱은 DB에 직접 붙지 않고 `cmd/api`만 쓴다. Supabase Data API로 테이블이 열리지 않게 막는다 |
| 비밀값 | Railway 서비스 변수 | `DATABASE_URL`, `NCP_MAPS_CLIENT_ID/SECRET`, `DATA_GO_KR_SERVICE_KEY`, `SEOUL_OPENAPI_KEY`, `OPENAI_API_KEY`, `KAKAO_REST_API_KEY`. 로컬은 `.env`(gitignore) |
| 이미지 | 원본 URL 사용(M1) | 저장·가공은 M2에서 필요 시 |
| 모니터링 | Railway 로그 + 수집 결과 요약 로그(리포트 표) | 수집 0건·급감은 로그로 확인(알림은 M2) |

### 월 비용 추정 (M1)
| 항목 | 추정 | 비고 |
|---|---|---|
| Railway Hobby | US$5 (기존 구독, 언제볼까와 공유) | 사용량 US$5까지 포함. 초과분만 추가 과금 |
| Railway 사용량: api | 약 US$1~3 | Go 바이너리, 상시 1대, 메모리 수십 MB |
| Railway 사용량: ingest cron | US$0.1 미만 | 하루 1회 1분 안팎 |
| Supabase | US$0 | 무료(DB 500MB, 7일 비활성 시 일시정지) |
| OpenAI (GPT 보강) | 약 US$0.5~2 | 해시 캐시로 새 항목만, 하루 최대 100요청 |
| 네이버 지오코딩·카카오 키워드 | US$0 | 무료 한도 안(카카오는 앱 '급할땐'과 쿼터 공유) |
| **합계(herezi 추가분)** | **약 US$2~5/월** | Hobby 포함 사용량이 남으면 추가 0에 가깝다 |

> **Supabase 무료 플랜은 7일간 활동이 없으면 프로젝트를 일시정지한다.** 매일 05:00 KST 수집 cron이 DB에 쓰므로
> 정상이라면 멈추지 않는다. cron이 일주일 넘게 실패하면 일시정지될 수 있으니 Railway 로그를 확인하고,
> 멈췄으면 Supabase 대시보드에서 Restore 한다. 트래픽이 생기면 Pro(US$25/월) 전환을 검토한다.
> 결정 근거: [ADR 0003](../adr/0003-railway-supabase.md). 배포 절차: `docs/ops/railway.md`.

## 6. 개발 방식 (하네스 엔지니어링)

- 레포마다 `AGENTS.md`(규칙·명령·구조), `docs/`(기획·ADR·실행 계획), `make check`(포맷·린트·테스트) 제공.
- GitHub Actions CI: PR마다 `make check`. 통과해야 머지.
- 모든 작업: 이슈 → 브랜치 → PR(`Closes #n`) → 머지. main 직접 푸시 금지.
- 테스트: 도메인 단위 테스트, API는 DB 없는 메모리 저장소로 핸들러 테스트 + PostGIS 통합 테스트(CI 서비스 컨테이너), 앱은 Fake 서비스로 뷰모델 테스트.

## 7. 일정 (마일스톤)

| 마일스톤 | 기간 | 산출물 |
|---|---|---|
| M0 기반 | 2026-11 말 ~ 12월 | 하네스(AGENTS.md·CI), API 키 발급, 데이터 실측, DB·배포 환경 |
| M1-a 수집 | 12월 ~ 2027-01 중순 | 소스별 수집기, 지오코딩, GPT 보강, 중복 제거 |
| M1-b API | 12월 ~ 2027-01 중순 | `/v1/spots` 실데이터 연결, 배포 |
| M1-c 앱 | 2027-01 ~ 02 중순 | 근처 목록·지도·필터·상세·길찾기, 위치 미허용 대응, 공유·찜·검색(P1) |
| 베타 | 2027-02 말 | TestFlight · 내부 테스트 트랙, 봄꽃 데이터 점검 |
| 출시 | 2027-03-16 | 스토어 출시 (심사 기간 고려해 3/9까지 제출) |

## 8. 기존 초안 코드 처리

승인 전에 만들어진 초안 코드(커밋 안 됨)가 세 레포에 있다.
- `herezi-api`: 빌드·테스트 통과. **M0 이슈에서 검토 후 PR로 반영**(`free` 카테고리 제거 등 v1.0 기획에 맞춰 수정).
- `herezi-ios`: 구조는 이 문서와 일치. M1-c에서 검토 후 반영.
- `herezi-android`: 빌드 파일만 있고 소스가 없음. M1-c에서 이 문서 기준으로 새로 작성.

## 변경 이력
| 날짜 | 버전 | 내용 |
|---|---|---|
| 2026-11-20 | v1.0 | 작성 및 승인 |
| 2026-11-20 | v1.1 | DB 접근을 sqlc → GORM으로 변경 ([ADR 0002](../adr/0002-gorm.md)) |
| 2026-11-20 | v1.2 | 출시 3/16로 당김(일정 조정), 상세는 끝난 항목도 반환 |
| 2026-11-24 | v1.3 | §4.1 인증·저장(카카오·Apple → Supabase Auth, /v1/me/saved), 끝난 볼거리 90일 보존 |
| 2026-11-24 | v1.4 | §5 인프라: GCP → Railway(API·수집 cron, 싱가포르) + Supabase 무료(조직·프로젝트 herezi, ap-southeast-1, session pooler), RLS·권한 회수(00005), 월 비용 추정, 7일 비활성 일시정지 메모 ([ADR 0003](../adr/0003-railway-supabase.md)) |
