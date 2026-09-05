// jd 프로세서 — wasm 엔진을 AudioWorklet 스레드에서 실행.
// 같은 파일이 실시간(AudioContext)과 오프라인(OfflineAudioContext) 양쪽에서 돈다.
// 결정론 주의: 오프라인 해시 비교 시 mult는 반드시 1(collect:false 경로와 무관하게).
class JDProcessor extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const o = (options && options.processorOptions) || {};
    this.seed = (o.seed ?? 1) | 0;
    this.mult = Math.max(1, o.mult | 0);
    this.collect = !!o.collect; // 통계 수집은 실시간 컨텍스트만
    const instance = new WebAssembly.Instance(o.module, {});
    // wasm-unknown: 전역 초기화를 명시적으로 1회 호출해야 한다.
    if (typeof instance.exports._initialize === 'function') {
      instance.exports._initialize();
    }
    instance.exports.jd_init(this.seed);
    this.w = instance.exports;
    this.f32 = new Float32Array(instance.exports.memory.buffer, this.w.jd_out_ptr(), 256);
    // AudioWorkletGlobalScope에 performance가 있는지는 브라우저마다 다르다 — 프로브해서 기록.
    this.hasPerf = typeof performance !== 'undefined';
    this.timerSource = this.hasPerf ? 'performance' : 'Date';
    this.n = 0; this.sum = 0; this.max = 0;
    this.bins = new Float64Array(20); // 100µs 간격 히스토그램, 마지막 구간은 1900µs+
    this.stalls = 0;           // 원시 프록시: 연속 process() 벽시계 간격 > 8ms
    // 간격 히스토그램(정수 ms, 0..63). iOS Safari는 하드웨어 콜백 1회에 여러 128블록을
    // 몰아 렌더하므로(실측: 4블록 버스트) "간격 > 8ms"가 버스트 주기마다 1회 찍혀
    // 언더런이 아닌 것을 스톨로 센다. 메인에서 최빈 간격(버스트 주기)을 구해
    // 그 2.5배를 넘는 간격만 스톨로 다시 판정한다.
    this.gapBins = new Float64Array(64);
    this.maxGapMs = 0;
    this.lastWall = null;
    this.tick = 0;
    this.port.onmessage = (ev) => {
      const d = ev.data;
      if (d.t === 'param') this.w.jd_set_param(d.id | 0, +d.v);
      else if (d.t === 'mult') this.mult = Math.max(1, d.mult | 0);
    };
  }

  process(inputs, outputs) {
    if (this.collect) {
      const now = this.hasPerf ? performance.now() : Date.now();
      // 스톨 프록시: 연속 process() 사이 벽시계 간격 > 8ms (블록 2.667ms의 3배 초과)
      if (this.lastWall !== null) {
        const gap = now - this.lastWall;
        if (gap > 8) this.stalls++;
        this.gapBins[Math.min(63, Math.max(0, Math.round(gap)))]++;
        if (gap > this.maxGapMs) this.maxGapMs = gap;
      }
      this.w.jd_render(this.mult);
      const after = this.hasPerf ? performance.now() : Date.now();
      const us = (after - now) * 1000;
      this.n++;
      this.sum += us;
      if (us > this.max) this.max = us;
      this.bins[Math.min(19, Math.floor(us / 100))]++;
      this.lastWall = after;
      if (++this.tick >= 375) { // 48000/128 = 375블록 ≈ 1초
        this.tick = 0;
        this.port.postMessage({
          t: 'stats', n: this.n, sum: this.sum, max: this.max,
          bins: Array.from(this.bins), stalls: this.stalls,
          gapBins: Array.from(this.gapBins), maxGapMs: this.maxGapMs,
          timerSource: this.timerSource,
        });
      }
    } else {
      this.w.jd_render(this.mult);
    }
    const f = this.f32;
    const L = outputs[0][0];
    const R = outputs[0][1];
    for (let i = 0; i < 128; i++) {
      L[i] = f[2 * i];
      R[i] = f[2 * i + 1];
    }
    return true;
  }
}
registerProcessor('jd', JDProcessor);
