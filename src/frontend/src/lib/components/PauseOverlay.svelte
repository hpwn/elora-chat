<script lang="ts">
  let { newMessageCount, unpauseChat }: { newMessageCount: number; unpauseChat: () => void } =
    $props();
</script>

<button id="pauseOverlay" aria-label="Pause Overlay" type="button" onclick={unpauseChat}>
  {#if newMessageCount === 0}
    <p>Chat Paused</p>
  {:else if newMessageCount === 1}
    <p>1 New Message</p>
  {:else}
    <p>{newMessageCount} New Messages</p>
  {/if}
</button>

<style lang="scss">
  #pauseOverlay {
    position: absolute;
    top: 80%;
    left: 50%;
    transform: translate(-50%, -80%);
    z-index: 1000;
    text-align: center;
    border: 2px solid var(--accent-color);
    border-radius: 8px;
    color: var(--text-color);
    background-color: var(--secondary-color);
    opacity: 0.9;
    font-weight: bold;
    padding: 0px 8px;
  }
</style>
