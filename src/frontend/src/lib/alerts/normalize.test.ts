import { describe, expect, it } from 'vitest';
import { describeAlert, normalizeAlertPayload } from './normalize';

describe('normalizeAlertPayload', () => {
  it('normalizes twitch alert payloads', () => {
    const out = normalizeAlertPayload({
      id: 'tw-1',
      ts: 1735770000,
      platform: 'twitch',
      type: 'twitch.subs',
      username: 'alice',
      source_channel: 'dayoman'
    });

    expect(out).toMatchObject({
      id: 'tw-1',
      platform: 'twitch',
      type: 'subs',
      username: 'alice',
      sourceChannel: 'dayoman'
    });
    expect(out && out.ts > 1_000_000_000_000).toBe(true);
  });

  it('normalizes youtube alert payload envelopes', () => {
    const out = normalizeAlertPayload({
      type: 'alert',
      data: JSON.stringify({
        id: 'yt-1',
        created_at: '2026-02-24T12:00:00Z',
        source: 'youtube',
        kind: 'gifted_members',
        author: 'bob',
        source_url: 'https://www.youtube.com/@example/live'
      })
    });

    expect(out).toMatchObject({
      id: 'yt-1',
      platform: 'youtube',
      type: 'gifted_members',
      username: 'bob',
      sourceUrl: 'https://www.youtube.com/@example/live'
    });
  });

  it('returns null for unknown alert type', () => {
    const out = normalizeAlertPayload({
      platform: 'twitch',
      type: 'twitch.unknown',
      username: 'nobody'
    });
    expect(out).toBeNull();
  });
});

describe('describeAlert', () => {
  it('formats alert text with amount and message', () => {
    const text = describeAlert({
      id: 'a1',
      ts: Date.now(),
      platform: 'youtube',
      type: 'super_chats',
      username: 'charlie',
      amount: 5,
      currency: 'USD',
      message: 'great stream'
    });

    expect(text).toContain('charlie');
    expect(text).toContain('super chat');
    expect(text).toContain('5 USD');
    expect(text).toContain('great stream');
  });
});
