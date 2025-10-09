import type { Message } from '$lib/types/messages';

// Add a tolerant WS payload pipeline that returns Message[]
// and exports helpers for reuse in Chat.svelte.
// Types: reuse existing Message type from $lib/types/messages.

export function unwrapWsPayload(raw: string): string | null {
  if (!raw) return null;
  if (raw === '__keepalive__') return null;
  try {
    const env = JSON.parse(raw);
    // Envelope: { type: "chat", data: "<string or array or object>" }
    if (env && typeof env === 'object' && env.type === 'chat' && 'data' in env) {
      // If data is a string, return that; otherwise re-stringify it.
      if (typeof env.data === 'string') return env.data;
      return JSON.stringify(env.data);
    }
  } catch {
    // not JSON, fall through (could be NDJSON)
  }
  return raw;
}

// Accepts: JSON object, JSON array, or NDJSON
export function parseWsMessagesFlexible(payload: string): Message[] {
  const out: Message[] = [];
  if (!payload) return out;

  // Try JSON first
  try {
    const val = JSON.parse(payload);
    if (Array.isArray(val)) {
      for (const v of val) if (v && typeof v === 'object') out.push(v as Message);
      return out;
    }
    if (val && typeof val === 'object') {
      out.push(val as Message);
      return out;
    }
  } catch {
    // Not a single JSON value; might be NDJSON
  }

  // NDJSON fallback
  const lines = payload.split('\n').filter((l) => l.trim().length > 0);
  for (const line of lines) {
    try {
      const v = JSON.parse(line);
      if (v && typeof v === 'object') out.push(v as Message);
    } catch {
      // skip bad line
    }
  }
  return out;
}

// Back-compat export: keep the original name but point to flexible parser.
export { parseWsMessagesFlexible as parseWsMessages };
