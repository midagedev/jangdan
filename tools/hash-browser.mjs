// hash-browser.mjs — playwright 브라우저의 OfflineAudioContext에서 app/web/engine.wasm +
// app/web/processor.js를 돌려 SHA-256을 낸다. 해시 바이트는 tools/hash-node.mjs와
// cmd/render/main.go와 같다(블록마다 float32 리틀엔디언 인터리브 1024바이트).
// 사용: node tools/hash-browser.mjs --browser chromium --seconds 30 --seed 1 [--script cmds.json]
//   --script: hash-node.mjs와 같은 형식. processorOptions.script로 생성 시점에 큐에
//   선주입한다(오프라인 렌더에서 메시지 전달 경쟁을 원천 봉쇄).
// serve.mjs(8444, https)를 재사용/구동한다 — 계측 서빙 경로와 같은 파일을 돌리기 위해서.
// 브라우저는 순차 1개만 구동한다(과제 제약).
import fs from 'node:fs';
import path from 'node:path';
import https from 'node:https';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url)); // tools/
const appDir = path.join(here, '..', 'app');
const BASE = 'https://localhost:8444/';

const args = process.argv.slice(2);
const opt = (k, d) => { const i = args.indexOf(`--${k}`); return i >= 0 ? args[i + 1] : d; };
const browserName = opt('browser', 'chromium');
const seconds = Number(opt('seconds', 30));
const seed = Number(opt('seed', 1));
const scriptPath = opt('script', null);

let script = [];
if (scriptPath) {
  const raw = JSON.parse(fs.readFileSync(scriptPath, 'utf8'));
  if (!Array.isArray(raw)) throw new Error('--script: JSON 배열이어야 한다');
  script = raw.map((e) => ({
    block: e.block | 0, k: e.k | 0, a: e.a | 0, b: e.b | 0, c: e.c | 0, d: e.d | 0, v: +e.v,
  })).sort((x, y) => x.block - y.block);
}

if (!fs.existsSync(path.join(appDir, 'web', 'engine.wasm'))) {
  console.error('app/web/engine.wasm 없음 — 먼저 `bash app/build.sh`');
  process.exit(1);
}

// --- playwright 로딩: app/node_modules → spike/worklet 재사용(measure.mjs와 같은 방식) ---
async function loadPlaywright() {
  try {
    return await import('playwright');
  } catch (e) {
    const rel = path.join(appDir, '..', 'spike', 'worklet', 'node_modules', 'playwright', 'index.mjs');
    if (fs.existsSync(rel)) {
      console.error(`playwright: app/node_modules에 없어 spike/worklet을 상대 import로 재사용 (${path.basename(e.message.split('\n')[0])})`);
      return await import(rel);
    }
    throw e;
  }
}

// --- serve.mjs 재사용: 이미 떠 있으면 그걸 쓰고, 아니면 구동 후 정리 ---
function serverUp(timeoutMs = 3000) {
  return new Promise((resolve) => {
    const req = https.request(BASE, { method: 'HEAD', rejectUnauthorized: false, timeout: timeoutMs }, (res) => {
      res.resume();
      resolve(res.statusCode != null && res.statusCode < 500);
    });
    req.on('timeout', () => { req.destroy(); resolve(false); });
    req.on('error', () => resolve(false));
    req.end();
  });
}
async function waitUp(deadlineMs = 15000) {
  const t0 = Date.now();
  while (Date.now() - t0 < deadlineMs) {
    if (await serverUp(1500)) return true;
    await new Promise((r) => setTimeout(r, 400));
  }
  return false;
}

let child = null;
if (!(await serverUp())) {
  console.error('serve.mjs 구동(이미 떠 있지 않음)…');
  child = spawn(process.execPath, [path.join(appDir, 'serve.mjs')], { stdio: 'inherit' });
  if (!(await waitUp())) {
    console.error('serve.mjs가 15초 안에 안 떴다');
    process.exit(1);
  }
}

const { chromium, webkit } = await loadPlaywright();
const opts = { headless: true };
if (browserName === 'chromium' || browserName === 'chrome') {
  opts.args = ['--enable-unsafe-swiftshader'];
}
const pw = browserName === 'webkit' ? webkit : chromium;
const browser = await pw.launch(opts);

try {
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  const page = await context.newPage();
  page.on('pageerror', (e) => console.error('[pageerror]', e.message));
  page.on('console', (m) => { if (m.type() === 'error') console.error('[console.error]', m.text()); });
  const t0 = Date.now();
  await page.goto(BASE, { waitUntil: 'load', timeout: 45000 });

  const sha = await page.evaluate(async ({ seconds: sec, seed: sd, script: sc }) => {
    const oc = new OfflineAudioContext(2, Math.round(48000 * sec), 48000);
    await oc.audioWorklet.addModule('processor.js');
    const module = await WebAssembly.compileStreaming(fetch('engine.wasm'));
    const node = new AudioWorkletNode(oc, 'jd', {
      numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2],
      processorOptions: { module, seed: sd, collect: false, script: sc },
    });
    node.connect(oc.destination);
    const buf = await oc.startRendering();
    const Lc = buf.getChannelData(0);
    const Rc = buf.getChannelData(1);
    const inter = new Float32Array(Lc.length * 2);
    for (let i = 0; i < Lc.length; i++) {
      inter[2 * i] = Lc[i];
      inter[2 * i + 1] = Rc[i];
    }
    const dig = await crypto.subtle.digest('SHA-256', new Uint8Array(inter.buffer));
    return Array.from(new Uint8Array(dig)).map((b) => b.toString(16).padStart(2, '0')).join('');
  }, { seconds, seed, script });

  console.log(JSON.stringify({
    engine: 'offline-' + browserName,
    hashOf: 'float32le-interleaved-256-per-block (OfflineAudioContext 출력, cmd/render와 동일 바이트)',
    seed, seconds, blocks: Math.round((seconds * 48000) / 128),
    scriptEntries: script.length,
    sha256: sha,
    render_ms: Date.now() - t0,
  }));
} finally {
  await browser.close();
  if (child) child.kill();
}
