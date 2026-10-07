<script lang="ts">
  let { checked, onchange }: { checked: boolean; onchange?: (v: boolean) => void } = $props();
</script>

<button
  class="box interactive"
  class:on={checked}
  role="checkbox"
  aria-checked={checked}
  aria-label="checkbox"
  onclick={(e) => {
    e.stopPropagation();
    onchange?.(!checked);
  }}
>
  <span class="square">
    {#if checked}
      <svg width="18" height="18" viewBox="0 -960 960 960" fill="currentColor"><path d="M382-354 721-693q12-12 28-12t28 12q12 12 12 28.5T777-636L410-268q-12 12-28 12t-28-12L182-440q-12-12-11.5-28.5T183-497q12-12 28.5-12t28.5 12l142 143Z" /></svg>
    {/if}
  </span>
</button>

<style>
  .box {
    width: 40px;
    height: 40px;
    border-radius: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--on-surface-variant);
    flex-shrink: 0;
  }
  .square {
    width: 18px;
    height: 18px;
    border-radius: 2px;
    border: 2px solid var(--on-surface-variant);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .on .square {
    background: var(--primary);
    border-color: var(--primary);
    color: var(--on-primary);
  }
  .on {
    color: var(--primary);
  }
</style>
