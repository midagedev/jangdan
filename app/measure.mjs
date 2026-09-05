// measure.mjs — playwright로 UI 측을 열고 예산 항목을 회수한다.
// 사용:
//   node app/measure.mjs --browser chromium --seconds 20
//   node app/measure.mjs --browser chrome   --seconds 60 --shot app/results/chrome.png  (헤디드, 기록용)
//   node app/measure.mjs --browser webkit   --seconds 20
// 서버는 app/serve.mjs(8444, https + 사전압축)를 재사용/구동한다 — 계측 경로와
// 서빙 경로가 같아야 로딩 시각이 의미 있다. 이 스크립트가 구동한 서버만 정리한다.
// 시퀀스(P2-host): 로드 → 시작 전 게이트(캡션1·#tools 숨김·신규 API 종류) → 오디오
// 제스처 드래그 → (시드 타이핑은 ?dev=1 전용 — 숨겨져 있으면 건너뛴다) → 캡션2 게이트 →
// 호스트 검증(firstSound·param 섀도·state 왕복·replay) → 섀도 일치 게이트(30초 지점) →
// markFrames → N초 정상 구간(step 16값) → 배경 탭 20초 → 기기 뷰 진입(캡션3)·CUTOFF
// 드래그(캡션 종료)·Transport → 공유 URL 게이트 → 텔레메트리 저장 확인 → JSON 회수.
// 공유·기기 조작을 배경 창 뒤에 두는 이유: 정상 구간 프레임 계측은 방 뷰(활성)에서만.
import fs from 'node:fs';
import path from 'node:path';
import https from 'node:https';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url)); // app/
const web = path.join(here, 'web');
const resultsDir = path.join(here, 'results');
const PORT = Number(process.env.JD_PORT || 8444);
const BASE = `https://localhost:${PORT}/`;

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
const pageErrors = [];
page.on('pageerror', (e) => { pageErrors.push(e.message); console.error('[pageerror]', e.message); });
page.on('console', (m) => { if (m.type() === 'error') { pageErrors.push(m.text()); console.error('[console.error]', m.text()); } });

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

  // --- 시작 전 게이트: 캡션 1(탭 유도) 표시 · 개발 버튼(#tools·#overlay) 숨김(?dev=1 없음) ·
  // 섀도 읽기 API 종류(number — Go intOf가 undefined를 받으면 2026-09-05 패닉 계열이었다). ---
  await page.waitForFunction(() => window.jd && typeof window.jd.hint === 'function'
    && window.JD_CAPTIONS && document.getElementById('caption'), null, { timeout: 10000 });
  const pre = await page.evaluate(() => {
    const cap = document.getElementById('caption');
    const tools = document.getElementById('tools');
    const overlay = document.getElementById('overlay');
    return {
      captionHidden: cap.hidden,
      captionText: cap.textContent,
      want1: window.JD_CAPTIONS[1],
      toolsDisplay: tools ? getComputedStyle(tools).display : null,
      overlayDisplay: overlay ? getComputedStyle(overlay).display : null,
      keyRootType: typeof window.jd.keyRoot,
      keyRoot: typeof window.jd.keyRoot === 'function' ? window.jd.keyRoot() : null,
      chordType: typeof window.jd.chord,
      modeType: typeof window.jd.mode,
      mutedType: typeof window.jd.muted,
      muted0: typeof window.jd.muted === 'function' ? window.jd.muted(0) : null,
    };
  });
  const caption1Ok = pre.captionHidden === false && pre.captionText === pre.want1;
  const toolsHiddenOk = pre.toolsDisplay === 'none' && pre.overlayDisplay === 'none';
  const apiTypesOk = pre.keyRootType === 'function' && pre.chordType === 'function'
    && pre.modeType === 'function' && pre.mutedType === 'function'
    && Number.isInteger(pre.keyRoot) && pre.keyRoot >= 0 && pre.keyRoot < 12
    && (pre.muted0 === 0 || pre.muted0 === 1);

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

  // 시드 오버레이 타이핑(키보드 증명) — ?dev=1에서만 보인다. 이 흐름은 dev 없이 도니
  // 숨겨져 있으면 건너뛴다(overlayKeys 증명은 dev 실행으로 갈라둔다).
  const overlayVisible = await page.evaluate(() => getComputedStyle(document.getElementById('overlay')).display !== 'none');
  let seedTyped = false;
  if (overlayVisible) {
    await page.click('#seedtoggle');
    await page.type('#seedbox', 'jangdan');
    seedTyped = true;
  }

  // --- 호스트 검증 1: 첫 탭→첫 소리. start()는 드래그 pointerdown에서 이미 불렸다. ---
  await page.waitForFunction(() => {
    const s = window.__jdStats();
    return s && s.firstSoundMs !== null && s.ticks > 20;
  }, null, { timeout: 20000, polling: 100 });
  const firstSoundMs = await page.evaluate(() => window.__jdStats().firstSoundMs);

  // --- 캡션 2 게이트: 시작 후 4초(20초 창 안) — 기기 뷰 진입 전엔 이 문구가 떠 있다. ---
  const tFirstSound = Date.now();
  while (Date.now() - tFirstSound < 4000) await page.waitForTimeout(100);
  const cap2 = await page.evaluate(() => {
    const cap = document.getElementById('caption');
    return { hidden: cap.hidden, text: cap.textContent, want: window.JD_CAPTIONS[2] };
  });
  const caption2Ok = cap2.hidden === false && cap2.text === cap2.want;

  // --- 호스트 검증 2: cmd → 섀도 param. SetParam MASTER(31) = 0.9 ---
  const paramAt = await page.evaluate(() => window.jd.cmd(0, 31, 0, 0, 0, 0.9, 0)); // MASTER(31) — 레지던트가 움직이지 않는 파라미터(CutoffA는 에너지 곡선이 덮어써 거짓 FAIL)
  const paramMirror = await page.evaluate(() => window.jd.param(31));
  const paramExpected = Math.fround(3686 / 4095); // quantN(0.9)=3686 — 엔진 양자화와 동일
  // param은 이제 섀도 엔진의 float32 반환값이다. Math.fround(3686/4095)는 f64→f32 반올림이라
  // 엔진의 f32 나눗셈과 마지막 비트가 어긋날 수 있다(이중 반올림) — 1ulp 관대하게, state
  // 바이트(아래)가 3686 정확히인 것으로 비트 수준을 잡는다.
  const paramExact = paramMirror === paramExpected;
  const paramOk = Math.abs(paramMirror - paramExpected) < 1e-6;

  // --- 호스트 검증 3: state:get 왕복 — 워클릿 파라미터 바이트가 미러와 같은가. ---
  // cmd의 at 블록이 지나 적용된 뒤에 요청해야 한다(메시지는 도착 순이지만 적용은 블록 경계).
  await page.waitForFunction((b) => window.__jdStats().liveBlock > b, paramAt + 8, { timeout: 10000, polling: 100 });
  const stateRound = await page.evaluate(async () => {
    const st = await window.jd.debugStateGet();
    return { block: st.block, bytes: Array.from(st.bytes) };
  });
  const stateN = stateRound.bytes[64] | (stateRound.bytes[65] << 8); // params[31] u16 LE @ offset 2+2*31
  const stateOk = stateN === 3686 && Math.abs(Math.fround(stateN / 4095) - paramMirror) < 1e-6;

  // --- 호스트 검증 4: 리플레이. bar 0 키프레임이 있으므로 세션 초반에도 가능하다.
  // 단 T = now − 5s가 첫 키프레임 블록(≈4)보다 커야 한다 — 오디오 나이 5.9초(2200블록)를 기다린다.
  await page.waitForFunction(() => window.__jdStats().liveBlock > 2200, null, { timeout: 20000, polling: 200 });
  const replayInitiated = await page.evaluate(() => window.jd.replay(5));
  await page.waitForFunction(() => window.__jdStats().replayDone >= 1, null, { timeout: 90000, polling: 250 });
  const replayDone = await page.evaluate(() => window.__jdStats().replayDone);

  // --- 호스트 검증 5: 섀도 일치 게이트(오디오 시작 + 30초 = 11250블록).
  // 워클릿 상태(debugStateGet) vs 섀도 상태(debugShadowState)를 다음 영역에서 비교한다:
  //   params [2..68) · mute [678] · keyRoot [679] · mode [680..682) · playing [682] · chord [683..691)
  // 패턴 영역 [68..678)(베이스·드럼·슬롯)은 제외 — SelectPattern이 "다음 바에 확정"되는
  // 시차 때문에 스냅숏 위치에 따라 워클릿과 섀도가 1슬롯 어긋날 수 있다(엔진 설계상 정상,
  // jd_sync가 FLAG_BAR tick에서 확정한다). 섀도의 리셋·재적용은 shadowReset(로그가 정본),
  // 리플레이 직후엔 replay:done에서 워클릿 상태를 복사받아 재동기화한다.
  // 적용 중 cmd(cmd의 at = now+2)와의 경쟁이 있으면 600ms 뒤 1회 재시도한다.
  await page.waitForFunction(() => window.__jdStats().liveBlock > 11250, null, { timeout: 30000, polling: 250 });
  async function shadowCompare() {
    return page.evaluate(async () => {
      const live = await window.jd.debugStateGet();
      const sh = window.jd.debugShadowState();
      const regions = [[2, 68], [678, 679], [679, 680], [680, 682], [682, 683], [683, 691]];
      const diffs = [];
      if (sh) {
        for (const [a, b] of regions) {
          for (let i = a; i < b; i++) {
            if (live.bytes[i] !== sh[i]) diffs.push(i);
          }
        }
      }
      return {
        block: live.block, diffs, shadowNull: sh == null,
        liveLen: live.bytes.length, shadowLen: sh ? sh.length : null,
      };
    });
  }
  let shadowCmp = await shadowCompare();
  const shadowRetried = shadowCmp.diffs.length > 0;
  if (shadowRetried) {
    await page.waitForTimeout(600);
    shadowCmp = await shadowCompare();
  }
  const shadowOk = !shadowCmp.shadowNull && shadowCmp.diffs.length === 0;

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

  // --- 기기 뷰 첫 접촉(캡션 3)·공유 URL 게이트. 좌표는 레이아웃 JSON(논리 720×1280)을
  // CSS px로 환산한다(캔버스가 뷰포트를 채운다: 540/720 = 0.75). ---
  const roomLayout = JSON.parse(fs.readFileSync(path.join(here, 'assets', 'room', 'layout.json'), 'utf8'));
  const devLayout = JSON.parse(fs.readFileSync(path.join(here, 'assets', 'device', 'layout.json'), 'utf8'));
  const S = 540 / 720;
  const [dx, dy, dw, dh] = roomLayout.device;
  const shareBefore = await page.evaluate(() => ({
    url: window.jdShareURL ? window.jdShareURL() : '',
    human: window.jd.log().filter((e) => e.author === 0).length,
    logLen: window.__jdStats().logLen,
    logArrLen: window.jd.log().length,
  }));
  await page.mouse.click((dx + dw / 2) * S, (dy + dh / 2) * S);
  await page.waitForTimeout(900); // 진입 프레임 + 캡션 갱신
  const cap3 = await page.evaluate(() => {
    const cap = document.getElementById('caption');
    return { hidden: cap.hidden, text: cap.textContent, want: window.JD_CAPTIONS[3] };
  });
  const caption3Ok = cap3.hidden === false && cap3.text === cap3.want;

  // MASTER(FX) 노브 드래그 — 기기 뷰 사람 경로가 host 로그·Go 미러 로그를 모두 지나는지
  // (cmdBridge가 단일 소유자). MASTER(31)을 고른 이유: 레지던트 moverParams(CutoffA·
  // Reso A/B·EnvMod A/B·Drive·Delay)가 닿지 않는 파라미터라 URL 감량 어떤 단계에서도
  // 이 드래그가 살아남는다(CUTOFF로 실측했더니 레지던트 스윕이 뒤덮어 URL이 동일 바이트
  // 로 정체 — 감량 상한 단계에서 superseded). 첫 조작이므로 캡션 3도 여기서 끝난다.
  const k = devLayout.knobs.find((q) => q.section === 'fx' && q.name === 'MASTER');
  await page.mouse.move(k.cx * S, k.cy * S);
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) {
    await page.mouse.move(k.cx * S, k.cy * S - i * 8);
    await page.waitForTimeout(17);
  }
  await page.waitForTimeout(100);
  await page.mouse.up();
  await page.waitForTimeout(400);
  const cap3Hidden = await page.evaluate(() => document.getElementById('caption').hidden);
  const caption3EndsOk = cap3Hidden === true;
  const shareMid = await page.evaluate(() => ({
    url: window.jdShareURL ? window.jdShareURL() : '',
    human: window.jd.log().filter((e) => e.author === 0).length,
  }));
  // Transport(재생 유지) — 기기 뷰 재생 버튼이 아직 없어 API로 보낸다(CmdKind 11).
  // 스펙이 기대한 "URL이 자란다"는 이 경로에선 성립하지 않는다(실측): jd.cmd는 호스트
  // 로그에만 남고 Go 미러 로그(share URL 인코더의 입력)는 Go가 보낸 Cmd만 받는다 — 제품
  // UI 전부가 Go이므로 현재 제품 경로엔 구멍이 없지만, JS에서 직접 jd.cmd를 쓰는 미래
  // UI는 공유 URL에 누락된다(잔여 간극, 보고서 참조). 이 단계의 게이트는 host 로그
  // 사람 수 증가·logLen 정합으로 잡는다.
  await page.evaluate(() => window.jd.cmd(11, 1, 0, 0, 0, 0, 0));
  await page.waitForTimeout(300);
  const shareAfter = await page.evaluate(() => ({
    url: window.jdShareURL ? window.jdShareURL() : '',
    human: window.jd.log().filter((e) => e.author === 0).length,
    logLen: window.__jdStats().logLen,
    logArrLen: window.jd.log().length,
  }));
  // 사람 경로 증명: (a) MASTER 드래그 뒤 Go 미러 로그가 자라 URL이 **내용·길이 모두**
  // 달라진다(레지던트가 안 닿는 파라미터라 감량에 살아남는다), (b) host 로그 author=0
  // 증가(드래그 ≥1 + 트랜스포트 1 = ≥2), (c) logLen 정합(logLen === log().length) —
  // Go 미러는 같은 Go 스트림을 cmdBridge로 받으므로 host 정합이 필요조건이다.
  // 미러 로그 직접 디코드는 이 게이트 범위 밖(URL 내용 변화가 간접 증거).
  const shareOk = shareMid.url !== shareBefore.url && shareMid.url.length > shareBefore.url.length
    && shareAfter.human >= shareBefore.human + 2
    && shareAfter.logLen === shareAfter.logArrLen;

  // --- 호스트 검증 6: 텔레메트리가 app/results에 저장되는가(kind=telemetry). ---
  const flushStatus = await page.evaluate(() => window.jd.telemetryFlush());
  const telemetrySent = await page.evaluate(() => window.__jdStats().telemetrySent);
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
  const telemetryOk = (flushStatus === 'sent(beacon)' || flushStatus === 'sent')
    && telemetrySent >= 1 && telemetryFile !== null;

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
    consoleErrors: pageErrors.length,
    // 시작 전 게이트
    preCaption1Ok: caption1Ok,
    preCaptionText: pre.captionText,
    preToolsHiddenOk: toolsHiddenOk,
    preToolsDisplay: pre.toolsDisplay,
    preApiTypesOk: apiTypesOk,
    preKeyRoot: pre.keyRoot,
    preMuted0: pre.muted0,
    seedTyped,
    // 캡션
    caption2Ok,
    caption2Text: cap2.text,
    caption3Ok,
    caption3Text: cap3.text,
    caption3EndsOk,
    // 호스트 검증
    hostFirstSoundMs: firstSoundMs,
    hostTicks: active.stats.ticks,
    hostStepsSeen: stepsSeen,
    hostParamAt: paramAt,
    hostParamMirror: paramMirror,
    hostParamExpected: paramExpected,
    hostParamExact: paramExact,
    hostParamOk: paramOk,
    hostStateBlock: stateRound.block,
    hostStateParamN: stateN,
    hostStateOk: stateOk,
    hostReplayInitiated: replayInitiated,
    hostReplayDone: replayDone,
    hostKeyframes: active.stats.keyframes,
    // 섀도 일치
    shadowOk,
    shadowBlock: shadowCmp.block,
    shadowDiffs: shadowCmp.diffs.slice(0, 24),
    shadowRetried,
    shadowLiveLen: shadowCmp.liveLen,
    shadowLen: shadowCmp.shadowLen,
    // 공유 URL·사람 로그
    shareOk,
    shareUrlLenBefore: shareBefore.url.length,
    shareUrlLenMid: shareMid.url.length,
    shareUrlLenAfter: shareAfter.url.length,
    shareUrlChanged: shareMid.url !== shareBefore.url,
    shareHumanBefore: shareBefore.human,
    shareHumanAfter: shareAfter.human,
    shareLogLen: shareAfter.logLen,
    shareLogArrLen: shareAfter.logArrLen,
    hostTelemetryFlush: flushStatus,
    hostTelemetrySent: telemetrySent,
    hostTelemetryOk: telemetryOk,
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
    `bg20s frames+${j.bgFrames} hidden+${j.bgHiddenFrames} | errors=${j.consoleErrors}`
  );
  console.log(
    `[host] firstSound=${j.hostFirstSoundMs != null ? j.hostFirstSoundMs.toFixed(0) : 'TIMEOUT'}ms(≤300 목표) ` +
    `ticks=${j.hostTicks} steps=${j.hostStepsSeen}/16 ` +
    `param=${j.hostParamOk ? 'OK' + (j.hostParamExact ? '(exact)' : '(1ulp)') : 'FAIL ' + j.hostParamMirror} state=${j.hostStateOk ? 'OK n=' + j.hostStateParamN : 'FAIL n=' + j.hostStateParamN} ` +
    `replay=${j.hostReplayInitiated && j.hostReplayDone >= 1 ? 'done' : 'FAIL'} kf=${j.hostKeyframes} ` +
    `shadow=${j.shadowOk ? 'OK' : 'FAIL diffs=' + JSON.stringify(j.shadowDiffs) + (j.shadowRetried ? '(재시도 후)' : '')} ` +
    `caption1=${caption1Ok ? 'OK' : 'FAIL'} 2=${caption2Ok ? 'OK' : 'FAIL'} 3=${caption3Ok ? 'OK' : 'FAIL'}/end=${caption3EndsOk ? 'OK' : 'FAIL'} ` +
    `tools=${toolsHiddenOk ? 'hidden' : 'FAIL(' + pre.toolsDisplay + ')'} api=${apiTypesOk ? 'OK' : 'FAIL'} ` +
    `share=${j.shareOk ? 'OK ' + j.shareUrlLenBefore + '→' + j.shareUrlLenMid + ' human+' + (j.shareHumanAfter - j.shareHumanBefore) : 'FAIL'} ` +
    `activeHidden=${j.activeHiddenFrames} telemetry=${telemetryOk ? 'OK ' + flushStatus + ' ' + (j.hostTelemetryEvents ?? 0) + 'ev' : 'FAIL ' + flushStatus + ' file=' + (j.hostTelemetryFile ?? 'none')}`
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
