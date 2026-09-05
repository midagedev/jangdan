// serve.mjs — 의존 0 정적 https 서버 + POST /report 저장.
// AudioWorklet은 Secure Context 전용: localhost는 http로도 되지만
// iPhone에서 LAN IP로 접속하려면 https가 필요하다(자체서명 자동 생성).
import http from 'node:http';
import https from 'node:https';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const pub = path.join(here, 'public');
const resultsDir = path.join(here, 'results');
const certDir = path.join(here, '.cert');
const PORT = 8443;

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.wasm': 'application/wasm',
  '.json': 'application/json',
  '.png': 'image/png',
};

// 첫 IPv4 비루프백 = LAN IP
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
  const ip = lanIP();
  console.log(`인증서 생성(/CN=jangdan-spike, SAN=IP:${ip},DNS:localhost)…`);
  execFileSync('openssl', [
    'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
    '-keyout', key, '-out', crt,
    '-subj', '/CN=jangdan-spike',
    '-addext', `subjectAltName=IP:${ip},DNS:localhost`,
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
          const p = String(j.platform || 'unknown').toLowerCase().replace(/[^a-z0-9]+/g, '-').slice(0, 24) || 'unknown';
          slug = p;
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
    let file = path.join(pub, url.pathname === '/' ? 'index.html' : url.pathname);
    file = path.normalize(file);
    if (!file.startsWith(pub)) { res.writeHead(403); res.end(); return; }
    fs.readFile(file, (err, data) => {
      if (err) { res.writeHead(404); res.end('not found'); return; }
      // 계측 페이지는 매번 최신 JS·wasm을 받아야 한다 — 캐시 금지(시뮬레이터·iPhone Safari가 구 스크립트로 측정하는 것을 막는다).
      res.writeHead(200, { 'content-type': MIME[path.extname(file)] || 'application/octet-stream', 'cache-control': 'no-store' });
      res.end(data);
    });
  }
);
server.listen(PORT, () => {
  console.log(`https://localhost:${PORT}  (LAN: https://${lanIP()}:${PORT})`);
  console.log('iPhone: 같은 Wi-Fi에서 LAN 주소 접속 → 자체서명 경고 통과(README 참조)');
});
