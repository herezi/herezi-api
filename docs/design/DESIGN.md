# Herezi 디자인 시스템 (M1)

| 항목 | 내용 |
|---|---|
| 상태 | 초안 (M1 기준) |
| 적용 | `herezi-ios`(SwiftUI) · `herezi-android`(Jetpack Compose) 공통 |
| 근거 | [SERVICE-PLAN §7·§8·§9](../product/SERVICE-PLAN.md), [PRODUCT.md](../product/PRODUCT.md), [api/openapi.yaml](../../api/openapi.yaml) |
| 화면 스펙 | [screens/](screens/) — 홈 [S1-home](screens/S1-home.md)(사진 카드 피드) · 전체 목록 [S1b-list](screens/S1b-list.md) · 로그인 시트 [S7-login](screens/S7-login.md) · 목업 [mockups/](mockups/) |
| 변경 | #29 홈을 B안 사진 카드 피드 + A안 전체 목록으로, 탭 바 주변·지도·저장 · #33 대체 이미지를 타이포 포스터로, 홈 카테고리 줄을 글자 칩으로(일러스트·색 아이콘 타일 폐기) · #67 로그인 시트(S7)·소셜 로그인 버튼·계정 행·탈퇴 확인, 상세 신고 행(기획 v1.3) |

> 디자인 문서는 두 앱이 함께 쓰므로 **이 레포(`herezi-api/docs/design/`) 한 곳에만** 둔다. 앱 레포는 복사하지 말고 링크한다.
> 토큰 값을 바꾸면 이 문서를 먼저 고치고, 두 앱의 테마 파일을 같은 PR 묶음에서 맞춘다.

---

## 0. 원칙
1. **평평하고 넓게.** 그림자·그라데이션·테두리 대신 여백과 글자 크기 차이로 위계를 만든다. 예외는 하나: 사진 위 글자를 읽게 하는 히어로 카드의 어두운 그라데이션(§6.13).
2. **잉크 한 가지 색이 주인공.** 주요 버튼·선택 상태·클러스터는 잉크 `#191F28`. 색은 카테고리 구분에만 쓴다.
3. **쓰지 않는 것:** 토스 블루 계열 주색, 노란색, 지도 핀 모양 로고, 손글씨.
   - **유일한 예외 — 카카오 노랑 `#FEE500`.** 카카오 로그인 디자인 가이드가 버튼 바탕색을 강제하므로 카카오 로그인 버튼(§6.18)과 설정 계정 행의 카카오 표시(§6.19)에만 쓴다. 그 밖의 곳(강조·배지·일러스트 등)에 노란색을 쓰지 않는다. §2.5 참고.
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
| `color.overlay.poster` | `#191F28` 0%(55%) → 45%(100%) | 히어로 카드 안 타이포 포스터 위(§6.13) | 메타 줄용 |
| `color.poster.pill` | white 22% | 포스터 카테고리 알약 배경(§6.16) | – |
| `color.overlay.hero` | `#191F28` 0% → 60% → 85% 세로 그라데이션 | 히어로 카드 사진 위(§6.13) | 흰 글자 ≥ 4.5 : 1 |

**다크 모드:** M1은 라이트 모드만 출시한다(두 앱 모두 강제 라이트). 의미 토큰만 쓰면 M2에서 값만 바꿔 대응할 수 있다.

### 2.3 카테고리 색 · 아이콘 (9종)
색은 **핀 배경, 카드·칩의 작은 점, 썸네일 대체 배경, 타이포 포스터 배경(§6.16)**에만 쓴다. 넓은 면을 칠하는 건 포스터뿐이다. 글자는 카테고리 색으로 칠하지 않는다(대비 부족). 모든 색은 흰 아이콘과 3:1 이상(그래픽 대비 기준).

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
| 탭: 주변 / 지도 / 저장 / MY | `location.circle` / `map` / `bookmark` / `person` | `explore` / `map` / `bookmark` / `person` |
| 전체 보기 | `chevron.right` | `chevron_right` |
| 닫기 (시트) | `xmark` | `close` |
| 오류 제보 (S3 신고 행) | `exclamationmark.bubble` | `feedback` |
| 로그인 오류 | `exclamationmark.circle` | `error` |
| 완료 (탈퇴 완료) | `checkmark.circle` | `check_circle` |
| 계정 연결 끊김 (탈퇴 확인) | `person.crop.circle.badge.xmark` | `link_off` |

### 2.5 외부 브랜드 색 (예외, 토큰 아님)
브랜드 가이드가 정한 값이라 **우리 토큰 체계에 넣지 않고** 버튼 컴포넌트 안에 상수로 둔다. 값을 바꾸거나 다른 곳에 재사용하지 않는다.

| 이름 | 값 | 쓰는 곳 | 근거 |
|---|---|---|---|
| 카카오 버튼 바탕 | `#FEE500` | 카카오 로그인 버튼, 설정 계정 행 카카오 원 | 카카오 로그인 디자인 가이드 (Container) |
| 카카오 심볼·글자 | `#000000` (글자 85%) | 카카오 로그인 버튼 | 같은 가이드 (Symbol / Label) |
| Apple 버튼 바탕 | `#000000` | Apple 로그인 버튼(black 스타일), 설정 계정 행 Apple 원 | Apple HIG Sign in with Apple — 잉크 `#191F28`로 바꾸지 않는다 |
| Apple 로고·글자 | `#FFFFFF` | Apple 로그인 버튼 | 같은 가이드 |

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
| `size.pin` | 36 | 지도 핀 지름 |

---

## 5. 모서리 · 그림자 · 모션

| 키 | 값 | 쓰는 곳 |
|---|---|---|
| `radius.xs` | 6 | 배지 |
| `radius.sm` | 8 | 작은 버튼, 토스트 |
| `radius.md` | 12 | 썸네일, 배너, 입력창 |
| `radius.lg` | 16 | 주 버튼, 카드(시트 안 단일 카드), 히어로 카드 |
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
- 썸네일: `image_url` 있으면 72×72 `radius.md` 꽉 채움(가운데 자름). 없거나 실패하면 카테고리 색 12% 배경 + 카테고리 아이콘(28, 카테고리 색) — 72에서는 포스터 글자가 읽히지 않으므로 아이콘을 쓴다(§6.16 크기 규칙).
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
- 칩: 높이 36, 좌우 14, `radius.full`, `type.caption` SemiBold. **아이콘 없음** — 카테고리 칩은 글자 앞에 카테고리 색 점(지름 8, 글자와 간격 6). `전체`·`무료만`은 점 없음.
  - 비선택: 배경 `bg.secondary`, 글자 `text.primary`, 점 카테고리 색.
  - 선택: 배경 `bg.inverse`(잉크), 글자 흰색, 점은 카테고리 색 그대로.
- 복수 선택. 아무것도 선택 안 함 = `전체` 선택 상태. `전체`를 누르면 선택을 모두 해제. 마지막 선택을 해제하면 `전체`로 돌아간다.
- 접근성: 각 칩은 토글 버튼(`.isSelected` / `Role.Checkbox`). 라벨 "공연, 선택됨". 점은 장식(이름이 글자로 있음).
- **홈 모드** (S1 카테고리 줄, #33 잠정 A안 — 사용자가 바꿀 수 있음): 같은 컴포넌트, `무료만`과 구분선 없이 `[전체] [자연·계절] … [스포츠]`. `전체`가 항상 선택 모양이고 다른 칩은 선택 상태가 없다 — 누르면 S1b로 **이동**하는 버튼(필터 토글 아님). 접근성 역할도 버튼("공연, 버튼").

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
- **진행 중**(로그인·탈퇴처럼 몇 초 걸리는 동작): 버튼 글자 자리에 작은 진행 표시 20(버튼 글자색), 버튼 크기·색 유지. §6.7 "스피너 금지"의 유일한 예외 — 화면 가운데 스피너는 여전히 쓰지 않는다.

### 6.11 토스트 `Toast`
- 하단 탭바 위 16, 잉크 배경 `radius.sm`, 흰 `type.caption`, 2.5초. 예: "저장했어요", "저장을 취소했어요", "링크를 복사했어요", "로그인했어요", "로그아웃했어요", "기기에 저장했던 N개를 계정으로 옮겼어요".
- 여러 개가 이어지면 겹치지 않고 차례로(큐). 스크린리더로 읽어준다(iOS 알림 announcement / Android `liveRegion`).

### 6.12 탭 바
- 4개: **주변**(S1 홈 → S1b 전체 목록) · **지도**(S2) · **저장**(S4) · **MY**(S6).
- **MY 탭 = 설정(S6) 화면 자체**(탭 루트, push 아님). 맨 위 계정 행(§6.19, 로그아웃 상태면 "로그인" 행), 그 아래 위치 권한·정보 출처·약관·오류 제보·로그아웃·회원 탈퇴. 상단 바 제목은 "MY".
  - 왜 바꿨나 (2026-11-25 사용자 결정): 저장 탭 상단 톱니로 설정에 들어가는 구조가 실제로 써보니 어색했다. '내 것'(계정·설정)은 마지막 탭에서 찾는 국내 앱 관례(당근·토스 등)를 따른다. 저장 탭 상단 바의 톱니는 없앤다.
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
│           사진 / 타이포 포스터             │
│ ░░░░░░░░░░░ 아래로 갈수록 어둡게 ░░░░░░░░░ │
│ 서울빛초롱축제                             │  title1, 흰색, 최대 2줄
│ 축제·행사 · 350m · 12.14(일)까지 · 무료     │  caption, 흰색 92%
└──────────────────────────────────────────┘
```
- 크기: 좌우 여백 20 안 꽉 채움, 높이 `size.hero.h`, `radius.lg`, 그림자 없음.
- 이미지: 가운데 잘라 꽉 채움. 없거나 실패하면 타이포 포스터(§6.16)를 **히어로 모드**로:
  - 포스터 제목이 곧 카드 제목이다(제목을 두 번 쓰지 않는다). 34pt ExtraBold, 최대 3줄, 아래 여백 44(메타 줄 위).
  - 카테고리 알약은 숨긴다(배지 자리, 카테고리는 메타 줄에 있음). 두 눈은 저장 버튼 왼쪽 10, 저장 버튼과 세로 가운데 맞춤.
  - 그라데이션은 약하게 `color.overlay.poster`(55% 지점 0% → 아래 45%) — 메타 줄 대비용. 34pt 제목은 그라데이션 없이도 9색 모두 흰색 3:1 이상(큰 글자 기준).
- **그라데이션** `color.overlay.hero`: 위→아래 잉크 `#191F28` 불투명도 0%(30% 지점) → 60%(60% 지점) → 85%(100%). 글자는 60% 지점보다 아래에만 놓는다 — 가장 밝은 흰 사진 위에서도 흰 글자 대비 4.5:1 이상(60% 잉크 위 흰색 ≈ 4.8:1).
- 글자: 좌우 16, 아래 16. 제목 `type.title1` 흰색 최대 2줄. 메타 `type.caption` 흰색 92%: `카테고리 · 거리 · 기간 · 무료`(무료 아니면 생략). 기간 표기는 §6.1 기간 줄 규칙, 진행 중 여러 날이면 `12.14(일)까지`.
- **상단 배지** (왼쪽 위 12): 흰 배경 알약, 높이 26, 좌우 10, `type.caption` Bold 잉크. 하나만, 우선순위:
  1. 오늘 나중에 시작 → `오늘 17:00 시작`
  2. 진행 중이고 오늘 끝남 → `오늘 22:00까지` (23:59 끝이면 `오늘까지`)
  3. 진행 중 → `지금 진행 중`
- **저장 버튼** (오른쪽 위 12): 흰 원 36(불투명도 90%) + `bookmark`/`bookmark.fill` 20 잉크. 터치 영역 44/48. 동작·토스트·햅틱은 S3과 같다.
- 누름: 0.98 축소 + 불투명도 90% (`motion.fast`).

### 6.14 가로 카드 `SpotTile` · 가로 줄 `SpotTileRow`
- 카드: 폭 148, 이미지 148×112 `radius.md`(없으면 타이포 포스터 §6.16), 아래 8에 제목 `type.bodyStrong` **1줄 말줄임**, 그 아래 메타 `type.caption` `text.secondary` `● 카테고리 · 거리`(점 6pt 카테고리 색).
- 이미지 위 왼쪽 위 8에 작은 배지 하나(선택): 섹션이 '무료'가 아니면서 `is_free`면 `무료`(badge.free), '오늘 끝나요' 섹션이면 `오늘 18:00까지`(흰 배경·잉크). 배지 모양은 §6.9.
- 줄: 가로 스크롤, 처음·끝 여백 20, 카드 사이 12, 카드 단위 스냅. 섹션당 최대 10장. 화면에 2.3장쯤 보여 더 있다는 걸 알린다.
- 저장 버튼 없음(길게 누르기 메뉴로 저장). 누름 상태는 HeroCard와 같다.

### 6.15 섹션 제목 `SectionHeader`
- 높이 44, 좌우 20. 왼쪽 제목 `type.title2` `text.primary`. 오른쪽 글자 버튼 **전체 보기** `type.caption` `text.secondary` + `chevron.right` 14 (터치 44/48).
- 제목과 내용 사이 `space.3`, 섹션 사이 `space.8`.
- 접근성: 제목은 헤더, 버튼 라벨은 "{섹션 제목}, 전체 보기".

### 6.16 이미지 대체 — 타이포 포스터 `SpotImagePlaceholder`
`image_url`이 **없거나**, 불러오기에 **실패**하면(4xx·5xx·디코딩 실패·10초 초과) 그림 대신 **행사 이름을 큰 글자로 넣은 포스터**를 그린다. 카드마다 제목이 달라 같은 그림이 반복돼 보이지 않는다. 불러오는 중에는 `bg.secondary`만. (#29의 그림 일러스트 9종은 'AI스러움'으로 폐기, #33.)

목업: [mockups/placeholder-poster.png](mockups/placeholder-poster.png)

| 표시 크기 | 대체 |
|---|---|
| 폭 120 이상 — 히어로(§6.13), 가로 카드(§6.14), 상세 대표 이미지(S3) | **타이포 포스터** |
| 폭 120 미만 — SpotCard 썸네일 72, 지도 시트 카드 | 카테고리 색 12% 배경 + 카테고리 아이콘(§6.1) |

```
┌──────────────────────────────────────────┐
│ (축제·행사)                           ●● │  카테고리 알약 · 브랜드 두 눈
│                                          │
│ 서울빛초롱                                │  흰 ExtraBold, 왼쪽 아래 정렬
│ 축제                                     │  최대 3줄, 줄높이 1.12, 자간 -0.04em
└──────────────────────────────────────────┘
```
- **배경:** 카테고리 색(§2.3) 꽉 채움. 무늬·그라데이션·그림 없음. 알 수 없는 카테고리는 `gray700`, 알약 "기타".
- **제목:** Pretendard ExtraBold(800), 흰색, 줄높이 1.12, 자간 -0.04em, **최대 3줄**(넘치면 마지막 줄 끝 `…`). 왼쪽 아래 정렬. 줄바꿈은 단어(띄어쓰기) 단위, 한 단어가 한 줄보다 길 때만 글자 단위.
  - 크기는 카드 폭으로 정한다: **폭 240 이상 34pt**(히어로·상세), **그 미만 17pt**(가로 카드). 다이나믹 타입을 따르지 않는다(그림 취급, 같은 제목이 옆에 진짜 글자로 있음).
  - 여백: 34pt일 때 왼쪽 16·아래 14·오른쪽 40, 17pt일 때 왼쪽·오른쪽·아래 10.
- **카테고리 알약** (왼쪽 위 12): 흰색 22% 배경, 높이 22, 좌우 8, 12 Bold 흰 글자(예: **축제·행사**). **폭 240 미만이면 숨김.**
- **브랜드 두 눈** (오른쪽 위): 로고 두리번의 눈 — 흰 타원 2개(가로:세로 = 0.68:1, 사이 간격 = 눈 높이 × 0.15), 잉크 눈동자(지름 = 눈 높이 × 0.34)가 **오른쪽 위**를 본다. 눈 높이: 폭 240 이상 20(오른쪽 14·위 12), 그 미만 11(오른쪽 8·위 8). 로고 SVG에서 잘라 쓰지 말고 이 비율로 그린다(작은 크기에서 또렷하게).
- **놓는 곳별 차이:**
  | 놓는 곳 | 알약 | 눈 | 제목 |
  |---|---|---|---|
  | 가로 카드(148) | 숨김 | 오른쪽 위 8 | 17pt. 카드 배지(무료 등)는 왼쪽 위 그대로 |
  | 히어로(§6.13) | 숨김(배지 자리) | 저장 버튼 왼쪽 | 34pt, 카드 제목 역할, 메타 줄 위 |
  | 상세 대표 이미지(S3) | 보임 | 오른쪽 위 | 34pt. 알약·눈은 상단 바(안전 영역 + 56) **아래**에서 시작 |
- **대비:** 34pt는 큰 글자 기준 3:1 — 9색 모두 흰색과 3.4:1 이상(nature 3.4, food 3.6, family 3.6, festival 3.9, exhibition 4.0, popup 4.2, performance 4.9, pet 4.9, sports 5.2). 17pt 가로 카드 포스터 글자는 카드 아래에 같은 제목이 진짜 글자로 있으므로 장식으로 본다.
- **구현:** 이미지 파일이 아니라 뷰로 그린다(iOS `ZStack` + `Text`, Android `Box` + `Text`). 에셋 없음.
- **접근성:** 장식 — 스크린리더에서 숨김(`accessibilityHidden(true)` / `clearAndSetSemantics {}`). 제목은 이미 카드·상세의 진짜 글자로 읽힌다. 사진일 때만 "{title} 사진".

### 6.17 로그인 시트 `LoginSheet`
화면 스펙: [S7-login.md](screens/S7-login.md) · 목업: [mockups/login-sheet.png](mockups/login-sheet.png)
- 모달 바텀시트, 스크림 `color.scrim`, 위 모서리 `radius.xl`, `elevation.2`, 손잡이 36×4 `gray200`. 높이는 내용에 맞춤.
- 구성(위→아래): 닫기(오른쪽 위 `xmark`/`close`) → 제목 `type.title2` → 이유 한 줄 `type.body` `text.secondary` → (오류 줄) → `SocialLoginButton` 카카오 → Apple(사이 12, **iOS만**. Android는 카카오 버튼 하나) → 약관 안내 `type.caption` 가운데(링크 2개 밑줄).
- 입력: `title`, `pendingAction`(저장할 spot id 또는 없음). 성공하면 시트를 닫고 `pendingAction`을 실행한다.
- 상태: 기본 / 진행 중(누른 버튼에 진행 표시, 나머지·닫기 비활성, 끌어서 닫기 막음) / 취소됨(조용히 기본으로) / 오류(오류 줄 + 같은 버튼으로 재시도).
- **로그인 UI는 이 시트 한 곳에만 둔다.** S4·S6의 "로그인"은 이 시트를 여는 버튼일 뿐 소셜 버튼을 따로 그리지 않는다.

### 6.18 소셜 로그인 버튼 `SocialLoginButton`
두 버튼은 **같은 크기**: 높이 56(다이나믹 타입에 따라 늘어남, 최소 56), 폭 꽉 채움, 모서리 12, 심볼 24는 왼쪽 20, 글자는 버튼 가운데 `type.title3`(17 SemiBold). 그림자·테두리 없음. 순서는 **카카오 → Apple**. Apple 버튼은 iOS에만 있다(Android는 카카오만, SERVICE-PLAN §12 Q6).

| | 카카오 `KakaoLoginButton` | Apple `AppleLoginButton` |
|---|---|---|
| 근거 | 카카오 로그인 디자인 가이드 | Apple HIG "Sign in with Apple", App Store 심사 지침 4.8 |
| 바탕 | `#FEE500` (가이드 필수, §2.5) | 검정 `#000000` (black 스타일) |
| 심볼 | 카카오 말풍선 심볼 `#000000`, **카카오 배포 리소스 그대로** | Apple 로고 흰색, **Apple 배포 에셋 그대로** |
| 글자 | **카카오로 계속하기**, `#000000` 85% | **Apple로 계속하기**, 흰색 |
| iOS | 직접 그림 | **시스템 버튼** `SignInWithAppleButton(.continue)` + `.signInWithAppleButtonStyle(.black)`, 높이 56, `cornerRadius` 12 — 글자·로고는 시스템이 넣는다 |
| Android | 직접 그림 | 직접 그림(가이드 비율 유지), 누르면 Custom Tabs 웹 흐름 |
| 하지 말 것 | 심볼 색·모양 바꾸기, 바탕을 다른 노랑/잉크로 바꾸기, 심볼 없이 글자만 | 바탕을 잉크로 바꾸기, 로고 다시 그리기, 카카오보다 작게·흐리게 두기 |

- 눌림: 불투명도 85%(축소 없음 — 브랜드 버튼은 모양을 바꾸지 않는다). 진행 중·비활성 규칙은 §6.10.
- 접근성: 라벨 = 버튼 글자. 대비 카카오 ≈ 13:1, Apple 21:1.

### 6.19 계정 행 `AccountRow`
설정(S6) 계정 그룹 맨 위. 화면 스펙: [S6-settings.md](screens/S6-settings.md) · 목업: [mockups/settings-account.png](mockups/settings-account.png)
- 행 높이 56, 좌우 20. 왼쪽 제공자 표시 28 원(카카오: `#FEE500` + 검정 심볼 18 / Apple: 검정 + 흰 로고 16), 원–글자 12, 글자 `type.body` **카카오로 로그인됨** / **Apple로 로그인됨**.
- 탭 동작·화살표 없음. 제공자 표시는 장식(스크린리더 숨김).
- 로그아웃 상태에서는 이 행 대신 일반 설정 행 **로그인** + 화살표(→ §6.17).

### 6.20 파괴 동작 확인 `DestructiveConfirm`
되돌릴 수 없는 동작(M1은 **회원 탈퇴**만) 앞에 두는 확인 화면. 빨간 화면을 만들지 않고 **무엇이 사라지는지 글로** 보여준다.
- push 화면, 상단 바는 뒤로만. 제목 `type.title1` 2줄 → "지워지는 것" 박스(`bg.secondary`, `radius.md`, 행 48: 아이콘 22 `text.secondary` + `type.bodyStrong`) → 안내 `type.caption` → 확인 체크박스(24, `radius.xs`) → 하단 고정 주 버튼(체크 전 비활성).
- 주 버튼은 **잉크**. 빨강(`color.status.error`)은 그다음 뜨는 **시스템 확인 대화상자의 파괴 버튼**에만(iOS `role: .destructive`, Android `TextButton` 글자색) — 두 번 확인.
- 진행 중: 주 버튼 진행 표시(§6.10), 뒤로 막음. 완료: 확인 화면을 완료 화면(§6.7 상태 화면 모양 + 하단 주 버튼 **확인**)으로 **교체**해 뒤로 돌아갈 수 없게.
- 실패: 화면 유지 + 토스트.

### 6.21 신고 행 `ReportRow`
상세(S3) 출처 카드 아래. 스펙: [S3-detail.md](screens/S3-detail.md) 8번 (herezi-ios#28, herezi-android#25에서 구현됨).
- 테두리 1px `color.border` + `radius.md`, 안쪽 16, 배경 흰색. 아이콘 20 `text.secondary`(`exclamationmark.bubble` / `feedback`) · 제목 `type.bodyStrong` **정보가 달라요** · 설명 `type.caption` `text.secondary` **기간·장소·요금이 틀렸다면 알려 주세요** · 오른쪽 `chevron.right`.
- §0 원칙 1(테두리 대신 여백)의 예외: 글자 버튼만으로는 "신고"로 읽히지 않아서 행 하나만 테두리로 구분한다. 다른 목록 행에 퍼뜨리지 않는다.

---

## 7. 접근성 기준 (모든 화면 공통)
- 글자 대비 4.5:1 이상(18pt Bold/24pt 이상은 3:1). `text.tertiary`는 작은 본문에 쓰지 않는다.
- 아이콘만 있는 버튼은 반드시 라벨: 검색 "검색", 저장 "저장"/"저장 취소", 공유 "공유", 내 위치 "내 위치로 이동".
- 다이나믹 타입 최대(AX5 / 200%)에서 카드는 썸네일을 위로 올린 세로 배치로 바뀐다. 칩·탭은 가로 스크롤 유지.
- 색만으로 정보를 전하지 않는다: 카테고리는 항상 이름(글자)과 함께, 종료는 배지 글자로.
- 지도는 스크린리더 사용자에게 목록 보기가 대안이다. 지도 화면에서 VoiceOver/TalkBack이 켜져 있으면 시트를 펼침 상태로 시작.

## 8. 개발자 주의점
- **현재 위치명**(S1 상단)은 API에 없다. 기기 역지오코딩(iOS `CLGeocoder`, Android `Geocoder`)으로 "구 동"(예: `중구 태평로1가`)을 만들고, 실패하면 `내 주변`.
- **출처 이름 매핑**은 앱에서: `tourapi`→한국관광공사, `kopis`→KOPIS 공연예술통합전산망, `seoul_culture`→서울 열린데이터광장, 그 외→원문 값.
- **갱신 시각**(`fetched_at`): 1시간 안 `N분 전 갱신`, 24시간 안 `N시간 전 갱신`, 그 이상 `11월 18일 갱신`.
- **`is_ended`**: 상세 응답에만 의미가 있다. 저장 목록은 화면에 들어올 때 각 항목을 `GET /v1/spots/{id}`로 다시 받아 종료 여부를 갱신한다(404면 종료로 취급).
- **검색(S5)**은 `GET /v1/spots`의 `q` 파라미터를 쓴다(#28에서 추가됨).
- **홈(S1)은 `/v1/spots` 한 번**(`when=week&limit=100`)으로 받아 앱에서 섹션을 나눈다. 섹션 규칙은 [S1-home.md](screens/S1-home.md). 서버 변경 없음.
- **대체 이미지**(`SpotImagePlaceholder`)는 에셋 없이 뷰로 그린다(§6.16). 제목 줄바꿈·말줄임은 플랫폼 텍스트 레이아웃에 맡기고 `lineLimit(3)` / `maxLines = 3`.
- **로그인·저장(v1.3)**: 둘러보기(S1·S1b·S2·S3·S5)는 로그인 없이. 저장 버튼만 로그인 상태를 본다 — 로그아웃이면 저장 대신 S7 시트. 인증·엔드포인트는 [TECH-PLAN §4.1](../product/TECH-PLAN.md). 첫 로그인 때 기기 로컬 저장을 계정으로 옮긴다([S4](screens/S4-saved.md) "기기 저장 옮기기").
- **로그인 버튼 에셋**: 카카오 심볼은 Kakao Developers 리소스, Apple 로고는 iOS 시스템 버튼 / Android는 Apple 배포 에셋. 목업 속 심볼은 자리 표시다.

## 9. 미결 항목
| 항목 | 지금 문서에 쓴 값 | 상태 |
|---|---|---|
| 공유 링크 도메인 | `https://herezi.com/s/<spot-id>` (S3) | **도메인 구매 후 확정.** 구매 전 빌드는 `source_url`을 공유 |
| 지원·오류 제보 메일 | `help@herezi.com` (S3 정보가 달라요, S6) | **도메인 구매 후 확정** |
| Pretendard 번들 | 시스템 글꼴로 대체 중 | 라이선스·용량 확인 후 결정. 포스터 ExtraBold(800)가 필요해 번들 쪽이 유리 |
| 홈 카테고리 줄 모양 | 글자 칩 + 색 점(§6.2 홈 모드, #33 잠정 A안) | 사용자 최종 선택 대기(B 큰 글자 탭 / C 회색 타일 + 선 아이콘) |
| 로그인 버튼 순서 | 카카오 → Apple (§6.18) | 국내 사용자 기준 결정. 심사에서 문제 되면 Apple을 위로 |
| 약관·개인정보 처리방침 갱신 | S7 약관 안내가 기존 링크를 씀 | **로그인 출시 전 필수** — 카카오/Apple 계정 식별자 수집 항목 추가(TECH-PLAN §4.1) |
| Apple 탈퇴 토큰 철회 | 서버 revoke 전제(S6 개발자 주의) | 서버가 refresh token을 보관할지, 탈퇴 직전 재인증할지 서버 이슈에서 결정 |
| Android Apple 로그인 | 넣지 않음 (2026-11-25 결정) | iOS에서 Apple로 가입한 계정은 Android 설정 화면에 "Apple로 로그인됨"으로 표시만 될 수 있다 |
| 카카오 로그인 버튼 글자 | "카카오로 계속하기" | 가이드가 허용하는 문구 범위 안 — 카카오 앱 검수 때 확인 |
