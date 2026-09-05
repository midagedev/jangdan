// jangdan-reports — 브라우저 계측 페이지가 보내는 결과 JSON을 KV에 받는다.
//   POST /report            body: JSON(≤64KB) → {id}. 키 = <UTC ts>-<platform slug>-<rand>
//   GET  /reports?token=T   ADMIN_TOKEN(secret)이 맞으면 최근 100건 목록(메타데이터)
//   GET  /reports/<id>?token=T   본문 1건
//   POST /sessions          body: JSON(≤256KB) {v, seed, word, log, state, meta} → {id}. 공유 세션 저장(쓰기 1회, 수정 없음).
//                           키 = <base62 10자>. URL은 ?s=<id>만 싣는다(사용자 결정 2026-09-06 — 로그를 URL에 넣지 않는다).
//   GET  /sessions/<id>     본문(오리진 무관 — 링크를 가진 누구나; 불변이라 1년 캐시)
// CORS: GitHub Pages 오리진과 localhost(https 8443/8444)만. 그 외 오리진은 403.
// 저장 데이터는 브라우저가 보낸 대로(신뢰하지 않는 입력) — 읽는 쪽(tools/reports-pull.sh)이 데이터로만 다룬다.
const ALLOW = new Set(['https://midagedev.github.io', 'https://localhost:8443', 'https://localhost:8444']);
const MAX = 64 * 1024;
const MAX_SESSION = 256 * 1024;
const SESSION_TTL = 60 * 60 * 24 * 365;
const ID_ALPHABET = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
const ID_RE = /^[0-9A-Za-z]{10}$/;

function newId() {
  const b = crypto.getRandomValues(new Uint8Array(10));
  let s = '';
  for (let i = 0; i < 10; i++) s += ID_ALPHABET[b[i] % 62];
  return s;
}


function cors(origin) {
  return {
    'access-control-allow-origin': origin,
    'access-control-allow-methods': 'POST, GET, OPTIONS',
    'access-control-allow-headers': 'content-type',
    'access-control-max-age': '86400',
    'vary': 'origin',
  };
}
const json = (obj, status = 200, extra = {}) =>
  new Response(JSON.stringify(obj), { status, headers: { 'content-type': 'application/json', ...extra } });

export default {
  async fetch(req, env) {
    const url = new URL(req.url);
    const origin = req.headers.get('origin') || '';
    const allowed = ALLOW.has(origin);
    const h = allowed ? cors(origin) : {};

    if (req.method === 'OPTIONS') return new Response(null, { status: allowed ? 204 : 403, headers: h });

    if (req.method === 'POST' && url.pathname === '/report') {
      if (!allowed) return json({ error: 'origin not allowed' }, 403);
      const len = +(req.headers.get('content-length') || 0);
      if (len > MAX) return json({ error: 'too large' }, 413, h);
      const text = await req.text();
      if (text.length > MAX) return json({ error: 'too large' }, 413, h);
      let body;
      try { body = JSON.parse(text); } catch { return json({ error: 'invalid json' }, 400, h); }
      if (typeof body !== 'object' || body === null || Array.isArray(body)) return json({ error: 'object expected' }, 400, h);
      const ts = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z');
      const slug = String(body.platform || body.ua || 'unknown').toLowerCase().replace(/[^a-z0-9]+/g, '-').slice(0, 24).replace(/^-|-$/g, '') || 'unknown';
      const id = `${ts}-${slug}-${crypto.randomUUID().slice(0, 6)}`;
      const record = { ...body, _receivedAt: new Date().toISOString(), _country: req.cf?.country || null, _origin: origin, _kind: url.searchParams.get('kind') || 'worklet' };
      await env.REPORTS.put(id, JSON.stringify(record), {
        metadata: { platform: body.platform || null, kind: record._kind, load: body.loadPctMean ?? null, stalls: body.stalls ?? null, receivedAt: record._receivedAt },
        expirationTtl: 60 * 60 * 24 * 180,
      });
      return json({ id }, 200, h);
    }

    if (req.method === 'POST' && url.pathname === '/sessions') {
      if (!allowed) return json({ error: 'origin not allowed' }, 403);
      const len = +(req.headers.get('content-length') || 0);
      if (len > MAX_SESSION) return json({ error: 'too large' }, 413, h);
      const text = await req.text();
      if (text.length > MAX_SESSION) return json({ error: 'too large' }, 413, h);
      let body;
      try { body = JSON.parse(text); } catch { return json({ error: 'invalid json' }, 400, h); }
      if (typeof body !== 'object' || body === null || Array.isArray(body)) return json({ error: 'object expected' }, 400, h);
      if (typeof body.log !== 'string' || body.log.length === 0) return json({ error: 'log (string) required' }, 400, h);
      // 재정규화: 저장하는 필드만 골라 담는다(브라우저가 보낸 여분 필드는 버린다).
      const rec = {
        v: body.v | 0, seed: body.seed >>> 0, word: typeof body.word === 'string' ? body.word.slice(0, 64) : '',
        log: body.log, state: typeof body.state === 'string' ? body.state : null,
        meta: typeof body.meta === 'object' && body.meta !== null ? body.meta : {},
        _receivedAt: new Date().toISOString(), _country: req.cf?.country || null, _origin: origin,
      };
      // id 충돌은 62^10 공간에서 사실상 없지만, 있으면 새로 뽑는다(덮어쓰기 금지 — 세션은 불변).
      let id = newId();
      for (let tries = 0; tries < 3 && (await env.SESSIONS.get(id)) !== null; tries++) id = newId();
      await env.SESSIONS.put(id, JSON.stringify(rec), {
        metadata: { receivedAt: rec._receivedAt, bytes: text.length, seed: rec.seed },
        expirationTtl: SESSION_TTL,
      });
      return json({ id }, 200, h);
    }

    if (req.method === 'GET' && url.pathname.startsWith('/sessions/')) {
      const id = url.pathname.slice('/sessions/'.length);
      if (!ID_RE.test(id)) return json({ error: 'bad id' }, 400, allowed ? h : {});
      const v = await env.SESSIONS.get(id);
      if (v === null) return json({ error: 'not found' }, 404, allowed ? h : {});
      // 공개 읽기: 링크를 가진 누구나. 불변이므로 길게 캐시한다.
      const extra = { 'cache-control': 'public, max-age=31536000, immutable' };
      if (allowed) Object.assign(extra, h); else extra['access-control-allow-origin'] = '*';
      return new Response(v, { headers: { 'content-type': 'application/json', ...extra } });
    }

    if (req.method === 'GET' && url.pathname.startsWith('/reports')) {
      const token = url.searchParams.get('token');
      if (!env.ADMIN_TOKEN || token !== env.ADMIN_TOKEN) return json({ error: 'unauthorized' }, 401);
      const id = url.pathname.slice('/reports/'.length);
      if (id) {
        const v = await env.REPORTS.get(id);
        return v ? new Response(v, { headers: { 'content-type': 'application/json' } }) : json({ error: 'not found' }, 404);
      }
      const list = await env.REPORTS.list({ limit: 100 });
      return json({ keys: list.keys.map((k) => ({ id: k.name, ...k.metadata })), complete: list.list_complete });
    }

    return json({ ok: true, service: 'jangdan-reports' });
  },
};
