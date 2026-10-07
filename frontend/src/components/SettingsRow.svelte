<script lang="ts">
  import type { Snippet } from "svelte";
  import type { IconName } from "../lib/icons";
  import Icon from "./Icon.svelte";

  let {
    icon,
    title,
    subtitle = "",
    onclick,
    trailing,
    primaryIcon = false,
    disabled = false,
  }: {
    icon?: IconName;
    title: string;
    subtitle?: string;
    onclick?: () => void;
    trailing?: Snippet;
    primaryIcon?: boolean;
    disabled?: boolean;
  } = $props();
</script>

{#if onclick}
  <button class="list-item interactive" class:three={!!subtitle} {disabled} onclick={() => onclick?.()}>
    {@render body()}
  </button>
{:else}
  <div class="list-item" class:three={!!subtitle}>{@render body()}</div>
{/if}

{#snippet body()}
  {#if icon}<span class="lead" class:primary={primaryIcon}><Icon name={icon} /></span>{/if}
  <span class="grow col">
    <span class="body-large">{title}</span>
    {#if subtitle}<span class="body-medium muted">{subtitle}</span>{/if}
  </span>
  {@render trailing?.()}
{/snippet}

<style>
  .list-item:disabled {
    opacity: 0.5;
  }
</style>
