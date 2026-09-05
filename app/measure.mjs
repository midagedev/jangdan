// measure.mjs — playwright로 UI 측을 열고 예산 항목을 회수한다.
// 사용:
//   node app/measure.mjs --browser chromium --seconds 20
//   node app/measure.mjs --browser chrome   --seconds 60 --shot app/results/chrome.png  (헤디드, 기록용)
//   node app/measure.mjs --browser webkit   --seconds 20
// 서버는 app/serve.mjs(8444, https + 사전압축)를 재사용/구동한다 — 계측 경로와
// 서빙 경로가 같아야 로딩 시각이 의미 있다. 이 스크립트가 구동한 서버만 정리한다.
// 시퀀스: 로드 → 오버레이 타이핑(키보드 증명) → 노브 0 드래그(회전 증명+오디오 제스처) →
// 호스트 검증(firstSound·param 미러·state:get 왕복·replay) → markFrames → N초 정상 구간
// (tick step 16값 관측) → 배경 탭 20초(백그라운드 렌더 확인) → 텔레메트리 저장 확인 → 캡처·회수.
import fs from 'node:fs';
import path from 'node:path';
import https from 'node:https';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url)); // app/
const web = path.join(here, 'web');
const resultsDir = path.join(here, 'results');
const BASE = 'https://localhost:8444/';

const args = process.argv.slice(2);
const opt = (k, d) => { const i = args.indexOf(`--${k}`); return i >= 0 ? args[i + 1] : d; };
const browserName = opt('browser', 'chromium');
const seconds = Number(opt('seconds', 60));
const shot = opt('shot', null);

if (!fs.existsSync(path.join(web, 'app.wasm'))) {
  console.error('app/web/app.wasm 없음 — 먼저 `bash app/build.sh`');
  process.exit(1);
}

// --- playwright 로딩: app/node_modules → NODE_PATH → spike/worklet 재사용(상대 import) ---
async function loadPlaywright() {
  try {
    return await import('playwright');
  } catch (e) {
    const rel = path.join(here, '..', 'spike', 'worklet', 'node_modules', 'playwright', 'index.mjs');
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
  child = spawn(process.execPath, [path.join(here, 'serve.mjs')], { stdio: 'inherit' });
  if (!(await waitUp())) {
    console.error('serve.mjs가 15초 안에 안 떴다');
    process.exit(1);
  }
}

const { chromium, webkit } = await loadPlaywright();
const opts = { headless: browserName !== 'chrome' };
if (browserName === 'chrome') opts.channel = 'chrome';
if (browserName === 'chromium' || browserName === 'chrome') {
  // 최신 Chrome은 소프트웨어 WebGL를 --enable-unsafe-swiftshader 없이 막는다(헤드리스용).
  opts.args = ['--enable-unsafe-swiftshader'];
}
const pw = browserName === 'webkit' ? webkit : chromium;
const browser = await pw.launch(opts);
const context = await browser.newContext({
  viewport: { width: 540, height: 960 },
  deviceScaleFactor: 2,
  ignoreHTTPSErrors: true,
});
const page = await context.newPage();
page.on('pageerror', (e) => console.error('[pageerror]', e.message));
page.on('console', (m) => { if (m.type() === 'error') console.error('[console.error]', m.text()); });

try {
  const t0 = Date.now();
  await page.goto(BASE, { waitUntil: 'load', timeout: 45000 });
  await page.waitForFunction(() => window.__jdStats && window.__jdStats().tFirstFrame !== null, null, { timeout: 60000 });

  // gzip/brotli 크기의 원본은 파일이다 — 브라우저는 원본 바이트만 본다.
  // wasmBytes도 파일에서 주입한다(서빙이 no-store라 페이지가 받은 것과 같은 파일).
  const raw = fs.statSync(path.join(web, 'app.wasm')).size;
  const gz = fs.existsSync(path.join(web, 'app.wasm.gz')) ? fs.statSync(path.join(web, 'app.wasm.gz')).size : null;
  const br = fs.existsSync(path.join(web, 'app.wasm.br')) ? fs.statSync(path.join(web, 'app.wasm.br')).size : null;
  await page.evaluate(([r, g, b]) => {
    window.__jdStatsSet('wasmBytes', r);
    window.__jdStatsSet('wasmGzipBytes', g);
    window.__jdStatsSet('wasmBrotliBytes', b);
    // 텔레메트리가 serve.mjs POST /report → app/results에 저장되는지 확인한다.
    window.JD_REPORT_URL = 'report';
  }, [raw, gz, br]);
  const resultsBefore = new Set(fs.readdirSync(resultsDir));

  // 노브 0 프로그램 드래그: 세로 -100px = 값 +0.5. 첫 pointerdown이 오디오 시작 제스처.
  // 프레임마다 한 칸씩 움직인다 — 한 번에 몰아서 보내면 Chrome이 mousemove를
  // 코얼레싱해 마지막 위치가 드래그에 반영되기 전에 버튼이 떨어진다(실측).
  await page.mouse.move(60, 300);
  await page.mouse.down();
  for (let y = 290; y >= 200; y -= 10) {
    await page.mouse.move(60, y);
    await page.waitForTimeout(17);
  }
  // 마지막 위치가 버튼 해제와 같은 입력 배치로 합쳐지지 않게 한 번 더 확실히 샘플링.
  await page.waitForTimeout(100);
  await page.mouse.up();

  // 시드 오버레이: 캔버스 위 DOM 입력이 키보드를 받는다는 증명.
  await page.click('#seedtoggle');
  await page.type('#seedbox', 'jangdan');

  // --- 호스트 검증 1: 첫 탭→첫 소리. start()는 드래그 pointerdown에서 이미 불렸다. ---
  await page.waitForFunction(() => {
    const s = window.__jdStats();
    return s && s.firstSoundMs !== null && s.ticks > 20;
  }, null, { timeout: 20000, polling: 100 });
  const firstSoundMs = await page.evaluate(() => window.__jdStats().firstSoundMs);

  // --- 호스트 검증 2: cmd → 12비트 양자화 미러. SetParam CutoffA(id 1) = 0.9 ---
  const paramAt = await page.evaluate(() => window.jd.cmd(0, 31, 0, 0, 0, 0.9, 0)); // MASTER(31) — 레지던트가 움직이지 않는 파라미터(CutoffA는 에너지 곡선이 덮어써 거짓 FAIL)
  const paramMirror = await page.evaluate(() => window.jd.param(31));
  const paramExpected = Math.fround(3686 / 4095); // quantN(0.9)=3686 — 엔진 양자화와 동일
  const paramOk = paramMirror === paramExpected;

  // --- 호스트 검증 3: state:get 왕복 — 워클릿 파라미터 바이트가 미러와 같은가. ---
  // cmd의 at 블록이 지나 적용된 뒤에 요청해야 한다(메시지는 도착 순이지만 적용은 블록 경계).
  await page.waitForFunction((b) => window.__jdStats().liveBlock > b, paramAt + 8, { timeout: 10000, polling: 100 });
  const stateRound = await page.evaluate(async () => {
    const st = await window.jd.debugStateGet();
    return { block: st.block, bytes: Array.from(st.bytes) };
  });
  const stateN = stateRound.bytes[64] | (stateRound.bytes[65] << 8); // params[31] u16 LE @ offset 2+2*31
  const stateOk = stateN === 3686 && Math.fround(stateN / 4095) === paramMirror;

  // --- 호스트 검증 4: 리플레이. bar 0 키프레임이 있으므로 세션 초반에도 가능하다.
  // 단 T = now − 5s가 첫 키프레임 블록(≈4)보다 커야 한다 — 오디오 나이 5.9초(2200블록)를 기다린다.
  await page.waitForFunction(() => window.__jdStats().liveBlock > 2200, null, { timeout: 20000, polling: 200 });
  const replayInitiated = await page.evaluate(() => window.jd.replay(5));
  await page.waitForFunction(() => window.__jdStats().replayDone >= 1, null, { timeout: 90000, polling: 250 });
  const replayDone = await page.evaluate(() => window.__jdStats().replayDone);

  // 정상 구간 측정(초기화·리플레이 프레임 제외) — 이 창에 tick step 16값 관측을 곁들인다.
  await page.evaluate(() => {
    window.jd.markFrames();
    window.__jdStepSeen = new Set();
    window.__jdStepTimer = setInterval(() => {
      window.__jdStepSeen.add(window.__jdStats().liveStep);
    }, 50);
  });
  const markHidden = await page.evaluate(() => window.__jdStats().hiddenFrames);
  await page.waitForTimeout(seconds * 1000);
  const active = await page.evaluate(() => {
    clearInterval(window.__jdStepTimer);
    return { stats: window.__jdStats(), steps: Array.from(window.__jdStepSeen) };
  });
  const stepsSeen = active.steps.filter((s) => s >= 0 && s < 16).length;
  const activeHiddenFrames = active.stats.hiddenFrames - markHidden;

  // 백그라운드 탭 20초: 다른 페이지를 앞으로 올려 원 탭을 숨기고 카운트를 비교한다.
  await page.evaluate(() => window.jd.markFrames());
  const bg = await context.newPage();
  await bg.goto('about:blank');
  await bg.bringToFront();
  await page.waitForTimeout(20000);
  const afterBg = await page.evaluate(() => window.__jdStats());
  await bg.close();
  await page.bringToFront();

  // --- 호스트 검증 5: 텔레메트리가 app/results에 저장되는가(kind=telemetry). ---
  const flushStatus = await page.evaluate(() => window.jd.telemetryFlush());
  let telemetryFile = null;
  let telemetryEvents = null;
  for (let i = 0; i < 10 && telemetryFile === null; i++) {
    await page.waitForTimeout(300);
    const appeared = fs.readdirSync(resultsDir).filter((f) => !resultsBefore.has(f));
    if (appeared.length) telemetryFile = appeared[0];
  }
  if (telemetryFile) {
    try {
      const j = JSON.parse(fs.readFileSync(path.join(resultsDir, telemetryFile), 'utf8'));
      telemetryEvents = Array.isArray(j.events) ? j.events.length : -1;
      if (j.kind !== 'telemetry') { telemetryFile += ' (kind!=' + j.kind + ')'; }
    } catch (e) {
      telemetryFile += ' (parse 실패: ' + e.message + ')';
    }
  }

  if (shot) {
    fs.mkdirSync(path.dirname(path.resolve(shot)), { recursive: true });
    await page.screenshot({ path: shot });
  }

  const j = {
    browser: browserName,
    headless: browserName !== 'chrome',
    seconds,
    ...afterBg,
    // frameMs·rAF·할당 계열은 정상 구간(markFrames 직후) 스냅샷을 쓴다 —
    // 배경 20초가 섞이면 P95와 마지막 할당 창이 오염된다.
    frameMsMean: active.stats.frameMsMean,
    frameMsP50: active.stats.frameMsP50,
    frameMsP95: active.stats.frameMsP95,
    frameMsMax: active.stats.frameMsMax,
    frameSamples: active.stats.frameSamples,
    rafModeMs: active.stats.rafModeMs,
    rafPctWithin2ms: active.stats.rafPctWithin2ms,
    rafMaxMs: active.stats.rafMaxMs,
    fpsEst: active.stats.fpsEst,
    allocPerFrame: active.stats.allocPerFrame,
    activeFrames: active.stats.frames - active.stats.framesMark,
    activeHiddenFrames,
    bgFrames: afterBg.frames - active.stats.frames,
    bgHiddenFrames: afterBg.hiddenFrames - active.stats.hiddenFrames,
    loadMs: Date.now() - t0,
    // 호스트 검증
    hostFirstSoundMs: firstSoundMs,
    hostTicks: active.stats.ticks,
    hostStepsSeen: stepsSeen,
    hostParamAt: paramAt,
    hostParamMirror: paramMirror,
    hostParamExpected: paramExpected,
    hostParamOk: paramOk,
    hostStateBlock: stateRound.block,
    hostStateParamN: stateN,
    hostStateOk: stateOk,
    hostReplayInitiated: replayInitiated,
    hostReplayDone: replayDone,
    hostKeyframes: active.stats.keyframes,
    hostTelemetryFlush: flushStatus,
    hostTelemetryFile: telemetryFile,
    hostTelemetryEvents: telemetryEvents,
  };

  const fmtMB = (n) => (n == null ? 'n/a' : (n / 1024 / 1024).toFixed(2) + 'MB');
  console.log(
    `[${browserName}${j.headless ? '(headless)' : '(headed)'} ${seconds}s 540x960@2x] ` +
    `wasm raw=${fmtMB(j.wasmBytes)} gzip=${fmtMB(j.wasmGzipBytes)} brotli=${fmtMB(j.wasmBrotliBytes)} | ` +
    `frameMs p50=${j.frameMsP50?.toFixed(2)} p95=${j.frameMsP95?.toFixed(2)} max=${j.frameMsMax?.toFixed(1)} n=${j.frameSamples} | ` +
    `raf mode=${j.rafModeMs}ms ±2ms=${(100 * j.rafPctWithin2ms).toFixed(1)}% max=${j.rafMaxMs}ms fps≈${j.fpsEst?.toFixed(1)} | ` +
    `load still=${j.tFirstStill != null ? j.tFirstStill.toFixed(0) : null}ms wasm=${j.tWasmLoaded != null ? j.tWasmLoaded.toFixed(0) : null}ms firstFrame=${j.tFirstFrame != null ? j.tFirstFrame.toFixed(0) : null}ms | ` +
    `alloc=${j.allocPerFrame != null ? (j.allocPerFrame < 1024 ? j.allocPerFrame.toFixed(1) + 'B' : (j.allocPerFrame / 1024).toFixed(2) + 'KB') : null}/frame | ` +
    `drags=${j.dragChanges} paramsSent=${j.paramMsgsSent} overlayKeys=${j.overlayKeys} | ` +
    `bg20s frames+${j.bgFrames} hidden+${j.bgHiddenFrames}`
  );
  console.log(
    `[host] firstSound=${j.hostFirstSoundMs != null ? j.hostFirstSoundMs.toFixed(0) : 'TIMEOUT'}ms(≤300 목표) ` +
    `ticks=${j.hostTicks} steps=${j.hostStepsSeen}/16 ` +
    `param=${j.hostParamOk ? 'OK' : 'FAIL ' + j.hostParamMirror} state=${j.hostStateOk ? 'OK n=' + j.hostStateParamN : 'FAIL n=' + j.hostStateParamN} ` +
    `replay=${j.hostReplayInitiated && j.hostReplayDone >= 1 ? 'done' : 'FAIL'} kf=${j.hostKeyframes} ` +
    `activeHidden=${j.activeHiddenFrames} telemetry=${j.hostTelemetryFile ?? 'FAIL'}(${j.hostTelemetryEvents ?? 0}ev)`
  );

  fs.mkdirSync(resultsDir, { recursive: true });
  const file = path.join(resultsDir, `host-${browserName}.json`);
  fs.writeFileSync(file, JSON.stringify(j, null, 2));
  console.log(`saved: ${file}`);
  if (shot) console.log(`shot: ${path.resolve(shot)}`);
} finally {
  await browser.close();
  if (child) {
    child.kill();
  }
}
