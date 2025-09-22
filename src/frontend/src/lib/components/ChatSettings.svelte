<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import SettingsIcon from './icons/SettingsIcon.svelte';

  const dispatch = createEventDispatcher();

  function toggleChatSettings() {
    dispatch('open-settings');
  }
</script>

<button type="button" id="chat-settings-button" title="Chat Settings" on:click={toggleChatSettings}>
  <SettingsIcon alt="Chat Settings" />
</button>

<style lang="scss">
  #chat-settings-button {
    background: none;
    height: 50px;
    width: 50px;
    padding: 0;
    background: none;
    border: none;
    color: #fff;
    font-size: 1em;
    cursor: pointer;

    transition: color 0.3s;
  }
</style>
