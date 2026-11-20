# Herezi 디자인 시스템 (M1)

| 항목 | 내용 |
|---|---|
| 상태 | 초안 (M1 기준) |
| 적용 | `herezi-ios`(SwiftUI) · `herezi-android`(Jetpack Compose) 공통 |
| 근거 | [SERVICE-PLAN §7·§8·§9](../product/SERVICE-PLAN.md), [PRODUCT.md](../product/PRODUCT.md), [api/openapi.yaml](../../api/openapi.yaml) |
| 화면 스펙 | [screens/](screens/) — 홈 [S1-home](screens/S1-home.md)(사진 카드 피드) · 전체 목록 [S1b-list](screens/S1b-list.md) · 목업 [mockups/](mockups/) · 대체 일러스트 [assets/fallback/](assets/fallback/) |
| 변경 | #29 홈을 B안 사진 카드 피드 + A안 전체 목록으로, 탭 바 주변·지도·저장 |

> 디자인 문서는 두 앱이 함께 쓰므로 **이 레포(`herezi-api/docs/design/`) 한 곳에만** 둔다. 앱 레포는 복사하지 말고 링크한다.
> 토큰 값을 바꾸면 이 문서를 먼저 고치고, 두 앱의 테마 파일을 같은 PR 묶음에서 맞춘다.

---

## 0. 원칙
1. **평평하고 넓게.** 그림자·그라데이션·테두리 대신 여백과 글자 크기 차이로 위계를 만든다. 예외는 하나: 사진 위 글자를 읽게 하는 히어로 카드의 어두운 그라데이션(§6.13).
2. **잉크 한 가지 색이 주인공.** 주요 버튼·선택 상태·클러스터는 잉크 `#191F28`. 색은 카테고리 구분에만 쓴다.
3. **쓰지 않는 것:** 토스 블루 계열 주색, 노란색, 지도 핀 모양 로고, 손글씨.
4. **"지금"이 먼저 보이게.** 카드에서 가장 먼저 읽히는 순서는 제목 → 거리 → 기간.
5. **플랫폼 관례 유지.** iOS는 HIG(내비게이션 바·스와이프 뒤로가기·시트), Android는 Material 3(Top app bar·Navigation bar·ModalBottomSheet)를 따르고, 색·글자·모서리만 같게 맞춘다.

---

## 1. 토큰 이름 규칙
- 문서·디자인의 키는 점 표기: `color.text.primary`, `space.4`, `radius.md`.
- 코드에서는 같은 단어를 camelCase로 쓴다.

| 문서 키 | iOS (SwiftUI) | Android (Compose) |
|---|---|---|
| `color.text.primary` | `Color.hz.textPrimary` | `HereziTheme.colors.textPrimary` |
| `color.palette.ink` | `Color.hz.ink` | `HereziColors.Ink` (이미 있음) |
| `type.title2` | `Font.hz.title2` / `.hzText(.title2)` | `HereziTheme.typography.title2` |
| `space.4` | `HZSpace.s4` (= 16) | `HereziSpace.s4` (= 16.dp) |
| `radius.md` | `HZRadius.md` | `HereziRadius.md` |
| `motion.base` | `HZMotion.base` | `HereziMotion.base` |

> Android `core/ui/theme/HereziColors.kt`의 `Ink·Gray700·Gray500·Gray100·White·Error`는 아래 팔레트와 이름이 같다. 의미 토큰(`textPrimary` 등)은 팔레트를 가리키는 별칭으로 추가한다.

---

## 2. 색

### 2.1 팔레트 (원색값)
| 키 | 값 | 비고 |
|---|---|---|
| `color.palette.ink` | `#191F28` | 브랜드 잉크. 주 버튼·선택·클러스터·본문 |
| `color.palette.gray700` | `#4E5968` | 보조 본문 |
| `color.palette.gray500` | `#8B95A1` | 힌트·비활성·아이콘 (작은 본문 금지, 대비 3.0) |
| `color.palette.gray200` | `#E5E8EB` | 구분선·칩 테두리 |
| `color.palette.gray100` | `#F2F4F6` | 보조 배경·스켈레톤·배너 |
| `color.palette.white` | `#FFFFFF` | 배경 |
| `color.palette.red` | `#F04452` | 오류 아이콘 (글자는 잉크로) |
| `color.palette.green700` | `#08734F` | '무료' 배지 글자 |
| `color.palette.green50` | `#E6F6EF` | '무료' 배지 배경 |

### 2.2 의미 토큰 (코드는 이것만 쓴다)
| 키 | 값 | 쓰는 곳 | 흰 배경 대비 |
|---|---|---|---|
| `color.bg.primary` | white | 화면 배경, 카드 | – |
| `color.bg.secondary` | gray100 | 배너, 비선택 칩, 썸네일 자리, 스켈레톤 | – |
| `color.bg.inverse` | ink | 주 버튼, 선택된 칩·탭, 클러스터, 토스트 | – |
| `color.text.primary` | ink | 제목·본문 | 16.6 : 1 |
| `color.text.secondary` | gray700 | 기간·거리·주소·출처·갱신 시각 | 7.1 : 1 |
| `color.text.tertiary` | gray500 | 입력 힌트, 비활성 글자, 18pt 이상 보조 문구에만 | 3.0 : 1 |
| `color.text.inverse` | white | 잉크 위 글자 | 16.6 : 1 |
| `color.border` | gray200 | 구분선(1px), 입력창 테두리 | – |
| `color.icon.primary` | ink | 툴바 아이콘 | – |
| `color.icon.secondary` | gray500 | 정보 행 아이콘 | 3.0 : 1 (그래픽 기준 충족) |
| `color.status.error` | red | 오류 일러스트·아이콘 | 3.7 : 1 |
| `color.badge.free.fg` / `.bg` | green700 / green50 | '무료' 배지 | 5.3 : 1 |
| `color.badge.ended.fg` / `.bg` | gray700 / gray100 | '종료됨' 배지 | 6.5 : 1 |
| `color.scrim` | `#191F28` 40% | 시트 뒤 어둡게 | – |
| `color.overlay.hero` | `#191F28` 0% → 60% → 85% 세로 그라데이션 | 히어로 카드 사진 위(§6.13) | 흰 글자 ≥ 4.5 : 1 |

**다크 모드:** M1은 라이트 모드만 출시한다(두 앱 모두 강제 라이트). 의미 토큰만 쓰면 M2에서 값만 바꿔 대응할 수 있다.

### 2.3 카테고리 색 · 아이콘 (9종)
색은 **핀 배경, 카드의 작은 점, 썸네일 대체 배경, 홈 카테고리 아이콘 타일(§6.15), 대체 일러스트 배경(§6.17)**에만 쓴다. 글자는 카테고리 색으로 칠하지 않는다(대비 부족). 모든 색은 흰 아이콘과 3:1 이상(그래픽 대비 기준).

| 코드 | 이름 | 토큰 | 색 | SF Symbol (iOS) | Material Symbol (Android, Rounded) |
|---|---|---|---|---|---|
| `nature` | 자연·계절 | `color.category.nature` | `#2E9E5B` | `leaf` | `eco` |
| `festival` | 축제·행사 | `color.category.festival` | `#E5484D` | `party.popper` | `celebration` |
| `performance` | 공연 | `color.category.performance` | `#8E4FD8` | `music.mic` | `mic_external_on` |
| `exhibition` | 전시 | `color.category.exhibition` | `#0E8F89` | `photo.artframe` | `palette` |
| `popup` | 팝업 | `color.category.popup` | `#DB3F78` | `bag` | `storefront` |
| `food` | 먹거리 | `color.category.food` | `#D8661F` | `fork.knife` | `restaurant` |
| `family` | 아이·가족 | `color.category.family` | `#5E9414` | `figure.2.and.child.holdinghands` | `family_restroom` |
| `pet` | 반려동물 | `color.category.pet` | `#9A6436` | `pawprint` | `pets` |
| `sports` | 스포츠 | `color.category.sports` | `#5A5FD6` | `figure.run` | `directions_run` |

- iOS 심볼 이름은 `Domain/SpotCategory.swift`의 `symbolName`과 같다.
- Android는 `material-icons-extended`의 Rounded 변형(`Icons.Rounded.Eco` 등)을 쓴다. `mic_external_on`이 없으면 `Icons.Rounded.Mic`.
- 알 수 없는 카테고리(서버가 새 값을 추가한 경우): 이름 "기타", 색 `gray700`, 아이콘 `sparkles` / `auto_awesome`.

### 2.4 공통 아이콘
| 의미 | SF Symbol | Material Symbol |
|---|---|---|
| 뒤로 | `chevron.backward` (시스템) | `arrow_back` (시스템) |
| 검색 | `magnifyingglass` | `search` |
| 저장 / 저장됨 | `bookmark` / `bookmark.fill` | `bookmark_border` / `bookmark` |
| 공유 | `square.and.arrow.up` | `share` |
| 기간 | `calendar` | `calendar_today` |
| 시간 | `clock` | `schedule` |
| 장소 | `mappin.and.ellipse` | `location_on` |
| 거리 | `figure.walk` | `directions_walk` |
| 출처 링크 | `arrow.up.right` | `open_in_new` |
| 내 위치 | `location` / `location.fill` | `my_location` |
| 위치 꺼짐 | `location.slash` | `location_off` |
| 지도 보기 / 목록 보기 | `map` / `list.bullet` | `map` / `format_list_bulleted` |
| 다시 찾기 | `arrow.clockwise` | `refresh` |
| 오류 | `wifi.exclamationmark` | `wifi_off` |
| 결과 없음 | `mappin.slash` | `search_off` |
| 탭: 주변 / 지도 / 저장 | `location.circle` / `map` / `bookmark` | `explore` / `map` / `bookmark` |
| 설정 (저장 탭 상단 바) | `gearshape` | `settings` |
| 전체 (홈 카테고리 타일) | `sparkle` | `auto_awesome` |
| 전체 보기 | `chevron.right` | `chevron_right` |

---

## 3. 글자

- 글꼴: **Pretendard** (Regular 400 · Medium 500 · SemiBold 600 · Bold 700). 번들 결정 전까지는 시스템 글꼴(iOS SF Pro / Apple SD Gothic Neo, Android Roboto / Noto Sans KR)로 대체하고 크기·굵기만 맞춘다.
- 로고타입 "히어지"는 이미지 에셋을 쓰고 글자로 조판하지 않는다.
- 크기는 iOS pt = Android sp. **다이나믹 타입 / 글꼴 크기 설정을 따른다**(iOS는 `relativeTo:` 기준 스타일 지정, Android는 sp 사용).

| 키 | 크기/줄높이 | 굵기 | 쓰는 곳 | iOS relativeTo | Material 3 대응 |
|---|---|---|---|---|---|
| `type.display` | 26 / 36 | Bold | S0 큰 문장 | `.largeTitle` | `headlineMedium` |
| `type.title1` | 22 / 30 | Bold | 화면 제목, 상세 제목 | `.title2` | `headlineSmall` (이미 22/30) |
| `type.title2` | 19 / 26 | Bold | 섹션 제목, 빈 상태 제목 | `.title3` | `titleLarge` |
| `type.title3` | 17 / 24 | SemiBold | 카드 제목, 버튼 | `.headline` | `titleMedium` (이미 17/24) |
| `type.body` | 15 / 22 | Regular | 본문, 요약 | `.body` | `bodyMedium` (이미 15/22) |
| `type.bodyStrong` | 15 / 22 | SemiBold | 정보 행 값, 칩 선택 | `.body` | `bodyMedium` + SemiBold |
| `type.caption` | 13 / 18 | Medium | 카드 메타(거리·기간), 칩, 배너 | `.footnote` | `labelMedium` (이미 13/18) |
| `type.micro` | 11 / 14 | SemiBold | 배지, 클러스터 숫자, 탭 라벨 | `.caption2` | `labelSmall` |

- 숫자(거리·시간)는 고정폭 숫자(`monospacedDigit()` / `fontFeatureSettings = "tnum"`).
- 한 줄 최대 2줄 말줄임: 카드 제목 2줄, 요약은 상세에서 전부.

---

## 4. 간격 · 크기
4pt 격자. 화면 좌우 여백은 **20**.

| 키 | 값 | 예 |
|---|---|---|
| `space.1` | 4 | 아이콘–글자 |
| `space.2` | 8 | 칩 사이, 배지 사이 |
| `space.3` | 12 | 카드 썸네일–글자 |
| `space.4` | 16 | 카드 위아래 안쪽 |
| `space.5` | 20 | **화면 좌우 여백**, 섹션 안 |
| `space.6` | 24 | 섹션 사이 |
| `space.8` | 32 | 큰 섹션 사이, 빈 상태 위 |
| `space.10` | 40 | 빈 상태 아이콘 위 |

| 크기 키 | 값 | 비고 |
|---|---|---|
| `size.touch` | 44 (iOS) / 48 (Android) | 최소 터치 영역 |
| `size.chip` | 36 | 칩 높이 (터치 영역은 44/48로 확장) |
| `size.button.lg` | 56 | 하단 주 버튼 |
| `size.button.md` | 44 | 배너·빈 상태 버튼 |
| `size.thumb` | 72 | 카드 썸네일 |
| `size.hero.h` | 폭 × 4/7 (390폭 = 200) | 히어로 카드 높이(최소 200) |
| `size.tile.w` / `size.tile.img` | 148 / 148×112 | 가로 카드 폭 / 이미지 |
| `size.catTile` | 52 | 홈 카테고리 아이콘 타일 |
| `size.pin` | 36 | 지도 핀 지름 |

---

## 5. 모서리 · 그림자 · 모션

| 키 | 값 | 쓰는 곳 |
|---|---|---|
| `radius.xs` | 6 | 배지 |
| `radius.sm` | 8 | 작은 버튼, 토스트 |
| `radius.md` | 12 | 썸네일, 배너, 입력창 |
| `radius.lg` | 16 | 주 버튼, 카드(시트 안 단일 카드), 히어로 카드, 카테고리 타일 |
| `radius.xl` | 24 | 바텀시트 위쪽 모서리 |
| `radius.full` | 999 | 칩, 탭 알약, 플로팅 버튼, 핀 |

그림자는 **지도 위에 뜨는 것에만** 쓴다. 목록 화면은 그림자 0.

| 키 | iOS (`shadow`) | Android (`shadowElevation`) | 쓰는 곳 |
|---|---|---|---|
| `elevation.0` | 없음 | 0.dp | 목록·카드·칩 |
| `elevation.1` | `#000` 12%, y 2, blur 8 | 3.dp | 핀, 플로팅 버튼, "다시 찾기" |
| `elevation.2` | `#000` 8%, y -4, blur 16 | 6.dp | 바텀시트 |

| 키 | 값 | 쓰는 곳 |
|---|---|---|
| `motion.fast` | 150ms, ease-out | 칩·토글 선택, 저장 아이콘 |
| `motion.base` | 250ms, ease-in-out | 목록↔지도 전환(교차 페이드), 배너 등장 |
| `motion.slow` | 350ms, spring(감쇠 0.85) | 시트 올라옴, 선택 핀 확대 |

- 시스템 "동작 줄이기"(iOS Reduce Motion / Android 애니메이션 배율 0)가 켜지면 확대·이동 없이 페이드만.
- 저장 시 가벼운 햅틱(iOS `.sensoryFeedback(.selection)`, Android `HapticFeedbackType.TextHandleMove`). 그 외 햅틱 없음.

---

## 6. 컴포넌트

### 6.1 스팟 카드 `SpotCard`
목록·검색·저장·지도 시트에서 같은 카드를 쓴다. (현재 코드의 `SpotRow`를 이 이름으로 맞춘다.)

```
┌──────────────────────────────────────────────┐
│ ┌──────┐  ● 축제·행사 · 350m                 │  caption, text.secondary (점=카테고리 색 6pt)
│ │ 썸네일 │  서울빛초롱축제                      │  title3, text.primary, 최대 2줄
│ │ 72×72 │  11.20(금) – 12.14(일)              │  caption, text.secondary
│ └──────┘  [무료]                              │  배지 줄 (없으면 줄 생략)
└──────────────────────────────────────────────┘
```
- 레이아웃: 좌우 20, 위아래 16, 썸네일–글자 12. 카드 사이 구분선 없음(여백만). 배경 흰색, 그림자 없음.
- 썸네일: `image_url` 있으면 72×72 `radius.md` 꽉 채움(가운데 자름). 없거나 실패하면 카테고리 색 12% 배경 + 카테고리 아이콘(28, 카테고리 색) — 72에서는 일러스트보다 아이콘이 또렷하다(§6.17 크기 규칙).
- 메타 첫 줄: `카테고리 · 거리`. 거리: 1km 미만 `350m`(10m 단위 반올림), 이상 `1.2km`(소수 1자리), 10km 이상 `12km`. 거리 없음(저장·검색에서 위치 모름)이면 `카테고리 · 장소명`.
- 기간 줄 규칙(KST 기준):
  - 오늘 시작·오늘 끝: `오늘 18:00 – 22:00`
  - 진행 중이고 오늘 끝: `오늘까지` (text.primary, SemiBold)
  - 여러 날: `11.20(금) – 12.14(일)`; 같은 해가 아니면 `2026.12.20 – 2027.01.04`
  - `ends_at` 없음: `11.20(금)부터`
- 배지: `무료`(is_free), `종료됨`(is_ended, 저장 목록에서만 보임). 배지 사이 8.
- 종료된 카드(저장 목록): 썸네일·글자 불투명도 40%, 배지 `종료됨`은 100%. 탭 가능.
- 누름 상태: 배경 `bg.secondary` (iOS 하이라이트 / Android ripple 색 = ink 8%).
- 스크린리더: 카드 하나를 한 요소로 묶는다. 읽는 순서 "서울빛초롱축제, 축제·행사, 350미터, 11월 20일부터 12월 14일까지, 무료". 종료 시 앞에 "종료됨,".

### 6.2 카테고리 칩 `CategoryChips`
- 가로 스크롤 한 줄, 좌우 여백 20, 칩 사이 8.
- 순서: `[무료만] │ [전체] [자연·계절] [축제·행사] [공연] [전시] [팝업] [먹거리] [아이·가족] [반려동물] [스포츠]` — `무료만`은 토글(§6.4), 세로 구분선(1×16, `border`) 뒤에 카테고리.
- 칩: 높이 36, 좌우 14, `radius.full`, `type.caption`, 앞에 카테고리 아이콘 16.
  - 비선택: 배경 `bg.secondary`, 글자 `text.primary`, 아이콘 카테고리 색.
  - 선택: 배경 `bg.inverse`(잉크), 글자·아이콘 흰색.
- 복수 선택. 아무것도 선택 안 함 = `전체` 선택 상태. `전체`를 누르면 선택을 모두 해제. 마지막 선택을 해제하면 `전체`로 돌아간다.
- 접근성: 각 칩은 토글 버튼(`.isSelected` / `Role.Checkbox`). 라벨 "공연, 선택됨".

### 6.3 기간 탭 `WhenTabs`
- `지금 · 오늘 · 이번 주말 · 이번 주` (API `when` = `now | today | weekend | week`, 기본 `now`).
- 모양: 글자 탭(밑줄 없음). 높이 44, 탭 사이 20, `type.title3`. 선택 = `text.primary` Bold, 비선택 = `text.tertiary`. Toss식 큰 글자 탭으로 위계를 만든다(세그먼트 컨트롤·메뉴 대신).
- 선택 바뀌면 목록 맨 위로 스크롤 + 다시 조회. 변경 애니메이션 `motion.fast`.
- 접근성: 각 탭에 선택 상태(iOS `.accessibilityAddTraits(.isSelected)`, Android `Modifier.selectableGroup()` + `Role.Tab`). 라벨 "이번 주말, 기간, 선택됨".

### 6.4 무료 토글 `FreeToggle`
- 칩 줄 맨 앞의 칩 모양 토글. 라벨 `무료만`, 아이콘 없음. 켜짐 = 잉크 배경 + 앞에 체크 아이콘(`checkmark` / `check`) 14.
- API `free=true`. 기간·카테고리와 독립.

### 6.5 바텀시트 `SpotSheet`
- 지도 화면 하단. 위 모서리 `radius.xl`, `elevation.2`, 손잡이 36×4 `gray200` 위 8.
- 높이 단계: **접힘**(손잡이 + 한 줄 "이 지역 볼거리 24개", 약 88) / **카드**(핀 선택 시 SpotCard 1장, 약 156) / **펼침**(화면 60%, 목록 스크롤).
- iOS: `.sheet` + `presentationDetents([.height(88), .height(156), .fraction(0.6)])`, `.presentationBackgroundInteraction(.enabled)`. Android: `BottomSheetScaffold`(peek 88.dp).
- 시트 뒤 지도는 어둡게 하지 않는다(스크림 없음).

### 6.6 지도 핀 · 클러스터 `MapPin` / `MapCluster`
- **핀:** 지름 36 원, 카테고리 색 채움, 흰 테두리 2, 가운데 흰 카테고리 아이콘 18, `elevation.1`. 꼬리 없음(핀 모양 로고와 겹치지 않게 원형).
- **선택된 핀:** 지름 44로 확대(`motion.slow`), 테두리 잉크 3, 아래 6 간격에 제목 라벨(흰 배경 알약, `type.caption` SemiBold, `elevation.1`, 최대 12자 말줄임).
- **클러스터:** 잉크 원 + 흰 숫자(13 Bold, 고정폭 숫자), 흰 테두리 2. 지름 개수별 32(2–9) / 40(10–49) / 48(50+), 숫자 99개 넘으면 `99+`.
- **내 위치:** 네이버지도 기본 위치 오버레이(파란 점)는 **잉크 점(지름 14, 흰 테두리 3) + 잉크 12% 정확도 원**으로 바꾼다.
- 클러스터링은 앱에서(서버는 bbox 항목만 반환). 같은 화면 픽셀 기준 반경 48 안의 핀을 묶는다. 탭하면 묶인 영역으로 확대.
- 접근성: 핀 = "공연, 세종 체임버 콘서트", 클러스터 = "볼거리 12개, 두 번 탭하면 확대".

### 6.7 상태 화면 `EmptyState` · `ErrorState` · `LoadingSkeleton`
공통: 가운데 정렬, 위 여백 `space.10`, 아이콘 48(`gray500`), 제목 `type.title2`, 설명 `type.body` `text.secondary`, 버튼(선택) 높이 `size.button.md`, 잉크 배경, `radius.sm`, 좌우 20. 화면별 문구는 각 화면 스펙에 다시 적는다.

| 상태 | 아이콘 | 제목 | 설명 | 버튼 |
|---|---|---|---|---|
| 결과 없음(필터 없음) | `mappin.slash` / `search_off` | 근처에 지금 볼 만한 게 없어요 | 기간을 늘리거나 지도를 움직여 보세요. | 이번 주로 보기 |
| 결과 없음(필터 있음) | 같음 | 조건에 맞는 볼거리가 없어요 | 필터를 바꾸거나 기간을 늘려 보세요. | 필터 초기화 |
| 오류(네트워크·서버) | `wifi.exclamationmark` / `wifi_off` | 불러오지 못했어요 | 주변 정보를 불러오지 못했어요. 잠시 후 다시 시도해 주세요. | 다시 시도 |

- **로딩 스켈레톤:** 실제 카드와 같은 자리에 `bg.secondary` 블록(썸네일 72×72 `radius.md`, 글줄 3개: 폭 40%·80%·55%, 높이 12, `radius.xs`). 5장. 1.2초 주기로 불투명도 100%↔60% 깜빡임(동작 줄이기 시 정지). 스피너는 쓰지 않는다(당겨서 새로고침의 시스템 스피너만 예외).
- 첫 로딩 300ms 안에 끝나면 스켈레톤을 보여주지 않는다(깜빡임 방지).

### 6.8 위치 꺼짐 배너 `LocationBanner`
- 위치: 홈(S1) 헤드라인 아래, 목록(S1b)·지도(S2) 화면 필터 아래, 좌우 20 안쪽 카드형. 좁으면 문구는 두 줄로 접힌다. 배경 `bg.secondary`, `radius.md`, 안쪽 12/16.
- 내용: `location.slash` 아이콘 18(`text.secondary`) + 문구 `type.caption` `text.primary` + 오른쪽 글자 버튼 `설정` (`type.caption` Bold, 밑줄 없음).
- 문구: **위치를 확인할 수 없어 서울시청 기준으로 보여드려요** (현재 코드와 같음).
- `설정` → iOS `UIApplication.openSettingsURLString`, Android 앱 정보 화면(`ACTION_APPLICATION_DETAILS_SETTINGS`). 권한을 "한 번도 묻지 않음" 상태면 `설정` 대신 `허용하기`로 시스템 권한 창을 띄운다.
- 닫기 버튼 없음. 권한이 생기면 `motion.base`로 사라진다.

### 6.9 '종료됨' 배지 `EndedBadge`
- 글자 **종료됨**, `type.micro`, 배경 `color.badge.ended.bg`, 글자 `color.badge.ended.fg`, 안쪽 좌우 6·위아래 3, `radius.xs`.
- `무료` 배지도 같은 모양(색만 `badge.free`).
- 노출: 상세(S3) 제목 위, 저장 목록(S4) 카드. 목록·지도·검색 응답에는 종료 항목이 오지 않으므로 그 화면에는 나오지 않는다.

### 6.10 버튼
| 종류 | 모양 | 쓰는 곳 |
|---|---|---|
| 주 버튼 `PrimaryButton` | 높이 56, 잉크 배경, 흰 `type.title3`, `radius.lg`, 좌우 20 여백 안 꽉 채움 | 길찾기, 위치 허용 |
| 보조 버튼 `SecondaryButton` | 높이 56, `bg.secondary` 배경, 잉크 글자 | 상세 하단 저장 옆 |
| 글자 버튼 `TextButton` | 높이 44, 글자만 `text.secondary` | "나중에 할게요" |
| 플로팅 알약 `FloatingPill` | 높이 44, 잉크 배경, 흰 `type.caption` Bold + 아이콘 16, 좌우 16, `radius.full`, `elevation.1` | "지도로 보기"/"목록으로 보기", "이 지역에서 다시 찾기"(흰 배경·잉크 글자 변형) |
| 아이콘 버튼 | 터치 44/48, 아이콘 24 | 툴바 |

- 눌림: 불투명도 85% + 0.98 축소(`motion.fast`). 비활성: 배경 `gray200`, 글자 `gray500`.

### 6.11 토스트 `Toast`
- 하단 탭바 위 16, 잉크 배경 `radius.sm`, 흰 `type.caption`, 2.5초. 예: "저장했어요", "저장을 취소했어요", "링크를 복사했어요".

### 6.12 탭 바
- 3개: **주변**(S1 홈 → S1b 전체 목록) · **지도**(S2) · **저장**(S4). 비교안 B와 같은 구성.
- **설정(S6)은 저장 탭 상단 바 오른쪽 톱니 아이콘**(`gearshape` / `settings`, 라벨 "설정")으로 들어간다(push).
  - 왜 저장 탭인가: (1) 주변 상단 바는 위치명·검색으로 이미 찼고, 홈은 "볼거리"에만 집중해야 한다. (2) 저장은 '내 것' 공간이라 사용자가 개인 설정을 찾는 자리와 겹친다(앱들의 '마이' 탭 관례). (3) 설정의 주 용도인 위치 권한은 주변·지도의 위치 꺼짐 배너 `설정`이 바로 시스템 설정으로 보내므로, 설정 화면 자체는 자주 갈 일이 없다.
- 검색(S5)은 홈·S1b 상단 바 오른쪽 아이콘으로 들어간다.
- **지도 탭과 목록의 필터 공유:** S1b의 **지도로 보기**는 지도 탭으로 전환하며 필터를 넘기고, S2의 **목록으로 보기**는 주변 탭으로 전환해 S1b를 그 필터·지도 중심으로 연다. 탭을 직접 누를 때는 각 탭이 마지막 상태를 유지한다.
- 선택 = 잉크, 비선택 = `gray500`. 라벨 항상 표시. iOS `TabView`, Android `NavigationBar`(인디케이터 색 `gray100`).
- 이미 선택된 탭을 다시 누르면 그 탭의 첫 화면·맨 위로(주변 → 홈 맨 위).

### 6.13 히어로 카드 `HeroCard`
홈 '지금 가장 가까운 곳' 1장. 큰 사진으로 "가고 싶게" 만드는 카드.

```
┌──────────────────────────────────────────┐
│ (오늘 17:00 시작)                    (◻︎) │  배지(흰 알약)            저장 버튼
│                                          │
│              사진 / 대체 일러스트           │
│ ░░░░░░░░░░░ 아래로 갈수록 어둡게 ░░░░░░░░░ │
│ 서울빛초롱축제                             │  title1, 흰색, 최대 2줄
│ 축제·행사 · 350m · 12.14(일)까지 · 무료     │  caption, 흰색 92%
└──────────────────────────────────────────┘
```
- 크기: 좌우 여백 20 안 꽉 채움, 높이 `size.hero.h`, `radius.lg`, 그림자 없음.
- 이미지: 가운데 잘라 꽉 채움. 없거나 실패하면 §6.17 일러스트.
- **그라데이션** `color.overlay.hero`: 위→아래 잉크 `#191F28` 불투명도 0%(30% 지점) → 60%(60% 지점) → 85%(100%). 글자는 60% 지점보다 아래에만 놓는다 — 가장 밝은 흰 사진 위에서도 흰 글자 대비 4.5:1 이상(60% 잉크 위 흰색 ≈ 4.8:1).
- 글자: 좌우 16, 아래 16. 제목 `type.title1` 흰색 최대 2줄. 메타 `type.caption` 흰색 92%: `카테고리 · 거리 · 기간 · 무료`(무료 아니면 생략). 기간 표기는 §6.1 기간 줄 규칙, 진행 중 여러 날이면 `12.14(일)까지`.
- **상단 배지** (왼쪽 위 12): 흰 배경 알약, 높이 26, 좌우 10, `type.caption` Bold 잉크. 하나만, 우선순위:
  1. 오늘 나중에 시작 → `오늘 17:00 시작`
  2. 진행 중이고 오늘 끝남 → `오늘 22:00까지` (23:59 끝이면 `오늘까지`)
  3. 진행 중 → `지금 진행 중`
- **저장 버튼** (오른쪽 위 12): 흰 원 36(불투명도 90%) + `bookmark`/`bookmark.fill` 20 잉크. 터치 영역 44/48. 동작·토스트·햅틱은 S3과 같다.
- 누름: 0.98 축소 + 불투명도 90% (`motion.fast`).

### 6.14 가로 카드 `SpotTile` · 가로 줄 `SpotTileRow`
- 카드: 폭 148, 이미지 148×112 `radius.md`(대체 일러스트 §6.17), 아래 8에 제목 `type.bodyStrong` **1줄 말줄임**, 그 아래 메타 `type.caption` `text.secondary` `● 카테고리 · 거리`(점 6pt 카테고리 색).
- 이미지 위 왼쪽 위 8에 작은 배지 하나(선택): 섹션이 '무료'가 아니면서 `is_free`면 `무료`(badge.free), '오늘 끝나요' 섹션이면 `오늘 18:00까지`(흰 배경·잉크). 배지 모양은 §6.9.
- 줄: 가로 스크롤, 처음·끝 여백 20, 카드 사이 12, 카드 단위 스냅. 섹션당 최대 10장. 화면에 2.3장쯤 보여 더 있다는 걸 알린다.
- 저장 버튼 없음(길게 누르기 메뉴로 저장). 누름 상태는 HeroCard와 같다.

### 6.15 카테고리 아이콘 타일 `CategoryTile` · `CategoryTileRow`
- 타일: 52×52, `radius.lg`(둥근 사각형), 배경 카테고리 색, 가운데 흰 카테고리 아이콘 24. 아래 6에 라벨 `type.caption` `text.secondary` 가운데.
- 짧은 라벨: 전체 · 자연 · 축제 · 공연 · 전시 · 팝업 · 먹거리 · 가족 · 반려동물 · 스포츠. `전체`는 잉크 배경 + `sparkle` / `auto_awesome`.
- 줄: 가로 스크롤, 열 폭 56, 열 사이 10, 처음 여백 20. 선택 상태 없음(누르면 S1b로 이동하는 버튼).
- 흰 아이콘–카테고리 색 대비는 §2.3대로 3:1 이상(그래픽 기준).
- 칩(§6.2)과 구분: 칩은 S1b·S2에서 **필터 토글**, 타일은 홈에서 **이동 버튼**.

### 6.16 섹션 제목 `SectionHeader`
- 높이 44, 좌우 20. 왼쪽 제목 `type.title2` `text.primary`. 오른쪽 글자 버튼 **전체 보기** `type.caption` `text.secondary` + `chevron.right` 14 (터치 44/48).
- 제목과 내용 사이 `space.3`, 섹션 사이 `space.8`.
- 접근성: 제목은 헤더, 버튼 라벨은 "{섹션 제목}, 전체 보기".

### 6.17 이미지 대체 규칙 `SpotImage`
`image_url`이 **없거나**, 불러오기에 **실패**하면(4xx·5xx·디코딩 실패·10초 초과) 카테고리 대체 일러스트를 보여준다. 불러오는 중에는 `bg.secondary`만.

| 표시 크기 | 대체 |
|---|---|
| 폭 120 이상 — 히어로(§6.13), 가로 카드(§6.14), 상세 대표 이미지(S3) | **대체 일러스트** `assets/fallback/<category>.svg` |
| 폭 120 미만 — SpotCard 썸네일 72, 지도 시트 카드 | 카테고리 색 12% 배경 + 카테고리 아이콘(§6.1) |

- 일러스트 9종: [assets/fallback/](assets/fallback/) — `nature` `festival` `performance` `exhibition` `popup` `food` `family` `pet` `sports` `.svg`. 한눈에 보기: [mockups/fallback-illustrations.png](mockups/fallback-illustrations.png).
- 그리는 법(새로 추가·수정할 때도 지킬 것): viewBox **400×300**, 배경 = 카테고리 색 꽉 채움, 그 위엔 **흰 도형만**(불투명도 15–100%로 깊이), 선·그라데이션·글자 없음. 카테고리 색은 흰 도형 안의 작은 점·구멍에만 다시 쓴다. 핵심 그림은 가운데 안전 영역 **x 110–290, y 55–245** 안 — 4:3·16:9·1:1 어느 비율로 가운데 잘라도 보인다.
- 넣는 법: 가운데 잘라 꽉 채움(iOS `.scaledToFill()` + clip, Android `ContentScale.Crop`). iOS는 에셋 카탈로그에 SVG 그대로(Preserve Vector Data), Android는 Vector Drawable로 변환(`fallback_<category>.xml`).
- 알 수 없는 카테고리: `gray700` 배경 + 가운데 흰 `sparkles` 아이콘 64.
- 히어로 위에서도 그라데이션은 그대로 덮는다(글자 대비 규칙 동일).
- 접근성: 일러스트는 장식(스크린리더에서 숨김). 사진만 "{title} 사진".

---

## 7. 접근성 기준 (모든 화면 공통)
- 글자 대비 4.5:1 이상(18pt Bold/24pt 이상은 3:1). `text.tertiary`는 작은 본문에 쓰지 않는다.
- 아이콘만 있는 버튼은 반드시 라벨: 검색 "검색", 저장 "저장"/"저장 취소", 공유 "공유", 내 위치 "내 위치로 이동".
- 다이나믹 타입 최대(AX5 / 200%)에서 카드는 썸네일을 위로 올린 세로 배치로 바뀐다. 칩·탭은 가로 스크롤 유지.
- 색만으로 정보를 전하지 않는다: 카테고리는 항상 아이콘+이름, 종료는 배지 글자로.
- 지도는 스크린리더 사용자에게 목록 보기가 대안이다. 지도 화면에서 VoiceOver/TalkBack이 켜져 있으면 시트를 펼침 상태로 시작.

## 8. 개발자 주의점
- **현재 위치명**(S1 상단)은 API에 없다. 기기 역지오코딩(iOS `CLGeocoder`, Android `Geocoder`)으로 "구 동"(예: `중구 태평로1가`)을 만들고, 실패하면 `내 주변`.
- **출처 이름 매핑**은 앱에서: `tourapi`→한국관광공사, `kopis`→KOPIS 공연예술통합전산망, `seoul_culture`→서울 열린데이터광장, 그 외→원문 값.
- **갱신 시각**(`fetched_at`): 1시간 안 `N분 전 갱신`, 24시간 안 `N시간 전 갱신`, 그 이상 `11월 18일 갱신`.
- **`is_ended`**: 상세 응답에만 의미가 있다. 저장 목록은 화면에 들어올 때 각 항목을 `GET /v1/spots/{id}`로 다시 받아 종료 여부를 갱신한다(404면 종료로 취급).
- **검색(S5)**은 `GET /v1/spots`의 `q` 파라미터를 쓴다(#28에서 추가됨).
- **홈(S1)은 `/v1/spots` 한 번**(`when=week&limit=100`)으로 받아 앱에서 섹션을 나눈다. 섹션 규칙은 [S1-home.md](screens/S1-home.md). 서버 변경 없음.
- **대체 일러스트**는 앱 번들에 넣는다(네트워크로 받지 않음). 원본은 `docs/design/assets/fallback/`.

## 9. 미결 항목
| 항목 | 지금 문서에 쓴 값 | 상태 |
|---|---|---|
| 공유 링크 도메인 | `https://herezi.com/s/<spot-id>` (S3) | **도메인 구매 후 확정.** 구매 전 빌드는 `source_url`을 공유 |
| 지원·오류 제보 메일 | `help@herezi.com` (S3 정보가 달라요, S6) | **도메인 구매 후 확정** |
| Pretendard 번들 | 시스템 글꼴로 대체 중 | 라이선스·용량 확인 후 결정 |
