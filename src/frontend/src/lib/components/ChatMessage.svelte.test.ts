import { render, screen } from '@testing-library/svelte';
import { SvelteSet } from 'svelte/reactivity';
import { describe, expect, test, vi } from 'vitest';

import ChatMessage from './ChatMessage.svelte';
import { FragmentType, type Message } from '$lib/types/messages';

describe('ChatMessage', () => {
  test('renders fallback text when fragments are empty', () => {
    const message: Message = {
      author: 'Tester',
      message: 'plain text fallback',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [],
      emotes: [],
      fragments: []
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    expect(screen.getByText('plain text fallback')).toBeInTheDocument();
  });

  test('renders badge icons with fallback labels', () => {
    const message: Message = {
      author: 'BadgeUser',
      message: 'hi',
      colour: '#ffffff',
      source: 'Twitch',
      badges: [
        { id: 'subscriber', version: '42' },
        { id: 'bits', version: '1000' },
        { id: 'unknown', version: 'x' }
      ],
      emotes: [],
      fragments: []
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    expect(screen.getByAltText('Subscriber 42')).toBeInTheDocument();
    expect(screen.getByAltText('Bits 1000')).toBeInTheDocument();
    const fallback = screen.getByText('X');
    expect(fallback).toBeInTheDocument();
    expect(fallback).toHaveAttribute('title', 'Unknown x');
  });

  test('renders badge images when provided in payload', () => {
    const message: Message = {
      author: 'BadgeIcons',
      message: 'hi',
      colour: '#ffffff',
      source: 'Twitch',
      badges: [
        {
          id: 'subscriber',
          version: '6',
          images: [{ id: 'sub', url: 'https://example.com/sub.png', width: 18, height: 18 }]
        },
        {
          id: 'moderator',
          images: [{ id: 'mod', url: 'https://example.com/mod.png', width: 18, height: 18 }]
        }
      ],
      emotes: [],
      fragments: []
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const subBadge = screen.getByAltText('Subscriber 6') as HTMLImageElement;
    expect(subBadge.src).toContain(encodeURIComponent('https://example.com/sub.png'));
    const modBadge = screen.getByAltText('Moderator') as HTMLImageElement;
    expect(modBadge.src).toContain(encodeURIComponent('https://example.com/mod.png'));
  });

  test('renders youtube badge thumbnails when provided', () => {
    const message: Message = {
      author: 'YTUser',
      message: 'hello',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [],
      displayBadges: [
        {
          id: 'member',
          platform: 'YouTube',
          imageUrl: 'https://example.com/badge-large.png',
          title: 'Member badge'
        }
      ],
      emotes: [],
      fragments: []
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const badgeImg = screen.getByAltText('Member badge') as HTMLImageElement;
    expect(badgeImg).toBeInTheDocument();
    expect(badgeImg.src).toContain(encodeURIComponent('https://example.com/badge-large.png'));
  });

  test('renders youtube broadcaster badge as a label when no image exists', () => {
    const message: Message = {
      author: 'OwnerUser',
      message: 'hi',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [
        {
          id: 'broadcaster',
          platform: 'youtube'
        }
      ],
      emotes: [],
      fragments: []
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    expect(screen.queryByAltText('Broadcaster')).not.toBeInTheDocument();
    expect(screen.getByText('HOST', { selector: '.badge-label' })).toBeInTheDocument();
  });

  test('renders youtube moderator badge as shield icon instead of image/text label', () => {
    const message: Message = {
      author: 'ImageBadgeUser',
      message: 'hi',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [],
      displayBadges: [
        {
          id: 'moderator',
          platform: 'youtube',
          imageUrl: '/assets/badges/yt-mod-wrench.svg',
          title: 'Moderator'
        }
      ],
      emotes: [],
      fragments: []
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const badgeIcon = screen.getByLabelText('Moderator');
    expect(badgeIcon.tagName).toBe('SPAN');
    expect(badgeIcon).toHaveAttribute('data-badge-glyph', 'moderator');
    expect(screen.queryByText('Moderator', { selector: '.badge-label' })).not.toBeInTheDocument();
  });

  test('renders youtube moderator shield badge from normalized display badges', () => {
    const message: Message = {
      author: 'ModUser',
      message: 'hi',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [],
      displayBadges: [
        {
          id: 'moderator',
          platform: 'youtube',
          imageUrl: '/assets/badges/yt-mod-wrench.svg',
          images: [{ id: 'yt-mod', url: '/assets/badges/yt-mod-wrench.svg', width: 16, height: 16 }],
          title: 'Moderator'
        }
      ],
      emotes: [],
      fragments: [{ type: FragmentType.Text, text: 'hi', emote: null }]
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const badgeIcon = screen.getByLabelText('Moderator');
    expect(badgeIcon).toBeInTheDocument();
    expect(badgeIcon).toHaveAttribute('data-badge-glyph', 'moderator');
  });

  test('falls back to shield icon when youtube moderator badge lacks images', () => {
    const message: Message = {
      author: 'FallbackMod',
      message: 'hi',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [],
      displayBadges: [
        {
          id: 'moderator',
          platform: 'youtube'
        }
      ],
      emotes: [],
      fragments: [{ type: FragmentType.Text, text: 'hi', emote: null }]
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const badgeIcon = screen.getByLabelText('Moderator');
    expect(badgeIcon).toBeInTheDocument();
    expect(badgeIcon).toHaveAttribute('data-badge-glyph', 'moderator');
    expect(screen.queryByText('Moderator', { selector: '.badge-label' })).not.toBeInTheDocument();
  });

  test('renders youtube verified icon for VER badge placeholder', () => {
    const message: Message = {
      author: 'VerifiedUser',
      message: 'hi',
      colour: '#ffffff',
      source: 'YouTube',
      badges: [],
      displayBadges: [
        {
          code: 'VER',
          text: 'VER',
          platform: 'youtube'
        } as any
      ],
      emotes: [],
      fragments: [{ type: FragmentType.Text, text: 'hi', emote: null }]
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const verifiedIcon = screen.getByLabelText('Verified');
    expect(verifiedIcon).toBeInTheDocument();
    expect(verifiedIcon).toHaveAttribute('data-badge-glyph', 'verified');
    expect(screen.queryByText('VER', { selector: '.badge-label' })).not.toBeInTheDocument();
  });

  test('renders twitch badge images when provided', () => {
    const message: Message = {
      author: 'TwitchUser',
      message: 'hi',
      colour: '#ffffff',
      source: 'Twitch',
      badges: [],
      displayBadges: [
        {
          id: 'subscriber',
          version: '2',
          platform: 'twitch',
          imageUrl: 'https://static.twitchcdn.net/badges/v1/subscriber_1x.png',
          images: [
            {
              id: 'sub-1x',
              url: 'https://static.twitchcdn.net/badges/v1/subscriber_1x.png',
              width: 18,
              height: 18
            },
            {
              id: 'sub-2x',
              url: 'https://static.twitchcdn.net/badges/v1/subscriber_2x.png',
              width: 36,
              height: 36
            }
          ]
        }
      ],
      emotes: [],
      fragments: [{ type: FragmentType.Text, text: 'hi', emote: null }]
    };

    render(ChatMessage, {
      props: { message },
      context: new Map<string, any>([
        ['blacklist', new SvelteSet<string>()],
        [
          'keymods',
          {
            ctrl: false,
            shift: false,
            alt: false,
            reset: vi.fn()
          }
        ]
      ])
    });

    const badgeImg = screen.getByAltText('Subscriber 2') as HTMLImageElement;
    expect(badgeImg).toBeInTheDocument();
    expect(badgeImg.tagName).toBe('IMG');
    expect(badgeImg.src).toContain(encodeURIComponent('https://static.twitchcdn.net/badges/v1/subscriber_2x.png'));
  });
});
