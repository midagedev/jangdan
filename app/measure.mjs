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
// markFrames → N초 정상 구간(step 16값) → 배경 탭 20초 → 기기 뷰 진입(캡션3)·MASTER
// 드래그(캡션 종료) → 공유 세션 게이트(P2-share: 저장·id URL·쿨다운·왕복·열기 재생·404)
// → 텔레메트리 저장 확인 → JSON 회수.
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
let expected413Console = 0; // 의도적 413 주입의 자원 로드 실패(브라우저가 남기는 콘솔) — 일반 오류 카운트에서 제외
page.on('pageerror', (e) => { pageErrors.push(e.message); console.error('[pageerror]', e.message); });
page.on('console', (m) => {
  if (m.type() === 'error') {
    if (/status of 413/.test(m.text())) { expected413Console++; return; } // C3 실패 주입의 예상 결과
    pageErrors.push(m.text()); console.error('[console.error]', m.text());
  }
});

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

  // --- 파트별 레벨 게이트(P3-levels — 계약 ↔ 단언. 원본: engine.Level → jd_level →
  //     processor tick.levels(4블록 max) → host levelsAccum/levelPeaks(세션 누적)).
  //   C8 파트별 활동이 host까지 흐른다(라인 LED·VU 미터 원본)
  //      A19 시작 4초 뒤 levelPeaks[2](BD) > 0.01(초기 패턴 BD = 스텝 0·4·8·12)
  //      A20 시작 4초 뒤 levelPeaks[0](BassA) > 0.01(초기 패턴 게이트 밀도 0.75)
  //      A21 8값 전부 유한·비음수(host Number.isFinite 방어가 NaN을 통과시키지 않는다)
  //   C9 레벨 배선은 프레임당 할당을 만들지 않는다(누적 배열 재사용)
  //      A22 active 구간 allocPerFrame ≤ 900B/frame(기존 ≈850B 예산 + 여유)
  //      A23 allocPerFrame !== null(계측 도착 자체)
  //   FAIL-first(변경 전 host.js — 본 라운드 실측): stats에 levelPeaks가 없어
  //   length≠8로 A19·A20·A21이 실패한다(levels=FAIL 출력 확인).
  const levelPeaks = await page.evaluate(() => Array.from(window.__jdStats().levelPeaks ?? []));
  const levelsOk = levelPeaks.length === 8
    && levelPeaks.every((v) => Number.isFinite(v) && v >= 0)
    && levelPeaks[2] > 0.01 && levelPeaks[0] > 0.01;

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
  // A22·A23 — active 구간 스냅숏의 할당(acc은 Go가 창마다 jd.allocPerFrame로 올린다).
  const allocOk = active.stats.allocPerFrame != null && active.stats.allocPerFrame <= 900;

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
  const humanBefore = await page.evaluate(() => ({
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
  // Reso A/B·EnvMod A/B·Drive·Delay)가 닿지 않는 파라미터라 공유 페이로드에 살아남는다.
  // 첫 조작이므로 캡션 3도 여기서 끝난다.
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
  const humanAfter = await page.evaluate(() => ({
    human: window.jd.log().filter((e) => e.author === 0).length,
    logLen: window.__jdStats().logLen,
    logArrLen: window.jd.log().length,
  }));
  // 사람 경로 증명: (a) host 로그 author=0 증가(드래그 ≥1), (b) logLen 정합
  // (logLen === log().length) — Go 미러는 같은 Go 스트림을 cmdBridge로 받으므로 host
  // 정합이 필요조건이다. URL 내용 변화 증명은 아래 공유 게이트의 무손실 왕복이 잡는다.
  const humanLogOk = humanAfter.human >= humanBefore.human + 1
    && humanAfter.logLen === humanAfter.logArrLen;

  // --- 공유 세션 게이트(P2-share — docs/impl-plan-2026-09-05.md §12.6). 계약 ↔ 단언:
  //   C1 URL에는 id만 실린다(?s=<base62 10자>, ≤120자, 로그 페이로드·seed 없음)
  //      A1 url이 /\/\?s=[0-9A-Za-z]{10}$/ 일치   A2 url.length ≤ 120   A3 'v2.'·'seed=' 부정
  //   C2 저장 페이로드는 무손실(감량 0단계)·Worker 왕복 보존
  //      A4 node GET log의 fnv1a === 페이지 shareLogHash   A5 GET log.length === shareLogChars
  //      A6 열린 페이지 sharedEntries === Go shareDecodedEntries(같은 페이로드의 두 디코더 일치)
  //   C3 실패를 조용히 넘기지 않는다(413 → share_failed)
  //      A7 오버사이즈 직접 호출이 413으로 기각   A8 stats.shareFailed ≥ 1
  //   C4 쿨다운 10초(KV 쓰기 한도 보호) — 재호출은 같은 URL, POST 없음
  //      A9 두 번째 호출 URL === 첫 URL   A10 sharePosts 무증가
  //   C5 열기 — GET 즉시 · 도착한 로그가 재생 저자 Cmd로 흐른다
  //      A11 열린 페이지 sharedEntries > 0   A12 jd.log() author===2 ≥ 1   A13 tap 뒤 cmdsSent 증가
  //   C6 열기 실패(404)는 일반 세션 + open_failed
  //      A14 openFailed === 1   A15 해당 페이지 sharedEntries === 0 · pageerror 0
  //   FAIL-first(변경 전 실측 app/results/20260905-124458-chromium.json): 공유 URL이 로그를
  //   통째로 실어 shareUrlLenMid 4678자 — A1·A2·A3 전부 실패하는 입력이다.
  const WORKER = 'https://jangdan-reports.midagedev.workers.dev';
  // 로컬 telemetry(serve.mjs POST /report → app/results) 검증을 위해 JD_REPORT_URL은
  // 다시 'report'로 돌아간다 — 실제 Worker 주소는 이 공유 구간에만 건다(레포 파일에
  // 주소를 박지 않는다 — 배포 시 report-config.js가 소유).
  await page.evaluate((w) => { window.JD_REPORT_URL = w + '/report'; }, WORKER);
  // ① 오버사이즈(256KB 초과) → 413. Worker가 content-length에서 즉시 거부하므로 KV 쓰기 없음.
  const overStatus = await page.evaluate(() => window.jd
    .shareSession('v2.' + 'A'.repeat(300 * 1024), 1, 'x').then(() => null, (e) => e));
  // ② 공유 — jdShareURL()이 Promise로 resolve하는 '?s=<id>' URL
  const shareURL = await page.evaluate(() => window.jdShareURL());
  // ③ 쿨다운 중 재호출 — 같은 URL, POST 없음
  const shareURL2 = await page.evaluate(() => window.jdShareURL());
  const shareStats = await page.evaluate(() => {
    const s = window.__jdStats();
    return {
      posts: s.sharePosts, ok: s.shareOk, failed: s.shareFailed, bytes: s.shareBytes,
      logChars: s.shareLogChars, logHash: s.shareLogHash, mirror: s.shareMirrorEntries,
      decoded: s.shareDecodedEntries, block: s.liveBlock,
    };
  });
  await page.evaluate(() => { window.JD_REPORT_URL = 'report'; }); // 로컬 telemetry로 복원
  const shareIdOk = typeof shareURL === 'string' && /^https:\/\/[^/]+\/\?s=[0-9A-Za-z]{10}$/.test(shareURL);
  const shareLenOk = typeof shareURL === 'string' && shareURL.length > 0 && shareURL.length <= 120;
  const shareNoPayload = typeof shareURL === 'string' && !shareURL.includes('v2.') && !shareURL.includes('seed=');
  // ④ 저장 왕복(node에서 Worker GET — 공개 읽기). 같은 id를 다시 받아 해시·길이를 잰다.
  const shareId = shareIdOk ? new URL(shareURL).searchParams.get('s') : null;
  let getLog = null;
  let getBytes = null;
  if (shareId) {
    const r = await fetch(WORKER + '/sessions/' + shareId);
    // Cloudflare가 GET 본문을 gzip+chunked로 주면 content-length 헤더가 없다(실측 0) —
    // 크기는 해제 후 본문 문자 수로 기록한다.
    const text = await r.text();
    getBytes = text.length;
    getLog = JSON.parse(text).log ?? null;
  }
  const fnv32a = (str) => {
    const b = Buffer.from(str, 'utf8');
    let h = 2166136261 >>> 0;
    for (let i = 0; i < b.length; i++) { h = (h ^ b[i]) >>> 0; h = Math.imul(h, 16777619) >>> 0; }
    return h;
  };
  const roundtripHashOk = getLog !== null && fnv32a(getLog) === shareStats.logHash;
  const roundtripLenOk = getLog !== null && getLog.length === shareStats.logChars;
  // ⑤ 열기 — 같은 컨텍스트의 새 페이지에서 공유 URL을 띄운다. Worker 주소는 init script로
  // (세션 GET이 host.js 로드 즉시 필요해서), 직후 ''로 끊어 이 페이지의 telemetry가
  // 어디로도 전송되지 않게 한다(측정 페이지가 운영 Worker를 오염시키지 않게).
  const p2 = await context.newPage();
  const p2Errors = [];
  p2.on('pageerror', (e) => { p2Errors.push(e.message); console.error('[p2 pageerror]', e.message); });
  p2.on('console', (m) => { if (m.type() === 'error') { p2Errors.push(m.text()); console.error('[p2 console.error]', m.text()); } });
  await p2.addInitScript((w) => {
    // 접근자 주입 — 로컬 report-config.js('')의 대입은 setter로 흡수한다. 해제는 load 이후:
    // setTimeout(0)은 파서 블로킹 스크립트(report-config.js→host.js) fetch 틈에 끼어들어
    // host.js 평가보다 먼저 발사되는 경합이 있다(실측 — 그래서 host.js가 빈 주소를 읽었다).
    // load는 모든 동기 스크립트 뒤에만 오고, telemetry 첫 flush(60초·pagehide)보다 앞이다.
    let rep = w + '/report';
    Object.defineProperty(window, 'JD_REPORT_URL', {
      configurable: true,
      get: () => rep,
      set: () => {},
    });
    window.addEventListener('load', () => { rep = ''; }); // 세션 GET 후 이 페이지 telemetry 차단
  }, WORKER);
  let openSharedEntries = null;
  let openReplayCmds = null;
  let openCmdsDelta = null;
  let openOk = false;
  if (shareId) {
    await p2.goto(shareURL, { waitUntil: 'load', timeout: 45000 });
    try {
      await p2.waitForFunction(() => window.__jdStats && window.__jdStats().sharedEntries > 0, null, { timeout: 30000, polling: 200 });
    } catch (e) {
      const dbg = await p2.evaluate(() => ({
        url: location.href,
        jru: window.JD_REPORT_URL ?? null,
        openFailed: window.__jdStats ? window.__jdStats().openFailed : null,
        sharedEntries: window.__jdStats ? window.__jdStats().sharedEntries : null,
        ready: window.jd ? window.jd.sharedLogReady() : null,
      })).catch(() => null);
      console.error('[p2 debug]', JSON.stringify(dbg));
      throw e;
    }
    openSharedEntries = await p2.evaluate(() => window.__jdStats().sharedEntries);
    const before = await p2.evaluate(() => ({
      cmds: window.__jdStats().cmdsSent,
      replay: window.jd.log().filter((e) => e.author === 2).length,
    }));
    await p2.mouse.click(270, 480); // 첫 제스처 — 오디오 시작(재생은 진짜 tick 뒤에 흐른다)
    await p2.waitForFunction(
      (n) => window.__jdStats().cmdsSent > n && window.jd.log().filter((e) => e.author === 2).length > 0,
      before.cmds, { timeout: 20000, polling: 200 },
    );
    const after = await p2.evaluate(() => ({
      cmds: window.__jdStats().cmdsSent,
      replay: window.jd.log().filter((e) => e.author === 2).length,
    }));
    openReplayCmds = after.replay;
    openCmdsDelta = after.cmds - before.cmds;
    openOk = openSharedEntries > 0 && openReplayCmds > 0 && openCmdsDelta > 0
      && openSharedEntries === shareStats.decoded && p2Errors.length === 0;
  }
  await p2.close();
  // ⑥ 404 열기 방어 — 존재하지 않는 id(정규식은 통과) → open_failed + 일반 세션.
  const p3 = await context.newPage();
  const p3Errors = [];
  let expected404Console = 0; // C6 주입(존재하지 않는 id)의 자원 로드 실패 — 브라우저가 남기는 콘솔
  p3.on('pageerror', (e) => { p3Errors.push(e.message); console.error('[p3 pageerror]', e.message); });
  p3.on('console', (m) => {
    if (m.type() === 'error') {
      if (/status of 404/.test(m.text())) { expected404Console++; return; } // 의도적 404 프로브의 예상 결과
      p3Errors.push(m.text()); console.error('[p3 console.error]', m.text());
    }
  });
  await p3.addInitScript((w) => {
    // p2와 같은 접근자 주입(해제 경합 원본 주석은 p2 쪽에 있다).
    let rep = w + '/report';
    Object.defineProperty(window, 'JD_REPORT_URL', { configurable: true, get: () => rep, set: () => {} });
    window.addEventListener('load', () => { rep = ''; });
  }, WORKER);
  await p3.goto(BASE + '?s=abcdefghij', { waitUntil: 'load', timeout: 45000 });
  await p3.waitForFunction(() => window.__jdStats && window.__jdStats().openFailed >= 1, null, { timeout: 15000, polling: 200 });
  const p404 = await p3.evaluate(() => ({
    failed: window.__jdStats().openFailed,
    entries: window.__jdStats().sharedEntries,
  }));
  const open404Ok = p404.failed === 1 && p404.entries === 0 && p3Errors.length === 0;
  await p3.close();
  const coolOk = shareURL2 === shareURL && shareStats.posts === 2; // 413 시도 1 + 성공 1
  const failSurfacedOk = overStatus === 413 && shareStats.failed >= 1;
  const shareOk = shareIdOk && shareLenOk && shareNoPayload && shareStats.ok === 1;
  const roundtripOk = roundtripHashOk && roundtripLenOk;

  // --- 호스트 검증 5b: iOS 제스처 정책 에뮬레이션(2026-09-06 iPhone 실측 재현 — first_tap·first_knob 기록,
  // audioStarted=false, ticks=0). iOS Safari는 touchstart/pointerdown을 오디오 해제 제스처로 인정하지 않고
  // 그때 부른 resume()의 Promise를 영원히 미결로 둔다. 여기서는 AudioContext를 생성 직후 suspend하고,
  // resume()이 pointerup/click 디스패치 중에만 진짜로 통하게 패치해 그 형태를 흉내 낸다.
  // 계약 C7 "인정되지 않는 제스처의 resume 미결이 이후 제스처를 막지 않는다" ↔ 단언:
  //   A16 mouse.down 1.5초 뒤 audioStarted(노드 생성) true, audioState는 아직 running 아님(에뮬레이션 자체 확인)
  //   A17 mouse.up(pointerup+click) 뒤 5초 안에 audioState 'running'·ticks>20
  //   A18 resumeCalls ≥ 2(제스처마다 재시도)·audioStuck 0·pageerror 0
  // FAIL-first(구 host.js, 2026-09-06): start()가 await resume()에 영구 대기 → A16 started=false, A17 ticks=0.
  const p4 = await context.newPage();
  const p4Errors = [];
  p4.on('pageerror', (e) => p4Errors.push(e.message));
  await p4.addInitScript(() => {
    const OrigAC = window.AudioContext;
    const origResume = OrigAC.prototype.resume;
    let unlocked = false;
    const arm = () => { unlocked = true; setTimeout(() => { unlocked = false; }, 0); }; // 디스패치 끝까지만 열림
    window.addEventListener('pointerup', arm, { capture: true });
    window.addEventListener('click', arm, { capture: true });
    function AC(opts) { const c = new OrigAC(opts); c.suspend(); return c; } // 헤드리스 autoplay 허용 무효화
    AC.prototype = OrigAC.prototype;
    window.AudioContext = AC;
    OrigAC.prototype.resume = function () { return unlocked ? origResume.call(this) : new Promise(() => {}); };
  });
  await p4.goto(BASE, { waitUntil: 'load', timeout: 45000 });
  await p4.waitForFunction(() => window.__jdStats && window.__jdStats().tFirstFrame !== null, null, { timeout: 60000 });
  await p4.mouse.move(360, 300);
  await p4.mouse.down();
  await p4.waitForTimeout(1500);
  const iosDown = await p4.evaluate(() => { const s = window.__jdStats(); return { started: s.audioStarted, state: s.audioState, ticks: s.ticks, resumeCalls: s.resumeCalls }; });
  await p4.mouse.up();
  try {
    await p4.waitForFunction(() => window.__jdStats().ticks > 20 && window.__jdStats().audioState === 'running', null, { timeout: 5000, polling: 100 });
  } catch (e) { /* 아래 스냅샷이 FAIL을 말한다 */ }
  const iosUp = await p4.evaluate(() => { const s = window.__jdStats(); return { started: s.audioStarted, state: s.audioState, ticks: s.ticks, resumeCalls: s.resumeCalls, stuck: s.audioStuck }; });
  await p4.close();
  const iosGestureOk = iosDown.started === true && iosDown.state !== 'running'
    && iosUp.state === 'running' && iosUp.ticks > 20 && iosUp.resumeCalls >= 2 && iosUp.stuck === 0 && p4Errors.length === 0;

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
    allocOk,
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
    // 파트별 레벨(P3-levels)
    levelsOk,
    levelPeaks,
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
    // 공유 세션(P2-share)·사람 로그
    humanLogOk,
    humanBefore: humanBefore.human,
    humanAfter: humanAfter.human,
    humanLogLen: humanAfter.logLen,
    humanLogArrLen: humanAfter.logArrLen,
    shareOk,
    shareId,
    shareURL,
    shareLen: typeof shareURL === 'string' ? shareURL.length : null,
    shareStats,
    getBytes,
    roundtripOk,
    coolOk,
    failSurfacedOk,
    overStatus,
    openOk,
    iosGestureOk, iosDown, iosUp,
    openSharedEntries,
    openReplayCmds,
    openCmdsDelta,
    open404Ok,
    open404Failed: p404.failed,
    expected413Console,
    expected404Console,
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
    `share=${j.shareOk ? 'OK id=' + j.shareId + ' ' + j.shareStats.logChars + 'ch' : 'FAIL'} ` +
    `roundtrip=${j.roundtripOk ? 'OK ' + j.getBytes + 'B' : 'FAIL'} ` +
    `open=${j.openOk ? 'OK e=' + j.openSharedEntries + ' rep=' + j.openReplayCmds : 'FAIL'} ` +
    `404=${j.open404Ok ? 'OK' : 'FAIL'} cool=${j.coolOk ? 'OK' : 'FAIL'} 413=${j.failSurfacedOk ? 'OK' : 'FAIL'} ` +
    `human=${j.humanLogOk ? '+' + (j.humanAfter - j.humanBefore) : 'FAIL'} ` +
    `levels=${j.levelsOk ? 'OK' : 'FAIL'}(bd=${j.levelPeaks[2] != null ? j.levelPeaks[2].toFixed(3) : 'n/a'} bassA=${j.levelPeaks[0] != null ? j.levelPeaks[0].toFixed(3) : 'n/a'}) ` +
    `alloc=${j.allocOk ? (j.allocPerFrame != null ? j.allocPerFrame.toFixed(0) + 'B' : '') : 'FAIL ' + j.allocPerFrame}/frame ` +
    `iosGesture=${j.iosGestureOk ? 'OK' : 'FAIL down=' + JSON.stringify(j.iosDown) + ' up=' + JSON.stringify(j.iosUp)} ` +
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
