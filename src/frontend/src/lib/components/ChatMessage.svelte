<script lang="ts">
  import type { Message } from '$lib/types/messages';
  import { addMessageEffects, replaceEmotes, sanitizeMessage } from '$lib/utils';
  import { TwitchIcon, YoutubeIcon } from './icons';

  let { message }: { message: Message } = $props();

  function formatMessage(): { messageWithHTML: string; effects: string } {
    let messageWithEmotes = sanitizeMessage(message.message);
    const { messageText, effects } = addMessageEffects(messageWithEmotes);
    messageWithEmotes = messageText;

    return { messageWithHTML: replaceEmotes(messageWithEmotes, message.emotes), effects };
  }

  const { messageWithHTML, effects } = formatMessage();
</script>

<div class="chat-message">
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

    {#each message.badges as badge}
      {#if badge.icons && badge.icons.length > 0}
        <img
          class="badge-icon"
          src={badge.icons[badge.icons.length - 1].url}
          title={badge.title}
          alt={badge.title}
        />
      {/if}
    {/each}

    <span class="message-username" style="color: {message.colour}">
      {message.author}:
    </span>
  </span>

  <span class={['message-text', effects].filter(Boolean).join(' ')}>
    {@html messageWithHTML}
  </span>
</div>

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

    .emote-image {
      height: 28px;
      width: 28px;

      vertical-align: middle;
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

  .effect-wave {
    /* TODO: implement this */
  }

  .effect-shake {
    /* TODO: implement this */
  }
</style>
