import { describe, expect, it } from 'vitest';

import { parseWsMessages, unwrapWsPayload } from './messages';

describe('unwrapWsPayload', () => {
  it('returns null for keepalive frames', () => {
    expect(unwrapWsPayload('__keepalive__')).toBeNull();
  });

  it('unwraps chat envelopes', () => {
    const inner = JSON.stringify({ author: 'Elora' });
    const envelope = JSON.stringify({ type: 'chat', data: inner });
    expect(unwrapWsPayload(envelope)).toEqual(inner);
  });

  it('passes through plain chat JSON', () => {
    const message = JSON.stringify({ author: 'Elora', message: 'Hello' });
    expect(unwrapWsPayload(message)).toEqual(message);
  });
});

describe('parseWsMessages integration', () => {
  it('parses messages from unwrapped payload', () => {
    const payload = JSON.stringify({ author: 'Elora', message: 'hello world', source: 'Twitch' });
    const messages = parseWsMessages(payload);
    expect(messages).toHaveLength(1);
    expect(messages[0]?.author).toBe('Elora');
    expect(messages[0]?.message).toBe('hello world');
  });
});
