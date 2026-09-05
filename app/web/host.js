// host.js — 장단 호스트 층. AudioWorklet 프로세서(app/web/processor.js)를 만들고
// window.jd API(계약 표: app/core/bridge_js.go 파일 상단 주석)를 노출한다.
// 로그가 정본: 엔진에 닿는 모든 Cmd는 (block, author, cmd)로 기록되고 리플레이는
// 이 로그를 재생한다(docs/impl-plan-2026-09-05.md §1·§4). UI 상태 읽기는 메인
// 스레드 섀도 엔진(렌더하지 않는 두 번째 인스턴스)이 담당한다. 상세: app/web/README-host.md.
(() => {
  'use strict';

  // ==== 엔진 상수 전사(engine/params.go·cmd.go와 동기 유지 — README-host.md) ====
  const NUM_PARAMS = 33;
  const PARAM_STEPS = 4095;
  const BLOCKS_PER_SEC = 48000 / 128; // 375
  const FLAG_BAR = 1 << 8;
  const FLAG_DROP = 1 << 9;
  // CmdKind: 0 SetParam 1 BassStep 2 DrumStep 3 SelectPattern 4 Mute 5 Trigger 6 Drop 7 ResetPos
  //          8 SetKey 9 SetChord 10 BassMode 11 Transport

  // DefaultParams — engine/params.go의 DefaultParams() 전사. 값 변경은 양쪽 함께.
  const DEFAULT_PARAMS = [
    0.5, 0.45, 0.55, 0.5, 0.4, 0.6, 0.0, 0.5, // BassA: Tune Cutoff Reso EnvMod Decay Accent Wave Oct
    0.5, 0.35, 0.55, 0.5, 0.4, 0.6, 0.0, 0.2, // BassB(Oct 0.2 = 한 옥타브 아래)
    0.8, 0.5, 0.8, 0.5, 0.8, 0.5, 0.8, 0.5, 0.8, 0.5, 0.8, 0.5, // 드럼 6 × (Level, Tune)
    0.25, 0.2, 0.4, 0.8, 0.5, // Delay Drive Comp Master Tempo
  ];

  // ==== 계측(스파이크 bridge.js 필드 유지 + 호스트 확장) ====
  const tPageStart = performance.now();

  // ==== 자산 prefetch(큰 PNG는 wasm 밖) — 목록은 app/assets/assets.go의 Names와 같다 ====
  // app.wasm 다운로드와 병렬로 받는다. Go는 assets.WaitReady()로 완료를 기다린 뒤 jd.asset(name)으로 읽는다.
  const ASSET_NAMES = ['device/panel.png', 'device/sprites/knob-r25.png', 'device/sprites/knob-r32.png', 'device/sprites/knob-r42.png', 'room/plate-night.png'];
  const assetBytes = new Map();
  let assetsState = 0; // 0 진행 중, 1 전부 성공, -1 일부 실패(있는 것만 제공)
  Promise.all(ASSET_NAMES.map((n) => fetch('assets/' + n).then((r) => { if (!r.ok) throw new Error(n + ' ' + r.status); return r.arrayBuffer(); }).then((buf) => assetBytes.set(n, new Uint8Array(buf))).catch((e) => { console.warn('jd host: 자산 실패', e); assetsState = -1; })))
    .then(() => { if (assetsState === 0) assetsState = 1; stats.tAssetsReady = performance.now(); });
  const stats = {
    wasmBytes: 0,          // app.wasm 원본 바이트(measure.mjs가 주입)
    wasmGzipBytes: null,
    wasmBrotliBytes: null,
    tFirstStill: null,     // 정지 배경 <img> onload
    tWasmLoaded: null,     // app.wasm instantiateStreaming 완료(index.html)
    tFirstFrame: null,     // Go 첫 Draw 끝
    dpr: window.devicePixelRatio,
    ua: navigator.userAgent,
    platform: navigator.platform || null,
    frames: 0,
    framesMark: 0,
    hiddenFrames: 0,       // document.hidden 동안 도착한 frame 콜백 수
    overlayKeys: 0,
    overlayFocused: 0,
    dragChanges: 0,
    lastKnobDrag: null,
    paramMsgsSent: 0,
    allocPerFrame: null,
    // 호스트 확장
    firstSoundMs: null,
    ticks: 0,
    cmdsSent: 0,
    logLen: 0,
    keyframes: 0,
    replayDone: 0,
    telemetryQueued: 0,
    telemetrySent: 0,
  };
  window.__jdStatsSet = (k, v) => { stats[k] = v; };

  const still = document.getElementById('still');
  const stillDone = () => { stats.tFirstStill = performance.now(); };
  if (still && still.complete && still.naturalWidth > 0) stillDone();
  else if (still) still.addEventListener('load', stillDone);

  // --- rAF 간격(정수 ms 히스토그램). ebiten이 초기화 시점에
  // window.requestAnimationFrame을 잠그므로 이 패치가 먼저 실행돼야 한다. ---
  let rafLast = null;
  let winBins = new Array(1000).fill(0);
  let winCount = 0, winSumMs = 0, winMax = 0;
  const origRAF = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = (cb) => origRAF((t) => {
    if (rafLast !== null) {
      const gap = Math.max(0, Math.round(t - rafLast));
      winBins[Math.min(999, gap)]++;
      winCount++;
      winSumMs += t - rafLast;
      if (gap > winMax) winMax = gap;
    }
    rafLast = t;
    return cb(t);
  });

  let frameSamples = [];
  function markFrames() {
    frameSamples = [];
    stats.framesMark = stats.frames;
    winBins = new Array(1000).fill(0);
    winCount = 0; winSumMs = 0; winMax = 0;
  }
  function frame(ms) {
    stats.frames++;
    if (document.hidden) stats.hiddenFrames++;
    frameSamples.push(ms);
  }
  function pct(sorted, p) {
    if (!sorted.length) return null;
    return sorted[Math.min(sorted.length - 1, Math.round(p * (sorted.length - 1)))];
  }

  // ==== 시드 단어 → uint32(session/seed.go SeedFromWord와 같은 규칙) ====
  // 정규화: 코드포인트 단위 소문자화 + 공백/제어 문자 제거(NFC 정규화 없음 — seed.go와 같은 결정).
  // 빈 결과는 0x9E3779B9, FNV-1a 결과가 0이면 1(seed 0의 엔진 대체 회피).
  function isControl(r) {
    const c = r.codePointAt(0);
    return (c <= 0x1f) || (c >= 0x7f && c <= 0x9f);
  }
  function seedFromWord(s) {
    let w = '';
    for (const r of String(s)) {
      if ((/\s/u.test(r) && r !== '\uFEFF') || isControl(r)) continue;
      w += r.toLowerCase();
    }
    if (w === '') return 0x9E3779B9 >>> 0;
    const b = new TextEncoder().encode(w); // UTF-8 바이트 — Go의 []byte(s)와 같다
    let h = 2166136261 >>> 0;
    for (let i = 0; i < b.length; i++) {
      h = (h ^ b[i]) >>> 0;
      h = Math.imul(h, 16777619) >>> 0;
    }
    return h === 0 ? 1 : h;
  }

  // ==== 12비트 양자화 미러(engine quantize와 비트 동일 — Math.fround로 f32 산술 재현) ====
  function quantN(v) {
    v = +v;
    if (v !== v || v < 0) v = 0; // NaN 방어 포함(quantize와 같은 순서)
    if (v > 1) v = 1;
    const t = Math.fround(Math.fround(v) * PARAM_STEPS);
    return Math.fround(t + 0.5) | 0; // Go uint16 변환은 절단
  }

  // ==== 섀도 엔진 미러 — 메인 스레드 두 번째 wasm 인스턴스(렌더하지 않는다) ====
  // 손으로 쓰던 미러 배열(params·bassNote·drumFlags·…) 대신 엔진 그 자체를 한 벌 더
  // 돌린다: cmd()를 즉시 적용하고 UI(param/bassStep/keyRoot/…)는 이 인스턴스에서 읽는다.
  // 바 경계 대기값(SelectPattern·SetKey)은 FLAG_BAR tick마다 jd_sync()로 확정 — 엔진의
  // "다음 바에 적용"과 같은 의미. Step/Bar·Peak는 워클릿 tick이 정본(섀도는 렌더가
  // 없어 스텝이 안 나간다). 시작 전 도착한 cmd는 pendingShadow에 쌓아 initShadow 직후
  // 재생한다(초기화 경쟁으로 인한 적용 누락 봉쇄).
  const shadow = { w: null, stateView: null, seed: 0, ready: false };
  const pendingShadow = [];
  const fallbackParams = new Float64Array(NUM_PARAMS); // 섀도 전 파라미터 폴백(기본값 양자화)
  for (let i = 0; i < NUM_PARAMS; i++) {
    fallbackParams[i] = Math.fround(quantN(DEFAULT_PARAMS[i]) / PARAM_STEPS);
  }
  function shadowApply(k, a, b, c, d, v) {
    if (shadow.w) shadow.w.jd_cmd(k | 0, a | 0, b | 0, c | 0, d | 0, +v);
    else pendingShadow.push([k | 0, a | 0, b | 0, c | 0, d | 0, +v]);
  }
  // shadowReset — 리셋 경로의 단일 소유자. 섀도를 새 시드로 초기화하고 로그 전체를
  // 재적용한다(로그가 정본 — 시작 전 pendingCmds도 블록 0으로 이미 들어 있다).
  function shadowReset(seed) {
    shadow.seed = seed >>> 0;
    pendingShadow.length = 0;
    if (!shadow.w) return;
    shadow.w.jd_reset(shadow.seed);
    for (let i = 0; i < log.length; i++) {
      const e = log[i];
      shadow.w.jd_cmd(e.k, e.a, e.b, e.c, e.d, e.v);
    }
  }
  function initShadow(module) {
    try {
      const inst = new WebAssembly.Instance(module, {});
      if (typeof inst.exports._initialize === 'function') inst.exports._initialize();
      const seed = seedNumber();
      inst.exports.jd_init(seed);
      shadow.w = inst.exports;
      shadow.seed = seed >>> 0;
      shadowStateView(); // 첫 파생(이후 jd_reset이 메모리를 키우면 shadowStateView가 갱신)
      shadow.ready = true;
      shadowReset(shadow.seed); // 로그 재적용 → pendingShadow도 여기서 드레인된다
    } catch (e) {
      console.warn('jd host: 섀도 엔진 초기화 실패 — 폴백 값으로 동작:', e);
      telemetry('shadow_error', 1);
    }
  }
  // shadowStateView — 섀도 상태 뷰를 **매 호출마다 현재 메모리에서 다시 파생**한다.
  // jd_reset의 재할당 등으로 wasm 메모리가 자라면 이전 buffer는 detach되어
  // set/slice가 TypeError로 죽는다(실측 — 초기화 직후 jd_reset이 이미 한 번 키운다).
  // 상태 배열은 고정 전역이라 주소는 그대로, buffer 객체만 갱신하면 된다.
  function shadowStateView() {
    const w = shadow.w;
    if (!w) return null;
    const buf = w.memory.buffer;
    if (!shadow.stateView || shadow.stateView.buffer !== buf
      || shadow.stateView.byteOffset !== w.jd_state_ptr() || shadow.stateView.length !== w.jd_state_size()) {
      shadow.stateView = new Uint8Array(buf, w.jd_state_ptr(), w.jd_state_size());
    }
    return shadow.stateView;
  }

  // 리플레이 직후 워클릿 상태를 섀도에 복사(리플레이 중의 불일치는 허용 — 끝에서 맞춘다).
  function shadowSyncFromBytes(bytes) {
    const view = shadowStateView();
    if (!view) return;
    const n = Math.min(bytes.length, view.length);
    view.set(n === bytes.length ? bytes : bytes.subarray(0, n));
    shadow.w.jd_state_read(bytes.length);
  }

  // ==== 텔레메트리 ====
  const telemetryEvents = [];
  let sessionId = '';
  {
    const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 12; i++) sessionId += alphabet[(Math.random() * alphabet.length) | 0];
  }
  function telemetry(ev, v) {
    telemetryEvents.push({ t: Math.round(performance.now()), ev, v: v === undefined ? null : v });
    if (telemetryEvents.length > 400) telemetryEvents.splice(0, telemetryEvents.length - 400); // 오래된 것 버림
    stats.telemetryQueued = telemetryEvents.length;
  }
  function statsSummary() {
    const s = window.__jdStats();
    return {
      firstSoundMs: s.firstSoundMs, ticks: s.ticks, cmdsSent: s.cmdsSent, logLen: s.logLen,
      keyframes: s.keyframes, replayDone: s.replayDone, frameMsP95: s.frameMsP95,
      hiddenFrames: s.hiddenFrames, audioStarted: s.audioStarted, seedWord: s.seedWord,
    };
  }
  function buildTelemetryPayload(nEvents) {
    const evs = nEvents >= telemetryEvents.length ? telemetryEvents.slice() : telemetryEvents.slice(-nEvents);
    return {
      kind: 'telemetry',
      sessionId,
      ua: navigator.userAgent,
      platform: navigator.platform || null,
      dpr: window.devicePixelRatio,
      startedAt: new Date(tPageStart).toISOString(),
      events: evs,
      stats: statsSummary(),
    };
  }
  function telemetryFlush() {
    const url = window.JD_REPORT_URL;
    if (!url) return Promise.resolve('no report url');
    let n = telemetryEvents.length;
    let body = JSON.stringify(buildTelemetryPayload(n));
    while (body.length > 32 * 1024 && n > 0) { // 32KB 상한 — 오래된 이벤트부터 버린다
      n = Math.max(0, n - 50);
      body = JSON.stringify(buildTelemetryPayload(n));
    }
    // kind 질의는 절대 URL(운영 Worker — cf/worker.js가 질의에서 kind를 읽는다)에만 붙인다.
    // 로컬 serve.mjs는 POST /report를 경로 정확 일치로 처리해 질의가 붙으면 404가 된다
    // (읽기 전용 파일 — kind는 어차피 본문에 있다). README-host.md §3 참조.
    // 본문 종류는 text/plain으로 보낸다 — application/json 본문은 교차 출처 preflight를
    // 유발해 Worker 경로 beacon을 늦춘다(수신자는 본문을 JSON.parse한다, 종류는 안 본다).
    const full = /^https?:/i.test(url) ? url + '?kind=telemetry' : url;
    if (navigator.sendBeacon && navigator.sendBeacon(full, new Blob([body], { type: 'text/plain' }))) {
      stats.telemetrySent++;
      return Promise.resolve('sent(beacon)');
    }
    return fetch(full, {
      method: 'POST', body, keepalive: true, // 헤더 없음 — text/plain 단순 요청 유지(preflight 회피)
    }).then((r) => {
      stats.telemetrySent++;
      return r.ok ? 'sent' : 'failed ' + r.status;
    }).catch((e) => 'error ' + e);
  }
  setInterval(() => { telemetryFlush(); }, 60000);
  document.addEventListener('visibilitychange', () => { if (document.hidden) telemetryFlush(); });
  window.addEventListener('pagehide', () => {
    if (performance.now() - tPageStart < 60000) telemetry('exit_60s', 1);
    telemetryFlush();
  });

  // ==== 오디오 ====
  // 페이지 로드 즉시(제스처 전) 컴파일을 시작해 첫 탭→첫 소리 300ms 예산에 준다.
  const modulePromise = WebAssembly.compileStreaming(fetch('engine.wasm'));
  // 같은 모듈로 섀도 엔진도 즉시 구동(렌더 없는 UI 미러 — 위 섀로 절). 컴파일 실패는
  // start() 경로가 보고한다; 섀도만의 실패는 initShadow가 잡는다.
  modulePromise.then(initShadow, () => {});
  // AudioContext는 제스처 전에 만들어 둘 수 있다(suspended 상태) — addModule까지 미리 끝내 두면
  // 제스처 뒤에는 resume + 노드 생성만 남는다. 실측(2026-09-05): 제스처 뒤 생성·addModule 경로는
  // chromium 첫 소리 347ms(예산 300 초과), 이 선행 경로가 그 차이를 없앤다. 생성이 거부되는
  // 환경이면 start()가 종전처럼 제스처 뒤에 만든다.
  let audio = null, node = null, analyser = null;
  let preModule = null; // addModule 선행 Promise(성공 시 audio도 선행 생성됨)
  try {
    const pre = new AudioContext({ sampleRate: 48000, latencyHint: 'interactive' });
    preModule = pre.audioWorklet.addModule('processor.js').then(() => pre, (e) => { console.warn('jd host: addModule 선행 실패, 제스처 뒤 재시도:', e); return null; });
  } catch (e) { preModule = null; }
  let startingAudio = false;
  let tAudioStart = 0;
  const pendingCmds = []; // 시작 전 cmd — start 직후 at=0으로 발송(로그 블록 0)

  const log = [];         // {block, author, k, a, b, c, d, v} — 로그가 정본
  const keyframes = [];   // {block, bytes} — 16바마다 + 시작(bar 0)
  let lastTickMsg = null;
  let flagsAccum = 0;
  let lastKFBar = -1;
  let isReplaying = false;
  let firstKnobAt = null;
  let workletStats = null;
  const stateWaiters = new Map(); // id → fn
  let stateSeq = 0;

  function blockNow() {
    if (!audio || !lastTickMsg) return 0;
    return lastTickMsg.block + (audio.currentTime - lastTickMsg.ctxTime) * BLOCKS_PER_SEC;
  }

  function onWorkletMsg(ev) {
    const d = ev.data;
    if (d.t === 'tick') {
      stats.ticks++;
      lastTickMsg = d;
      flagsAccum |= d.flags;
      if (d.flags & FLAG_BAR) {
        // 바 경계 — 섀도의 대기값(SelectPattern·SetKey)을 확정한다. 구 내보출이 없는
        // engine.wasm에서는 건너뛴다(입력 방어 — 폴백은 0값 읽기).
        if (shadow.w && typeof shadow.w.jd_sync === 'function') shadow.w.jd_sync();
        // 키프레임: bar가 16의 배수로 바뀐 tick. bar 0(재생 시작)도 포함 —
        // 세션 초반에도 리플레이가 가능해야 한다(README-host.md).
        if (!isReplaying && d.bar % 16 === 0 && d.bar !== lastKFBar) {
          lastKFBar = d.bar;
          if (node) node.port.postMessage({ t: 'state:get', id: 'kf' });
        }
      }
      if (stats.firstSoundMs === null && tAudioStart > 0 && d.peak > 0.001) {
        stats.firstSoundMs = performance.now() - tAudioStart;
        telemetry('first_sound_ms', Math.round(stats.firstSoundMs));
      }
    } else if (d.t === 'state') {
      if (d.id === 'kf') {
        keyframes.push({ block: d.block, bytes: d.bytes });
        stats.keyframes = keyframes.length;
      }
      if (d.id === 'shadow-sync') { // 리플레이 직후 워클릿 상태 → 섀도
        shadowSyncFromBytes(d.bytes);
      }
      const fn = stateWaiters.get(d.id);
      if (fn) { stateWaiters.delete(d.id); fn(d.bytes, d.block); }
    } else if (d.t === 'state:ack') {
      const key = 'ack:' + d.id;
      const fn = stateWaiters.get(key);
      if (fn) { stateWaiters.delete(key); fn(d.ok); }
    } else if (d.t === 'replay:done') {
      isReplaying = false;
      stats.replayDone++;
      telemetry('replay_used', 1);
      // 워클릿이 리플레이를 마친 지금이 정본 — 상태를 가져와 섀도에 덮어쓴다.
      if (node) node.port.postMessage({ t: 'state:get', id: 'shadow-sync' });
    } else if (d.t === 'stats') {
      workletStats = d;
    }
  }

  async function start() {
    if (audio || startingAudio) return; // 중복 호출 무해
    startingAudio = true;
    try {
      tAudioStart = performance.now();
      const pre = preModule ? await preModule : null;
      if (pre) {
        audio = pre;
        await audio.resume();
      } else {
        audio = new AudioContext({ sampleRate: 48000, latencyHint: 'interactive' });
        await audio.resume();
        await audio.audioWorklet.addModule('processor.js');
      }
      const module = await modulePromise;
      const nodeSeed = seedNumber();
      node = new AudioWorkletNode(audio, 'jd', {
        numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2],
        // collect:true — 이 페이지가 곧 계측 페이지다(스톨 히스토그램용).
        processorOptions: { module, seed: nodeSeed, collect: true },
      });
      node.port.onmessage = onWorkletMsg;
      node.onprocessorerror = () => telemetry('worklet_error', 1);
      analyser = audio.createAnalyser();
      analyser.fftSize = 512;
      node.connect(analyser);
      analyser.connect(audio.destination);
      for (let i = 0; i < pendingCmds.length; i++) {
        const c = pendingCmds[i];
        node.port.postMessage({ t: 'cmd', at: 0, k: c.k, a: c.a, b: c.b, c: c.c, d: c.d, v: c.v });
        stats.paramMsgsSent++;
      }
      pendingCmds.length = 0;
      // 섀도가 다른 시드로 시작했으면(시작 전에 시드를 고쳤으면) 여기서 재동기화 —
      // 새 노드 = 새 세션이므로 섀도도 같은 리셋을 받고 로그를 다시 적용한다.
      if ((nodeSeed >>> 0) !== shadow.seed) shadowReset(nodeSeed);
    } catch (e) {
      console.error('jd host: 오디오 시작 실패:', e);
      telemetry('worklet_error', 1);
    } finally {
      startingAudio = false;
    }
  }

  const seedbox = document.getElementById('seedbox');
  function seedNumber() { return seedFromWord(seedbox ? seedbox.value : ''); }

  function cmd(kind, a, b, c, d, v, author) {
    kind |= 0; a |= 0; b |= 0; c |= 0; d |= 0; v = +v; author |= 0;
    stats.cmdsSent++;
    shadowApply(kind, a, b, c, d, v);
    if (kind === 6) telemetry('drop', 1); // Drop
    if (kind === 0 && author === 0 && firstKnobAt === null) { // 첫 Human SetParam
      firstKnobAt = performance.now();
      telemetry('first_knob_ms', Math.round(firstKnobAt - tPageStart));
    }
    let at = 0;
    if (node) {
      at = Math.max(1, Math.round(blockNow()) + 2); // 항상 현재+2 — 로그 블록 = 실제 적용 블록
      node.port.postMessage({ t: 'cmd', at, k: kind, a, b, c, d, v });
      stats.paramMsgsSent++;
    } else {
      pendingCmds.push({ k: kind, a, b, c, d, v });
    }
    log.push({ block: at, author, k: kind, a, b, c, d, v });
    stats.logLen = log.length;
    return at;
  }

  // tick() — Go가 프레임당 1회 읽는 스냅샷. flags는 호출 사이 누적 OR이고 읽으면 0으로 리셋.
  // playing은 워클릿이 jd_playing()으로 실어 보낸다(0|1). 필드 없는 tick(구 워클릿)은
  // 정지로 해석한다 — 멈춘 엔진을 재생 중으로 그리는 쪽이 더 나쁘다(입력 방어).
  const tickOut = { started: false, block: 0, step: 0, bar: 0, flags: 0, peak: 0, ctxTime: 0, playing: false };
  function tick() {
    if (!lastTickMsg) return tickOut;
    tickOut.started = true;
    tickOut.block = blockNow();
    tickOut.step = lastTickMsg.step;
    tickOut.bar = lastTickMsg.bar;
    tickOut.flags = flagsAccum;
    tickOut.peak = lastTickMsg.peak;
    tickOut.ctxTime = lastTickMsg.ctxTime;
    tickOut.playing = !!lastTickMsg.playing;
    flagsAccum = 0;
    return tickOut;
  }

  const scopeF32 = new Float32Array(256);
  const scopeU8 = new Uint8Array(scopeF32.buffer); // 프레임당 할당 0(같은 버퍼 재사용)
  function scope() {
    if (!analyser) return null;
    analyser.getFloatTimeDomainData(scopeF32);
    return scopeU8;
  }

  function replay(sec) {
    if (!node || isReplaying) { telemetry('replay_unavailable', 1); return false; }
    const now = Math.round(blockNow());
    const target = now - sec * BLOCKS_PER_SEC;
    let K = null;
    for (let i = keyframes.length - 1; i >= 0; i--) {
      if (keyframes[i].block <= target) { K = keyframes[i]; break; }
    }
    if (!K) { telemetry('replay_unavailable', 1); return false; }
    const entries = [];
    for (let i = 0; i < log.length; i++) {
      const e = log[i];
      if (e.block > K.block && e.block <= now) {
        entries.push({ block: e.block, k: e.k, a: e.a, b: e.b, c: e.c, d: e.d, v: e.v });
      }
    }
    const blocks = Math.max(1, now - K.block);
    isReplaying = true; // 카운트다운 포함 — 이 동안 키프레임 요청은 쉰다
    setTimeout(() => {
      if (!node || !isReplaying) { isReplaying = false; return; }
      node.port.postMessage({ t: 'replay', state: K.bytes, entries, blocks });
    }, 3000); // 3·2·1 — 세 번째 초에 전송(카운트다운은 호스트가 소유)
    return true;
  }

  // 측정·디버그 전용(bridge_js.go API 표 밖): state:get 왕복.
  function debugStateGet() {
    return new Promise((resolve, reject) => {
      if (!node) { reject(new Error('audio not started')); return; }
      const id = 'dbg' + (++stateSeq);
      stateWaiters.set(id, (bytes, block) => resolve({ bytes, block }));
      node.port.postMessage({ t: 'state:get', id });
    });
  }

  // ==== 시드 단어 DOM(#seedtext 미러 — 캔버스 폰트는 ASCII라 한글은 DOM이 담당) ====
  const seedtext = document.getElementById('seedtext');
  if (seedbox && seedtext) {
    const sync = () => { seedtext.textContent = seedbox.value; };
    seedbox.addEventListener('input', sync);
    sync();
  }
  const seedtoggle = document.getElementById('seedtoggle');
  if (seedtoggle && seedbox) {
    seedtoggle.addEventListener('click', () => {
      if (seedbox.classList.toggle('show')) seedbox.focus();
    });
    seedbox.addEventListener('keydown', () => stats.overlayKeys++);
    seedbox.addEventListener('focus', () => stats.overlayFocused++);
  }

  // URL ?seed=단어 — 미리 채워 넣는다(로그 파라미터는 session 라운드 소유).
  {
    const u = new URLSearchParams(location.search).get('seed');
    if (u !== null && seedbox) {
      seedbox.value = u;
      telemetry('seed_open', 1);
    }
  }

  // ==== 첫 탭 → 오디오 시작(제스처). Go 쪽 Start()와 같이 들어와도 무해. ====
  let firstTapAt = null;
  window.addEventListener('pointerdown', () => {
    if (firstTapAt === null) {
      firstTapAt = performance.now();
      telemetry('first_tap', Math.round(firstTapAt - tPageStart));
    }
    start();
  }, { capture: true, passive: true });

  const reducedMotionQ = window.matchMedia ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;
  const wallOut = [0, 0, 0];

  let cleanScreen = false; // 클린 스크린(UI 잡동사니 숨김) — Go가 매 프레임 읽는다

  // hint — 첫 접촉 캡션(상태는 Go main이 정한다, 문구는 index.html의 JD_CAPTIONS).
  // 등록 안 된 상태(0 포함)는 숨긴다(입력 방어 — 모르는 상태를 그리지 않는다).
  const captionEl = document.getElementById('caption');
  function hint(state) {
    if (!captionEl) return;
    state |= 0;
    const c = window.JD_CAPTIONS;
    if (c && Object.prototype.hasOwnProperty.call(c, state) && c[state] != null) {
      captionEl.textContent = c[state];
      captionEl.hidden = false;
    } else {
      captionEl.hidden = true;
    }
  }

  // 개발 버튼(#tools)·시드 오버레이(#overlay)는 ?dev=1에서만(CSS body.dev 규칙).
  // #seedtext는 사용자가 늘 보는 값이라 항상 둔다.
  if (new URLSearchParams(location.search).get('dev') === '1') {
    document.body.classList.add('dev');
  }

  window.jd = {
    cleanScreen() { return cleanScreen; },
    setCleanScreen(b) { cleanScreen = !!b; document.body.classList.toggle('clean', cleanScreen); return cleanScreen; },
    asset(name) { return assetBytes.get(name) || null; },
    assetsReady() { return assetsState; },
    start,
    cmd,
    tick,
    scope,
    // 상태 읽기는 전부 섀도 엔진에서(렌더 워클릿 왕복 없이 동기). 섀도 전·초기화 실패에는
    // 파라미터는 기본값 양자화 폴백, 나머지는 0 — 브리지(intOf)가 number를 받게 종류 유지.
    param: (id) => {
      id |= 0;
      if (id >= 0 && id < NUM_PARAMS) {
        if (shadow.w && typeof shadow.w.jd_param === 'function') return shadow.w.jd_param(id);
        return fallbackParams[id];
      }
      return 0;
    },
    bassStep: (p, step) => (shadow.w ? shadow.w.jd_bass_step(p | 0, (step | 0) & 15) : 0),
    drumStep: (p, step) => (shadow.w ? shadow.w.jd_drum_step(p | 0, (step | 0) & 15) : 0),
    muted: (p) => (shadow.w ? shadow.w.jd_muted(p | 0) : 0), // 0|1 number — 2026-09-05 boolean 패닉 교훈(bridge_js intOf)
    slot: (p) => (shadow.w ? shadow.w.jd_slot(p | 0) : 0),
    keyRoot: () => (shadow.w && typeof shadow.w.jd_key === 'function' ? shadow.w.jd_key() : 0),
    chord: (bar) => (shadow.w && typeof shadow.w.jd_chord === 'function' ? shadow.w.jd_chord(bar | 0) : 0),
    mode: (part) => (shadow.w && typeof shadow.w.jd_mode === 'function' ? shadow.w.jd_mode(part | 0) : 0),
    hint,
    telemetry,
    replay,
    replaying: () => isReplaying,
    log: () => log,
    seedWord: () => (seedbox ? seedbox.value : ''),
    reducedMotion: () => !!(reducedMotionQ && reducedMotionQ.matches),
    hidden: () => document.hidden,
    wallClock: () => {
      const d = new Date();
      wallOut[0] = d.getHours(); wallOut[1] = d.getMinutes(); wallOut[2] = d.getSeconds();
      return wallOut;
    },
    frame,
    firstFrame: () => { if (stats.tFirstFrame === null) stats.tFirstFrame = performance.now(); },
    allocPerFrame: (bytes) => { stats.allocPerFrame = bytes; },
    markFrames,
    // 아래 셋은 측정·계측 전용(bridge_js.go 표 밖 — measure.mjs가 쓴다).
    debugStateGet,
    debugShadowState: () => {
      const v = shadowStateView();
      return v ? v.slice(0, shadow.w.jd_state_write()) : null;
    },
    telemetryFlush,
  };

  window.__jdStats = function () {
    const sorted = [...frameSamples].sort((a, b) => a - b);
    const n = sorted.length;
    const mean = n ? sorted.reduce((s, v) => s + v, 0) / n : null;
    let mode = 0, modeN = -1, rafTotal = 0, within = 0;
    for (let ms = 1; ms < winBins.length; ms++) rafTotal += winBins[ms];
    for (let ms = 1; ms < winBins.length; ms++) if (winBins[ms] > modeN) { modeN = winBins[ms]; mode = ms; }
    for (let ms = Math.max(1, mode - 2); ms <= mode + 2; ms++) within += winBins[ms];
    const canvas = document.querySelector('canvas');
    return {
      ...stats,
      canvasBackingW: canvas ? canvas.width : null,
      canvasBackingH: canvas ? canvas.height : null,
      frameMsMean: mean,
      frameMsP50: pct(sorted, 0.5),
      frameMsP95: pct(sorted, 0.95),
      frameMsMax: n ? sorted[n - 1] : null,
      frameSamples: n,
      rafModeMs: rafTotal ? mode : null,
      rafPctWithin2ms: rafTotal ? within / rafTotal : null,
      rafMaxMs: rafTotal ? winMax : null,
      fpsEst: winSumMs > 0 ? winCount / (winSumMs / 1000) : null,
      contextSampleRate: audio ? audio.sampleRate : null,
      audioStarted: !!node,
      seedWord: seedbox ? seedbox.value : null,
      replaying: isReplaying,
      // 비소모 라이브 값(측정 폴링용 — tick()은 flags를 소비하므로 남겨 둔다).
      liveBlock: audio && lastTickMsg ? blockNow() : null,
      liveStep: lastTickMsg ? lastTickMsg.step : null,
      liveBar: lastTickMsg ? lastTickMsg.bar : null,
      workletStats: workletStats
        ? { timerSource: workletStats.timerSource, stalls: workletStats.stalls, maxGapMs: workletStats.maxGapMs }
        : null,
    };
  };
})();

// 계측 JSON 리포트 전송(kind=app — Send report 버튼). JD_REPORT_URL이 비면 아무것도 안 한다.
window.jdSendReport = async function () {
  const url = window.JD_REPORT_URL;
  if (!url) return 'no report url';
  try {
    const r = await fetch(url + '?kind=app', {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(window.__jdStats()),
    });
    return r.ok ? 'sent ' + (await r.json()).file : 'failed ' + r.status;
  } catch (e) { return 'error ' + e; }
};
