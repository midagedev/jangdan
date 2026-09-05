// dump-wasm.mjs — wasm 엔진 출력 비트열 덤프(dump-native와 동일 형식).
// 사용: node spike/worklet/dump-wasm.mjs --seconds 1 --seed 1 > out.bin
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { createWriteStream } from 'node:fs';

const here = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
const optNum = (k, d) => {
  const i = args.indexOf(`--${k}`);
  return i >= 0 ? Number(args[i + 1]) : d;
};
const optStr = (k, d) => {
  const i = args.indexOf(`--${k}`);
  return i >= 0 ? String(args[i + 1]) : d;
};
const seconds = optNum('seconds', 1);
const seed = optNum('seed', 1);
const out = optStr('out', '');
const muteBD = args.includes('--mute-bd');
const muteCH = args.includes('--mute-ch');

const bytes = await readFile(path.join(here, 'public', 'engine.wasm'));
const { instance } = await WebAssembly.instantiate(bytes, {});
if (typeof instance.exports._initialize === 'function') instance.exports._initialize();
instance.exports.jd_init(seed);
if (muteBD) instance.exports.jd_set_param(6, 0); // BDLevel
if (muteCH) instance.exports.jd_set_param(7, 0); // CHLevel
const ptr = instance.exports.jd_out_ptr();
const view = Buffer.from(instance.exports.memory.buffer, ptr, 2 * 128 * 4);
const blocks = Math.round((seconds * 48000) / 128);
const sink = out ? createWriteStream(out) : process.stdout;
for (let i = 0; i < blocks; i++) {
  instance.exports.jd_render(1);
  // view는 wasm 메모리를 가리키는 비복사 뷰 — 비동기 싱크가 늦게 플러시하면
  // 다음 블록 내용으로 덮어 쓰인다. 반드시 복사해서 넘긴다(실측 사고).
  sink.write(Buffer.from(view));
}
if (out) await new Promise((r) => sink.end(r));
