import { wsUrl as configuredWsUrl } from '$lib/config';
import { normalizeWsPayloads, type ChatMessage } from './normalize';

export type OnMessage = (m: ChatMessage) => void;

const textDecoder = new TextDecoder();

export function connectChat(onMessage: OnMessage, url = defaultWsUrl()): WebSocket {
  const ws = new WebSocket(url);
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
    const slice = buffer.slice(byteOffset, byteOffset + byteLength);
    emitMessages(normalizeWsPayloads(textDecoder.decode(slice)), onMessage);
    return;
  }

  if (data instanceof ArrayBuffer) {
    emitMessages(normalizeWsPayloads(textDecoder.decode(data)), onMessage);
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
  if (configuredWsUrl) {
    return configuredWsUrl;
  }
  if (typeof location === 'undefined') {
    return 'ws://localhost:8080/ws/chat';
  }
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  return `${proto}://${location.host}/ws/chat`;
}
