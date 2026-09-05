# 장단 스파이크 — TinyGo wasm in AudioWorklet

판정 스파이크: 순수 Go 엔진(`engine/`)을 TinyGo `wasm-unknown`으로 컴파일해
AudioWorklet 스레드 안에서 돌리고, ① 128프레임 콜백 점유율 ② 오프라인 300초
렌더의 네이티브 해시 일치 ③ 브라우저 간 해시 일치를 잰다.

## 실행 절차

```bash
# 1. 빌드 (레포 루트에서)
bash spike/worklet/build.sh
# → spike/worklet/public/engine.wasm (TinyGo -gc=leaking -no-debug -opt=2 → wasm-opt -O3)

# 2. node 단독 검증 (wasm vs native)
go run ./cmd/render -seed 1 -seconds 300 -json
node spike/worklet/hash-node.mjs --seconds 300 --seed 1
# 두 sha256이 같아야 한다.

# 3. 계측 (playwright — spike/worklet/에서 npm i -D playwright 필요)
cd spike/worklet && npm i -D playwright && npx playwright install chromium webkit
node measure.mjs --browser chromium --seconds 60 --mult 1      # 실시간 점유율
node measure.mjs --browser chromium --offline 300              # 오프라인 해시
node measure.mjs --browser chrome   --seconds 60 --mult 1      # 헤디드 Chrome(기록용)

# 4a. iOS 시뮬레이터 (기능·해시 확인용 — 성능 수치는 Mac CPU라 참고치)
xcrun simctl boot "iPhone 17 Pro" ; open -a Simulator
xcrun simctl openurl booted https://localhost:8443/   # 시뮬레이터는 Mac의 localhost 공유
# 자체서명 경고 → '자세히 보기' → '웹사이트 방문' → [Start] → [Send report]

# 4b. iPhone 실기 수동 측정 (성능 게이트는 여기서만 의미)
node serve.mjs   # https://<LAN IP>:8443 출력 — .cert/ 자동 생성
# - iPhone 같은 Wi-Fi → Safari로 https://192.168.x.x:8443 접속
# - 자체서명 경고: Safari "웹사이트 설정" → 이 사이트에 대해... 실제로는
#   경고 화면에서 하단 '자세히 보기' → '웹사이트 방문'(iOS 17+ 표현은 버전별 상이).
# - [Start] → 5분간 재생(stalls 관찰) → [Send report]
# - iPhone은 Safari만 쓸 것(Chrome 앱도 WebKit이지만 워클릿 디버깅이 어렵다).
```

## 결과 (2026-09-05, macOS darwin/arm64 — M계열, 헤드리스는 참고치)

**헤드리스 수치는 참고, 기록은 헤디드 Chrome·Safari·iPhone.**
측정 머신: 이 스파이크 라운드 단독(백그라운드 계측 없음, serve.mjs 정지 상태).

### 해시 일치 표 (seed 1, 엔진 출력 float32 LE SHA-256)

| 렌더러 | 30s | 300s |
|---|---|---|
| 네이티브 `cmd/render` | `be9b194dd35b44b4f2abec511ff99ffc1e507ebdfc19ae48efab08e39756a239` | `87c225b8ed3324617a2163922ced0cbe7df7739f5ed021dea45c6ddc3b766b91` |
| node + engine.wasm | `be9b194d…` **일치** | `87c225b8…` **일치** |
| Chromium 오프라인 | `be9b194d…` **일치** | `87c225b8…` **일치** |
| WebKit 오프라인 | `be9b194d…` **일치(Chromium과도 일치)** | (미측정 — iPhone Safari에서) |

오프라인 300s 렌더에 걸린 실시간: 약 0.8s(Chromium headless) — 약 375배속.
wasm: raw 4278 bytes / gzip 1989 bytes(wasm-opt -O3 적분, 최적화 전 4440 bytes —
최적화 전후 렌더 해시 동일로 의미 보존 실증).

### 실시간 점유율 (60s, 블록 예산 2666.7µs = 100%)

| 브라우저 | mult | renderUsMean | renderUsMax | loadPctMean | stalls(>8ms) |
|---|---|---|---|---|---|
| chromium(headless) | 1 | 18.1 | 1000.0* | 0.68% | 0 |
| chromium(headless) | 4 | 76.5 | 2000.0* | 2.87% | 0 |
| chromium(headless) | 16 | 303.8 | 1000.0* | 11.39% | 0 |
| webkit(headless) | 1 | 18.4 | 1000.0* | 0.69% | 1 |
| chrome(**헤디드**) | 1 | 16.3 | 1000.0* | 0.61% | 1 |
| **iPhone 실기** Safari 26.6.1 (Pages 경유, 30s) | 1 | 12.1 | 1000.0* | **0.45%** | **0** (원시 >8ms도 0, 최빈 간격 3ms = 블록당 콜백, max 3ms) |
| iOS 시뮬레이터 Safari 26.2 (36s) | 1 | 10.1 | 1000.0* | 0.38% | 원시 3372 = 4블록 버스트 오판(시뮬레이터 고유), 버스트 인식 판정으로 0 |
| Safari(macOS) | — | 수동(위 절차) | | | |

\* 워클릿에 `performance`가 없어 `Date.now()` 폴백(1ms 양자화) — max는
1000µs 단위로 양자화된 값이므로 **참고용**. mean은 충분한 표본 수에서
평균이라 대체로 신뢰. mult 스케일링은 선형(1→4→16: 18→77→304µs)으로
부하 승수가 정상 작동함을 보여준다.
계획 게이트(데스크톱 ≤20%, iPhone ≤50%)는 mult=1 기준 0.61~0.69%로
여유. 네이티브(gc) 블록당 mean 6.5µs, node+wasm 4.4µs.

## 알게 된 브라우저/툴체인 사실 (2026-09-05 실측)

- **FMA 융합.** 라운드 중 objdump에서 drums.go HPF·engine.go 소프트클립 두 곳에
  `FMADDS`가 남아 첫 불일치 샘플 11104에서 1~2ulp 발산했다. 해법으로 곱을 float64로
  계산해 float32로 반올림하는 `engine.mul32`를 전면 적용하자 native/wasm/브라우저 해시가
  전부 일치했다. **정정(리드 실측 2026-09-05):** 라운드 보고는 "`float32(x*y)+z`의
  명시 변환은 gc가 지운다"고 썼지만, 곱만 정확히 감싼 `float32(x*y)+z`는 두 곱·뺄셈·
  상수 곱·변수 경유·유리식·HPF·루프 전 형태에서 FMULS+FADDS로 컴파일된다(Go 스펙대로).
  융합되는 것은 `float32(x)*y+z`, `float32(x*y+z)` 처럼 감싼 위치가 틀린 형태이며, 원래
  두 지점도 그 부류였을 가능성이 높다. mul32는 실수를 grep으로 잡기 쉬운 형태라 유지하고,
  최종 게이트는 `tools/check-fma.sh`(엔진 네이티브 빌드 objdump에 FMADD/FMSUB 0개)다.
- **iOS 시뮬레이터 Safari(26.2)는 하드웨어 콜백 1회에 128프레임 블록 4개를 몰아 렌더한다**
  — 실기 iPhone(Safari 26.6.1)은 블록당 콜백(최빈 간격 3ms)이라 버스트는 시뮬레이터 고유 동작이다(2026-09-05 실측: 36초 13500블록에서 원시 ">8ms 간격" 카운트가
  3372 = 블록/4.0). 그래서 "연속 process() 간격 > 8ms"라는 원시 스톨 프록시는 iOS에서
  버스트 주기마다 1회 찍혀 언더런이 아닌 것을 스톨로 센다. 판정을 바꿨다: 워클릿이
  간격 히스토그램(정수 ms)을 보내고, 메인이 최빈 비영 간격을 버스트 주기로 보아
  `max(8ms, 최빈×2.5)`를 넘는 간격만 `stalls`로 센다. 원시 카운트는 `stalls8ms`로 남긴다.
  Chrome(1블록 콜백)에서는 종전 기준과 같다. 결과 JSON에 `gapModeMs`·`burstBlocksEst`·
  `stallThresholdMs`·`maxGapMs`가 추가됐다.
- **AudioWorkletGlobalScope에 `performance`가 없다**(Chromium 152 headless·
  헤디드 Chrome·WebKit 모두 `typeof performance === 'undefined'` → Date 폴백,
  `timerSource:'Date'`로 결과에 기록). 블록당 µs 정밀 측정은 이 스파이크의
  폴백(누적 평균)이 사실상 한계. 정밀 측정이 필요하면 별도 계측(예: wasm 내
  카운터)이 후속 과제.
- **`ctx.renderCapacity`는 이 환경에서 부재**(Chromium headless·헤디드 Chrome
  152 모두 `undefined`) — 결과 JSON에 `null`로 기록됨. AudioRenderCapacity가
  보이는 빌드에서는 자동으로 수집된다.
- **wasm-unknown 모듈은 import 0개**, export에 `memory`·`_initialize`·
  `//export` 함수. JS측 순서: `new WebAssembly.Instance(module, {})` →
  **`_initialize()` 1회** → `jd_init(seed)`. `_initialize` 생략 시 전역 초기화
  없이 호출돼 동작이 보장되지 않는다.
- **OfflineAudioContext + AudioWorklet**은 Chromium·WebKit 모두 정상 동작하고
  48k 고정 샘플레이트에서 네이티브와 바이트 단위 일치. 오프라인 렌더는
  실시간의 수백 배 속도(300s → 0.8s).
- AudioContext({sampleRate:48000})는 세 브라우저 모두 48000을 그대로 줬다
  (`contextSampleRate: 48000`). 하드웨어가 다른 기기(iPhone 포함)에서는
  경고가 뜰 수 있고 JSON에 기록된다.
- **비복사 Buffer 뷰를 비동기 싱크에 넘기면 조용히 오염된다**: wasm 메모리
  뷰를 `WriteStream.write`에 그대로 넘겼더니 플러시 시점에 다음 블록 내용으로
  덮어 써졌다(덤프 도구 디버깅 중 발견). `createHash.update`는 동기 복사라
  무관. wasm 메모리를 I/O로 내보낼 때는 반드시 복사(`Buffer.from(view)`).
- 엔진 비용(스탠드인 기준): wasm 4.4µs/블록(mult 1) → 예산 533µs(데스크톱
  20%)의 0.8%. TinyGo 바이너리 gzip 2KB.

## 못 한 것 / 남은 것

- **Safari(머신)·iPhone 실측** — 이 라운드 범위 밖(리드·사용자 수동).
  webkit(headless)은 참고치로 측정했고 오프라인 해시 일치는 확인했다.
- 5분 언디런 관찰(게이트) — 60s×4로 갈음. iPhone에서 [Send report]로 수집.
- iOS에서 `Date.now` 폴백의 정밀도(iPhone Safari 워클릿 타이머 실측).
- 폴리BLEP 톱니·슬라이드·정확한 303/808/909 모델 — 후속 라운드(스탠드인 명시).
- 파일: `dump-native/`, `dump-wasm.mjs`은 해시 디버깅용 상설 도구(첫 불일치
  샘플 탐색). 재사용: 양쪽 덤프 → python 비교.

## 원격 리포트 (2026-09-05)

Pages(https://midagedev.github.io/jangdan/worklet/)에서 Send report → Worker `jangdan-reports`(KV) → `bash tools/reports-pull.sh`로 `results/remote/`에 회수.
iPhone 실기 첫 리포트 `20260905T133211Z-iphone-*.json`: contextSampleRate 48000, baseLatency 0, renderUsMean 12.1µs, loadPctMean 0.45%(게이트 ≤50%), stalls 0 / 30s.
남은 게이트: 5분 언더런 0(실기 5분 측정 1회).
