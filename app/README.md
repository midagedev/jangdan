# 장단 UI 스파이크 — Ebitengine(js/wasm) 기기 뷰

UI 층 예산 판정 스파이크. Ebitengine이 스프라이트를 배치·회전하고, 워클릿 엔진
(`spike/worklet` 재사용, 수정 없음)에 파라미터를 넘기며, 크기·프레임·rAF·할당을 계측한다.

##실행

```bash
bash app/build.sh                                   # wasm 빌드 + 사전압축 + still.png + 크기 출력
node app/serve.mjs                                  # https://localhost:8444 (자체서명 자동 생성)
node app/measure.mjs --browser chromium --seconds 30
node app/measure.mjs --browser chrome   --seconds 60 --shot app/results/chrome.png  # 헤디드(기록용)
node app/measure.mjs --browser webkit   --seconds 30
go run ./app/tools/placeholders                     # 플레이스홀더 7장(있으면 skip, -force로만 덮음)
```

measure.mjs는 8444가 비어 있으면 serve.mjs를 직접 구동하고 끝날 때 정리한다.
시퀀스: 로드 → 노브 0 드래그(세로 -100px = 값 +0.5, 첫 pointerdown이 오디오 제스처) →
시드 오버레이 타이핑 → markFrames → N초 정상 구간 → 배경 탭 20초 → 캡처·회수.

## 계약↔측정 표

측정: 2026-09-05, darwin/arm64(이 머신 디스플레이 120Hz), headed Chrome 152 /
headless Chromium 153 / headless WebKit. 정상 구간 스냅샷(배경 20초 제외).
게이트 UI 층분: wasm gzip ≤ 3.0MB · brotli ≤ 2.5MB · 헤디드 Chrome frameMs P95 ≤ 4ms ·
rAF 최빈 16~17ms ±2ms ≥ 95% · 할당 ≤ 2KB/프레임.

| 계약 | 결과 JSON 키 | 실측(chrome 헤디드) | 판정 |
|---|---|---|---|
| wasm gzip ≤ 3.0MB | `wasmGzipBytes` | 2,585,779 (2.47MB) | **PASS** |
| wasm brotli ≤ 2.5MB | `wasmBrotliBytes` | 2,133,906 (2.04MB) | **PASS** |
| frameMs P95 ≤ 4ms (540×960@2×) | `frameMsP95` | 1.50ms (p50 0.40 / max 3.9, n=7201) | **PASS** |
| rAF 최빈 16~17ms ±2ms ≥ 95% | `rafModeMs`, `rafPctWithin2ms` | mode 8ms(120Hz 디스플레이), ±2ms 100.0% | **판정 유보** — 게이트 수치는 60Hz 가정. 120Hz 실측이고 vsync 고정(최빈 8.3ms 이론치 일치, max 10ms)으로 위험 신호 없음. 60Hz 머신에서 재측정 필요 |
| 힙 할당 ≤ 2KB/프레임 | `allocPerFrame` | 1.28KB (1,309B) | **PASS** |
| 첫 화면(정지 이미지) ≤ 2s @4G | `tFirstStill` | 24ms(localhost, 4G 미측정) | **localhost 참고치** — still.png 3.5KB라 전송 지배 아님; 4G 추정은 `wasmBrotliBytes` 2.04MB가 지배 |
| 전체 로딩 ≤ 6s @4G | `tFirstFrame` | 269ms(localhost) | **localhost 참고치** — brotli 2.04MB + 컴파일 10.5MB |
| 백그라운드 탭 렌더 0 | `bgFrames`, `hiddenFrames` | playwright 탭 전환은 `document.hidden`을 만들지 않음(bgFrames+2408, 렌더 계속) | **미검증** — 아래 iPhone 절차·수동 탭 전환 필요. 카운터는 내장 |
| 오디오 지속(백그라운드) | `audioStarted`, `contextSampleRate` | 시작됨, 48000 | 탭 숨김 상태 지속은 미검증(위와 같음) |

참고(headless): chromium frameMs p95 0.50ms·rAF mode 33ms(≈30fps, SwiftShader 소프트 vsync) /
webkit p95 2.0ms·rAF mode 17ms ±2ms 91.7%·max 115ms. headless 수치는 참고, 기록은 헤디드.

## 구조

- `layout.go` — 좌표의 단일 소유자(논리 540×960). 다른 파일에 좌표 상수 없음.
- `main.go` — 게임 본체. 브라우저·데스크톱 양쪽에서 빌드된다(`go vet ./app/...` 통과용).
- `bridge_js.go` / `bridge_desktop.go` — js 의존 격리. `window.jdBridge` 접점만.
- `assets/` — `go:embed` 스프라이트(플레이스홀더 → fal.ai 생성물로 같은 파일명 교체).
- `tools/placeholders` — 회색 단색 도형 생성기(룩 발명 금지, `-force`로만 덮음).
- `web/index.html` — still 이미지(z0) < ebiten canvas(z1) < DOM 오버레이(z10).
- `web/bridge.js` — AudioContext(첫 pointerdown)·워클릿 로드·파라미터 큐잉·스코프
  AnalyserNode·계측(`window.__jdStats()`).
- `build.sh` — 표준 Go wasm 빌드 + `wasm_exec.js` 복사(GOROOT/lib/wasm) + 워클릿
  산출물 복사(수정 없음) + gzip/brotli + still.png.

## 알게 된 사실 (Ebitengine v2.9.11 js/wasm 실측)

- **캔버스를 자체 생성**해 body 끝에 붙이고 inline으로 `width/height: 100%`를 건다.
  위치·z-index만 페이지 CSS로 잡을 수 있다. 백킹 크기 = `body.clientWidth ×
  devicePixelRatio` → 뷰포트 540×960 + DPR 2 = 1080×1920 자동.
- **`window.requestAnimationFrame`을 패키지 초기화 시점에 포획**한다 — rAF 계측
  패치는 wasm 실행 전에(bridge.js를 먼저) 로드해야 한다.
- 키·마우스·터치 리스너는 **캔버스에만** 단다 → DOM 오버레이 `<input>`은 키보드를
  그대로 받는다(overlayKeys 7 = "jangdan").
- mousedown/touchstart에 `preventDefault`는 하지만 전파는 막지 않는다 → window
  캡처 리스너로 pointerdown 제스처를 받아 AudioContext를 시작할 수 있다.
- `ebiten/v2/audio`를 import하지 않으면 ebiten이 만드는 AudioContext는 없다
  (모듈 전역 grep에서 AudioContext 0건) — 우리 컨텍스트와 충돌 없음.
- **TPS 기본 60 — 120Hz 디스플레이에서 Update가 프레임을 건너뛴다.** frameMs
  (Update 시작~Draw 끝)를 재려면 `ebiten.SetTPS(ebiten.SyncWithFPS)`가 필요.
  끄면 P95 11.1ms, 켜면 1.5ms(같은 바이너리, 측정 정의 차이).
- 할당 2.56KB→1.28KB/프레임: 프레임당 `&DrawImageOptions{}` 45개를 공유 1개로
  (DrawImage는 op를 호출 시점에 동기 읽음). 잔여 1.28KB의 출처 후보: syscall/js
  브리지 Call 3회/프레임, `vector.StrokePath` 내부 스트로크 경로, ebiten 드로우 배치.
- **Chrome은 프로그램 mousemove를 코얼레싱한다** — 계측 드래그는 프레임당 한 칸씩
  (17ms 간격) 보내야 마지막 위치가 반영된다. 한 번에 steps:10으로 보내면 꼬리가 유실.
- headless Chromium의 rAF는 ~30fps(SwiftShader), headless WebKit은 ~60fps지만
  ±2ms 91.7%·가끔 115ms 히컵. vsync 품질 판정은 헤디드에서만 의미 있다.
- 사전압축 서빙에서 `Content-Length`는 **압축된 바이트 길이**여야 한다 — 원본
  크기를 쓰면 브라우저가 남은 바이트를 기다리다 로딩이 막힌다.

## iPhone / Safari 절차 (이 라운드 미측정)

```bash
node app/serve.mjs        # https://<LAN IP>:8444 출력
# iPhone 같은 Wi-Fi → Safari로 접속, 자체서명 경고 통과(spike/worklet/README 참조)
# [seed] 버튼 → 입력 확인(키보드) → 노브 드래그(오디오 시작) → 백그라운드 전환 관찰
xcrun simctl boot "iPhone 17 Pro" ; open -a Simulator
xcrun simctl openurl booted https://localhost:8444/
# 백그라운드 렌더 0 확인: 페이지를 둔 채 홈으로 나갔다 돌아오면
# window.__jdStats().hiddenFrames / bgFrames 대조(수동 — playwright로는 document.hidden 불가)
```

## 못 한 것 / 남은 것

- 60Hz 머신에서의 rAF 게이트 재측정(이 머신은 120Hz — 위 표).
- 백그라운드 탭 렌더 0·오디오 지속의 실측(playwright 한계, 수동 절차는 위 참조).
- 4G 전송 시간 실측(localhost만 측정 — 크기 계약은 PASS).
- 패드 탭 → 120ms lit은 기계 검증 없음(스텝 lit과 같은 스프라이트 스왑 경로).
  노브 드래그·스텝 시계·오버레이 키보드는 계측으로 검증됨.
- 파형은 AnalyserNode 출력(엔진 출력과 위상 무관) — 오디오 시작 전에는 빈 화면.
- 다음 엔진 라운드 요청: **`Trigger(voice)`** — 패드가 엔진 성격을 직접 때리려면 필요
  (현재 엔진 API는 New/SetParam/Param/Render뿐, 보이스 트리거는 비공개).

## 독립 비전 판정 (opus, 2026-09-05) — 구현 라운드 자기판정과 대조

- 캡처 `results/chrome.png`(1080×1920, 배율 2). ① 배치 SHIP: 노브 12·패드 12·스텝 16·스코프 프레임이 계약 좌표와 0.25px 이내. ② 회전 SHIP: 노브 0 포인터 135.0°, 노브 1 359.2°(12시). ③ DOM SHIP: "jangdan" 입력과 포커스 링 온전. 종합 SHIP. 자기판정(SHIP/SHIP/SHIP)과 3/3 일치.
- 판정자 관찰 두 가지(다음 라운드 과제): (a) 실제 칠해진 폭이 선언 표시 크기보다 작다 — 노브 126/128, 패드 124/144, 스텝 64/80(플레이스홀더 안쪽 여백). 실제 아트로 교체할 때 표시 크기 계약을 다시 잰다. (b) 오버레이(논리 y 7..30)와 스코프 프레임(y≥82)이 겹치지 않아 z-order는 이 캡처가 시험하지 않았다 — 오버레이를 그려진 요소 위에 걸치는 프레이밍 한 컷이 필요하다.
