<script lang="ts">
  // Material 3 switch.
  let {
    checked,
    disabled = false,
    onchange,
    label = "",
  }: { checked: boolean; disabled?: boolean; onchange?: (v: boolean) => void; label?: string } = $props();
</script>

<button
  class="switch"
  class:on={checked}
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {disabled}
  onclick={(e) => {
    e.stopPropagation();
    onchange?.(!checked);
  }}
>
  <span class="thumb">
    {#if checked}
      <svg width="16" height="16" viewBox="0 -960 960 960" fill="currentColor"><path d="M382-354 721-693q12-12 28-12t28 12q12 12 12 28.5T777-636L410-268q-12 12-28 12t-28-12L182-440q-12-12-11.5-28.5T183-497q12-12 28.5-12t28.5 12l142 143Z" /></svg>
    {/if}
  </span>
</button>

<style>
  .switch {
    position: relative;
    width: 52px;
    height: 32px;
    border-radius: 16px;
    border: 2px solid var(--outline);
    background: var(--surface-container-highest);
    flex-shrink: 0;
    transition: background 150ms, border-color 150ms;
  }
  .switch.on {
    background: var(--primary);
    border-color: var(--primary);
  }
  .thumb {
    position: absolute;
    top: 50%;
    left: 6px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--outline);
    transform: translateY(-50%);
    transition: all 150ms cubic-bezier(0.2, 0, 0, 1);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--primary-container);
  }
  .switch.on .thumb {
    left: 22px;
    width: 24px;
    height: 24px;
    background: var(--on-primary);
    color: var(--on-primary-container);
  }
  .switch:hover:not(:disabled) .thumb {
    box-shadow: 0 0 0 8px color-mix(in srgb, var(--on-surface) 8%, transparent);
  }
  .switch.on:hover:not(:disabled) .thumb {
    box-shadow: 0 0 0 8px color-mix(in srgb, var(--primary) 12%, transparent);
  }
  .switch:disabled {
    opacity: 0.38;
    cursor: default;
  }
  .switch.on .thumb svg {
    color: var(--on-primary-container);
  }
</style>
