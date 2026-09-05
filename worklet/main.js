// main.js — 페이지 컨트롤·통계 수집·오프라인 해시·리포트 송신.
// 결과 JSON 키는 스펙 고정 스키마를 그대로 따른다.
const $ = (id) => document.getElementById(id);
const PARAM_NAMES = ['Cutoff', 'Resonance', 'EnvMod', 'Decay', 'Accent', 'Tempo', 'BDLevel', 'CHLevel'];
const BLOCK_US = 128 / 48000 * 1e6; // 2666.67µs

let ctx = null, node = null, module = null;
let wasmBytes = null, wasmGzipBytes = null;
let startedAt = 0;
let stats = null; // {n, sum, max, bins, stalls, timerSource}
let rcData = null; // renderCapacity 누적
let offlineHash = null, offlineSeconds = null;

async function gzipSize(bytes) {
  if (typeof CompressionStream !== 'function') return null;
  const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream('gzip'));
  const buf = await new Response(stream).arrayBuffer();
  return buf.byteLength;
}

function p99FromBins(bins, n) {
  const target = 0.99 * n;
  let acc = 0;
  for (let b = 0; b < bins.length; b++) {
    acc += bins[b];
    if (acc >= target) return (b + 0.5) * 100; // 구간 중간값(µs)
  }
  return bins.length * 100;
}


// 버스트 인식 스톨 판정. gapBins[ms] = 연속 process() 벽시계 간격 히스토그램.
// 최빈 비영 간격을 하드웨어 콜백 주기(버스트 주기)로 보고, 그 2.5배를 넘는 간격만 스톨.
// Chrome(1블록 콜백, 최빈 3ms)에서는 종전 ">8ms" 기준과 같고, iOS Safari(4블록 버스트,
// 최빈 11ms)에서는 버스트 주기 자체를 스톨로 세던 오판을 없앤다.
function burstStats(gapBins) {
  if (!gapBins) return null;
  let mode = 0, modeN = -1;
  for (let ms = 1; ms < gapBins.length; ms++) if (gapBins[ms] > modeN) { modeN = gapBins[ms]; mode = ms; }
  const threshold = Math.max(8, mode * 2.5);
  let stalls = 0;
  for (let ms = 0; ms < gapBins.length; ms++) if (ms > threshold) stalls += gapBins[ms];
  return { gapModeMs: mode, burstBlocksEst: Math.max(1, Math.round(mode / (128 / 48))), stallThresholdMs: threshold, stalls };
}

window.__collectJSON = function () {
  const bs = stats ? burstStats(stats.gapBins) : null;
  const n = stats ? stats.n : 0;
  const mean = stats && stats.n > 0 ? stats.sum / stats.n : null;
  const max = stats ? stats.max : null;
  return {
    ua: navigator.userAgent,
    platform: navigator.platform || null,
    contextSampleRate: ctx ? ctx.sampleRate : null,
    baseLatency: ctx && ctx.baseLatency != null ? ctx.baseLatency : null,
    outputLatency: ctx && ctx.outputLatency != null ? ctx.outputLatency : null,
    timerSource: stats ? stats.timerSource : null,
    mult: node ? node.__mult : null,
    seconds: n * 128 / 48000,
    blocks: n,
    renderUsMean: mean,
    renderUsMax: max,
    renderUsP99: stats && stats.n > 0 ? p99FromBins(stats.bins, stats.n) : null,
    loadPctMean: mean != null ? mean / BLOCK_US * 100 : null,
    stalls: bs ? bs.stalls : (stats ? stats.stalls : null),
    stalls8ms: stats ? stats.stalls : null,
    gapModeMs: bs ? bs.gapModeMs : null,
    burstBlocksEst: bs ? bs.burstBlocksEst : null,
    stallThresholdMs: bs ? bs.stallThresholdMs : null,
    maxGapMs: stats && stats.maxGapMs != null ? stats.maxGapMs : null,
    renderCapacity: rcData,
    wasmBytes,
    wasmGzipBytes,
    offlineHash,
    offlineSeconds,
    notes: [
      'contextSampleRate != 48000이면 엔진(48k 고정)을 리샘플 없이 그대로 재생한 것',
      'renderUsP99는 100µs 히스토그램 구간 중간값 근사',
      'stalls는 버스트 인식 판정(간격 > max(8ms, 최빈 간격×2.5)); stalls8ms는 원시 >8ms 카운트(iOS 버스트에서 과대)',
    ].join('; '),
  };
};

async function init() {
  const res = await fetch('engine.wasm');
  const bytes = new Uint8Array(await res.arrayBuffer());
  wasmBytes = bytes.length; // 스키마는 바이트 수(Uint8Array를 그대로 넣으면 객체로 직렬화된다)
  module = await WebAssembly.compile(bytes);
  wasmGzipBytes = await gzipSize(bytes);
  // 슬라이더 8개 (ParamID 0..7)
  const box = $('sliders');
  PARAM_NAMES.forEach((name, id) => {
    const row = document.createElement('div');
    row.className = 'row';
    const lab = document.createElement('label');
    lab.textContent = name;
    const range = document.createElement('input');
    range.type = 'range'; range.min = 0; range.max = 1; range.step = 0.01; range.value = 0.5;
    const val = document.createElement('span');
    val.textContent = ' 0.50';
    range.oninput = () => {
      val.textContent = ' ' + (+range.value).toFixed(2);
      if (node) node.port.postMessage({ t: 'param', id, v: +range.value });
    };
    row.append(lab, range, val);
    box.append(row);
  });
}

async function start() {
  if (ctx) return;
  ctx = new AudioContext({ sampleRate: 48000, latencyHint: 'interactive' });
  if (ctx.sampleRate !== 48000) {
    $('ctxwarn').textContent = `경고: 컨텍스트 ${ctx.sampleRate}Hz — 엔진은 48k 고정, 리샘플 없이 재생 중(해시 비교는 오프라인 48k로 함)`;
  }
  await ctx.resume();
  await ctx.audioWorklet.addModule('processor.js');
  const mult = +$('mult').value;
  node = new AudioWorkletNode(ctx, 'jd', {
    numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2],
    processorOptions: { module, seed: 1, mult, collect: true },
  });
  node.__mult = mult;
  node.port.onmessage = (ev) => {
    if (ev.data.t === 'stats') {
      stats = ev.data;
      drawStats();
    }
  };
  node.connect(ctx.destination);
  startedAt = performance.now();
  // Chrome 전용 AudioRenderCapacity — 있으면 공식 수치
  if (ctx.renderCapacity) {
    let cnt = 0, sum = 0;
    rcData = { averageLoadMean: null, peakLoadMax: 0, underrunRatioMax: 0 };
    ctx.renderCapacity.addEventListener('update', (e) => {
      cnt++; sum += e.averageLoad;
      if (e.peakLoad > rcData.peakLoadMax) rcData.peakLoadMax = e.peakLoad;
      if (e.underrunRatio > rcData.underrunRatioMax) rcData.underrunRatioMax = e.underrunRatio;
      rcData.averageLoadMean = sum / cnt;
    });
    ctx.renderCapacity.start({ updateInterval: 1 });
  }
}

function drawStats() {
  if (!stats) return;
  const mean = stats.sum / stats.n;
  $('stats').textContent =
    `blocks=${stats.n}  elapsed=${(stats.n * 128 / 48000).toFixed(1)}s  timer=${stats.timerSource}\n` +
    `renderUs mean=${mean.toFixed(1)} max=${stats.max.toFixed(1)} p99≈${p99FromBins(stats.bins, stats.n).toFixed(0)}\n` +
    `load% mean=${(mean / BLOCK_US * 100).toFixed(2)} (예산 데스크톱 20% / iPhone 50%)\n` +
    (() => { const bs = burstStats(stats.gapBins); return bs
      ? `stalls=${bs.stalls} (간격 > ${bs.stallThresholdMs.toFixed(1)}ms; 최빈 간격 ${bs.gapModeMs}ms ≈ ${bs.burstBlocksEst}블록 버스트, max ${(stats.maxGapMs||0).toFixed(0)}ms; 원시 >8ms=${stats.stalls})`
      : `stalls(>8ms 간격)=${stats.stalls}`; })() +
    (rcData ? `\nrenderCapacity avgLoad=${(rcData.averageLoadMean * 100).toFixed(2)}% peakLoad=${(rcData.peakLoadMax * 100).toFixed(1)}% underrunMax=${rcData.underrunRatioMax}` : '');
  $('json').textContent = JSON.stringify(window.__collectJSON(), null, 1);
  $('elapsed').textContent = (stats.n * 128 / 48000).toFixed(1) + 's';
}

async function offlineHashRun(seconds) {
  $('offlineState').textContent = ` 렌더 중(${seconds}s)…`;
  await new Promise((r) => setTimeout(r, 30));
  const oc = new OfflineAudioContext(2, 48000 * seconds, 48000);
  await oc.audioWorklet.addModule('processor.js');
  const on = new AudioWorkletNode(oc, 'jd', {
    numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2],
    processorOptions: { module, seed: 1, mult: 1, collect: false },
  });
  on.connect(oc.destination);
  const buf = await oc.startRendering();
  const Lc = buf.getChannelData(0), Rc = buf.getChannelData(1);
  const inter = new Float32Array(Lc.length * 2);
  for (let i = 0; i < Lc.length; i++) {
    inter[2 * i] = Lc[i];
    inter[2 * i + 1] = Rc[i];
  }
  const dig = await crypto.subtle.digest('SHA-256', new Uint8Array(inter.buffer));
  offlineHash = Array.from(new Uint8Array(dig)).map((b) => b.toString(16).padStart(2, '0')).join('');
  offlineSeconds = seconds;
  $('offlineState').textContent = ' 완료';
  $('json').textContent = JSON.stringify(window.__collectJSON(), null, 1);
}

window.__runOffline = offlineHashRun; // measure.mjs용

$('start').onclick = start;
$('offline').onclick = () => offlineHashRun(+$('offlineN').value).catch((e) => { $('offlineState').textContent = ' 실패: ' + e; });
$('mult').onchange = () => {
  const m = +$('mult').value;
  if (node) { node.port.postMessage({ t: 'mult', mult: m }); node.__mult = m; }
};
$('send').onclick = async () => {
  const j = window.__collectJSON();
  $('json').textContent = JSON.stringify(j, null, 1);
  try {
    const r = await fetch('report', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(j) });
    $('offlineState').textContent += r.ok ? ' [전송됨]' : ' [전송 실패 ' + r.status + ' — 위 JSON을 복사해 전달]';
  } catch (e) { $('offlineState').textContent += ' [서버 없음 — 위 JSON을 복사해 전달]'; }
};

init().catch((e) => { $('ctxwarn').textContent = '초기화 실패: ' + e; });
