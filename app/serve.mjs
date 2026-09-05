// serve.mjs — 의존 0 정적 https 서버(app/web) + POST /report → app/results.
// spike/worklet/serve.mjs와 같은 방식(포트만 8444). AudioWorklet이 https를
// 요구하므로 자체서명 인증서를 .cert/에 자동 생성한다.
// 사전압축 서빙: .br/.gz가 있고 Accept-Encoding이 받아주면 Content-Encoding으로
// 내보낸다 — 4G 로딩 추정(app.wasm 전송 크기)에 필요. .br/.gz를 그 이름으로
// 직접 요청하면 원본 바이트를 돌려준다(크기 재기용, Content-Encoding 없음).
import https from 'node:https';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const web = path.join(here, 'web');
const resultsDir = path.join(here, 'results');
const certDir = path.join(here, '.cert');
const PORT = 8444;

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.wasm': 'application/wasm',
  '.json': 'application/json',
  '.png': 'image/png',
  '.txt': 'text/plain; charset=utf-8',
};

function lanIP() {
  for (const ifs of Object.values(os.networkInterfaces())) {
    for (const i of ifs) {
      if (i.family === 'IPv4' && !i.internal) return i.address;
    }
  }
  return '127.0.0.1';
}

function ensureCert() {
  fs.mkdirSync(certDir, { recursive: true });
  const key = path.join(certDir, 'key.pem');
  const crt = path.join(certDir, 'cert.pem');
  if (fs.existsSync(key) && fs.existsSync(crt)) return { key, crt };
  console.log(`인증서 생성(/CN=jangdan-ui, SAN=IP:${lanIP()},DNS:localhost)…`);
  execFileSync('openssl', [
    'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
    '-keyout', key, '-out', crt,
    '-subj', '/CN=jangdan-ui',
    '-addext', `subjectAltName=IP:${lanIP()},DNS:localhost`,
    '-days', '30',
  ]);
  return { key, crt };
}

const { key, crt } = ensureCert();
fs.mkdirSync(resultsDir, { recursive: true });

const server = https.createServer(
  { key: fs.readFileSync(key), cert: fs.readFileSync(crt) },
  (req, res) => {
    if (req.method === 'POST' && req.url === '/report') {
      let body = '';
      req.on('data', (c) => { body += c; });
      req.on('end', () => {
        let slug = 'unknown';
        try {
          const j = JSON.parse(body);
          const b = String(j.browser || j.platform || 'unknown').toLowerCase().replace(/[^a-z0-9]+/g, '-').slice(0, 24);
          slug = b || 'unknown';
        } catch { /* 원문 저장이라도 */ }
        const ts = new Date().toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15);
        const file = path.join(resultsDir, `${ts}-${slug}.json`);
        fs.writeFileSync(file, body);
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ ok: true, file: path.relative(here, file) }));
        console.log('report saved:', file);
      });
      return;
    }

    const url = new URL(req.url, 'https://x');
    let file = path.join(web, url.pathname === '/' ? 'index.html' : url.pathname);
    file = path.normalize(file);
    if (!file.startsWith(web)) { res.writeHead(403); res.end(); return; }
    fs.readFile(file, (err, data) => {
      if (err) {
        console.log(`404 ${url.pathname}`);
        res.writeHead(404);
        res.end('not found');
        return;
      }
      // 계측 페이지는 매번 최신 JS·wasm을 받아야 한다(캐시 금지).
      const head = {
        'content-type': MIME[path.extname(file)] || 'application/octet-stream',
        'cache-control': 'no-store',
        vary: 'accept-encoding',
      };
      const enc = req.headers['accept-encoding'] || '';
      let body = data;
      if (path.extname(file) !== '.br' && path.extname(file) !== '.gz') {
        // 사전압축 파일이 있고 브라우저가 받아주면 그대로 Content-Encoding으로 서빙.
        // content-length는 전송 표현(압축된 바이트) 기준이어야 한다 — 원본 크기를
        // 쓰면 브라우저가 남은 바이트를 기다리다 로딩이 막힌다.
        if (/\bbr\b/.test(enc) && fs.existsSync(file + '.br')) {
          head['content-encoding'] = 'br';
          body = fs.readFileSync(file + '.br');
        } else if (/\bgzip\b/.test(enc) && fs.existsSync(file + '.gz')) {
          head['content-encoding'] = 'gzip';
          body = fs.readFileSync(file + '.gz');
        }
      }
      head['content-length'] = body.length;
      res.writeHead(200, head).end(body);
    });
  }
);
server.listen(PORT, () => {
  console.log(`https://localhost:${PORT}  (LAN: https://${lanIP()}:${PORT})`);
});
