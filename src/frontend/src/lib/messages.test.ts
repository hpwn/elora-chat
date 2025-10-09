import { expect, test } from 'vitest';

import { parseWsEvent, parseWsString, KEEPALIVE_TOKENS } from './messages';

const base = {
  author: 'A',
  message: 'hi',
  emotes: [],
  badges: [],
  fragments: [],
  source: 'YouTube' as const,
  colour: '#ffffff'
};

function assertMessageShape(message: unknown) {
  expect(message).toBeTruthy();
  const m = message as Record<string, unknown>;
  expect(typeof m.author).toBe('string');
  expect(typeof m.message).toBe('string');
  expect(typeof m.colour).toBe('string');
  expect(typeof m.source).toBe('string');
  expect(Array.isArray(m.fragments)).toBe(true);
  expect(Array.isArray(m.badges)).toBe(true);
  expect(Array.isArray(m.emotes)).toBe(true);
}

test('keepalive tokens are ignored', () => {
  for (const token of KEEPALIVE_TOKENS) {
    expect(parseWsString(token)).toHaveLength(0);
  }
});

test('raw single object', () => {
  const payload = JSON.stringify(base);
  const messages = parseWsString(payload);
  expect(messages).toHaveLength(1);
  assertMessageShape(messages[0]);
});

test('array of objects', () => {
  const payload = JSON.stringify([base, { ...base, author: 'B' }]);
  const messages = parseWsString(payload);
  expect(messages).toHaveLength(2);
  messages.forEach(assertMessageShape);
});

test('ndjson payload', () => {
  const payload = `${JSON.stringify(base)}\n${JSON.stringify({ ...base, author: 'B' })}\n`;
  const messages = parseWsString(payload);
  expect(messages).toHaveLength(2);
  messages.forEach(assertMessageShape);
});

test('envelope with string data', () => {
  const payload = JSON.stringify({ type: 'chat', data: JSON.stringify(base) });
  const messages = parseWsString(payload);
  expect(messages).toHaveLength(1);
  assertMessageShape(messages[0]);
});

test('envelope with array data', () => {
  const payload = JSON.stringify({ type: 'chat', data: [{ ...base }, { ...base, author: 'B' }] });
  const messages = parseWsString(payload);
  expect(messages).toHaveLength(2);
  messages.forEach(assertMessageShape);
});

test('coerces missing fields safely', () => {
  const payload = JSON.stringify({ author: '', message: 123, fragments: null, emotes: null, badges: null, source: 'Other' });
  const messages = parseWsString(payload);
  expect(messages).toHaveLength(1);
  const [message] = messages;
  expect(message.author).toBe('Unknown');
  expect(message.message).toBe('');
  expect(message.colour).toBe('#ffffff');
  expect(message.source).toBe('YouTube');
  expect(Array.isArray(message.fragments)).toBe(true);
  expect(Array.isArray(message.emotes)).toBe(true);
  expect(Array.isArray(message.badges)).toBe(true);
});

test('binary array buffer payload', async () => {
  const buffer = new TextEncoder().encode(JSON.stringify(base)).buffer;
  const event = new MessageEvent('message', { data: buffer });
  const messages = await parseWsEvent(event);
  expect(messages).toHaveLength(1);
  assertMessageShape(messages[0]);
});

test('blob payload', async () => {
  const blob = new Blob([JSON.stringify(base)], { type: 'application/json' });
  const event = new MessageEvent('message', { data: blob });
  const messages = await parseWsEvent(event);
  expect(messages).toHaveLength(1);
  assertMessageShape(messages[0]);
});
