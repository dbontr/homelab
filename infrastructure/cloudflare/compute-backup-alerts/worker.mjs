const permittedConditions = new Set([
  'backup_failure', 'missed_backup', 'repeated_failures',
  'insufficient_capacity', 'corrupt_snapshot',
]);
const encoder = new TextEncoder();

function equalHex(a, b) {
  if (a.length !== b.length || !/^[0-9a-f]{64}$/i.test(a) || !/^[0-9a-f]{64}$/i.test(b)) return false;
  let difference = 0;
  for (let i = 0; i < 64; i++) difference |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return difference === 0;
}

async function authenticated(request, expectedHash) {
  const header = request.headers.get('Authorization') || '';
  const match = /^Bearer ([a-f0-9]{64})$/.exec(header);
  if (!match || typeof expectedHash !== 'string') return false;
  // The configured expected digest includes the token file's trailing newline.
  const digest = await crypto.subtle.digest('SHA-256', encoder.encode(match[1] + '\n'));
  const hex = Array.from(new Uint8Array(digest), n => n.toString(16).padStart(2, '0')).join('');
  return equalHex(hex, expectedHash);
}

const noStore = { 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff' };
const answer = (body, status) => new Response(body, {status, headers: noStore});

export default {
  async fetch(request, env) {
    const path = new URL(request.url).pathname;
    if (path === '/healthz' && request.method === 'GET') return answer('ok\n', 200);
    if (path !== '/hooks/compute') return answer('not found\n', 404);
    if (request.method !== 'POST') return answer('method not allowed\n', 405);
    if (!await authenticated(request, env.TOKEN_SHA256)) return answer('unauthorized\n', 401);
    if (!env.ALERT_EVENTS || !env.NTFY_TOPIC || !/^[A-Za-z0-9_-]{30,64}$/.test(env.NTFY_TOPIC)) {
      return answer('notification destination unavailable\n', 503);
    }
    if (!(request.headers.get('content-type') || '').startsWith('application/json')) return answer('invalid content type\n', 415);
    let raw;
    try { raw = await request.text(); } catch { return answer('invalid request\n', 400); }
    if (raw.length > 2048) return answer('request too large\n', 413);
    let event;
    try { event = JSON.parse(raw); } catch { return answer('invalid JSON\n', 400); }
    if (!event || event.source !== 'compute-backup' || !permittedConditions.has(event.condition) ||
        !['firing', 'resolved'].includes(event.status) || typeof event.observed_at !== 'string' ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(event.observed_at) ||
        !Number.isFinite(Date.parse(event.observed_at))) return answer('invalid event\n', 400);

    const key = 'state:' + event.source + ':' + event.condition;
    try {
      const last = await env.ALERT_EVENTS.get(key);
      if (last === event.status) return answer('already delivered\n', 202);
      const notification = new Request('https://ntfy.sh/' + env.NTFY_TOPIC, {
        method: 'POST',
        headers: { 'Title': 'Homelab Compute backup', 'Priority': event.status === 'firing' ? 'high' : 'default' },
        body: event.condition + ' ' + event.status + ' at ' + event.observed_at,
      });
      const delivered = await fetch(notification);
      if (!delivered.ok) return answer('notification delivery failed\n', 503);
      const id = crypto.randomUUID();
      await env.ALERT_EVENTS.put('event:' + id, JSON.stringify({...event, received_at: new Date().toISOString()}));
      await env.ALERT_EVENTS.put(key, event.status);
      return answer('delivered\n', 200);
    } catch {
      return answer('notification persistence unavailable\n', 503);
    }
  },
};
