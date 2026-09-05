# 호스트 층 계약 — host.js · processor.js

계약 원본: `docs/impl-plan-2026-09-05.md` §3·§4. 이 문서는 구현 세부·측정법까지 포함한다(리드가
docs/로 옮긴다). 소비자: `app/core/bridge_js.go`(window.jd API 표 — 파일 상단 주석), `app/measure.mjs`,
`tools/hash-*.mjs`.

## 1. 프로토콜 (main → worklet / worklet → main)

워클릿 프로세서 이름 `jd`(`registerProcessor('jd', …)`). 노드 생성:

```js
new AudioWorkletNode(ctx, 'jd', {
  numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2],
  processorOptions: { module, seed, collect, script },
});
```

- `module` — `WebAssembly.Module`(engine.wasm). host.js는 페이지 로드 즉시
  `WebAssembly.compileStreaming(fetch('engine.wasm'))`을 시작해 둔다(제스처 전 — 첫 탭→첫 소리
  300ms 예산). 프로세서는 인스턴스화 후 `_initialize()` 1회 → `jd_init(seed)`.
- `seed` — uint32. `collect` — 통계 수집(계측 겸용 페이지에서만 true; 해시 경로는 항상 false).
- `script` — **오프라인 해시 전용**: `[{block,k,a,b,c,d,v}]`을 생성 시점에 명령 큐에 선주입한다.
  오프라인 렌더에서 포트 메시지 전달 시점은 보장되지 않으므로, 결정적 예약은 이 경로로만.

| main → worklet | 뜻 |
|---|---|
| `{t:'cmd', at, k,a,b,c,d,v}` | `at`(블록 인덱스, 0=즉시)에 적용할 명령. 큐는 at 오름차순, 같은 at는 도착 순. `process()`마다 렌더 **전에** `jd_block() >= at`인 것을 순서대로 `jd_cmd` |
| `{t:'reset', seed}` | `jd_reset(seed)` + 대기 명령 전량 폐기 + 틱 누적기 초기화 |
| `{t:'state:get', id}` | `jd_state_write()` 후 `{t:'state', id, bytes(Uint8Array 복사), block}` |
| `{t:'state:set', bytes}` | state 버퍼에 복사 후 `jd_state_read(len)` → `{t:'state:ack', ok}` |
| `{t:'replay', state, entries, blocks}` | 리플레이(§4) |

| worklet → main | 뜻 |
|---|---|
| `{t:'tick', block, step, bar, flags, peak, ctxTime, applied}` | 4블록마다. flags는 4블록의 `jd_flags()` 누적 OR, peak는 최댓값, ctxTime은 `currentTime`. applied는 큐에서 적용한 누적 명령 수 |
| `{t:'state', id, bytes, block}` | state:get 응답 |
| `{t:'state:ack', ok}` | state:set 응답 |
| `{t:'replay:done'}` | 리플레이 렌더 완료 |
| `{t:'stats', …}` | collect:true일 때 ≈1초마다(gap/스톨 히스토그램 — 스파이크 계측 스키마 유지) |

## 2. window.jd API

`app/core/bridge_js.go` 상단 표가 계약이다(전부 구현됨). 표에 없는 추가(측정·계측 전용):

- `jd.log()` — 로그 배열 `{block, author, k, a, b, c, d, v}` 그대로.
- `jd.replaying()` — 리플레이 요청(카운트다운 포함)부터 done까지 true.
- `jd.markFrames()` — 프레임 계측 구간 리셋(measure.mjs용).
- `jd.debugStateGet()` → `Promise<{bytes, block}>` — state:get 왕복(measure.mjs 검증용).
- `jd.telemetryFlush()` — 텔레메트리 배치를 즉시 전송(반환 Promise\<status 문자열\>).

`author`: 0 Human · 1 Resident · 2 Replay · 3 System(`app/core/core.go` Author와 같은 값).
CmdKind: 0 SetParam · 1 BassStep · 2 DrumStep · 3 SelectPattern · 4 Mute · 5 Trigger · 6 Drop ·
7 ResetPos(`engine/cmd.go`).

### 시드 규칙 (session/seed.go와 같은 규칙)

`seedFromWord(s)`: 코드포인트 단위 **소문자화 + 공백/제어 문자 제거**(NFC 정규화 없음) → 정규화 결과가
비면 `0x9E3779B9`, 아니면 UTF-8 바이트의 FNV-1a 32(오프셋 2166136261, 프라임 16777619), 결과가
0이면 1. JS와 Go의 차이는 소문자화 전체 매핑(예: 'İ'→'i̇' 2코드포인트)과 U+FEFF(JS \s는 공백,
Go는 유지)뿐 — 한글·ASCII는 동일. 시드는 `start()` 시점의 `#seedbox` 값으로 확정되고, 이후 입력
변경은 새 세션(reset)없이는 엔진에 반영되지 않는다. URL `?seed=단어`는 박스에 미리 채워 넣고
텔레메트리 `seed_open`을 남긴다(URL 로그 재생은 session 라운드 소유).

### 미러 (워클릿 왕복 없이 동기 읽기)

`params[33]`(12비트 양자화 — `quantN(v) = Math.fround(Math.fround(Math.fround(clamp01(v))*4095)+0.5)|0`,
값은 `Math.fround(n/4095)`; engine `quantize`와 비트 동일 — 4096개 전수 검증됨), 베이스 패턴
`[2파트][8슬롯][16스텝]`(BassStep은 **현재 슬롯**에 기록 — 엔진 Apply와 같다), 드럼 `[6][16]`,
뮤트 비트, 슬롯. `SelectPattern`은 다음 바 경계 tick(FlagBar)에 확정 — 엔진의 "다음 바에 적용"과
같은 의미로의 단순화. **초기값은 `engine/params.go`의 `DefaultParams()` 전사(`DEFAULT_PARAMS`
배열) — 값 변경은 양쪽 함께.** 리플레이는 키프레임 + 로그 재생으로 같은 제어 상태를 재현하므로
미러를 별도로 되돌리지 않는다(로그가 정본의 귀결).

`tick()`의 flags는 호출 사이 누적 OR이고 읽으면 0으로 리셋한다(Go가 프레임당 1회 읽어도
94Hz 틱의 사건이 60Hz 프레임에서 새지 않게).

## 3. 텔레메트리 이벤트 표

| 이벤트 | 값 | 시점 |
|---|---|---|
| `first_tap` | 페이지 로드부터 ms | 첫 pointerdown |
| `first_sound_ms` | start() 호출부터 ms | 첫 tick의 peak > 0.001 (목표 ≤ 300) |
| `first_knob_ms` | 페이지 로드부터 ms | 첫 Human SetParam |
| `exit_60s` | 1 | 60초 안에 pagehide |
| `drop` | 1 | Drop cmd |
| `replay_used` | 1 | replay:done |
| `replay_unavailable` | 1 | 키프레임 없음/이미 재생 중 |
| `seed_open` | 1 | URL에 seed 파라미터 |
| `worklet_error` | 1 | processorerror/시작 실패 |

전송: `visibilitychange(hidden)`·`pagehide`·60초마다 `navigator.sendBeacon(JD_REPORT_URL, …)`
(sendBeacon 실패/없음 → fetch keepalive). **URL 규칙**: 절대 URL(운영 Worker)에는
`?kind=telemetry`를 붙인다(cf/worker.js가 질의에서 kind를 읽는다). 상대 URL(로컬
serve.mjs)에는 붙이지 않는다 — serve.mjs가 `POST /report`를 경로 정확 일치로 처리해
질의가 붙으면 404가 된다(이 라운드에서 발견한 스펙 전제 오류, 보고됨). kind는 어차피
본문(`kind:'telemetry'`)에도 있다. 페이로드 `{sessionId(12자), ua, platform, dpr,
startedAt, events(≤400, 초과 시 오래된 것 버림), stats 요약}` ≤ 32KB. `JD_REPORT_URL`이
빈 문자열이면 전송하지 않는다. 로컬 확인: 페이지에서 `window.JD_REPORT_URL='report'`로
주입하고 `jd.telemetryFlush()` → serve.mjs POST /report → `app/results/`에 저장.

`window.__jdStats()`는 스파이크 bridge.js 필드를 유지하고
`firstSoundMs, ticks, cmdsSent, logLen, keyframes, replayDone, telemetryQueued, telemetrySent`를
더한다.

## 4. 로그·키프레임·리플레이

- 로그: `cmd()`가 `(block=at, author, cmd)`로 push. 오디오 시작 전 cmd는 pending에 쌓아 두고
  start 직후 at=0으로 발송(로그 블록 0). 라이브 cmd는 항상 `at = round(blockNow()) + 2` —
  `blockNow() = lastTick.block + (audio.currentTime − lastTick.ctxTime)·375`(선형 추정).
- 키프레임: **bar가 16의 배수로 바뀐 tick**(FlagBar 포함)에 `state:get`(id 'kf') →
  `keyframes.push({block, bytes})`. **bar 0(재생 시작 tick)도 포함**한다 — 세션 초반에도
  리플레이가 가능해야 하고, 16바(≈30s @130bpm)보다 짧은 세션에서 replay가 항상
  replay_unavailable이 되는 것을 막는다. 리플레이 중(카운트다운 포함)에는 요청을 쉰다.
- `replay(seconds)`: `T = round(blockNow()) − seconds·375`, `T` 이하 마지막 키프레임 K 선택
  (없으면 `replay_unavailable`, false). 로그에서 `K.block < block ≤ now` 엔트리를 골라
  `{t:'replay', state: K.bytes, entries, blocks: now−K.block}`을 **3초 뒤(세 번째 초)에** 전송 —
  3·2·1 카운트다운은 UI가 아니라 호스트가 소유한다. 워클릿은 상태 복원 + `jd_cmd(ResetPos)` +
  entries를 상대 블록(첫 엔트리 기준 0)에 예약해 blocks만큼 렌더한 뒤 `replay:done`.
- 리플레이 중 도착한 라이브 cmd는 큐에 남아 **종료 후 즉시 적용**된다(경과한 at의 재해석 보정
  없음 — 단순화). 엔진의 블록 카운터는 리플레이 중에도 단조 증가하므로 `blockNow()`·틱·이후
  예약은 그대로 유효하다.

## 5. 해시 게이트 (native == node == chromium == webkit)

공통: 블록마다 **인터리브 스테레오 float32 리틀엔디언 1024바이트**(16bit PCM 아님)를 SHA-256.
`cmd/render/main.go`가 무엇을 해시하는지에 맞췄다.

```sh
go run ./cmd/render -seconds 30 -seed 1 -json          # 네이티브
node tools/hash-node.mjs --seconds 30 --seed 1          # node + app/web/engine.wasm
node tools/hash-browser.mjs --browser chromium --seconds 30 --seed 1
node tools/hash-browser.mjs --browser webkit   --seconds 30 --seed 1
```

스크립트 명령 예약 게이트(절대 블록, 해당 블록 렌더 직전 적용 — processor와 같은 규칙;
`cmd/render`는 스크립트 미지원이라 제외):

```json
[{"block":400,"k":0,"a":1,"b":0,"c":0,"d":0,"v":0.9},
 {"block":1200,"k":6,"a":0,"b":0,"c":0,"d":0,"v":0},
 {"block":2000,"k":4,"a":2,"b":1,"c":0,"d":0,"v":0}]
```

node/hash-browser 공용 `--script <json>`로 비교한다.

## 6. 측정 (app/measure.mjs)

```sh
node app/measure.mjs --browser chromium --seconds 20   # webkit도 같은 인수로 1회
```

확인 항목: firstSoundMs(≤ 300 목표), tick 진행(20초 안 step 16값 전부 관측),
`jd.cmd(0,1,0,0,0,0.9,0)` 후 `jd.param(1) === Math.fround(3686/4095)`(0.9001…),
state:get 왕복으로 워클릿 파라미터 uint16(=3686)이 미러와 일치, `jd.replay(5)` → replayDone 1,
액티브 구간 hiddenFrames 0, 텔레메트리가 app/results에 kind=telemetry로 저장.
결과 JSON: `app/results/host-<browser>.json`.

## 7. index.html 메모

gzip 로더(app.wasm.gz → DecompressionStream)·정지 화면(still.png)·오버레이(seed 토글·입력·
Send report)는 유지. `#seedtext`(z-index 10, pointer-events none, system-ui 14px,
rgba(232,226,210,0.7), 하단)가 `#seedbox` 값을 미러한다 — 캔버스 폰트는 ASCII라서 한글 시드
단어는 DOM이 담당. 스크립트 순서: report-config.js → host.js → wasm_exec.js + 로더
(host.js의 rAF 패치가 Go 초기화보다 먼저여야 한다).
