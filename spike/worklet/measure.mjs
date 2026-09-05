// measure.mjs — playwright로 계측 페이지를 열고 JSON을 회수한다.
// 사용:
//   node measure.mjs --browser chromium --seconds 60 --mult 1
//   node measure.mjs --browser webkit  --seconds 60 --mult 1
//   node measure.mjs --browser chrome  --seconds 60 --mult 1   (헤디드 Chrome)
//   node measure.mjs --browser chromium --offline 30           (오프라인 해시만)
// localhost http 서버를 내부에서 띄운다(localhost는 secure context라 AudioWorklet OK).
// 헤드리스 수치는 "참고" — 기록은 헤디드 Chrome·Safari·iPhone(README 참조).
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const pub = path.join(here, 'public');

const args = process.argv.slice(2);
const opt = (k, d) => {
  const i = args.indexOf(`--${k}`);
  return i >= 0 ? args[i + 1] : d;
};
const browserName = opt('browser', 'chromium');
const seconds = Number(opt('seconds', 60));
const mult = Number(opt('mult', 1));
const offline = opt('offline', null) ? Number(opt('offline')) : null;

const MIME = { '.html': 'text/html', '.js': 'text/javascript', '.wasm': 'application/wasm' };
const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  const f = path.join(pub, u.pathname === '/' ? 'index.html' : u.pathname);
  fs.readFile(f, (err, data) => {
    if (err) { res.writeHead(404); res.end(); return; }
    res.writeHead(200, { 'content-type': MIME[path.extname(f)] || 'application/octet-stream' });
    res.end(data);
  });
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const port = server.address().port;
const url = `http://127.0.0.1:${port}/`;
console.error(`serving ${url} (${browserName}${offline ? ` offline=${offline}` : ` seconds=${seconds} mult=${mult}`})`);

const { chromium, webkit } = await import('playwright');
const opts = { headless: browserName !== 'chrome' };
if (browserName === 'chrome') opts.channel = 'chrome';
if (browserName === 'chromium' || browserName === 'chrome') {
  opts.args = ['--autoplay-policy=no-user-gesture-required'];
}
const pw = browserName === 'webkit' ? webkit : chromium;
const browser = await pw.launch(opts);
const page = await browser.newPage();
page.on('console', (m) => { if (m.type() === 'error') console.error('[page error]', m.text()); });

try {
  await page.goto(url, { waitUntil: 'load', timeout: 30000 });
  if (offline != null) {
    await page.waitForFunction(() => typeof window.__runOffline === 'function', null, { timeout: 10000 });
    const t0 = Date.now();
    await page.evaluate((n) => window.__runOffline(n), offline);
    const j = await page.evaluate(() => window.__collectJSON());
    const line = `[offline ${browserName} ${offline}s] hash=${j.offlineHash} (${((Date.now() - t0) / 1000).toFixed(1)}s 렌더)`;
    console.log(line);
    await save(j, `offline${offline}`);
  } else {
    await page.selectOption('#mult', String(mult));
    await page.click('#start');
    await page.waitForTimeout(seconds * 1000 + 1500);
    const j = await page.evaluate(() => window.__collectJSON());
    const line =
      `[rt ${browserName} ${seconds}s mult=${mult}] renderUsMean=${j.renderUsMean?.toFixed(1)} ` +
      `max=${j.renderUsMax?.toFixed(1)} p99≈${j.renderUsP99?.toFixed(0)} loadPctMean=${j.loadPctMean?.toFixed(2)} ` +
      `stalls=${j.stalls} ctxSR=${j.contextSampleRate} timer=${j.timerSource} ` +
      `renderCapacity=${j.renderCapacity ? `avg=${(j.renderCapacity.averageLoadMean * 100).toFixed(1)}% peak=${(j.renderCapacity.peakLoadMax * 100).toFixed(1)}% underrun=${j.renderCapacity.underrunRatioMax}` : 'null'}`;
    console.log(line);
    await save(j, `rt${seconds}-m${mult}`);
  }
} finally {
  await browser.close();
  server.close();
}

async function save(j, tag) {
  const dir = path.join(here, 'results');
  fs.mkdirSync(dir, { recursive: true });
  const ts = new Date().toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15);
  const file = path.join(dir, `${ts}-${browserName}-${tag}.json`);
  fs.writeFileSync(file, JSON.stringify(j, null, 2));
  console.log(`saved: ${path.relative(here, file)}`);
}
