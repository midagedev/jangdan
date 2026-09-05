// processor.js — 장단 제품 워클릿 프로세서(engine.wasm 실행). 계약 원본:
// docs/impl-plan-2026-09-05.md §3·§4, 상세는 app/web/README-host.md.
// 실시간(AudioContext)과 오프라인(OfflineAudioContext) 양쪽에서 같이 돈다.
// processorOptions: {module: WebAssembly.Module, seed: number,
//                    collect?: boolean(통계 수집, 계측 겸용 페이지만),
//                    script?: [{block,k,a,b,c,d,v}](오프라인 해시용 선주입 — 생성 시점에
//                    큐에 넣어 메시지 전달 경쟁을 원천 봉쇄한다)}.
const TICK_EVERY = 4; // 4블록(512샘플 ≈ 10.7ms)마다 tick postMessage

class JDProcessor extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const o = (options && options.processorOptions) || {};
    const instance = new WebAssembly.Instance(o.module, {});
    // wasm-unknown: 전역 초기화를 명시적으로 1회 호출해야 한다.
    if (typeof instance.exports._initialize === 'function') {
      instance.exports._initialize();
    }
    instance.exports.jd_init((o.seed ?? 1) | 0);
    const w = this.w = instance.exports;
    this.f32 = new Float32Array(w.memory.buffer, w.jd_out_ptr(), 256);
    this.stateSize = w.jd_state_size();
    this.stateView = new Uint8Array(w.memory.buffer, w.jd_state_ptr(), this.stateSize);

    // 명령 큐 — at(블록 인덱스) 오름차순, 같은 at는 도착 순(뒤). 병렬 배열 + head 인덱스로
    // 재사용(적용 지점 이후 copyWithin 압축 — process() 경로에서 새 배열 만들지 않는다).
    this.qAt = []; this.qK = []; this.qA = []; this.qB = []; this.qC = []; this.qD = []; this.qV = [];
    this.qHead = 0;
    this.applied = 0; // 큐에서 꺼내 jd_cmd에 넘긴 누적 수(tick에 실려 간다)

    // 틱 누적(4블록): flags는 OR, peak는 최댓값.
    this.tickN = 0; this.tickFlags = 0; this.tickPeak = 0;

    // 리플레이 — 라이브 큐와 별도의 상대 블록 큐. 진행 중엔 라이브 cmd를 적용하지 않는다.
    this.replaying = false;
    this.rpAt = []; this.rpK = []; this.rpA = []; this.rpB = []; this.rpC = []; this.rpD = []; this.rpV = [];
    this.rpHead = 0; this.rpBase = 0; this.rpRemaining = 0;

    // 통계 수집(계측 겸용 페이지만). AudioWorkletGlobalScope에 performance가 없을 수
    // 있다(iOS — 스파이크 실측) → 프로브해서 Date.now로 폴백.
    this.collect = !!o.collect;
    this.hasPerf = typeof performance !== 'undefined';
    this.timerSource = this.hasPerf ? 'performance' : 'Date';
    this.n = 0; this.sum = 0; this.max = 0;
    this.bins = new Float64Array(20);   // jd_render 소요 100µs 히스토그램
    this.gapBins = new Float64Array(64); // process() 간격 히스토그램(정수 ms)
    this.stalls = 0; this.maxGapMs = 0; this.lastWall = null;
    this.statsTick = 0;

    // 오프라인 해시용 script 선주입(절대 블록 그대로 — 시작 전 큐).
    if (Array.isArray(o.script)) {
      for (let i = 0; i < o.script.length; i++) {
        const e = o.script[i];
        this.insert(this.qAt, this.qK, this.qA, this.qB, this.qC, this.qD, this.qV, e.block | 0, e.k | 0, e.a | 0, e.b | 0, e.c | 0, e.d | 0, +e.v);
      }
    }

    this.port.onmessage = (ev) => this.onMsg(ev.data);
  }

  // insert — at 오름차순 안정 삽입(같은 at는 기존 것 뒤 = 도착 순). 대부분의 cmd는
  // 가까운 미래 블록이므로 뒤에서 앞으로 스캔한다.
  insert(atArr, kArr, aArr, bArr, cArr, dArr, vArr, at, k, a, b, c, d, v) {
    let i = atArr.length;
    while (i > 0 && atArr[i - 1] > at) i--;
    atArr.splice(i, 0, at); kArr.splice(i, 0, k); aArr.splice(i, 0, a);
    bArr.splice(i, 0, b); cArr.splice(i, 0, c); dArr.splice(i, 0, d); vArr.splice(i, 0, v);
  }

  onMsg(d) {
    const w = this.w;
    if (d.t === 'cmd') {
      this.insert(this.qAt, this.qK, this.qA, this.qB, this.qC, this.qD, this.qV,
        Math.max(0, d.at | 0), d.k | 0, d.a | 0, d.b | 0, d.c | 0, d.d | 0, +d.v);
    } else if (d.t === 'reset') {
      // 전면 재초기화 — 대기 중 명령은 폐기한다(새 시드의 새 세션이므로).
      w.jd_reset(d.seed | 0);
      this.qAt.length = 0; this.qK.length = 0; this.qA.length = 0; this.qB.length = 0;
      this.qC.length = 0; this.qD.length = 0; this.qV.length = 0;
      this.qHead = 0; this.applied = 0;
      this.tickN = 0; this.tickFlags = 0; this.tickPeak = 0;
    } else if (d.t === 'state:get') {
      const n = w.jd_state_write();
      // wasm 메모리 뷰는 얕다 — 복사해서 보낸다(구조적 복제 전에 값이 바뀔 수 있다).
      this.port.postMessage({ t: 'state', id: d.id, bytes: this.stateView.slice(0, n), block: w.jd_block() });
    } else if (d.t === 'state:set') {
      const src = d.bytes;
      const n = Math.min(src.length, this.stateView.length);
      this.stateView.set(n === src.length ? src : src.subarray(0, n));
      const ok = w.jd_state_read(src.length) === 1;
      this.port.postMessage({ t: 'state:ack', id: d.id, ok });
    } else if (d.t === 'replay') {
      // 리플레이: (1)상태 복원 (2)ResetPos (3)엔트리를 상대 블록(첫 엔트리 기준 0)에 예약.
      // 리플레이 중 도착한 라이브 cmd는 큐에 남고, 종료 후 jd_block() >= at 조건으로
      // 즉시 적용된다(블록 재해석 보정 없음 — 단순화, README-host.md 참조).
      const st = d.state;
      const n = Math.min(st.length, this.stateView.length);
      this.stateView.set(n === st.length ? st : st.subarray(0, n));
      w.jd_state_read(st.length);
      w.jd_cmd(7, 0, 0, 0, 0, 0); // ResetPos
      this.rpAt.length = 0; this.rpK.length = 0; this.rpA.length = 0; this.rpB.length = 0;
      this.rpC.length = 0; this.rpD.length = 0; this.rpV.length = 0;
      this.rpHead = 0;
      const es = d.entries || [];
      const base = es.length ? es[0].block | 0 : 0;
      for (let i = 0; i < es.length; i++) {
        const e = es[i];
        this.insert(this.rpAt, this.rpK, this.rpA, this.rpB, this.rpC, this.rpD, this.rpV,
          (e.block | 0) - base, e.k | 0, e.a | 0, e.b | 0, e.c | 0, e.d | 0, +e.v);
      }
      this.rpBase = w.jd_block();
      this.rpRemaining = Math.max(1, d.blocks | 0);
      this.replaying = true;
    }
  }

  // applyDue — due(이번에 렌더할 블록 인덱스 = jd_block()) 이하 예약을 순서대로 적용.
  // 큐 압축은 head가 쌓였을 때 copyWithin으로(새 배열 할당 없음).
  applyDue(due) {
    const w = this.w;
    while (this.qHead < this.qAt.length && this.qAt[this.qHead] <= due) {
      w.jd_cmd(this.qK[this.qHead], this.qA[this.qHead], this.qB[this.qHead],
        this.qC[this.qHead], this.qD[this.qHead], this.qV[this.qHead]);
      this.qHead++;
      this.applied++;
    }
    if (this.qHead >= 16 && this.qHead >= this.qAt.length) {
      const n = this.qHead;
      this.qAt.copyWithin(0, n); this.qK.copyWithin(0, n); this.qA.copyWithin(0, n);
      this.qB.copyWithin(0, n); this.qC.copyWithin(0, n); this.qD.copyWithin(0, n); this.qV.copyWithin(0, n);
      this.qAt.length -= n; this.qK.length -= n; this.qA.length -= n;
      this.qB.length -= n; this.qC.length -= n; this.qD.length -= n; this.qV.length -= n;
      this.qHead = 0;
    }
  }

  process(inputs, outputs) {
    const w = this.w;
    const now = this.collect ? (this.hasPerf ? performance.now() : Date.now()) : 0;
    if (this.collect && this.lastWall !== null) {
      const gap = now - this.lastWall;
      if (gap > 8) this.stalls++;
      this.gapBins[Math.min(63, Math.max(0, Math.round(gap)))]++;
      if (gap > this.maxGapMs) this.maxGapMs = gap;
    }

    const due = w.jd_block(); // 이번에 렌더할 블록 인덱스(0-based)
    if (this.replaying) {
      // 리플레이 블록: 상대 at = due - rpBase.
      const rel = due - this.rpBase;
      while (this.rpHead < this.rpAt.length && this.rpAt[this.rpHead] <= rel) {
        w.jd_cmd(this.rpK[this.rpHead], this.rpA[this.rpHead], this.rpB[this.rpHead],
          this.rpC[this.rpHead], this.rpD[this.rpHead], this.rpV[this.rpHead]);
        this.rpHead++;
        this.applied++;
      }
      w.jd_render();
      this.rpRemaining--;
      if (this.rpRemaining <= 0) {
        this.replaying = false;
        this.port.postMessage({ t: 'replay:done' });
      }
    } else {
      this.applyDue(due);
      w.jd_render();
    }

    if (this.collect) {
      const after = this.hasPerf ? performance.now() : Date.now();
      const us = (after - now) * 1000;
      this.n++;
      this.sum += us;
      if (us > this.max) this.max = us;
      this.bins[Math.min(19, Math.floor(us / 100))]++;
      this.lastWall = after;
      if (++this.statsTick >= 375) { // ≈1초
        this.statsTick = 0;
        this.port.postMessage({
          t: 'stats', n: this.n, sum: this.sum, max: this.max,
          bins: Array.from(this.bins), stalls: this.stalls,
          gapBins: Array.from(this.gapBins), maxGapMs: this.maxGapMs,
          timerSource: this.timerSource,
        });
      }
    }

    // 틱 신호 누적(4블록) — flags는 jd_flags()를 블록마다 OR.
    const fl = w.jd_flags();
    this.tickFlags |= fl;
    const pk = w.jd_peak();
    if (pk > this.tickPeak) this.tickPeak = pk;
    if (++this.tickN >= TICK_EVERY) {
      this.tickN = 0;
      this.port.postMessage({
        t: 'tick', block: w.jd_block(), step: w.jd_step(), bar: w.jd_bar(),
        flags: this.tickFlags, peak: this.tickPeak, ctxTime: currentTime, applied: this.applied,
      });
      this.tickFlags = 0; this.tickPeak = 0;
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
