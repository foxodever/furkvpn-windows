<script lang="ts">
  // Server list with the ping and refresh buttons and the emoji tabs. A dialog in a narrow window,
  // a permanent pane next to the main screen in a wide one. Right-click pings one server, or shows
  // what auto-select sees.
  import Icon from "../components/Icon.svelte";
  import Spinner from "../components/Spinner.svelte";
  import FlagAvatar from "../components/FlagAvatar.svelte";
  import PingText from "../components/PingText.svelte";
  import Menu, { type MenuItem } from "../components/Menu.svelte";
  import { app, ui, act, refreshSubscription } from "../lib/state.svelte";
  import { t } from "../lib/i18n";
  import type { ServerEntry } from "../lib/types";
  import type { Snippet } from "svelte";
  import { onMount } from "svelte";

  let { onselected, headerActions }: { onselected?: () => void; headerActions?: Snippet } = $props();

  // Names mix "⚡" and "⚡️": compare without the emoji variation selector.
  const norm = (s: string) => s.replaceAll("️", "");
  const matches = (e: ServerEntry, emoji: string) => e.kind !== "separator" && norm(e.remarks).includes(norm(emoji));

  const servers = $derived(app.subscription.info?.servers ?? []);
  const tabs = $derived(app.config.serverFilters.filter((f) => servers.some((s) => matches(s, f.emoji))));
  const active = $derived(tabs.some((f) => f.emoji === ui.serverFilter) ? ui.serverFilter : "");
  const visible = $derived.by(() => {
    if (!active) return servers;
    const out: ServerEntry[] = [];
    let header: ServerEntry | null = null;
    for (const e of servers) {
      if (e.kind === "separator") header = e;
      else if (matches(e, active)) {
        if (header) out.push(header);
        header = null;
        out.push(e);
      }
    }
    return out;
  });

  let list: HTMLDivElement;
  let menu = $state<{ x: number; y: number; items: MenuItem[] } | null>(null);

  $effect(() => {
    void active;
    list?.scrollTo({ top: 0 });
  });

  onMount(() => {
    if (Object.keys(app.pings.results).length === 0) act("testPings");
  });

  function select(e: ServerEntry) {
    act("selectServer", { key: e.key });
    onselected?.();
  }

  function contextMenu(ev: MouseEvent, e: ServerEntry) {
    ev.preventDefault();
    menu = {
      x: ev.clientX,
      y: ev.clientY,
      items: [
        { label: t("servers_ping_one"), icon: "speed", action: () => act("pingServer", { key: e.key }) },
        { label: t("servers_select"), icon: "check", action: () => select(e) },
      ],
    };
  }
</script>

<div class="servers">
  <div class="header">
    <span class="title-large grow">{t("servers_title")}</span>
    {@render headerActions?.()}
    <button class="icon-btn interactive" title={t("servers_test")} onclick={() => act("testPings")}>
      {#if app.pings.pinging}<Spinner size={20} stroke={2} />{:else}<Icon name="speed" />{/if}
    </button>
    <button
      class="icon-btn interactive"
      title={t("servers_update")}
      disabled={app.subscription.refreshing}
      onclick={() => refreshSubscription()}
    >
      {#if app.subscription.refreshing}<Spinner size={20} stroke={2} />{:else}<Icon name="refresh" />{/if}
    </button>
  </div>
  {#if app.pings.whitelistOnly}
    <div class="body-small muted hint">{t("servers_whitelist_ping")}</div>
  {/if}
  {#if tabs.length > 0}
    <div class="tabs">
      <button class="tab interactive" class:selected={!active} onclick={() => (ui.serverFilter = "")}>
        {t("servers_filter_all")}
      </button>
      {#each tabs as tab (tab.emoji)}
        <button
          class="tab interactive"
          class:selected={active === tab.emoji}
          title={tab.title}
          onclick={() => (ui.serverFilter = tab.emoji)}
        >
          <span class="emoji">{tab.emoji}</span>
          {#if active === tab.emoji}<span class="tab-title">{tab.title}</span>{/if}
        </button>
      {/each}
    </div>
  {/if}
  <div class="list" bind:this={list}>
    {#each visible as entry (entry.key)}
      {#if entry.kind === "separator"}
        <div class="section label-large">{entry.title}</div>
      {:else}
        {@const selected = entry.key === app.subscription.selected}
        <button
          class="server interactive"
          class:selected
          onclick={() => select(entry)}
          oncontextmenu={(ev) => contextMenu(ev, entry)}
        >
          <FlagAvatar flag={entry.flag} kind={entry.kind} size={40} />
          <span class="grow col">
            <span class="body-large ellipsis">{entry.title}</span>
            {#if entry.summary}<span class="body-small muted ellipsis">{entry.summary}</span>{/if}
          </span>
          <PingText kind={entry.kind} ping={app.pings.results[entry.key]} />
          {#if selected}<span class="primary-text"><Icon name="check" /></span>{/if}
        </button>
      {/if}
    {/each}
    <div class="body-small muted center">{t("servers_hold_hint")}</div>
  </div>
</div>

{#if menu}
  <Menu x={menu.x} y={menu.y} items={menu.items} onclose={() => (menu = null)} />
{/if}

<style>
  .servers {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .header {
    display: flex;
    align-items: center;
    padding: 0 12px 8px 24px;
    gap: 4px;
  }
  .hint {
    padding: 0 24px 8px;
  }
  .tabs {
    display: flex;
    gap: 8px;
    padding: 0 16px 8px;
    overflow-x: auto;
    flex-shrink: 0;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar {
    display: none;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    height: 40px;
    padding: 0 16px;
    border-radius: 20px;
    background: var(--surface-container-high);
    color: var(--on-surface);
    font-size: 14px;
    font-weight: 500;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .tab.selected {
    background: var(--secondary-container);
    color: var(--on-secondary-container);
  }
  .emoji {
    font-size: 18px;
    line-height: 1;
  }
  .tab-title {
    padding-left: 8px;
    animation: grow 180ms ease-out;
  }
  @keyframes grow {
    from {
      opacity: 0;
    }
  }
  .list {
    flex: 1;
    overflow-y: auto;
    padding: 0 12px 32px;
    min-height: 0;
  }
  .section {
    position: sticky;
    top: 0;
    z-index: 1;
    color: var(--primary);
    background: var(--pane-bg, var(--surface-container-low));
    padding: 16px 12px 8px;
  }
  .server {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    margin: 2px 0;
    padding: 10px 12px;
    border-radius: 18px;
    background: var(--pane-bg, var(--surface-container-low));
    text-align: left;
  }
  .server.selected {
    background: var(--secondary-container);
    color: var(--on-secondary-container);
  }
  .center {
    text-align: center;
    padding-top: 12px;
  }
</style>
