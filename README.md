# 장단 · Jangdan

다락방에서 밤새 도는, 브라우저 액시드 신스 + 노동요 오토파일럿. 만지면 악기, 두면 라디오, 모이면 방송.

**상태: Phase 1 구현 중(2026-09).** Phase 0 스파이크(워클릿 엔진·Ebitengine UI)는 통과했고, 제품 골격이 올라갔습니다.

라이브: https://midagedev.github.io/jangdan/ (`app/` 방 뷰·기기 뷰, `worklet/` 계측 페이지)

## 구조

| 디렉터리 | 역할 |
|---|---|
| `engine/` | 순수 Go DSP·시퀀서. 베이스라인 2(다이오드 래더 근사·슬라이드·액센트), 드럼 6보이스(BD SD CH OH CP CY, 전부 합성), 이펙트 3(사이드체인 덕킹·드라이브·템포 딜레이). 명령(`Cmd`)으로만 조작하고, 파라미터는 12비트로 양자화해 저장합니다 |
| `cmd/worklet/` | 엔진의 TinyGo `wasm-unknown` 진입점. AudioWorklet 안에서 돕니다 |
| `session/` | 이벤트 로그·키프레임·시드 단어·URL 직렬화. "로그가 정본"입니다 |
| `resident/` | 레지던트 DJ: 패턴 생성기·에너지 곡선·바이브 3종·포모도로·MANUAL 잠금 |
| `app/` | Ebitengine(Go wasm) 앱. `core/` 공용 계약, `view/room/` 방 뷰, `view/device/` 기기 뷰, `web/` 워클릿 호스트(host.js·processor.js) |
| `tools/` | 게이트·빌드·배포·fal.ai 파이프라인(`rack/` 기기 패널, `font/` 글리프 아틀라스) |
| `cf/` | 계측 리포트·텔레메트리를 받는 Cloudflare Worker |
| `spike/` | Phase 0 계측 스파이크(동결) |

원칙 하나로 묶입니다. 사람 손이든 레지던트든 엔진에 닿는 변경은 전부 `engine.Cmd`이고, 호스트가 블록 인덱스와 저자를 붙여 기록합니다. 리플레이·URL 열기·서버 렌더는 "새 엔진 + 같은 로그"입니다.

## 빌드와 게이트

```bash
go test ./... && bash tools/check-fma.sh          # 엔진·세션·레지던트·뷰 테스트, FMA 0
bash app/build.sh                                  # app.wasm + engine.wasm + 자산 복사
node app/serve.mjs                                 # https://localhost:8444
node app/measure.mjs --browser chromium            # 첫 소리·프레임·리플레이·텔레메트리 측정
node tools/hash-node.mjs --seconds 30 --seed 1     # 네이티브(cmd/render) == node == 브라우저 해시 게이트
node tools/hash-browser.mjs --browser webkit --seconds 30 --seed 1
go run ./cmd/jdsess                                # 5분 손 조작 로그의 URL 길이 게이트(≤ 2000자)
node tools/capture.mjs --out scratch/captures      # 방·기기 뷰 캡처(비전 판정 입력)
```

요구: Go 1.26, TinyGo 0.42, binaryen(선택), Node 24, Playwright(측정·캡처). 규칙과 실측 노트는 `CLAUDE.md`, 계획은 `docs/impl-plan-2026-09-05.md`, 기획서는 `docs/plan-2026-09-05-v2.html`.

## 라이선스

코드는 MIT(`LICENSE`). 이름·캐릭터·아트 자산과 제3자 자산 고지는 `ASSETS-LICENSE`.
