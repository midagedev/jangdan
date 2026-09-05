// hash-node.mjs — node에서 engine.wasm을 돌려 cmd/render와 같은 방식의
// sha256을 낸다(네이티브 vs wasm 분리용: 브라우저 개입 없음).
// 사용: node spike/worklet/hash-node.mjs --seconds 30 --seed 1
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { performance } from 'node:perf_hooks';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
const opt = (k, d) => {
  const i = args.indexOf(`--${k}`);
  return i >= 0 ? Number(args[i + 1]) : d;
};
const seconds = opt('seconds', 30);
const seed = opt('seed', 1);
const wasmPath = path.join(here, 'public', 'engine.wasm');

const bytes = await readFile(wasmPath);
const t0 = performance.now();
const { instance } = await WebAssembly.instantiate(bytes, {});
// wasm-unknown: 전역 초기화를 명시적으로 1회
if (typeof instance.exports._initialize === 'function') instance.exports._initialize();
instance.exports.jd_init(seed);
const ptr = instance.exports.jd_out_ptr();
const BLOCK = 128;
const f32 = new Float32Array(instance.exports.memory.buffer, ptr, 2 * BLOCK);
const blocks = Math.round((seconds * 48000) / 128);
const hash = createHash('sha256');
// wasm 메모리를 직접 해싱(엔진은 무할당이라 버퍼 주소 고정, 복사 0)
const view = Buffer.from(instance.exports.memory.buffer, ptr, 2 * BLOCK * 4); // ptr은 바이트 주소
let mean = 0, maxN = 0;
for (let i = 0; i < blocks; i++) {
  const t1 = performance.now();
  instance.exports.jd_render(1);
  const d = (performance.now() - t1) * 1e6;
  if (i > 0) { mean += d; if (d > maxN) maxN = d; }
  hash.update(view);
}
console.log(JSON.stringify({
  seed, seconds, blocks,
  sha256: hash.digest('hex'),
  ns_per_block_mean: Math.round(mean / (blocks - 1) * 10) / 10,
  ns_per_block_max: Math.round(maxN * 10) / 10,
  wasm_bytes: bytes.length,
  instantiate_ms: Math.round(performance.now() - t0),
}));
