// tools/capture.mjs — 방 뷰·기기 뷰 캡처(비전 판정 입력). playwright chromium, 뷰포트 720×1280 DPR 1
// (논리 좌표 = CSS px, 레이아웃 JSON 좌표로 바로 탭한다). 서버는 app/serve.mjs(8444) 재사용/구동.
//   node tools/capture.mjs --out <dir> [--browser chromium]
// 산출: room-hint.png(탭 전) room-live.png(탭 후 4초) room-live2.png(12초) device.png(기기 탭 후)
//       device-knob.png(CUTOFF A 드래그 후) · device-chord.png(코드 셀 탭 → 선택기) + MANIFEST.json(커밋 해시·시각·UA)
import fs from 'node:fs';
import path from 'node:path';
import https from 'node:https';
import { spawn, execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(here, '..');
const args = process.argv.slice(2);
const opt = (k, d) => { const i = args.indexOf(`--${k}`); return i >= 0 ? args[i + 1] : d; };
const out = opt('out', path.join(root, 'scratch', 'captures'));
const browserName = opt('browser', 'chromium');
// room-hint 컷 대기(ms). 시작 전 링 펄스(1Hz, 알파 0.5..1.0)의 위상은 앱 시작 후 경과로
// 정해지므로 캡처 시점을 옮겨 밝은 구간에 맞출 수 있다 — pixcheck 휘도 상승 게이트용.
const hintWait = Number(opt('hint-wait', 800));
fs.mkdirSync(out, { recursive: true });
const PORT = Number(process.env.JD_PORT || 8444); // app/serve.mjs와 같은 변수
const BASE = `https://localhost:${PORT}/`;

async function loadPlaywright() {
  try { return await import('playwright'); } catch {
    return await import(path.join(root, 'spike', 'worklet', 'node_modules', 'playwright', 'index.mjs'));
  }
}
function serverUp() {
  return new Promise((res) => {
    const req = https.get(BASE + 'index.html', { rejectUnauthorized: false }, (r) => { r.resume(); res(r.statusCode === 200); });
    req.on('error', () => res(false)); req.setTimeout(1500, () => { req.destroy(); res(false); });
  });
}
let server = null;
if (!(await serverUp())) {
  server = spawn('node', [path.join(root, 'app', 'serve.mjs')], { stdio: 'ignore', env: { ...process.env, JD_PORT: String(PORT) } });
  for (let i = 0; i < 40 && !(await serverUp()); i++) await new Promise((r) => setTimeout(r, 250));
}
const layout = JSON.parse(fs.readFileSync(path.join(root, 'app/assets/room/layout.json'), 'utf8'));
const dev = JSON.parse(fs.readFileSync(path.join(root, 'app/assets/device/layout.json'), 'utf8'));
const pw = await loadPlaywright();
const browser = await pw[browserName].launch({ args: browserName === 'chromium' ? ['--autoplay-policy=no-user-gesture-required', '--use-gl=angle', '--ignore-gpu-blocklist'] : [] });
const ctx = await browser.newContext({ viewport: { width: 720, height: 1280 }, deviceScaleFactor: 1, ignoreHTTPSErrors: true });
const page = await ctx.newPage();
const errors = [];
page.on('pageerror', (e) => errors.push(String(e)));
const logs = [];
page.on('console', (m) => { logs.push(m.type() + ': ' + m.text()); if (m.type() === 'error') errors.push(m.text()); });
await page.goto(BASE + 'index.html', { waitUntil: 'load' });
await page.waitForFunction(() => window.__jdStats && window.__jdStats().tFirstFrame !== null, null, { timeout: 30000 });
await page.waitForTimeout(hintWait);
const shot = async (name) => { await page.screenshot({ path: path.join(out, name) }); console.log('shot', name); };
await shot('room-hint.png');
// 탭(제스처) → 오디오 시작. 캔버스 가운데 빈 곳.
await page.mouse.click(360, 300);
await page.waitForTimeout(4000);
await shot('room-live.png');
await page.waitForTimeout(8000);
await shot('room-live2.png');
// 기기 탭 → 기기 뷰
const [dx, dy, dw, dh] = layout.device;
await page.mouse.click(dx + dw / 2, dy + dh / 2);
await page.waitForTimeout(1200);
await shot('device.png');
// CUTOFF A 드래그(위로 120px)
const k = dev.knobs.find((q) => q.section === 'basslineA' && q.name === 'CUTOFF');
await page.mouse.move(k.cx, k.cy); await page.mouse.down();
for (let i = 1; i <= 12; i++) { await page.mouse.move(k.cx, k.cy - i * 10); await page.waitForTimeout(30); }
await page.mouse.up();
await page.waitForTimeout(600);
await shot('device-knob.png');
// 코드 트랙 셀 2(마디 3) 탭 → 선택기 열림(비전 판정 항목 6 — 첫 캡처 세트에 빠져 있었다)
if (dev.chord_track && dev.chord_track.rect) {
  const [cx0, cy0, cw, ch] = dev.chord_track.rect;
  await page.mouse.click(cx0 + cw * (2.5 / 8), cy0 + ch / 2);
  await page.waitForTimeout(500);
  await shot('device-chord.png');
}
const stats = await page.evaluate(() => window.__jdStats());
const commit = execSync('git rev-parse --short HEAD', { cwd: root }).toString().trim();
fs.writeFileSync(path.join(out, 'MANIFEST.json'), JSON.stringify({ commit, at: new Date().toISOString(), browser: browserName, viewport: '720x1280@1', ua: stats.ua, firstSoundMs: stats.firstSoundMs, frameMsP95: stats.frameMsP95, cmdsSent: stats.cmdsSent, errors, logs: logs.slice(-80) }, null, 1));
console.log('errors', errors.length, 'firstSound', stats.firstSoundMs, 'cmds', stats.cmdsSent);
await browser.close();
if (server) server.kill();
