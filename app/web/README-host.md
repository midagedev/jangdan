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
| `{t:'tick', block, step, bar, flags, peak, ctxTime, applied, playing, levels}` | 4블록마다. flags는 4블록의 `jd_flags()` 누적 OR, peak는 최댓값, ctxTime은 `currentTime`. applied는 큐에서 적용한 누적 명령 수. `playing`은 `jd_playing()`의 0\|1(트랜스포트) — 필드가 없는 구 워클릿은 호스트가 정지로 해석한다. `levels`는 `Float32Array(8)` — 파트별 프리 FX 블록 피크의 4블록 max(engine.Part 순 BassA BassB BD SD CH OH CP CY, `jd_level` 원본 — 라인 LED·VU 미터). `jd_level` 내보출이 없는 구 워클릿은 필드를 싣지 않는다 → 호스트가 0 유지(입력 방어) |
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
- `jd.debugShadowState()` → `Uint8Array|null` — 섀도 엔진 상태 덤프(measure.mjs 섀도 일치 게이트용).
- `jd.hint(state)` — 첫 접촉 캡션 표시. `window.JD_CAPTIONS[state]`를 `#caption`에 넣고
  보이게; 등록 안 된 상태(0 포함)는 숨긴다(입력 방어 — 문구 소유자는 Go main, 상태 1·2·3).
- `jd.telemetryFlush()` — 텔레메트리 배치를 즉시 전송(반환 Promise\<status 문자열\>).
- `jd.shareSession(payload, seed, word)` → `Promise<url>` — 공유 세션 저장(§8). 쿨다운 중
  재호출은 POST 없이 마지막 URL을 그대로 resolve한다.
- `jd.sharedLog()` / `jd.sharedLogReady()` — `?s=` 열기 상태(§8). 페이로드 문자열과
  `-1 없음/실패 · 0 GET 진행 중 · 1 도착`(Go `sharedLogState`와 같은 값).

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

### 섀도 엔진 (워클릿 왕복 없이 동기 읽기)

UI 상태 읽기(`param`·`bassStep`·`drumStep`·`muted`·`slot`·`keyRoot`·`chord`·`mode`)는
**메인 스레드의 두 번째 engine.wasm 인스턴스**(렌더하지 않는 섀도)에서 한다. 페이지 로드 즉시
`compileStreaming`과 같은 모듈로 `WebAssembly.Instance(module, {})` → `_initialize()` →
`jd_init(seedFromWord(#seedbox))`를 돌리고:

- `cmd()`는 워클릿 발송과 함께 섀도에도 **즉시** 적용(`jd_cmd`). 섀도 초기화 전 도착한 cmd는
  `pendingShadow`에 쌓아 `initShadow` 직후 재생한다(초기화 경쟁으로 인한 적용 누락 봉쇄).
- 바 경계 대기값(SelectPattern·SetKey)은 **FLAG_BAR tick마다 `jd_sync()`** 로 확정 — 엔진의
  "다음 바에 적용"과 같은 의미. `jd_sync` 내보출이 없는 engine.wasm에서는 건너뛴다(입력 방어).
- **`shadowReset(seed)`이 리셋 경로의 단일 소유자**: `jd_reset(seed)` + 로그 전체 재적용(로그가
  정본). `start()`에서 노드 시드(시작 시점 `#seedbox` 값)와 섀도 시드가 다르면(시작 전에 시드를
  고쳤으면) 여기서 재동기화한다.
- **`replay:done` 직후** 워클릿에 `state:get`(id `shadow-sync`)을 보내 돌아온 바이트를 섀도에
  복사 + `jd_state_read` — 리플레이로 워클릿이 과거로 돌아간 시점이 정본이므로 섀도도 거기에
  맞춘다(리플레이 중의 불일치는 허용, 끝에서 맞춘다).
- 섀도 전·초기화 실패 폴백: `param`은 `DEFAULT_PARAMS` 전사의 12비트 양자화값
  (`quantN`/4095, engine `quantize`와 비트 동일 — 4096개 전수 검증됨), 나머지 읽기는 0.
  **`muted`는 0|1 number** — boolean을 돌려주면 Go `bridge_js.go`의 `intOf`가 패닉하는
  계열이라(2026-09-05 교훈) 종류 계약이다.
- Step/Bar·Peak는 워클릿 tick이 정본(섀도는 렌더가 없어 스텝이 안 나간다).

섀도 일치 게이트(measure.mjs): 오디오 시작 +30초에 워클릿 상태와 섀도 상태를 params
[2..68)·mute[678]·key[679]·mode[680..682)·playing[682]·chord[683..691) 영역에서 비교한다.
패턴 영역 [68..678)은 제외 — SelectPattern의 "다음 바 확정" 시차 때문에 스냅숏 위치에 따라
1슬롯 어긋날 수 있다(엔진 설계상 정상).

`tick()`의 flags는 호출 사이 누적 OR이고 읽으면 0으로 리셋한다(Go가 프레임당 1회 읽어도
94Hz 틱의 사건이 60Hz 프레임에서 새지 않게). levels도 같은 수명 — 틱마다 max 누적 후
`tick()`이 `tickOut.levels`(같은 `Float32Array` 재사용, 프레임당 할당 0)에 복사·리셋한다
(Go `Tick.Levels`로 흐른다). `__jdStats().levelPeaks`는 소모하지 않는 세션 누적 피크
(측정·게이트용).

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
| `shadow_error` | 1 | 섀도 엔진 인스턴스화/초기화 실패(폴백 값으로 동작) |
| `share_ok` | POST 본문 문자 수 | 공유 저장 성공(§8) |
| `share_failed` | HTTP 상태(0=네트워크/주소 없음, −1=응답 이상) | 공유 저장 실패 — 조용히 넘기지 않는다 |
| `open_failed` | 1 | `?s=` 열기 실패(404·손상·주소 없음) — 일반 세션으로 진행 |

전송: `visibilitychange(hidden)`·`pagehide`·60초마다 `navigator.sendBeacon(JD_REPORT_URL, …)`
(sendBeacon 실패/없음 → fetch keepalive — **헤더 없음**). **본문 종류는 `text/plain` Blob** —
`application/json` 본문은 교차 출처 preflight를 유발해 운영 Worker 경로 beacon을 늦춘다
(수신자는 본문을 JSON.parse한다, 종류 헤더는 안 본다). **URL 규칙**: 절대 URL(운영 Worker)에는
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

### 오디오 해제 제스처 (2026-09-06 iPhone 실측 뒤)

iOS Safari는 `touchstart`/`pointerdown`을 오디오 해제 제스처로 인정하지 않는다(`touchend`·`click`은 인정).
구 host.js는 `pointerdown`에서만 `start()`를 부르고 `await audio.resume()`을 걸었는데, iOS에서 그 Promise는
영원히 미결이라 첫 탭에서 굳고 이후 모든 탭이 `if (audio) return`으로 무시됐다(텔레메트리: first_tap·first_knob 기록,
`audioStarted=false`, `ticks=0`). 지금은 ① pointerdown/pointerup/touchend/click/keydown 전부에서 핸들러 안 동기
`resume()` ② `start()`는 resume을 기다리지 않고 노드를 만든다(suspended에서도 된다) ③ `statechange`로 running을
관측해 `audio_running` 이벤트, 2초 뒤에도 아니면 `audio_stuck`(값 = resume 호출 수). 리포트 stats에 `audioState`·
`resumeCalls`·`gestures`·`audioRunningMs`·`audioStuck`가 실린다. 게이트는 measure.mjs "호스트 검증 5b"(iOS 형태 에뮬레이션).

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
`jd.cmd(0,31,0,0,0,0.9,0)`(MASTER) 후 `jd.param(31)`이 `Math.fround(3686/4095)`(0.9001…)
±1ulp(param은 섀도 엔진의 f32 반환값 — fround의 이중 반올림과 마지막 비트가 어긋날 수 있다),
state:get 왕복으로 워클릿 파라미터 uint16(=3686)이 **정확히** 일치, `jd.replay(5)` → replayDone 1,
**섀도 일치**(위 §2 — 시작+30초, 6개 영역, 불일치 시 600ms 뒤 1회 재시도),
**시작 전 게이트**(캡션1 표시·`#tools`/`#overlay` display none·keyRoot/chord/mode/muted 종류),
**캡션2**(시작 후 4초 시점 문구)·**캡션3**(기기 뷰 첫 진입)·**캡션3 종료**(MASTER 드래그 뒤 hidden),
**사람 로그 게이트**(MASTER 드래그 → host 로그 author=0 증가 + `logLen === log().length`),
**공유 세션 게이트**(§8: id URL 형식·≤120자·무손실 왕복[fnv1a]·쿨다운 재호출·413 표면화·
새 페이지 열기 재생[sharedEntries·author=2·cmdsSent]·404 → open_failed + 일반 세션 —
계약↔단언 표는 measure.mjs 주석), 액티브 구간 hiddenFrames 0,
**파트별 레벨 게이트**(P3-levels: 시작 4초 뒤 `levelPeaks[2]`(BD)·`levelPeaks[0]`(BassA)
> 0.01, 8값 전부 유한·비음수 — 원본 engine.Level → jd_level → tick.levels → levelPeaks),
**할당 게이트**(액티브 구간 allocPerFrame ≤ 900B/frame — 기존 ≈850B 예산, 레벨 누적 배열
재사용으로 프레임당 할당 증가 없음),
텔레메트리가 app/results에 kind=telemetry로 저장(flush `sent(beacon)`/`sent`,
`telemetrySent ≥ 1`), console/pageerror 0건(의도적 실패 주입[413·404]이 브라우저 콘솔에
남기는 자원 오류는 예상 필드 `expected413Console`·`expected404Console`로 분리 계상).
공유 구간에서만 페이지의 `JD_REPORT_URL`에 실제 Worker 주소를 주입한다(레포 파일에는
주소를 박지 않는다 — 배포 시 report-config.js가 소유). 열기(p2·p3) 페이지는 접근자
주입으로 세션 GET 뒤 telemetry를 차단한다(measure.mjs p2 주석의 경합 원본 참조).
시드 타이핑(overlayKeys 증명)은 `?dev=1` 실행에서만
— 이 흐름은 dev 없이 도므로 `#overlay`가 숨겨져 있으면 건너뛴다.
결과 JSON: `app/results/host-<browser>.json`.

## 7. index.html 메모

gzip 로더(app.wasm.gz → DecompressionStream)·정지 화면(still.png)·오버레이(seed 토글·입력·
Send report)는 유지. `#seedtext`(z-index 10, pointer-events none, system-ui 14px,
rgba(232,226,210,0.7), 하단)가 `#seedbox` 값을 미러한다 — 캔버스 폰트는 ASCII라서 한글 시드
단어는 DOM이 담당. 스크립트 순서: report-config.js → host.js → wasm_exec.js + 로더
(host.js의 rAF 패치가 Go 초기화보다 먼저여야 한다).

첫 접촉 캡션: `<div id="caption" hidden>` + `window.JD_CAPTIONS` 스크립트(문구·스타일은 리드
소유, 상태 판정은 Go main의 `updateCaption` → `jd.hint(state)`). **`#tools`는 host.js 스크립트보다
앞에 있어야 한다** — share 버튼의 클릭 와이어링(비동기 저장·쿨다운·클립보드)을 host.js가 로드
시점에 소유한다(인라인 onclick이 아님). 개발 버튼(`#tools`)·시드
오버레이(`#overlay`)는 **`?dev=1`에서만** — host.js가 `body.dev`를 단다(CSS
`body:not(.dev)` 규칙). `#seedtext`는 사용자가 늘 보는 값이라 항상 표시. `body.clean`은
`#caption`도 함께 숨긴다. `<title>`은 "장단 / Jangdan".

## 8. 공유 세션 (§12.6 — URL에는 id만)

공유 URL은 `location.origin + location.pathname + '?s=' + <id 10자>` — 로그 페이로드·seed가
URL에 실리지 않는다(id-only 계약). 저장소는 리포트 Worker의 세션 라우트(cf/worker.js —
`POST {베이스}/sessions` → `{id}`, `GET {베이스}/sessions/<id>`, 공개·불변·1년 캐시).
**베이스의 단일 소유자는 `JD_REPORT_URL`**(report-config.js — 배포 시 deploy-pages.sh가 쓴다):
host.js는 `.../report` 접미를 뗀 `sessionsBase()`를 파생하고, 레포 파일에 Worker 주소를
박지 않는다. 로컬 serve.mjs(8444)도 오리진 허용 목록에 들어 있다.

**저장 흐름** — Go `jdShareURL()`(share_js.go)이 무손실 인코딩
`session.EncodeURL(log)`(감량 0단계 — `EncodeURLBudget` 사다리는 공유 경로에서 폐지)으로
페이로드를 만들고 `jd.shareSession(payload, seed, word)`을 기다린다:

```
[share 버튼](?dev=1 #tools) → jdShareURL() → EncodeURL(log)
  → jd.shareSession → (오디오 시작 뒤면) state:get으로 키프레임 상태 base64 동봉
  → POST {베이스}/sessions  본문 {v:2, seed, word, log, state, meta}  text/plain(단순 요청·preflight 없음)
  → {id} → '?s=<id>' URL → 클립보드(거부 시 prompt 폴백) · 버튼 "copied <id>"
  telemetry: 성공 share_ok(본문 문자 수) / 실패 share_failed(HTTP 상태; 0=네트워크·주소 없음)
  버튼: 실패 시 "share failed <상태>" — 실패를 조용히 넘기지 않는다
```

**쿨다운 10초**(`SHARE_COOLDOWN_MS`) — 무료 KV 쓰기 한도(1,000/일) 보호가 목적. 성공 직후부터
10초 동안 `shareSession` 재호출은 POST 없이 마지막 URL을 그대로 resolve하고, 버튼 클릭은
그 URL을 다시 복사한다(`copied <id> (cooldown)` 라벨로 직전 세션 id임을 표시). 진행 중 호출은
같은 Promise를 공유한다(중복 POST 방지). `stats.sharePosts`는 실제 POST 시도만 센다.

**열기 흐름** — `?s=`가 있으면 host.js 평가 시점(페이지 로드 즉시, app.wasm과 병렬)에 GET을
시작한다. 도착한 페이로드는 `jd.sharedLog()`로 Go가 프레임 폴링해 `session.DecodeURL`로
디코드, 엔트리를 블록 순서로 `Replay` 저자 Cmd로 재생한다(늦은 도착은 첫 엔트리를 현재
블록+2로 평행이동 — 호스트 cmd가 항상 now+2로 예약하기 때문). 재생이 남은 동안 로컬
레지던트는 쉬고(공유 로그에 만든 사람의 레지던트 궤적이 이미 있다), 끝나면 이어 튼다.
저장된 `word`는 `?seed=`가 없고 사용자가 시드 박스를 안 고쳤을 때만 채운다. 저장된 `state`
필드는 이 라운드의 열기에서 쓰지 않는다(리플레이 재생으로 충분 — state 즉시 적용은 후속).

```
[링크 열기] ?s=<id> → host.js 즉시 GET {베이스}/sessions/<id> → {log, word}
  → jd.sharedLog()/sharedLogReady() → Go 디코드(sharedEntries 계상) → 첫 제스처 뒤 재생
  실패(404·200인데 log가 v2.가 아님·주소 없음): open_failed 텔레메트리 + 경고 1줄 → 일반 세션
```

**입력 3클래스 방어**: ① 악의 입력(`?s=../..`·긴 문자열 등) — id 정규식 `^[0-9A-Za-z]{10}$`
불일치로 **조용히 무시**(콘솔 출력 없음). ② 손상(200인데 `log`가 `v2.` 페이로드가 아니거나
Go 디코드 실패) — `open_failed` + 일반 세션. ③ 구버전 `v1.` — 거부 경고 1줄 + 일반 세션.
인라인 `?s=v2.…` 페이로드(과거 URL 형식)는 당분간 병행 디코드한다.

**용량** — Worker 본문 상한 256KB(문자 수로 계량, 초과 시 413 → `share_failed`). 실측
(2026-09-06, chromium 60s 런): 114.8초 세션 = 로그 9,323자·1,349 엔트리(≈11.8 엔트리/초,
엔트리당 ≈6.9자) → POST 본문 10,334자. 30분 외삽 ≈ **146K자(143KB)** — 상한의 56%로,
30분 세션이 안쪽에 들어온다. 엔트리 발생률은 레지던트 활동·조작량에 비례해 오르므로
장시간 세션은 감량 사다리(EncodeURLBudget) 재도입이 후속 과제다.
