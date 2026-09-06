# 장단 / Jangdan — 프로젝트 규칙

제품명 **장단 / Jangdan**(초기 코드네임은 ReBirth 상표와 철자 하나 차이라 2026-09-06 폐기 — 디렉터리·모듈 경로·문서 어디에도 남기지 않는다), 주 도메인 후보 jangdan.fm — 근거와 남은 확인은 `docs/naming-2026-09-05.md`. 브라우저에서 돌아가는 액시드 신스 + 노동요 오토파일럿.
**스택은 전부 Go**(사용자 결정 2026-09-05, 워클릿 스파이크 통과 후): 엔진 `engine/`(TinyGo → AudioWorklet), UI·방 Ebitengine(Go wasm), 서버·스트림 호스트 Go. Rust·TS UI 전환은 논의 종료 — 재론은 새 실측이 있을 때만.
**공개 레포** https://github.com/midagedev/jangdan (2026-09-05 공개, MIT 코드 + `ASSETS-LICENSE` 자산 고지). 로컬 디렉터리는 `~/repo/jangdan`, 모듈 경로 `github.com/midagedev/jangdan`. 배포: `bash tools/deploy-pages.sh` → gh-pages → https://midagedev.github.io/jangdan/ (worklet/ 계측, app/ 기기 뷰). Pages는 사전압축을 못 해 로더가 `app.wasm.gz`를 DecompressionStream으로 푼다. 공개 레포이므로 커밋 전 키·인증서·results·scratch가 gitignore에 있는지 확인(`git ls-files | grep -E 'app\.wasm|\.cert|results/|scratch/'` 가 비어야 한다).
기획서: https://claude.ai/code/artifact/1cc5ab85-bbab-4d8f-90ca-5a9856a02195 · 조사 노트 `docs/research-2026-09-05.md` · 교차 리뷰 `docs/reviews/`.

## 이미지 생성 — fal.ai

- 이미지(컨셉·에셋 초안)는 **fal.ai API**로 만든다. 사용자 지시 2026-09-05.
- **룩은 확정됐다** — `docs/concepts/README.md`의 스타일 계약(스타일·캐릭터·기기·방 서술 4개 블록)을 프롬프트 앞머리에 그대로 붙인다. 새 룩·새 캐릭터·새 기기 배치를 발명하지 않는다. 기준컷은 `room-attic-portrait-v2.png`(방 뷰)와 `device-panel-v1.png`(악기 뷰).
- 키의 단일 소유자는 `~/.zshrc`의 `export FAL_KEY=...`. 로컬에서는 이 환경변수를 쓴다.
  레포·스크래치·메모리·문서 어디에도 키를 적지 않는다. 채팅에 노출된 이력이 있으니 배포 전 회전.
- 실행: `tools/fal-gen.sh <model> <out.png> '<prompt>' ['<extra json fields>']` — 응답 원본을 `<out>.json`으로 함께 남긴다(출처 추적).
  fal은 webp를 돌려주므로 확장자는 실제 포맷을 따르거나 `sips -s format png`로 변환.
- 모델 실측(2026-09-05): `fal-ai/flux-pro/v1.1`은 무드는 좋지만 광택·과밀로 "AI 이미지"처럼 보이기 쉽다. `fal-ai/recraft-v3`는 `digital_illustration/{pixel_art,hand_drawn,grain,2d_art_poster}` 스타일 선택 가능, **프롬프트 1000자 초과 시 본문 없는 422**(스크립트가 사전 차단). `fal-ai/flux/dev`는 `guidance_scale` 2~3으로 낮추면 과렌더가 준다. 세로 컷은 `portrait_4_3`.
- 룩 규칙(사용자 판정 2026-09-05): 리얼리즘·광택·블룸·소품 과밀은 감점. 손그림 선·평면 색·무광·여백·소품 최소(책상 위 3~4개)가 기준. 캐릭터는 고유 디자인만(참조 작품 캐릭터 금지).
- 프롬프트 금지어: Roland, TB-303, TR-808, TR-909, ReBirth, Winamp, lofi girl(캐릭터 명칭). 상표·외관 회피 규칙은 기획서 08절.

## 산출물 위치

- 생성 컨셉 이미지는 `docs/concepts/`에 파일명 `<주제>-v<n>.png`로, 채택된 것만 커밋. 시험 생성은 세션 스크래치패드.

## 엔진 규칙 — `engine/`

- **순수 Go, 외부 의존 0.** 표준 라이브러리도 `math`의 비트 변환(`Float32bits`/`Float32frombits`)과 `Abs`·`Sqrt`만 허용. `math.Exp/Sin/Tanh/Pow` 금지 — gc(arm64 어셈블리)와 TinyGo(wasm)의 마지막 비트가 다를 수 있어 네이티브·브라우저 해시 일치가 깨진다. 근사식(다항·유리식·비트 트릭)을 직접 쓴다.
- **내부 샘플레이트 48000 고정, 블록 128프레임, 스테레오 인터리브(`out []float32`, len 256).** 다른 레이트는 출력단에서 리샘플한다(엔진 밖).
- **핫 루프 무할당.** `New(seed)`에서 전부 할당, `Render`·`SetParam`은 힙 할당 0(`testing.AllocsPerRun` 게이트). 슬라이스 append·클로저·인터페이스 박싱·맵 금지. TinyGo `-gc=leaking`에서 해제가 없으므로 이것이 곧 메모리 상한이다.
- **FMA 융합 차단.** Go 컴파일러는 arm64에서 `x*y + z`를 FMADD로 융합하고 wasm은 융합하지 않는다. 덧셈·뺄셈에 닿는 곱은 `mul32(a, b)`(engine/approx.go — float64 곱을 float32로 반올림, 타입 장벽) 또는 곱만 정확히 감싼 `float32(a*b)`로 쓴다. `float32(a)*b + c`와 `float32(a*b + c)`는 감싼 위치가 틀려 융합된다(2026-09-05 objdump 실측). 게이트 둘: `bash tools/check-fma.sh`(네이티브 arm64 빌드 objdump에 FMADD/FMSUB 0개) · 네이티브 300초 렌더 SHA-256 == 브라우저 OfflineAudioContext 렌더 SHA-256.
- **결정론.** 같은 seed·같은 파라미터 이력이면 어느 플랫폼에서든 같은 샘플. 난수는 엔진 내부 LCG/xorshift만(`math/rand` 금지). 시간·벽시계 참조 금지.
- **fmt·os·panic 경로 없음.** wasm-unknown 타깃에는 런타임 출력이 없다. 범위 밖 입력은 클램프한다(검증 후 폐기가 아니라 정규화).
- 파일 상단 주석에 "이 파일의 곱셈-덧셈은 전부 명시 변환" 같은 계약 문장을 두고, 테스트가 계약을 하나씩 단언한다.

## UI 기기 뷰 — 와이어프레임 → 채색 → 절단 (fal.ai)

- UI는 코드로 그리지 않고 fal.ai 그림을 쓴다(사용자 결정 2026-09-05). 단 **요소를 따로 뽑아 조립하지 않는다** — 따로 뽑은 노브·패드·패널은 조명·선 굵기가 제각각이라 콜라주가 되고, 빈 판 위 익명 노브는 악기로 읽히지 않았다(2026-09-05 목업 실패). 리버스가 주는 것은 **밀도·라벨·섹션 구조·한 장의 일관성**이다.
- 파이프라인(`tools/rack/`): ① `wire.py`가 기기 뷰 와이어프레임 PNG와 **레이아웃 JSON**(노브 중심·반지름, 버튼·패드·라벨판 rect, 모듈 rect)을 만든다 — 이 JSON이 히트 영역·스프라이트 절단·라벨 위치의 단일 소유자 ② `paint.py`가 모듈(layout.panels)별로 2× 확대해 flux/dev image-to-image 2패스(0.7 → 0.5, seed 고정)로 칠하고 합성한다 ③ `scrub.py`가 레이아웃에 선언되지 않은 자리의 밝은/채도 높은 자국(diffusion의 가짜 글자·노브 면 낙서)을 주변 중앙값으로 메운다 ④ 노브는 **칠해진 패널에서 반지름 클래스별로 가장 깨끗한 것을 잘라**(r+3, 눈금 제외) 코드가 회전(−135°..+135°)한다 ⑤ 라벨·숫자는 앱이 폰트로 올린다(diffusion은 글자를 망친다) ⑥ 버튼 lit은 밝기 틴트.
- 실측: 전체 한 장 채색은 실행마다 패드·버튼 줄이 사라지거나 두 배가 되는 드리프트가 있다 → 모듈 단위. 강도 0.74 이상은 노브 포인터가 제멋대로 돈다. canny 컨트롤(flux-lora-canny)은 색이 바랜다. 요소 단독 생성은 "top-down"을 자주 무시한다(3/4 시점 원통). recraft-v3 hand_drawn은 단독 객체 대신 장면을 그린다.
- 기기 뷰는 **세로에 가로 모듈을 쌓은 랙**(720×1280 기준, 2× 백킹): 헤더(이름판·스코프) / 베이스라인 A / 베이스라인 B / 드럼(6보이스×LEVEL·TUNE + 패드 6) / FX·SEQ(DELAY DRIVE COMP MASTER TEMPO + 스텝 16 + 트랜스포트). 밀도는 리버스급이 목표.
- 채택 패널·스프라이트·layout.json은 `app/assets/`에 커밋, 시험 생성은 세션 스크래치. 프롬프트 금지어와 스타일 계약은 위 절과 같다.
