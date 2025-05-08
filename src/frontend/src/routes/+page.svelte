<script>
  import { checkLoginStatus } from '$lib/api/auth.svelte';
  import { Chat, Header, SendMessage } from '$lib/components';
  import { onMount } from 'svelte';

  const urlParams = new URLSearchParams(window.location.search);
  let isPopout = urlParams.has('popout');

  onMount(() => {
    checkLoginStatus();
  });
</script>

<svelte:head>
  <title>Chat Display</title>
</svelte:head>

{#if !isPopout}
  <Header />
{/if}
<Chat />
<SendMessage />

<style lang="scss">
  :global {
    * {
      box-sizing: border-box;
    }

    html,
    body {
      height: 100%;
      margin: 0;
      padding: 0;
    }

    body {
      display: flex;
      flex-direction: column;

      background-color: black;
      color: white;
      font-family: Arial, sans-serif;
      forced-color-adjust: none;
    }
  }

  /* Message effect keyframes */
  @keyframes -global-glideInBounce {
    0% {
      transform: translateX(100%);
      opacity: 0;
    }
    70% {
      transform: translateX(-10%);
      opacity: 1;
    }
    100% {
      transform: translateX(0);
      opacity: 1;
    }
  }

  @keyframes -global-flash1 {
    0% {
      color: #ff0000;
    }
    50% {
      color: #ffff00;
    }
  }

  @keyframes -global-flash2 {
    0% {
      color: #0000ff;
    }
    50% {
      color: #00ffff;
    }
  }

  @keyframes -global-flash3 {
    0% {
      color: #00b000;
    }
    50% {
      color: #80ff80;
    }
  }

  @keyframes -global-glow1 {
    0% {
      color: #ff0000;
    }
    33% {
      color: #00b000;
    }
    66% {
      color: #0000ff;
    }
    100% {
      color: #ff0000;
    }
  }

  @keyframes -global-glow2 {
    0% {
      color: #ff0000;
    }
    33% {
      color: #800080;
    }
    66% {
      color: #0000ff;
    }
    100% {
      color: #ff0000;
    }
  }

  @keyframes -global-glow3 {
    0% {
      color: #ffffff;
    }
    25% {
      color: #00b000;
    }
    50% {
      color: #ffffff;
    }
    67.5% {
      color: #00ffff;
    }
    75% {
      color: #0000ff;
    }
    100% {
      color: #ffffff;
    }
  }
</style>
