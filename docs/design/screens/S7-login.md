# S7 로그인 시트

> 토큰·컴포넌트: [DESIGN.md](../DESIGN.md) §6.17 `LoginSheet` · §6.18 `SocialLoginButton` · 기획: SERVICE-PLAN v1.3 §6 원칙 1, §8 P1 저장·계정, §9 S7, Q6 · 기술: TECH-PLAN §4.1
> 목업: [../mockups/login-sheet.png](../mockups/login-sheet.png)

## 목적
둘러보기는 로그인 없이 하고, **저장하려는 순간에만** 로그인을 받는다. 왜 로그인해야 하는지 한 줄로 말하고, 버튼 두 개 중 하나만 누르면 끝나게 한다. 로그인이 끝나면 사용자가 하려던 저장을 바로 마친다.

## 여는 곳
| 진입 | 시트 제목 | 로그인 후 할 일 |
|---|---|---|
| 로그아웃 상태에서 **저장** 누름 — S3 상단 바 저장, 홈 히어로 카드 저장(DESIGN §6.13), 가로 카드 길게 누르기 메뉴 **저장** | **로그인하면 저장할 수 있어요** | 누른 볼거리 저장(`PUT /v1/me/saved/{spot_id}`) + 토스트 **저장했어요** |
| S4 로그아웃 상태 **로그인** 버튼 | **로그인하고 저장한 볼거리를 모아 보세요** | S4 목록 표시. 토스트 **로그인했어요** |
| S6 계정 그룹 **로그인** 행 | 위와 같음 | S6 계정 그룹이 로그인 상태로 바뀜. 토스트 **로그인했어요** |

- 저장 아이콘은 로그인 전에는 **채우지 않는다**(시트만 띄움). 성공한 뒤에 채운다.
- 시트는 앱 어디서 열려도 같은 컴포넌트다. 화면별로 다른 건 제목과 "로그인 후 할 일"(대기 중 동작)뿐이다.

## 레이아웃 (위→아래)
모달 바텀시트. 뒤는 `color.scrim`(잉크 40%)으로 어둡게. 위 모서리 `radius.xl`(24), `elevation.2`. 높이는 내용에 맞춤(390폭 기준 약 360 + 아래 안전 영역).

1. **손잡이** 36×4 `gray200`, 위 8, 가운데.
2. **닫기** 버튼 오른쪽 위: `xmark` / `close` 아이콘 24 `icon.primary`, 터치 44/48, 오른쪽 8·위 12. 라벨 "닫기".
3. **제목** `type.title2` `text.primary`, 좌우 20, 위 `space.8`(손잡이 기준 약 52). 위 표의 문구. 최대 2줄.
4. **이유 한 줄** `type.body` `text.secondary`, 제목 아래 `space.2`: **저장한 볼거리를 어느 기기에서든 다시 볼 수 있어요.**
5. **버튼 2개** (이유 아래 `space.6`, 좌우 20 안 꽉 채움, 사이 `space.3`)
   1. **카카오로 계속하기** — DESIGN §6.18 카카오 버튼
   2. **Apple로 계속하기** — DESIGN §6.18 Apple 버튼
6. **약관 안내** `type.caption` `text.secondary` 가운데 정렬, 버튼 아래 `space.5`:
   **계속하면 [이용약관]과 [개인정보 처리방침]에 동의하는 것으로 봐요.**
   - `이용약관`·`개인정보 처리방침`은 링크: `text.primary` SemiBold + 밑줄. 누르면 인앱 브라우저(SFSafariViewController / Custom Tabs), 시트는 그대로 유지.
   - 링크 주소는 S6과 같은 값(iOS `SettingsViewModel.termsURL` / Android `HereziLinks.TERMS`·`PRIVACY`)을 쓴다.
7. 아래 여백: 안전 영역 + `space.3`.

### 버튼 순서와 크기 — 결정
- **카카오 위, Apple 아래.** 사용자 대부분이 카카오 계정을 쓰는 국내 서비스라 첫 번째 자리를 카카오에 준다. 두 플랫폼 모두 같은 순서.
- **두 버튼은 같은 크기·같은 모서리**(높이 56, 폭 꽉 채움, 모서리 12). Apple 가이드(HIG Sign in with Apple, App Store 심사 지침 4.8)는 Apple 버튼을 다른 로그인 버튼보다 작거나 덜 눈에 띄게 두지 말라고 한다 — 같은 크기·같은 줄 폭이면 충족. Apple 버튼을 스크롤해야 보이게 두지 않는다(시트 높이는 다이나믹 타입 최대에서도 두 버튼이 한 화면에 보이도록, 넘치면 제목·이유 쪽을 스크롤).
- 버튼 사이에 "또는" 구분선 없음. 구글·이메일 로그인 없음(Q6).

## 상태
| 상태 | 모습 · 문구 |
|---|---|
| 기본 | 위 레이아웃. 두 버튼 활성 |
| 진행 중 | 누른 버튼: 글자 자리에 작은 진행 표시(20, 버튼 글자색 — 카카오는 검정 85%, Apple은 흰색) + 스크린리더 라벨 "로그인 중". 버튼 색·크기는 그대로. 다른 버튼과 닫기는 비활성(불투명도 40%), 시트 끌어내리기·스크림 탭 닫기도 막는다. **시스템/카카오 화면이 떠 있는 동안 + Supabase 세션 교환이 끝날 때까지** 유지 |
| 취소됨 | 사용자가 카카오톡·카카오 계정 화면, Apple 시트, (Android) Apple 웹 탭에서 취소·닫기 → **기본 상태로 조용히 돌아온다.** 문구·토스트 없음. 대기 중 저장은 하지 않는다 |
| 오류 | 버튼 위(이유 줄 아래)에 오류 줄: `exclamationmark.circle` / `error` 18 `color.status.error` + `type.caption` `text.primary` **로그인하지 못했어요. 다시 시도해 주세요.** 두 버튼 다시 활성. **다시 시도 = 같은 버튼을 다시 누르기**(별도 버튼 없음). 오류 줄은 버튼을 다시 누르면 사라진다 |
| 오류 — 네트워크 없음 | 오류 줄 문구만 **인터넷 연결을 확인하고 다시 시도해 주세요.** |
| 성공 | 시트가 닫힌다(`motion.slow`, 동작 줄이기면 페이드). 그다음 "로그인 후 할 일" 수행 → 토스트 |
| 성공 후 저장 실패 | 로그인은 유지, 저장 아이콘은 비운 채로. 토스트 **저장하지 못했어요. 다시 눌러 주세요.** |

- 진행 표시는 DESIGN §6.7 "스피너 금지"의 예외다(버튼 안 작은 표시만, 화면 전체 스피너 아님 — DESIGN §6.18).
- 오류 문구는 원인(토큰 만료, 서버 5xx, 카카오/Apple 오류 코드)을 사용자에게 보여주지 않는다. 로그로만 남긴다.

## 로그인 후 처리 (성공 시 순서)
1. Supabase 세션 저장(키체인 / EncryptedSharedPreferences).
2. **첫 로그인 옮기기** — 이 기기에 로그인 전 버전에서 저장한 항목(기기 로컬 저장)이 있으면 계정으로 옮긴다(`PUT /v1/me/saved/{id}` 반복, 이미 있으면 그대로). 다 옮기면 로컬에서 지운다. 자세한 규칙은 [S4-saved.md](S4-saved.md) "기기 저장 옮기기".
3. 대기 중 저장 수행(저장 진입일 때).
4. 토스트. 두 개가 겹치면 차례로 보여준다(각 2.5초): **저장했어요** → **기기에 저장했던 N개를 계정으로 옮겼어요**.

## 인터랙션
- 닫기 / 스크림 탭 / 아래로 끌기 / Android 뒤로 → 시트 닫힘, 대기 중 저장 버림. 문구 없음.
- 버튼 연타: 진행 중에는 두 번째 탭을 무시한다.
- 약관 링크는 시트를 닫지 않는다.
- 로그인한 상태에서는 이 시트가 뜰 일이 없다. 세션이 만료돼 갱신에 실패하면(401 + 리프레시 실패) 로그아웃 상태로 바꾸고, 다음 저장 때 이 시트가 다시 뜬다.

## 플랫폼별
| | iOS | Android |
|---|---|---|
| 시트 | `.sheet` + `.presentationDetents([.height(측정값)])` + `.presentationDragIndicator(.visible)`, 진행 중 `.interactiveDismissDisabled(true)` | `ModalBottomSheet`(`sheetState` `skipPartiallyExpanded = true`), 진행 중 `confirmValueChange = { false }`로 닫기 막음 |
| 카카오 | Kakao SDK `UserApi`: 카카오톡 설치 시 `loginWithKakaoTalk`, 아니면 `loginWithKakaoAccount`(웹). 받은 OIDC `id_token` → Supabase `signInWithIdToken(provider: .kakao)` | 같음(`UserApiClient`). 카카오톡 → 없으면 카카오 계정 웹 |
| Apple | **시스템 버튼** `ASAuthorizationAppleIDButton(type: .continue, style: .black)` — 한국어 기기에서 "Apple로 계속하기"가 시스템이 넣어 준다. SwiftUI `SignInWithAppleButton(.continue)` + `.signInWithAppleButtonStyle(.black)`, `.frame(height: 56)`, `cornerRadius` 12. 시스템 Apple 시트 → `identityToken` → `signInWithIdToken(provider: .apple, nonce)` | 시스템 버튼이 없으므로 **Apple 가이드대로 그린 버튼**(검정 배경, Apple이 배포하는 로고 에셋, 글자 "Apple로 계속하기"). 누르면 **웹 흐름**: Custom Tabs로 Apple 로그인 페이지(Supabase OAuth `signInWithOAuth(apple)`, 앱 딥링크로 복귀). 탭이 떠 있는 동안 진행 상태 유지, 사용자가 탭을 닫고 돌아오면 = 취소됨 |
| Apple 버튼 글자 | 시스템이 정함(바꾸지 않는다) | "Apple로 계속하기" 고정 |

- iOS의 Apple 버튼은 **직접 그리지 않는다**(시스템 버튼이 로고·글자·간격을 가이드대로 맞춘다). 카카오 버튼만 직접 그린다.
- 카카오 심볼은 카카오가 배포하는 리소스(Kakao Developers > 디자인 가이드 > 리소스 다운로드)를 쓴다. 목업의 말풍선은 자리 표시일 뿐이다.

## 접근성
- 시트가 열리면 초점을 **제목**으로 옮긴다. 시트는 모달(뒤 화면 읽지 않음, iOS `accessibilityViewIsModal`, Android `ModalBottomSheet` 기본).
- 읽는 순서: 제목 → 이유 → 카카오 버튼 → Apple 버튼 → 약관 안내(링크 2개는 각각 링크로) → 닫기.
- 버튼 라벨: "카카오로 계속하기, 버튼", "Apple로 계속하기, 버튼". 진행 중 "로그인 중", 비활성 버튼은 "흐리게 표시됨/사용 중지됨".
- 오류 줄은 나타날 때 바로 읽어준다(iOS `AccessibilityNotification.Announcement`, Android `liveRegion = Polite`).
- 대비: 카카오 버튼 글자 검정 85% on `#FEE500` ≈ 13 : 1, Apple 흰 글자 on 검정 21 : 1, 약관 안내 `text.secondary` 7.1 : 1.
- 다이나믹 타입: 버튼 높이는 최소 56, 글자가 커지면 버튼이 세로로 늘어난다(말줄임 금지). 심볼은 글자 크기를 따라 커지지 않는다(24 고정).
- 터치 영역: 버튼 전체, 닫기 44/48, 약관 링크는 글자 줄 높이 + 위아래 여백으로 최소 44/48.
