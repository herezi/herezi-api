# S3 상세

> 토큰·컴포넌트: [DESIGN.md](../DESIGN.md) · 기획: SERVICE-PLAN §6 원칙 3·4, §8 P0 상세·길찾기, P1 공유·저장, §9 S3 · 목업: [../mockups/detail.png](../mockups/detail.png)
> 코드: iOS `Features/SpotDetail/SpotDetailView.swift` · Android `feature/spotdetail/SpotDetailScreen.kt`, `NaverMapDirections.kt`

## 목적
갈지 말지 결정할 정보(무엇·언제·어디·얼마나 먼지)를 한 화면에 보여주고, 네이버지도 길찾기로 보낸다. 출처와 갱신 시각을 항상 보여 신뢰를 준다.

## 데이터
`GET /v1/spots/{id}`. 목록에서 들어오면 목록의 Spot으로 먼저 그리고, 응답이 오면 갱신한다(빈 화면 없이). 링크·저장 목록에서 들어오면 로딩부터.

## 레이아웃 (위→아래, 하나의 스크롤)
1. **대표 이미지**: 화면 폭 × 비율 4:3, 위 안전 영역까지 꽉 채움. 없거나 실패하면 카테고리 대체 일러스트(DESIGN §6.17, `assets/fallback/<category>.svg`).
2. **상단 바** (이미지 위 투명 → 스크롤해 이미지가 지나가면 흰 배경 + 제목 `type.title3` 노출)
   - 왼쪽 뒤로, 오른쪽 공유 · 저장 아이콘. 이미지 위에서는 아이콘 뒤에 흰 원 36(불투명도 90%)을 깔아 대비 확보.
3. **제목 블록** (좌우 20, 위 `space.5`)
   - 배지 줄: `종료됨`(is_ended) · `무료`(is_free). 둘 다 없으면 줄 생략.
   - 카테고리: 6pt 점 + `type.caption` `text.secondary` **축제·행사**
   - 제목 `type.title1` (줄 수 제한 없음)
4. **정보 행** (위 `space.5`, 행 사이 `space.3`; 아이콘 20 `icon.secondary` + 값 `type.bodyStrong` + 보조 `type.caption` `text.secondary`)
   - 기간 `calendar`: **11월 20일(금) – 12월 14일(일)** / 보조: 오늘 끝나면 **오늘까지**, `ends_at` 없으면 값 **11월 20일(금)부터**
   - 시간 `clock`: 시작·끝 시각이 00:00이 아닐 때만 **매일 17:00 – 22:00**(여러 날) / **18:00 – 22:00**(하루)
   - 장소 `mappin.and.ellipse`: **청계광장** / 보조: 주소 **서울 중구 청계천로 1** — 길게 누르면 주소 복사, 토스트 **주소를 복사했어요**
   - 거리 `figure.walk`: **내 위치에서 350m** (위치 거부 시 **서울시청에서 1.2km**, 거리 모르면 행 생략)
5. **구분 여백** 높이 8, 배경 `bg.secondary` (좌우 끝까지)
6. **요약** (좌우 20, 위아래 `space.6`): 섹션 제목 없이 `type.body` 본문. `summary` 없으면 블록 생략.
7. **출처 카드** (좌우 20, `bg.secondary`, `radius.md`, 안쪽 16)
   - 1줄 `type.caption` `text.secondary`: **출처 · 서울 열린데이터광장**
   - 2줄 `type.caption` `text.secondary`: **2시간 전 갱신**
   - 오른쪽 글자 버튼 **원문 보기** + `arrow.up.right` (`source_url` 없으면 숨김) → 인앱 브라우저(SFSafariViewController / Custom Tabs)
8. **오류 제보** 글자 버튼 가운데 `type.caption` `text.secondary`: **정보가 달라요** → `help@herezi.com`(도메인 구매 후 확정) 메일 작성(제목 `[정보 오류] {id} {title}`) — 위 `space.4`, 아래 하단 바 높이 + `space.6`
9. **하단 고정 바** (흰 배경, 위 1px `border`, 안쪽 좌우 20 위 12 아래 안전 영역+12)
   - 주 버튼 **네이버지도로 길찾기** (전체 폭)

## 상태
| 상태 | 모습 · 문구 |
|---|---|
| 로딩 | 이미지 자리 `bg.secondary`, 제목 줄 2개·정보 행 3개 스켈레톤. 하단 바 숨김 |
| 데이터 있음 | 위 레이아웃 |
| 종료됨 (`is_ended: true`) | 제목 위 `종료됨` 배지. 이미지·카테고리 점은 그대로(흐리게 하지 않음 — 읽어야 하므로). 정보 행 위에 `bg.secondary` 안내 박스 `type.caption`: **11월 17일에 끝난 볼거리예요.** 하단 바의 길찾기 버튼은 **보조 버튼 `장소 보기`**(네이버지도에서 장소 표시만)로 바뀐다. 저장·공유는 그대로 |
| 없음 (404) | 아이콘 `mappin.slash` · **끝났거나 없는 볼거리예요** · 목록에서 다른 볼거리를 찾아보세요. · 버튼 **주변 보기**(→ S1 홈). 저장된 항목이면 버튼 아래 글자 버튼 **저장에서 지우기** |
| 오류 | 아이콘 `wifi.exclamationmark` · **불러오지 못했어요** · 잠시 후 다시 시도해 주세요. · 버튼 **다시 시도**. 목록에서 들어와 미리 그린 내용이 있으면 화면 유지 + 토스트 **최신 정보를 불러오지 못했어요** |

## 인터랙션
- **네이버지도로 길찾기** → 네이버지도 앱 `nmap://route/walk?dlat&dlng&dname={place_name}&appname=com.herezi.app`; 앱이 없으면 앱스토어/플레이스토어 대신 웹 `https://map.naver.com/p/directions/-/{lng},{lat},{place_name}/-/walk`. 거리 2km 이상이면 `route/public`.
- **저장** 아이콘 → 즉시 토글(`motion.fast` 채움 + 햅틱), 토스트 **저장했어요** / **저장을 취소했어요**. 기기 로컬 저장, 가입 없음.
- **공유** → 시스템 공유 시트. 내용: `{title}\n{기간} · {place_name}\nhttps://herezi.com/s/{id}` (도메인 구매 후 확정 — DESIGN §9. 구매 전 빌드는 `source_url`). 카카오톡은 공유 시트 안에서 선택(M1은 카카오 SDK 미사용).
- **iOS 스와이프 뒤로 / Android 뒤로** → 이전 화면, 스크롤 위치 유지.

## 접근성
- 이미지 라벨: "{title} 사진". 이미지 없으면 장식으로 숨김.
- 정보 행은 행 하나를 한 요소로: "기간, 11월 20일 금요일부터 12월 14일 일요일까지".
- 갱신 시각은 절대 시각도 읽어준다: "2시간 전 갱신, 11월 20일 오전 9시 12분".
- 저장 버튼 라벨 "저장" / "저장 취소", 상태 바뀌면 값 "저장됨".
- 종료 안내 박스는 화면 진입 시 가장 먼저 읽히도록 제목 다음 순서.
