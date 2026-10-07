<script lang="ts">
  // Settings → Журнал подключений: programs and the addresses they opened this session, through VPN
  // or not. The log lives in the service's memory; this screen looks at it once a second.
  import TopBar from "../components/TopBar.svelte";
  import Switch from "../components/Switch.svelte";
  import Icon from "../components/Icon.svelte";
  import { app, act, setSetting, snackbar } from "../lib/state.svelte";
  import { call, system } from "../lib/bridge";
  import { t, plural, type StringKey } from "../lib/i18n";
  import { formatTime } from "../lib/format";
  import type { LogApp, LogEntry } from "../lib/types";
  import { onMount } from "svelte";

  type Filter = "all" | "vpn" | "direct" | "blocked";
  const filters: { id: Filter; title: StringKey }[] = [
    { id: "all", title: "connlog_filter_all" },
    { id: "vpn", title: "connlog_filter_vpn" },
    { id: "direct", title: "connlog_filter_direct" },
    { id: "blocked", title: "connlog_filter_blocked" },
  ];

  let apps = $state<LogApp[]>([]);
  let version = 0;
  let filter = $state<Filter>("all");
  let query = $state("");
  let expanded = $state<Record<string, boolean>>({});
  let icons = $state<Record<string, string>>({});
  const askedIcons = new Set<string>();

  async function poll() {
    try {
      const r = await call<{ version: number; apps?: LogApp[] }>("connectionLog", { since: version });
      if (r.apps) {
        apps = r.apps;
        const missing = r.apps.map((a) => a.path ?? "").filter((p) => p && !askedIcons.has(p));
        missing.forEach((p) => askedIcons.add(p));
        if (missing.length) system.programIcons(missing).then((m) => (icons = { ...icons, ...m })).catch(() => {});
      }
      version = r.version;
    } catch {
      // the service will be back
    }
  }

  onMount(() => {
    poll();
    const timer = setInterval(() => {
      if (!document.hidden) poll();
    }, 1000);
    return () => clearInterval(timer);
  });

  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const out: LogApp[] = [];
    for (const a of apps) {
      const appMatches = q !== "" && label(a).toLowerCase().includes(q);
      const entries = a.entries.filter(
        (e) => (filter === "all" || e.route === filter) && (q === "" || appMatches || e.host.toLowerCase().includes(q)),
      );
      if (entries.length) out.push({ ...a, entries });
    }
    return out;
  });

  function label(a: LogApp) {
    switch (a.kind) {
      case "unknown":
        return t("connlog_unknown_app");
      case "device":
        return t("connlog_share_device", a.label);
      case "system":
        return a.label && a.label !== "System" ? `${t("connlog_system")} · ${a.label}` : t("connlog_system");
    }
    return a.label;
  }

  function counts(a: LogApp) {
    const c = { vpn: 0, direct: 0, blocked: 0 };
    for (const e of a.entries) c[e.route]++;
    const parts = [plural("connlog_addresses", a.entries.length)];
    if (c.vpn) parts.push(t("connlog_count_vpn", c.vpn));
    if (c.direct) parts.push(t("connlog_count_direct", c.direct));
    if (c.blocked) parts.push(t("connlog_count_blocked", c.blocked));
    return parts.join(" · ");
  }

  const badge: Record<LogEntry["route"], { text: StringKey; bg: string; fg: string }> = {
    vpn: { text: "connlog_route_vpn", bg: "var(--primary-container)", fg: "var(--on-primary-container)" },
    direct: { text: "connlog_route_direct", bg: "var(--tertiary-container)", fg: "var(--on-tertiary-container)" },
    blocked: { text: "connlog_route_blocked", bg: "var(--error-container)", fg: "var(--on-error-container)" },
  };

  async function copyHost(host: string) {
    await system.copyText(host);
    snackbar(t("share_copied"));
  }

  async function clear() {
    await act("clearConnectionLog");
    version = -1;
    poll();
  }

  const enabled = $derived(!!app.settings?.connectionLog);
</script>

<div class="page">
  <TopBar title={t("connlog_title")}>
    {#snippet actions()}
      {#if apps.length > 0}
        <button class="icon-btn interactive" title={t("connlog_clear")} onclick={clear}><Icon name="delete_sweep" /></button>
      {/if}
    {/snippet}
  </TopBar>
  <div class="scroll">
    <div class="content flat">
      <div class="pad-x">
        <button class="card pad row switch-card interactive" onclick={() => setSetting("connectionLog", !enabled)}>
          <span class="grow col">
            <span class="title-medium">{t("connlog_enable")}</span>
            <span class="body-medium muted">{t("connlog_enable_hint")}</span>
          </span>
          <Switch checked={enabled} onchange={(v) => setSetting("connectionLog", v)} />
        </button>
      </div>
      {#if enabled || apps.length > 0}
        <div class="row chips pad-x">
          {#each filters as f}
            <button class="chip interactive" class:selected={filter === f.id} onclick={() => (filter = f.id)}>
              {#if filter === f.id}<Icon name="check" size={18} />{/if}{t(f.title)}
            </button>
          {/each}
        </div>
        <div class="pad-x">
          <label class="field small">
            <Icon name="search" />
            <input bind:value={query} placeholder={t("connlog_search")} />
            {#if query}<button class="icon-btn interactive" title={t("action_close")} onclick={() => (query = "")}><Icon name="close" /></button>{/if}
          </label>
        </div>
        {#if shown.length === 0}
          <div class="body-medium muted empty">
            {t(apps.length > 0 ? "routing_nothing_found" : app.vpn.status === "connected" ? "connlog_empty" : "connlog_empty_disconnected")}
          </div>
        {/if}
        {#each shown as a (a.key)}
          {@const open = !!expanded[a.key]}
          <button class="app-row interactive" onclick={() => (expanded[a.key] = !open)}>
            <span class="app-icon">
              {#if a.path && icons[a.path]}<img src={icons[a.path]} alt="" width="32" height="32" />
              {:else}<Icon name={a.kind === "device" ? "devices" : a.kind === "unknown" ? "help" : a.kind === "system" ? "desktop_windows" : "deployed_code"} />{/if}
            </span>
            <span class="grow col">
              <span class="body-large ellipsis">{label(a)}</span>
              <span class="body-small muted">{counts(a)}</span>
            </span>
            <span class="muted"><Icon name={open ? "keyboard_arrow_up" : "keyboard_arrow_down"} /></span>
          </button>
          {#if open}
            {#each a.entries as e (e.host + e.route)}
              <!-- Addresses like uc-client-ru.heytapmobile.com are not for retyping: a click copies it. -->
              <button class="entry interactive" title={t("copy")} onclick={() => copyHost(e.host)}>
                <span class="grow col">
                  <span class="body-medium ellipsis">{e.host}</span>
                  <span class="body-small muted">{t("connlog_entry_details", e.udp ? "UDP" : "TCP", e.port, e.count, formatTime(e.lastSeen))}</span>
                </span>
                <span class="label-medium route" style="background:{badge[e.route].bg};color:{badge[e.route].fg}">{t(badge[e.route].text)}</span>
              </button>
            {/each}
          {/if}
        {/each}
        <div class="body-small muted note">{t("connlog_excluded_note")}</div>
      {/if}
    </div>
  </div>
</div>

<style>
  .flat {
    padding: 0 0 24px;
  }
  .pad-x {
    padding: 0 16px;
  }
  .switch-card {
    width: 100%;
    gap: 12px;
    text-align: left;
  }
  .chips {
    gap: 8px;
    padding-top: 12px;
    padding-bottom: 8px;
    flex-wrap: wrap;
  }
  .chip :global(svg) {
    margin-left: -6px;
  }
  .field input {
    padding: 10px 4px;
  }
  .empty,
  .note {
    padding: 16px 24px;
  }
  .app-row {
    display: flex;
    align-items: center;
    gap: 16px;
    width: 100%;
    padding: 10px 16px;
    text-align: left;
    margin-top: 4px;
  }
  .app-icon {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--on-surface-variant);
    flex-shrink: 0;
  }
  .entry {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 16px 6px 72px;
    text-align: left;
  }
  .route {
    padding: 3px 8px;
    border-radius: 8px;
    white-space: nowrap;
  }
</style>
