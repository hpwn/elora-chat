<script lang="ts">
  let { kind, title }: { kind: 'verified' | 'moderator'; title: string } = $props();
</script>

<span
  class="badge-glyph"
  data-badge-glyph={kind}
  role="img"
  aria-label={title}
  title={title}
>
  {#if kind === 'verified'}
    <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <circle cx="8" cy="8" r="6" />
      <path d="M5.1 8.1 7.1 10l3.8-4" />
    </svg>
  {:else}
    <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <path d="M8 1.7 13 3.7v3.8c0 3.1-2.2 5.9-5 6.8-2.8-.9-5-3.7-5-6.8V3.7Z" />
      <path d="m5.4 8.1 1.8 1.7 3.4-3.7" />
    </svg>
  {/if}
</span>

<style lang="scss">
  .badge-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    margin-right: 5px;
    border-radius: 4px;
    border: 1px solid rgba(148, 163, 184, 0.5);
    background: rgba(226, 232, 240, 0.7);
    color: #0f172a;
    vertical-align: middle;
  }

  svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.75;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
