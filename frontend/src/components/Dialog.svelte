<script lang="ts">
  // Material 3 dialog; "sheet" is the desktop form of Android's bottom sheets: wider, scrolling.
  import type { Snippet } from "svelte";
  import { onMount } from "svelte";

  let {
    title = "",
    onclose,
    children,
    actions,
    sheet = false,
    persistent = false,
    width = 0,
  }: {
    title?: string;
    onclose?: () => void;
    children?: Snippet;
    actions?: Snippet;
    sheet?: boolean;
    persistent?: boolean;
    width?: number;
  } = $props();

  let panel: HTMLDivElement;

  onMount(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = panel.querySelector<HTMLElement>("[autofocus], input, textarea");
    (first ?? panel).focus();
    return () => previous?.focus?.();
  });

  function key(e: KeyboardEvent) {
    if (e.key === "Escape" && !persistent) {
      e.stopPropagation();
      onclose?.();
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="scrim"
  onmousedown={(e) => {
    if (e.target === e.currentTarget && !persistent) onclose?.();
  }}
  onkeydown={key}
>
  <div
    class="panel"
    class:sheet
    bind:this={panel}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    style={width ? `max-width:${width}px` : ""}
  >
    {#if title}
      <div class="title headline-small">{title}</div>
    {/if}
    <div class="body body-medium">
      {@render children?.()}
    </div>
    {#if actions}
      <div class="actions">{@render actions()}</div>
    {/if}
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    z-index: 100;
    animation: fade 150ms ease-out;
  }
  .panel {
    background: var(--surface-container-high);
    color: var(--on-surface);
    border-radius: 28px;
    width: 100%;
    max-width: 480px;
    max-height: 100%;
    display: flex;
    flex-direction: column;
    padding: 24px 0 0;
    outline: none;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25);
    animation: pop 180ms cubic-bezier(0.2, 0, 0, 1);
    --field-bg: var(--surface-container-high);
  }
  .panel.sheet {
    max-width: 560px;
    background: var(--surface-container-low);
    --field-bg: var(--surface-container-low);
  }
  .title {
    padding: 0 24px 16px;
  }
  .body {
    padding: 0 24px;
    overflow-y: auto;
    color: var(--on-surface-variant);
    flex: 1 1 auto;
    min-height: 0;
  }
  .sheet .body {
    padding: 0;
    color: var(--on-surface);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 24px 24px 20px;
    flex-wrap: wrap;
  }
  .sheet:not(:has(.actions)) {
    padding-bottom: 16px;
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: scale(0.96);
    }
  }
</style>
