<script>
  import { checkLoginStatus } from '$lib/api/auth.svelte';
  import { Chat, Header, Footer, ExportPanel } from '$lib/components';
  import SettingsModal from '$lib/components/SettingsModal.svelte';
  import { settings } from '$lib/stores/settings';
  import { onMount } from 'svelte';

  const urlParams = new URLSearchParams(window.location.search);
  let isPopout = urlParams.has('popout');
  let settingsOpen = false;

  onMount(() => {
    checkLoginStatus();
  });
</script>

<h1
  id="app-title-elora-chat"
  class="visually-hidden"
  style="position:absolute;left:-10000px;top:auto;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0"
>
  Elora Chat
</h1>

<svelte:head>
  <title>Chat Display</title>
</svelte:head>

{#if !isPopout}
  <Header />
  {#if $settings.showExportPanel}
    <div class="export-wrapper">
      <ExportPanel />
    </div>
  {/if}
{/if}
<Chat />
<Footer on:open-settings={() => (settingsOpen = true)} />

<SettingsModal bind:open={settingsOpen} />

<style lang="scss">
  :global(:root) {
    --primary-color: #000000;
    --secondary-color: #1c1c1c;
    --accent-color: #282828;
    --text-color: #cccccc;
  }
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

  @keyframes -global-wave {
    0%,
    100% {
      transform: translateY(-0.25rem);
    }
    50% {
      transform: translateY(0.25rem);
    }
  }

  @keyframes -global-wave2 {
    0%,
    100% {
      transform: translateX(0) translateY(-0.25rem);
    }
    50% {
      transform: translateX(0.5rem) translateY(0.25rem);
    }
  }

  .export-wrapper {
    max-width: 640px;
    margin: 0 auto;
    padding: 0 16px;
  }
</style>
