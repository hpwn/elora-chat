<script>
  import { TwitchIcon } from './icons';

  import { authState, logout, redirectToTwitchLogin, restartServer } from '$lib/api/auth.svelte';

  function popoutChat() {
    const popoutFeatures =
      'scrollbars=no,resizable=yes,status=no,location=no,toolbar=no,menubar=no';
    window.open('/?popout', 'ChatPopout', popoutFeatures);
  }
</script>

<div id="header">
  <img src="images/logo.jpg" alt="Logo" id="logo" class="login-button" />

  <div class="buttons">
    {#if authState.loggedIn}
      <!-- Logout Button -->
      <button id="logout-button" on:click={logout}>logout</button>

      <!-- Refresh Server Button -->
      <button id="refresh-server-button" title="Refresh Server" on:click={restartServer}>
        <img src="images/refresh2.png" alt="Refresh" />
      </button>
    {:else}
      <!-- Twitch Login Button -->
      <button
        id="twitch-login-button"
        title="Login with Twitch"
        class="login-button"
        on:click={redirectToTwitchLogin}
      >
        <TwitchIcon alt="Login with Twitch" />
      </button>
    {/if}

    <!-- Popout Chat Button -->
    <button id="popout-chat-button" title="Popout Chat" on:click={popoutChat}>
      <img src="images/popout.png" alt="Popout" />
    </button>
  </div>
</div>

<style lang="scss">
  #header {
    display: flex;
    flex-shrink: 1;
    align-items: center;
    justify-content: space-between;

    padding: 5px 10px 0;
  }

  #logo {
    height: 30px;
    width: 30px;

    border-radius: 50%;
    margin-right: 10px;
  }

  .login-button {
    background: none;
    border: none;
    cursor: pointer;
    padding: 0;
  }

  .buttons {
    display: flex;
    justify-content: space-between;
    gap: 10px;
  }

  .buttons :global {
    svg,
    img {
      height: 30px;
      width: 30px;
    }
  }

  #twitch-login-button,
  #logout-button,
  #refresh-server-button,
  #popout-chat-button {
    padding: 0;
    background: none;
    border: none;
    color: #bbb;
    font-size: 1em;
    cursor: pointer;

    transition: color 0.3s;
  }
</style>
