import { render, screen } from '@testing-library/svelte';
import { SvelteSet } from 'svelte/reactivity';
import { describe, expect, test, vi } from 'vitest';

import ChatMessage from './ChatMessage.svelte';
import type { Message } from '$lib/types/messages';

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
      context: new Map([
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
      context: new Map([
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
      context: new Map([
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
      context: new Map([
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

  test('renders badge images as icons instead of text labels when image is present', () => {
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
      context: new Map([
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

    const badgeIcon = screen.getByAltText('Moderator');
    expect(badgeIcon.tagName).toBe('IMG');
    expect((badgeIcon as HTMLImageElement).src).toContain(encodeURIComponent('/assets/badges/yt-mod-wrench.svg'));
    expect(screen.queryByText('Moderator', { selector: '.badge-label' })).not.toBeInTheDocument();
  });

  test('renders youtube moderator wrench badge from normalized display badges', () => {
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
          images: [{ url: '/assets/badges/yt-mod-wrench.svg', width: 16, height: 16 }],
          title: 'Moderator'
        }
      ],
      emotes: [],
      fragments: [{ type: 'text', text: 'hi', emote: null }]
    };

    render(ChatMessage, {
      props: { message },
      context: new Map([
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

    const badgeImg = screen.getByAltText('Moderator') as HTMLImageElement;
    expect(badgeImg).toBeInTheDocument();
    expect(badgeImg.src).toContain(encodeURIComponent('/assets/badges/yt-mod-wrench.svg'));
  });
});
