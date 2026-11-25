# Railway 배포 (herezi-api)

운영 구성은 [TECH-PLAN §5](../product/TECH-PLAN.md#5-인프라), 결정 근거는 [ADR 0003](../adr/0003-railway-supabase.md).
**비밀값은 이 문서·레포에 적지 않는다.** 값은 로컬 `.env`(gitignore)와 Railway 서비스 변수에만 있다.

## 현재 상태 (2026-11-24)

| 항목 | 값 |
|---|---|
| Railway 프로젝트 | `herezi` (워크스페이스: 기존 Hobby 계정, `whenwemeet` 프로젝트와 별개) |
| 환경 | `production` |
| 서비스 `api` | 이 레포 `Dockerfile`, 시작 `/app/api`, 헬스체크 `/healthz`(60초), 재시작 ON_FAILURE(최대 5) |
| 서비스 `ingest` | 같은 이미지, 시작 `/app/ingest -max-gpt 100`, cron `0 20 * * *`(UTC) = **매일 05:00 KST**, 재시작 NEVER |
| 리전 | `asia-southeast1-eqsg3a` (Southeast Asia, Singapore), 각 1대 |
| 공개 URL | https://api-production-4ebcf3e2.up.railway.app |
| DB | Supabase `herezi`(ap-southeast-1) session pooler — `DATABASE_URL` |

서비스 변수(이름만):

| 변수 | api | ingest |
|---|---|---|
| `DATABASE_URL` | ✓ | ✓ |
| `NCP_MAPS_CLIENT_ID`, `NCP_MAPS_CLIENT_SECRET` | ✓ (역지오코딩) | ✓ (지오코딩) |
| `KAKAO_REST_API_KEY` | ✓ | ✓ (장소명 검색) |
| `DATA_GO_KR_SERVICE_KEY`, `SEOUL_OPENAPI_KEY`, `OPENAI_API_KEY` | ✓ (미사용) | ✓ |
| `MIGRATE_ON_START=1` | ✓ (시작 시 goose 마이그레이션) | — |
| `SUPABASE_URL` | ✓ (로그인 토큰 JWKS 검증, 없으면 `/v1/me/*` 503) | — |
| `SUPABASE_JWT_SECRET` | 선택 (레거시 HS256 토큰도 받을 때만) | — |
| `SUPABASE_SERVICE_ROLE_KEY` | ✓ (회원 탈퇴: Admin API 사용자 삭제, 없으면 `DELETE /v1/me` 503) | — |

`PORT`는 Railway가 넣는다(api는 `$PORT`를 리슨, 기본 8080).

## 처음부터 만들기 (CLI)

```sh
railway whoami                        # 로그인 확인
cd herezi-api
railway init --name herezi            # 새 프로젝트 + 현재 폴더 연결
railway add --service api    --repo herezi/herezi-api --branch develop
railway add --service ingest --repo herezi/herezi-api --branch develop
```

서비스 설정은 GraphQL로 넣는다(railway.json 설정 파일 방식은 Railway에서 지원 중단).
`<ENV_ID>`·`<API_ID>`·`<INGEST_ID>`는 `railway status`, `railway service list --json`에서 확인한다.

```sh
M='mutation($s:String!,$e:String!,$i:ServiceInstanceUpdateInput!){ serviceInstanceUpdate(serviceId:$s, environmentId:$e, input:$i) }'
railway api "$M" --raw-var s=<API_ID> --raw-var e=<ENV_ID> --var 'i={"dockerfilePath":"Dockerfile","startCommand":"/app/api","healthcheckPath":"/healthz","healthcheckTimeout":60,"restartPolicyType":"ON_FAILURE","restartPolicyMaxRetries":5,"multiRegionConfig":{"asia-southeast1-eqsg3a":{"numReplicas":1}}}'
railway api "$M" --raw-var s=<INGEST_ID> --raw-var e=<ENV_ID> --var 'i={"dockerfilePath":"Dockerfile","startCommand":"/app/ingest -max-gpt 100","cronSchedule":"0 20 * * *","restartPolicyType":"NEVER","multiRegionConfig":{"asia-southeast1-eqsg3a":{"numReplicas":1}}}'
```

### 변수 넣기 (값을 출력하지 않는다)

`.env`에서 값을 읽어 stdin으로 넘긴다. `railway variable list --kv`·`--json`은 값을 그대로 출력하므로 쓰지 않는다(이름 확인은 아래처럼).

```sh
for svc in api ingest; do
  for k in DATABASE_URL NCP_MAPS_CLIENT_ID NCP_MAPS_CLIENT_SECRET DATA_GO_KR_SERVICE_KEY SEOUL_OPENAPI_KEY OPENAI_API_KEY KAKAO_REST_API_KEY; do
    sed -n "s/^${k}=//p" .env | head -1 | tr -d '\n' \
      | railway variable set "$k" --stdin --service "$svc" --skip-deploys >/dev/null && echo "$svc $k set"
  done
done
printf 1 | railway variable set MIGRATE_ON_START --stdin --service api --skip-deploys >/dev/null

# 이름만 확인
railway variable list --service api --json | python3 -c 'import sys,json; print(sorted(json.load(sys.stdin)))'
```

### 공개 도메인

```sh
railway domain --service api          # *.up.railway.app 생성
```

## 배포

> **자동 배포(푸시 → 배포)는 아직 꺼져 있다.** Railway GitHub 앱이 `herezi` 조직에 설치되어 있지 않아
> 배포 트리거를 만들 수 없다("no one in the project has access"). 조직 관리자가 브라우저에서
> https://github.com/apps/railway-app/installations/new → `herezi` 조직 → `herezi-api` 접근을 허용하면
> 서비스 Settings → Source에서 `develop` 브랜치 자동 배포를 켤 수 있다.

그 전까지는 `develop` 머지 후 커밋을 지정해 배포한다(공개 레포라 커밋을 직접 받아 빌드한다).

```sh
SHA=$(git rev-parse origin/develop)
D='mutation($s:String!,$e:String!,$c:String){ serviceInstanceDeployV2(serviceId:$s, environmentId:$e, commitSha:$c) }'
railway api "$D" --raw-var s=<API_ID>    --raw-var e=<ENV_ID> --raw-var c=$SHA
railway api "$D" --raw-var s=<INGEST_ID> --raw-var e=<ENV_ID> --raw-var c=$SHA
railway deployment list --service api --json | head    # 상태 확인
railway logs --service api                              # 런타임 로그
```

`ingest`는 cron 서비스라 배포해도 바로 실행되지 않고 다음 05:00 KST에 돈다. 즉시 한 번 돌리려면 `railway cron --help`.

## 확인

```sh
URL=https://api-production-4ebcf3e2.up.railway.app
curl -s $URL/healthz
curl -s "$URL/v1/spots?lat=37.5663&lng=126.9779&radius_m=3000&when=week&limit=5"
curl -s $URL/v1/notices                                     # 공지 목록(작성법: notices.md)
```

## 운영 메모
- **Supabase 무료 7일 비활성 일시정지**: 매일 수집 cron이 DB에 쓰므로 정상이라면 멈추지 않는다. ingest 실패가 이어지면 Railway 로그를 확인하고, 멈췄으면 Supabase 대시보드에서 Restore.
- **GPT 비용**: cron은 `-max-gpt 100`(새·바뀐 항목만, 해시 캐시). OpenAI 선불 잔액을 주기적으로 확인한다.
- **data.go.kr 일일 한도**: 문화포털 상세(`detail2`)가 429면 그날은 목록 정보로만 저장하고 다음 날 보완된다.
- **공지사항**: Supabase Table Editor에서 `notices` 행을 추가·수정한다(배포 불필요). [notices.md](notices.md)
- **DB 노출**: public 테이블은 RLS + `anon`·`authenticated` 권한 회수(마이그레이션 00005). 새 테이블을 만드는 마이그레이션은 RLS를 같이 켠다.
