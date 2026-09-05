// hash-node.mjs — app/web/engine.wasm을 node에서 돌려 SHA-256을 낸다(브라우저 개입 없음).
// 해시 대상은 cmd/render/main.go와 같은 바이트: 블록(128프레임 스테레오 인터리브)마다
// float32 리틀엔디언 1024바이트를 순서대로 갱신.
// 사용: node tools/hash-node.mjs --seconds 30 --seed 1 [--script cmds.json]
//   --script: [{block,k,a,b,c,d,v}] — block을 절대 블록 인덱스로 해석, 해당 블록 렌더
//   시작 시점에 적용(processor.js의 큐 규칙과 동일: at 이하를 그 블록 직전에 jd_cmd).
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { performance } from 'node:perf_hooks';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url)); // tools/
const args = process.argv.slice(2);
const opt = (k, d) => {
  const i = args.indexOf(`--${k}`);
  return i >= 0 ? args[i + 1] : d;
};
const seconds = Number(opt('seconds', 30));
const seed = Number(opt('seed', 1));
const scriptPath = opt('script', null);
const wasmPath = path.join(here, '..', 'app', 'web', 'engine.wasm');

let script = [];
if (scriptPath) {
  const raw = JSON.parse(await readFile(scriptPath, 'utf8'));
  if (!Array.isArray(raw)) throw new Error('--script: JSON 배열이어야 한다');
  script = raw
    .map((e) => ({
      block: e.block | 0, k: e.k | 0, a: e.a | 0, b: e.b | 0, c: e.c | 0, d: e.d | 0, v: +e.v,
    }))
    .sort((x, y) => x.block - y.block); // node sort는 안정적 — 같은 block은 파일 순서(도착 순)
}

const bytes = await readFile(wasmPath);
const t0 = performance.now();
const { instance } = await WebAssembly.instantiate(bytes, {});
// wasm-unknown: 전역 초기화를 명시적으로 1회
if (typeof instance.exports._initialize === 'function') instance.exports._initialize();
instance.exports.jd_init(seed);
const w = instance.exports;
const ptr = w.jd_out_ptr(); // 바이트 주소
const f32 = new Float32Array(w.memory.buffer, ptr, 256);
if (f32.length !== 256) throw new Error('jd_out_ptr 뷰 길이 이상');
// wasm 메모리를 직접 해싱(엔진은 무할당이라 버퍼 주소 고정, 복사 0)
const view = Buffer.from(w.memory.buffer, ptr, 256 * 4);
const hash = createHash('sha256');
const blocks = Math.round((seconds * 48000) / 128);
let si = 0;
let mean = 0, maxN = 0;
for (let i = 0; i < blocks; i++) {
  const t1 = performance.now();
  while (si < script.length && script[si].block <= i) {
    const e = script[si++];
    w.jd_cmd(e.k, e.a, e.b, e.c, e.d, e.v);
  }
  w.jd_render();
  const d = (performance.now() - t1) * 1e6;
  if (i > 0) { mean += d; if (d > maxN) maxN = d; }
  hash.update(view);
}
console.log(JSON.stringify({
  engine: 'wasm-node',
  hashOf: 'float32le-interleaved-256-per-block (cmd/render/main.go와 동일)',
  seed, seconds, blocks,
  scriptEntries: script.length,
  sha256: hash.digest('hex'),
  ns_per_block_mean: Math.round(mean / (blocks - 1) * 10) / 10,
  ns_per_block_max: Math.round(maxN * 10) / 10,
  wasm_bytes: bytes.length,
  instantiate_ms: Math.round(performance.now() - t0),
}));
