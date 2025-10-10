import type { Message } from '$lib/types/messages';

export const KEEPALIVE_TOKENS = new Set<string>(['__keepalive__']);

const DEFAULT_COLOUR = '#ffffff';
const DEFAULT_SOURCE: Message['source'] = 'YouTube';

const textDecoder = new TextDecoder();

const DEBUG = typeof import.meta !== 'undefined' && (import.meta as any).env?.VITE_CHAT_DEBUG === '1';

function debugLog(...args: unknown[]) {
  if (DEBUG) console.debug('[chat:parser]', ...args);
}

type Envelope = {
  type?: string;
  data?: unknown;
};

export async function parseWsEvent(ev: MessageEvent): Promise<Message[]> {
  const { data } = ev;

  if (typeof data === 'string') {
    return parseWsString(data);
  }

  if (ArrayBuffer.isView(data)) {
    const { buffer, byteOffset, byteLength } = data;
    const sliced = buffer.slice(byteOffset, byteOffset + byteLength);
    return parseWsString(textDecoder.decode(sliced));
  }

  if (data instanceof ArrayBuffer) {
    return parseWsString(textDecoder.decode(data));
  }

  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    try {
      const text = await data.text();
      return parseWsString(text);
    } catch (error) {
      debugLog('failed to read Blob payload', error);
      return [];
    }
  }

  if (data == null) {
    return [];
  }

  if ((data as { toString?: () => string }).toString) {
    return parseWsString(String(data));
  }

  debugLog('unhandled WS payload type', typeof data);
  return [];
}

export function parseWsString(payload: string): Message[] {
  if (!payload) return [];

  const trimmed = payload.trim();
  if (!trimmed) return [];
  if (KEEPALIVE_TOKENS.has(trimmed)) return [];

  const parsed = tryParseJson(trimmed);
  if (parsed.success) {
    return normalizeParsedValue(parsed.value);
  }

  const messages: Message[] = [];
  const lines = trimmed.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);

  for (const line of lines) {
    if (KEEPALIVE_TOKENS.has(line)) continue;
    const lineParsed = tryParseJson(line);
    if (!lineParsed.success) {
      debugLog('skipping unparsable NDJSON line', lineParsed.error);
      continue;
    }
    messages.push(...normalizeParsedValue(lineParsed.value));
  }

  return messages;
}

function tryParseJson(input: string): { success: true; value: unknown } | { success: false; error: unknown } {
  try {
    return { success: true, value: JSON.parse(input) };
  } catch (error) {
    return { success: false, error };
  }
}

function normalizeParsedValue(value: unknown): Message[] {
  if (value == null) {
    return [];
  }

  if (Array.isArray(value)) {
    return value.flatMap((entry) => normalizeParsedValue(entry));
  }

  if (typeof value === 'string') {
    return parseWsString(value);
  }

  if (typeof value === 'object') {
    const envelope = value as Envelope;
    if (envelope.type === 'chat' && 'data' in envelope) {
      if (typeof envelope.data === 'string') {
        return parseWsString(envelope.data);
      }
      return normalizeParsedValue(envelope.data);
    }

    const message = coerceMessage(value as Record<string, unknown>);
    return message ? [message] : [];
  }

  return [];
}

function coerceMessage(raw: Record<string, unknown>): Message | null {
  const author = typeof raw.author === 'string' && raw.author.trim().length > 0 ? raw.author : 'Unknown';
  const message = typeof raw.message === 'string' ? raw.message : '';
  const colour = typeof raw.colour === 'string' && raw.colour.trim().length > 0 ? raw.colour : DEFAULT_COLOUR;

  const allowedSources: Message['source'][] = ['YouTube', 'Twitch', 'Test'];
  const rawSource = typeof raw.source === 'string' ? (raw.source as Message['source']) : undefined;
  const source = rawSource && allowedSources.includes(rawSource) ? rawSource : DEFAULT_SOURCE;

  const fragments = Array.isArray(raw.fragments) ? (raw.fragments as Message['fragments']) : [];
  const emotes = Array.isArray(raw.emotes) ? (raw.emotes as Message['emotes']) : [];
  const badges = Array.isArray(raw.badges) ? (raw.badges as Message['badges']) : [];

  return {
    author,
    message,
    colour,
    source,
    fragments,
    emotes,
    badges
  } satisfies Message;
}
