// jdBridge — Ebitengine wasm(Go) ↔ AudioWorklet 엔진 다리 + UI 층 계측.
// 소비자: app/main.go의 bridge_js.go, app/measure.mjs(window.__jdStats).
// 엔진 워클릿은 spike/worklet/public/processor.js를 그대로 재사용(collect:false).
// 오디오는 사용자 제스처(첫 pointerdown) 뒤에만 시작한다.
(() => {
  'use strict';

  const stats = {
    wasmBytes: 0,          // app.wasm 원본 바이트(이 페이지가 fetch한 크기)
    wasmGzipBytes: null,   // node측(measure.mjs)이 디스크에서 주입
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
    overlayKeys: 0,        // 시드 입력이 받은 keydown 수(DOM 오버레이 증명)
    overlayFocused: 0,
    dragChanges: 0,
    lastKnobDrag: null,
    paramMsgsSent: 0,
    allocPerFrame: null,
  };
  window.__jdStatsSet = (k, v) => { stats[k] = v; };

  // --- 정지 첫 화면 시각(캐시 재생이라 load 이벤트가 안 오는 경우 포함) ---
  const still = document.getElementById('still');
  const stillDone = () => window.__jdStatsSet('tFirstStill', performance.now());
  if (still && still.complete && still.naturalWidth > 0) stillDone();
  else if (still) still.addEventListener('load', stillDone);

  // --- rAF 간격(정수 ms 히스토그램). ebiten은 패키지 초기화 시점에
  // window.requestAnimationFrame을 잠근다 — 이 패치가 반드시 먼저 실행된다. ---
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

  // --- 프레임 시간 샘플. markFrames()로 측정 구간을 나눈다(초기화·배경 구간 제외). ---
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

  // --- 오디오: 첫 pointerdown(사용자 제스처)에서 시작. 시작 전 파라미터는 큐잉. ---
  let audio = null, node = null, analyser = null;
  const pending = []; // 시작 전 파라미터 [id, v]
  async function startAudio() {
    if (audio) return;
    audio = new AudioContext({ sampleRate: 48000, latencyHint: 'interactive' });
    await audio.resume();
    await audio.audioWorklet.addModule('processor.js');
    const module = await WebAssembly.compileStreaming(fetch('engine.wasm'));
    node = new AudioWorkletNode(audio, 'jd', {
      numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2],
      processorOptions: { module, seed: 1, collect: false },
    });
    analyser = audio.createAnalyser();
    analyser.fftSize = 512;
    node.connect(analyser);
    analyser.connect(audio.destination);
    for (const [id, v] of pending) {
      node.port.postMessage({ t: 'param', id, v });
      stats.paramMsgsSent++;
    }
    pending.length = 0;
  }
  window.addEventListener('pointerdown', () => {
    startAudio().catch((e) => console.error('jdBridge: 오디오 시작 실패:', e));
  }, { capture: true, passive: true });

  // --- Go에서 부르는 진입점 ---
  const scopeF32 = new Float32Array(256);
  const scopeU8 = new Uint8Array(scopeF32.buffer); // 프레임당 1KB 복사(계약)
  window.jdBridge = {
    setParam(id, v) {
      if (node) {
        node.port.postMessage({ t: 'param', id: id | 0, v: +v });
        stats.paramMsgsSent++;
      } else {
        pending.push([id | 0, +v]);
      }
    },
    scope() {
      if (!analyser) return null;
      analyser.getFloatTimeDomainData(scopeF32);
      return scopeU8;
    },
    clock() { return audio ? audio.currentTime : 0; },
    frame,
    markFrames,
    firstFrame() { if (stats.tFirstFrame === null) stats.tFirstFrame = performance.now(); },
    allocPerFrame(bytes) { stats.allocPerFrame = bytes; },
    knobDrag(id, v) { stats.dragChanges++; stats.lastKnobDrag = { id, v }; },
  };

  // --- 시드 단어 오버레이(DOM) — 캔버스 위 입력·키보드 동작 확인용 ---
  const seedbox = document.getElementById('seedbox');
  const seedtoggle = document.getElementById('seedtoggle');
  if (seedtoggle && seedbox) {
    seedtoggle.addEventListener('click', () => {
      const show = seedbox.classList.toggle('show');
      if (show) seedbox.focus();
    });
    seedbox.addEventListener('keydown', () => stats.overlayKeys++);
    seedbox.addEventListener('focus', () => stats.overlayFocused++);
  }

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
    };
  };
})();
