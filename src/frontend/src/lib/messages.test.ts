import { expect, test } from 'vitest';

import { unwrapWsPayload, parseWsMessages } from './messages';

const msg = {
  author: 'A',
  message: 'hi',
  emotes: [],
  badges: [],
  fragments: [],
  source: 'YouTube',
  colour: ''
};

test('keepalive is ignored', () => {
  expect(unwrapWsPayload('__keepalive__')).toBeNull();
});

test('raw single object', () => {
  const p = JSON.stringify(msg);
  expect(parseWsMessages(p)).toHaveLength(1);
});

test('array of objects', () => {
  const p = JSON.stringify([msg, msg]);
  expect(parseWsMessages(p)).toHaveLength(2);
});

test('ndjson', () => {
  const p = JSON.stringify(msg) + '\n' + JSON.stringify(msg) + '\n';
  expect(parseWsMessages(p)).toHaveLength(2);
});

test('envelope with string data', () => {
  const env = JSON.stringify({ type: 'chat', data: JSON.stringify(msg) });
  const u = unwrapWsPayload(env)!;
  expect(parseWsMessages(u)).toHaveLength(1);
});

test('envelope with array data', () => {
  const env = JSON.stringify({ type: 'chat', data: [msg, msg] });
  const u = unwrapWsPayload(env)!;
  expect(parseWsMessages(u)).toHaveLength(2);
});
