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
});
