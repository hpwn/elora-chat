import { describe, expect, it } from 'vitest';
import { normalizeWsPayload } from './normalize';

describe('normalizeWsPayload', () => {
  it('ignores keepalive', () => {
    expect(normalizeWsPayload('__keepalive__')).toBeNull();
  });

  it('parses harvester shape', () => {
    const obj = { author: 'A', message: 'hi', source: 'YouTube', colour: '#808080' };
    const out = normalizeWsPayload(JSON.stringify(obj));
    expect(out?.username).toBe('A');
    expect(out?.text).toBe('hi');
    expect(out?.platform).toBe('YouTube');
    expect(typeof out?.ts).toBe('number');
  });

  it('parses tailer/row shape with json fields', () => {
    const obj = {
      id: 'x1',
      ts: '2025-10-13T06:58:44.876671Z',
      username: 'B',
      platform: 'Test',
      text: 'hello',
      emotes_json: '[]',
      badges_json: '["subscriber/42","bits/100"]',
      raw_json: '{}'
    };
    const out = normalizeWsPayload(obj);
    expect(out?.id).toBe('x1');
    expect(out?.username).toBe('B');
    expect(out?.text).toBe('hello');
    expect(Array.isArray(out?.emotes)).toBe(true);
    expect(Array.isArray(out?.badges)).toBe(true);
    expect(out?.badges).toEqual([
      { id: 'subscriber', version: '42' },
      { id: 'bits', version: '100' }
    ]);
    expect(out && out.ts > 1000000000000).toBe(true);
  });

  it('normalizes badge objects and strings', () => {
    const obj = {
      author: 'BadgeTester',
      message: 'hi',
      badges: [
        'vip/1',
        { id: 'moderator', version: '1' },
        { name: 'founder' }
      ]
    };
    const out = normalizeWsPayload(obj);
    expect(out?.badges).toEqual([
      { id: 'vip', version: '1' },
      { id: 'moderator', version: '1' },
      { id: 'founder' }
    ]);
  });

  it('drops empty frames', () => {
    const obj = { author: '', message: '', source: 'YouTube' };
    expect(normalizeWsPayload(obj)).toBeNull();
  });
});
