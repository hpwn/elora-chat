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
    display: inline-flex;
    width: auto;
    align-items: center;
    justify-content: center;
    text-align: center;
    border: 2px solid var(--accent-color);
    border-radius: 8px;
    color: var(--text-color);
    background-color: var(--secondary-color);
    opacity: 0.9;
    font-weight: bold;
    padding: 6px 12px;
    cursor: pointer;
  }

  #pauseOverlay p {
    margin: 0;
    white-space: nowrap;
  }
</style>
