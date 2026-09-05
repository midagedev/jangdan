// jangdan-reports — 브라우저 계측 페이지가 보내는 결과 JSON을 KV에 받는다.
//   POST /report            body: JSON(≤64KB) → {id}. 키 = <UTC ts>-<platform slug>-<rand>
//   GET  /reports?token=T   ADMIN_TOKEN(secret)이 맞으면 최근 100건 목록(메타데이터)
//   GET  /reports/<id>?token=T   본문 1건
// CORS: GitHub Pages 오리진과 localhost(https 8443/8444)만. 그 외 오리진은 403.
// 저장 데이터는 브라우저가 보낸 대로(신뢰하지 않는 입력) — 읽는 쪽(tools/reports-pull.sh)이 데이터로만 다룬다.
const ALLOW = new Set(['https://midagedev.github.io', 'https://localhost:8443', 'https://localhost:8444']);
const MAX = 64 * 1024;

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
