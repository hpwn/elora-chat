<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import type { SvelteSet } from 'svelte/reactivity';
  import { getContext } from 'svelte';
  import { loadImage, formatMessageFragments, validNameColors, sanitizeMessage } from '$lib/utils';
  import { resolveBadgeIcon } from '$lib/chat/badge-icons';
  import { TwitchIcon, YoutubeIcon } from './icons';

  let { message }: { message: Message } = $props();
  let visible = $state(true);

  const blacklist: SvelteSet<string> = getContext('blacklist');
  const keymods: Keymods = getContext('keymods');

  const { messageWithHTML, effects } = formatMessageFragments(message.fragments);
  const fallbackMessage = sanitizeMessage(message.message ?? '');
  const hasFragmentHtml = messageWithHTML.trim().length > 0;
  const hasFallbackHtml = fallbackMessage.trim().length > 0;
  const shouldRender = hasFragmentHtml || hasFallbackHtml;
  const displayHtml = hasFragmentHtml ? messageWithHTML : fallbackMessage;
  const messageClasses = ['message-text', hasFragmentHtml ? effects : ''].filter(Boolean).join(' ');

  const hexColour = validNameColors.get(message.colour);
  if (hexColour != undefined) {
    message.colour = hexColour;
  }

  type DisplayBadge = {
    key: string;
    id: string;
    version?: string;
    icon: ReturnType<typeof resolveBadgeIcon>;
  };

  let badgeViews = $state<DisplayBadge[]>([]);

  $effect(() => {
    const rawBadges = Array.isArray(message.badges) ? message.badges : [];
    badgeViews = rawBadges.flatMap((badge) => {
      if (!badge || typeof badge !== 'object') return [] as DisplayBadge[];
      const id = typeof badge.id === 'string' ? badge.id.trim() : '';
      if (!id) return [] as DisplayBadge[];
      const version =
        typeof badge.version === 'string' && badge.version.trim().length > 0 ? badge.version.trim() : undefined;
      const icon = resolveBadgeIcon(id, version);
      return [
        {
          key: `${id}-${version ?? 'default'}`,
          id,
          version,
          icon
        }
      ];
    });
  });

  function badgeImageSource(src: string | undefined): string | undefined {
    if (!src) return undefined;
    if (src.startsWith('data:')) {
      return src;
    }
    return loadImage(src);
  }

  function toggleVisible() {
    visible = !visible;
  }

  function blacklistAuthor() {
    if (confirm(`Ban ${message.author}.\nThis is permanent. Are you sure?`)) {
      blacklist.add(message.author);
    }
    keymods.reset();
  }

  function handleClickMessage() {
    if (keymods.ctrl) {
      blacklistAuthor();
    } else if (keymods.shift) {
      toggleVisible();
    }
  }

  function keyHandler(event: KeyboardEvent) {
    switch (event.key) {
      case 'h':
      case 'H':
        toggleVisible();
        break;
    }
  }
</script>

{#if shouldRender}
  <div
    role="button"
    aria-pressed="false"
    tabindex="0"
    onkeypress={keyHandler}
    onclick={handleClickMessage}
    class="chat-message"
    data-platform={message.source}
    data-author={message.author}
  >
    <span class="sender">
      {#if message.source === 'Twitch'}
        <span title="Twitch">
          <TwitchIcon class="badge-icon" alt="Twitch user" width={18} height={18} />
        </span>
      {:else if message.source === 'YouTube'}
        <span title="YouTube">
          <YoutubeIcon class="badge-icon" alt="YouTube user" width={18} height={18} />
        </span>
      {/if}

      {#each badgeViews as badge (badge.key)}
        {#if badge.icon.src}
          <img
            class="badge-icon"
            src={badgeImageSource(badge.icon.src)}
            title={badge.icon.alt}
            alt={badge.icon.alt}
          />
        {:else}
          <span class="badge-label" title={badge.icon.alt} aria-label={badge.icon.alt}>
            {badge.icon.label}
          </span>
        {/if}
      {/each}
      <span class="message-username" style="color: {message.colour}">
        {message.author}:
      </span>
    </span>

    {#if visible}
      <span class={messageClasses}>
        {@html displayHtml}
      </span>
    {/if}
  </div>
{/if}

<style lang="scss">
  .chat-message {
    margin: 3px 0;
    opacity: 0;
    word-wrap: break-word;
    animation: glideInBounce 0.5s forwards;
  }

  .sender {
    display: inline-flex;
    align-items: center;
  }

  :global {
    .badge-icon {
      width: 18px;
      height: 18px;
      margin-right: 5px;
      vertical-align: middle;
    }

    .badge-label {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 18px;
      height: 18px;
      padding: 0 4px;
      margin-right: 5px;
      border-radius: 4px;
      border: 1px solid rgba(148, 163, 184, 0.5);
      background: rgba(226, 232, 240, 0.7);
      color: #0f172a;
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.3px;
      text-transform: uppercase;
      line-height: 1;
    }

    .emote-image {
      height: 28px;
      margin: 0 0.2rem; /* top/bottom left/right */
      vertical-align: middle;
    }

    .message-text > img + img {
      margin-left: 0;
    }

    .message-text > img:has(+ img) {
      margin-right: 0;
    }
  }

  .message-username {
    position: relative;
    top: 1px;
    font-weight: bold;
  }

  .message-text {
    vertical-align: middle;
  }

  /* Message effects */
  .text-bold {
    font-weight: bold;
  }
  .text-italic {
    font-style: italic;
  }

  .color-yellow {
    color: #ffff00;
  }
  .color-red {
    color: #ff0000;
  }
  .color-green {
    color: #00ff00;
  }
  .color-cyan {
    color: #00ffff;
  }
  .color-purple {
    color: #9c59d1;
  }
  .color-pink {
    color: #ff00ff;
  }

  .color-rainbow {
    background: linear-gradient(
      to right,
      #ef5350,
      #f48fb1,
      #7e57c2,
      #2196f3,
      #26c6da,
      #43a047,
      #eeff41,
      #f9a825,
      #ff5722
    );
    background-clip: text;
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
  }

  .color-flash1 {
    animation: flash1 0.45s steps(1, end) infinite;
  }
  .color-flash2 {
    animation: flash2 0.45s steps(1, end) infinite;
  }
  .color-flash3 {
    animation: flash3 0.45s steps(1, end) infinite;
  }
  .color-glow1 {
    animation: glow1 3s linear infinite;
  }
  .color-glow2 {
    animation: glow2 3s linear infinite;
  }
  .color-glow3 {
    animation: glow2 3s linear infinite;
  }

  :global {
    .effect-wave span {
      display: inline-block;
      animation: wave 1s ease-in-out infinite;
    }
    .effect-wave2 span {
      display: inline-block;
      animation: wave2 1s ease-in-out infinite;
    }

    .effect-wave span:nth-child(16n),
    .effect-wave2 span:nth-child(16n) {
      animation-delay: 0s;
    }
    .effect-wave span:nth-child(16n + 1),
    .effect-wave2 span:nth-child(16n + 1) {
      animation-delay: 0.0625s;
    }
    .effect-wave span:nth-child(16n + 2),
    .effect-wave2 span:nth-child(16n + 2) {
      animation-delay: 0.125s;
    }
    .effect-wave span:nth-child(16n + 3),
    .effect-wave2 span:nth-child(16n + 3) {
      animation-delay: 0.1875s;
    }
    .effect-wave span:nth-child(16n + 4),
    .effect-wave2 span:nth-child(16n + 4) {
      animation-delay: 0.25s;
    }
    .effect-wave span:nth-child(16n + 5),
    .effect-wave2 span:nth-child(16n + 5) {
      animation-delay: 0.3125s;
    }
    .effect-wave span:nth-child(16n + 6),
    .effect-wave2 span:nth-child(16n + 6) {
      animation-delay: 0.375s;
    }
    .effect-wave span:nth-child(16n + 7),
    .effect-wave2 span:nth-child(16n + 7) {
      animation-delay: 0.4375s;
    }
    .effect-wave span:nth-child(16n + 8),
    .effect-wave2 span:nth-child(16n + 8) {
      animation-delay: 0.5s;
    }
    .effect-wave span:nth-child(16n + 9),
    .effect-wave2 span:nth-child(16n + 9) {
      animation-delay: 0.5625s;
    }
    .effect-wave span:nth-child(16n + 10),
    .effect-wave2 span:nth-child(16n + 10) {
      animation-delay: 0.625s;
    }
    .effect-wave span:nth-child(16n + 11),
    .effect-wave2 span:nth-child(16n + 11) {
      animation-delay: 0.6875s;
    }
    .effect-wave span:nth-child(16n + 12),
    .effect-wave2 span:nth-child(16n + 12) {
      animation-delay: 0.75s;
    }
    .effect-wave span:nth-child(16n + 13),
    .effect-wave2 span:nth-child(16n + 13) {
      animation-delay: 0.8125s;
    }
    .effect-wave span:nth-child(16n + 14),
    .effect-wave2 span:nth-child(16n + 14) {
      animation-delay: 0.875s;
    }
    .effect-wave span:nth-child(16n + 15),
    .effect-wave2 span:nth-child(16n + 15) {
      animation-delay: 0.9375s;
    }

    .effect-cheddar span:nth-child(4n) {
      color: #feddb0;
    }
    .effect-cheddar span:nth-child(4n + 1) {
      color: #f8aa72;
    }
    .effect-cheddar span:nth-child(4n + 2) {
      color: #ef965b;
    }
    .effect-cheddar span:nth-child(4n + 3) {
      color: #fdc28d;
    }

    .effect-shake {
      /* TODO: implement this */
      animation: wave 1s linear infinite;
    }
  }
</style>
