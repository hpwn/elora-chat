import { getWsUrl, isFetchHistoryOnLoad } from '$lib/config';
import { normalizeWsPayloads, type ChatMessage } from './normalize';

export type { ChatMessage } from './normalize';

export type OnMessage = (m: ChatMessage) => void;

const textDecoder = new TextDecoder();

let __latestOnMessage: OnMessage | null = null;
export function __pushMockMessage(m: ChatMessage){__latestOnMessage?.(m);}

export function connectChat(onMessage: OnMessage, url = defaultWsUrl()): WebSocket {
  __latestOnMessage = onMessage;

  const ws = new WebSocket(withReplayParam(url));
  ws.binaryType = 'arraybuffer';

  ws.onmessage = (evt) => {
    deliverPayload(evt.data, onMessage);
  };

  ws.onerror = () => {};
  return ws;
}

function deliverPayload(data: unknown, onMessage: OnMessage): void {
  if (typeof data === 'string') {
    emitMessages(normalizeWsPayloads(data), onMessage);
    return;
  }

  if (ArrayBuffer.isView(data)) {
    const { buffer, byteOffset, byteLength } = data;
    const view = new Uint8Array(buffer, byteOffset, byteLength);
    emitMessages(normalizeWsPayloads(textDecoder.decode(view)), onMessage);
    return;
  }

  if (data instanceof ArrayBuffer) {
    emitMessages(normalizeWsPayloads(textDecoder.decode(new Uint8Array(data))), onMessage);
    return;
  }

  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    data
      .text()
      .then((text) => emitMessages(normalizeWsPayloads(text), onMessage))
      .catch(() => {});
    return;
  }

  emitMessages(normalizeWsPayloads(data), onMessage);
}

function emitMessages(messages: ChatMessage[], onMessage: OnMessage): void {
  for (const message of messages) {
    onMessage(message);
  }
}

export function defaultWsUrl(): string {
  const configuredWsUrl = getWsUrl();
  if (configuredWsUrl) return configuredWsUrl;
  if (typeof location === 'undefined') {
    return 'ws://localhost:8080/ws/chat';
  }
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  return `${proto}://${location.host}/ws/chat`;
}

function withReplayParam(rawUrl: string): string {
  const replay = isFetchHistoryOnLoad() ? '1' : '0';
  try {
    const base = typeof location !== 'undefined' ? location.origin : 'http://localhost';
    const url = new URL(rawUrl, base);
    url.searchParams.set('replay', replay);
    return url.toString();
  } catch {
    const separator = rawUrl.includes('?') ? '&' : '?';
    return `${rawUrl}${separator}replay=${replay}`;
  }
}
