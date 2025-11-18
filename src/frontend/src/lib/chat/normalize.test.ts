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
      badges_raw: { youtube: { authorBadges: [{ id: 1 }] } },
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
    expect(out?.badges_raw).toEqual(obj.badges_raw);
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

  it('retains badge images when provided', () => {
    const obj = {
      author: 'BadgeImages',
      message: 'hi',
      badges: [
        {
          id: 'subscriber',
          version: '12',
          images: [{ id: 'sub', url: 'https://example.com/subscriber.png', width: 18, height: 18 }]
        }
      ]
    };

    const out = normalizeWsPayload(obj);
    expect(out?.badges).toEqual([
      {
        id: 'subscriber',
        version: '12',
        images: [{ id: 'sub', url: 'https://example.com/subscriber.png', width: 18, height: 18 }]
      }
    ]);
  });

  it('drops empty frames', () => {
    const obj = { author: '', message: '', source: 'YouTube' };
    expect(normalizeWsPayload(obj)).toBeNull();
  });

  it('maps youtube member badges to image urls', () => {
    const obj = {
      author: 'MemberUser',
      message: 'hi',
      platform: 'YouTube',
      badges: [{ id: 'member', version: '6 months', platform: 'youtube' }],
      badges_raw: {
        youtube: {
          authorBadges: [
            {
              liveChatAuthorBadgeRenderer: {
                tooltip: 'Member (6 months)',
                customThumbnail: {
                  thumbnails: [
                    { url: 'https://example.com/badge-16.png', width: 16, height: 16 },
                    { url: 'https://example.com/badge-32.png', width: 32, height: 32 }
                  ]
                }
              }
            }
          ]
        }
      }
    };

    const out = normalizeWsPayload(obj);
    expect(out?.displayBadges?.[0]).toMatchObject({
      id: 'member',
      platform: 'youtube',
      imageUrl: 'https://example.com/badge-32.png'
    });
  });

  it('uses wrench icon for youtube moderators without thumbnails', () => {
    const obj = {
      author: 'ModUser',
      message: 'hi',
      platform: 'YouTube',
      badges: [{ id: 'moderator', platform: 'youtube' }],
      badges_raw: {
        youtube: {
          authorBadges: [
            {
              liveChatAuthorBadgeRenderer: {
                icon: { iconType: 'MODERATOR' },
                tooltip: 'Moderator'
              }
            }
          ]
        }
      }
    };

    const out = normalizeWsPayload(obj);
    expect(out?.displayBadges?.[0]).toMatchObject({
      id: 'moderator',
      platform: 'youtube',
      imageUrl: '/images/youtube-moderator.svg'
    });
    expect(out?.displayBadges?.[0].title).toBe('Moderator');
  });

  it('retains twitch badges when no images are present', () => {
    const obj = {
      author: 'TwitchUser',
      message: 'hi',
      platform: 'Twitch',
      badges: [
        { id: 'subscriber', version: '17', platform: 'twitch' },
        { id: 'premium', version: '1', platform: 'twitch' }
      ],
      badges_raw: {
        twitch: {
          badge_info: 'subscriber/17',
          badges: 'subscriber/12,premium/1'
        }
      }
    };

    const out = normalizeWsPayload(obj);
    expect(out?.displayBadges).toEqual([
      { id: 'subscriber', version: '17', platform: 'twitch' },
      { id: 'premium', version: '1', platform: 'twitch' }
    ]);
  });
});
