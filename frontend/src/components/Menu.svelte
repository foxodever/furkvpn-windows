<script lang="ts" module>
  export interface MenuItem {
    label: string;
    icon?: import("../lib/icons").IconName;
    action: () => void;
    disabled?: boolean;
  }
</script>

<script lang="ts">
  // Material 3 menu at a point (a button's corner or the mouse for right-click).
  import Icon from "./Icon.svelte";
  import { onMount } from "svelte";

  let { items, x, y, onclose }: { items: MenuItem[]; x: number; y: number; onclose: () => void } = $props();
  let menu: HTMLDivElement;
  let left = $state(0);
  let top = $state(0);

  onMount(() => {
    const r = menu.getBoundingClientRect();
    left = Math.min(x, window.innerWidth - r.width - 8);
    top = y + r.height > window.innerHeight - 8 ? Math.max(8, y - r.height) : y;
    menu.querySelector<HTMLElement>("button")?.focus();
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="layer"
  onmousedown={(e) => {
    if (e.target === e.currentTarget) onclose();
  }}
  oncontextmenu={(e) => {
    e.preventDefault();
    onclose();
  }}
  onkeydown={(e) => {
    if (e.key === "Escape") onclose();
  }}
>
  <div class="menu" bind:this={menu} style="left:{left}px;top:{top}px" role="menu">
    {#each items as item}
      <button
        class="item interactive"
        role="menuitem"
        disabled={item.disabled}
        onclick={() => {
          onclose();
          item.action();
        }}
      >
        {#if item.icon}<Icon name={item.icon} size={22} />{/if}
        <span class="body-large">{item.label}</span>
      </button>
    {/each}
  </div>
</div>

<style>
  .layer {
    position: fixed;
    inset: 0;
    z-index: 200;
  }
  .menu {
    position: fixed;
    min-width: 200px;
    max-width: 320px;
    padding: 8px 0;
    border-radius: 4px;
    background: var(--surface-container);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    animation: show 120ms ease-out;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 48px;
    padding: 0 16px;
    color: var(--on-surface);
    text-align: left;
  }
  .item :global(svg) {
    color: var(--on-surface-variant);
  }
  .item:disabled {
    opacity: 0.38;
  }
  @keyframes show {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
</style>
