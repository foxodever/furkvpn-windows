<script lang="ts">
  // The main screen: status, connect button, current server, white lists, subscription, links. In a
  // wide window the server list stays next to it; the swap button moves it to the other side.
  import Icon from "../components/Icon.svelte";
  import Logo from "../components/Logo.svelte";
  import Spinner from "../components/Spinner.svelte";
  import FlagAvatar from "../components/FlagAvatar.svelte";
  import PingText from "../components/PingText.svelte";
  import LinearProgress from "../components/LinearProgress.svelte";
  import Linkified from "../components/Linkified.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import Dialog from "../components/Dialog.svelte";
  import ServersList from "./ServersList.svelte";
  import Whitelist from "./Whitelist.svelte";
  import Support from "./Support.svelte";
  import TelegramProxies from "./TelegramProxies.svelte";
  import { app, open, act, setSetting, snackbar } from "../lib/state.svelte";
  import { call, system } from "../lib/bridge";
  import { t, plural } from "../lib/i18n";
  import { formatBytes, formatDate, formatDuration, formatSpeed } from "../lib/format";

  let width = $state(window.innerWidth);
  let showServers = $state(false);
  let showSupport = $state(false);
  let showProxies = $state(false);
  let now = $state(Date.now());

  const wide = $derived(width >= 720);
  const status = $derived(app.vpn.status);
  const connected = $derived(status === "connected");
  const busy = $derived(status === "connecting" || status === "stopping");
  const server = $derived(
    app.subscription.info?.servers.find((s) => s.key === app.subscription.selected) ??
      app.subscription.info?.servers.find((s) => s.kind !== "separator"),
  );

  $effect(() => {
    if (!connected) return;
    now = Date.now();
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  function toggle() {
    if (status === "idle") act("connect");
    else act("disconnect");
  }

  // --- the subscription card ---
  const account = $derived(app.account?.found ? app.account : null);
  const sub = $derived(app.subscription.info);
  const expiresAt = $derived(account ? account.expiresAt : (sub?.expire ?? null));
  const nowSeconds = $derived(Math.floor(now / 1000));
  const expired = $derived(expiresAt !== null && expiresAt <= nowSeconds);
  const daysLeft = $derived(expiresAt !== null ? Math.max(0, Math.floor((expiresAt - nowSeconds) / 86400)) : null);
  // "Продлить" when less than 2 days are left (or none), "Купить тариф" on the free plan.
  const endsSoon = $derived(expiresAt !== null && expiresAt - nowSeconds < 2 * 86400);
  const trafficLimit = $derived((account ? account.trafficLimit : (sub?.total ?? null)) || null);
  const trafficUsed = $derived(account ? account.trafficUsed : sub ? (sub.upload ?? 0) + (sub.download ?? 0) : null);
  const showAccount = $derived(!!account || expiresAt !== null);
  const planTitle = $derived.by(() => {
    if (expired) return t("your_subscription");
    if (account?.isFree) return t("plan_free");
    if (account?.planName) return t("plan_title", account.planName);
    return t("your_subscription");
  });
  const planDetails = $derived.by(() => {
    if (expired) return t("plan_expired");
    if (expiresAt !== null) return t("plan_until", formatDate(expiresAt)) + " · " + t("plan_days_left", plural("days", daysLeft ?? 0));
    if (account?.isFree) return "";
    return t("plan_unlimited");
  });

  // --- update ---
  async function installUpdate() {
    try {
      const r = await call<{ path: string }>("downloadUpdate");
      snackbar(t("update_starting"));
      await call("prepareInstall");
      await system.installUpdate(r.path);
    } catch (e) {
      const code = (e as { code?: string }).code;
      snackbar(code === "cancelled" ? t("update_cancelled") : t("update_failed"));
    }
  }

  function linkTitle(l: { type: string; title?: string; url: string }) {
    if (l.title) return l.title;
    if (l.type === "website") return t("link_website");
    if (l.type === "bot") return t("link_bot");
    return l.url.replace(/^\w+:\/\//, "").split("/")[0];
  }
</script>

<svelte:window bind:innerWidth={width} />

<div class="page">
  <div class="topbar home-bar">
    <span class="logo"><Logo /></span>
    <span class="grow"></span>
    <button class="icon-btn interactive" title={t("settings")} onclick={() => open("settings")}>
      <Icon name="settings" />
    </button>
  </div>

  <div class="panes" class:wide class:left={wide && app.settings?.serversOnLeft}>
    <div class="home scroll">
      <div class="column">
        <div class="status col">
          <span class="headline-small">
            {t(connected ? "status_connected" : status === "connecting" ? "status_connecting" : status === "stopping" ? "status_disconnecting" : "status_disconnected")}
          </span>
          <span class="title-medium muted">{connected && app.vpn.since ? formatDuration(now - app.vpn.since) : " "}</span>
          <span class="body-medium muted session" class:hidden={!connected}>
            {t("session_traffic", formatBytes(app.traffic.totalDown), formatBytes(app.traffic.totalUp))}
          </span>
          {#if connected && app.vpn.localPort}
            <button class="proxy-mode body-small interactive" onclick={() => open("advanced")}>{t("home_proxy_mode", String(app.vpn.localPort))}</button>
          {/if}
        </div>

        <div class="connect-box">
          {#if connected}<div class="halo"></div>{/if}
          {#if busy}<div class="ring"><Spinner size={200} stroke={4} /></div>{/if}
          <button class="connect" class:on={connected} onclick={toggle} disabled={!app.online}>
            <Icon name="power_settings_new" size={60} />
            <span class="label-large">{t(status === "idle" ? "action_connect" : "action_disconnect")}</span>
          </button>
        </div>

        <div class="speed row" class:hidden={!connected}>
          <span class="row muted"><Icon name="arrow_downward" size={16} /><span class="body-medium">{formatSpeed(app.traffic.down)}</span></span>
          <span class="row muted"><Icon name="arrow_upward" size={16} /><span class="body-medium">{formatSpeed(app.traffic.up)}</span></span>
        </div>

        <div class="cards">
          {#if wide}
            <div class="card server-card">{@render serverRow()}</div>
          {:else}
            <button class="card server-card interactive" onclick={() => (showServers = true)}>{@render serverRow()}</button>
          {/if}

          <Whitelist />

          {#if showAccount}
            <div class="card pad account" class:danger={expired}>
              <div class="title-medium">{planTitle}</div>
              {#if planDetails}<div class="body-medium" class:muted={!expired}>{planDetails}</div>{/if}
              {#if trafficLimit && trafficUsed !== null}
                <div class="progress"><LinearProgress value={Math.min(1, trafficUsed / trafficLimit)} /></div>
                <div class="body-small muted">
                  {t("traffic_used", formatBytes(trafficUsed), formatBytes(trafficLimit))} · {t("traffic_left", formatBytes(Math.max(0, trafficLimit - trafficUsed)))}
                </div>
              {:else if trafficUsed}
                <div class="body-small muted used">{t("traffic_used_only", formatBytes(trafficUsed))}</div>
              {/if}
              {#if account?.isFree || endsSoon}
                <button class="btn tonal interactive renew" onclick={() => system.openURL(app.subscription.renewUrl || app.config.renewUrl)}>
                  {t(account?.isFree ? "action_buy_plan" : "action_renew")}
                </button>
              {/if}
            </div>
          {/if}

          {#if app.update.available}
            {@const u = app.update.available}
            <div class="card primary update">
              <div class="row gap12">
                <Icon name="install_desktop" />
                <span class="title-small grow">{t("update_available", u.versionName)}</span>
              </div>
              {#if u.changelog}<div class="body-small changelog">{u.changelog}</div>{/if}
              {#if app.update.progress !== null}
                <div class="progress"><LinearProgress value={app.update.progress} /></div>
              {:else}
                <div class="row actions">
                  <button class="btn text interactive" onclick={() => act("skipUpdate")}>{t("update_skip")}</button>
                  <button class="btn text interactive" onclick={installUpdate}>{t("update_install")}</button>
                </div>
              {/if}
            </div>
          {/if}

          {#if sub?.announce}
            <div class="card pad row announce">
              <span class="primary-text"><Icon name="campaign" /></span>
              <span class="body-medium grow"><Linkified text={sub.announce} /></span>
            </div>
          {/if}
        </div>

        <div class="quick row">
          {#each app.config.links as link}
            <button class="action interactive" onclick={() => system.openURL(link.url)}>
              <span class="circle">
                {#if link.type === "bot"}<TelegramIcon />{:else if link.type === "website"}<Icon name="language" />{:else}<Icon name="link" />{/if}
              </span>
              <span class="label-medium">{linkTitle(link)}</span>
            </button>
          {/each}
          <button class="action interactive" onclick={() => (showSupport = true)}>
            <span class="circle"><Icon name="support_agent" /></span>
            <span class="label-medium">{t("support_title")}</span>
          </button>
        </div>
        {#if app.config.telegramProxies.length > 0}
          <button class="tg-proxies row interactive" onclick={() => (showProxies = true)}>
            <span class="primary-text"><TelegramIcon /></span>
            <span class="grow col">
              <span class="title-small">{t("tg_proxies_title")}</span>
              <span class="body-small muted">{t("tg_proxies_subtitle")}</span>
            </span>
            <span class="muted"><Icon name="chevron_right" /></span>
          </button>
        {/if}
      </div>
    </div>

    {#if wide}
      <div class="servers-pane">
        <ServersList>
          {#snippet headerActions()}
            <button
              class="icon-btn interactive"
              title={t("servers_swap_side")}
              onclick={() => setSetting("serversOnLeft", !app.settings?.serversOnLeft)}
            >
              <Icon name="swap_horiz" />
            </button>
          {/snippet}
        </ServersList>
      </div>
    {/if}
  </div>
</div>

{#snippet serverRow()}
  <div class="row server-row">
    <FlagAvatar flag={server?.flag} kind={server?.kind ?? "server"} />
    <span class="grow col">
      <span class="label-medium muted">{t("server_label")}</span>
      <span class="title-medium ellipsis">{server?.title ?? "—"}</span>
    </span>
    <PingText kind={server?.kind} ping={server ? app.pings.results[server.key] : undefined} />
    {#if !wide}<span class="muted"><Icon name="chevron_right" /></span>{/if}
  </div>
{/snippet}

{#if showServers && !wide}
  <Dialog sheet onclose={() => (showServers = false)}>
    <div class="sheet-servers">
      <ServersList onselected={() => (showServers = false)} />
    </div>
  </Dialog>
{/if}

{#if showProxies}
  <TelegramProxies onclose={() => (showProxies = false)} />
{/if}

{#if showSupport}
  <Support onclose={() => (showSupport = false)} />
{/if}

<style>
  .home-bar {
    padding: 0 12px 0 20px;
  }
  .logo {
    display: flex;
    color: var(--on-surface);
  }
  .panes {
    flex: 1;
    display: flex;
    gap: 16px;
    min-height: 0;
  }
  .panes.wide {
    padding: 0 16px 16px;
  }
  .panes.left {
    flex-direction: row-reverse;
  }
  .home {
    flex: 1;
  }
  .column {
    max-width: 560px;
    margin: 0 auto;
    padding: 12px 20px 16px;
    display: flex;
    flex-direction: column;
    align-items: center;
  }
  .servers-pane {
    flex: 1;
    min-width: 0;
    border-radius: 28px;
    background: var(--surface-container-low);
    padding-top: 12px;
    --pane-bg: var(--surface-container-low);
    overflow: hidden;
  }
  .status {
    align-items: center;
  }
  .session {
    margin-top: 2px;
  }
  .proxy-mode {
    margin-top: 8px;
    padding: 4px 12px;
    border-radius: 8px;
    background: var(--surface-container);
    color: var(--on-surface-variant);
  }
  .hidden {
    visibility: hidden;
  }
  .connect-box {
    position: relative;
    width: 232px;
    height: 232px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-top: 20px;
  }
  .halo {
    position: absolute;
    width: 220px;
    height: 220px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--primary) 14%, transparent);
    animation: pulse 1.6s ease-in-out infinite alternate;
  }
  @keyframes pulse {
    from {
      transform: scale(0.92);
    }
    to {
      transform: scale(1.06);
    }
  }
  .ring {
    position: absolute;
    display: flex;
  }
  .connect {
    position: relative;
    width: 176px;
    height: 176px;
    border-radius: 50%;
    background: var(--surface-container-high);
    color: var(--on-surface);
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    transition: background 250ms, color 250ms, box-shadow 250ms, transform 120ms;
  }
  .connect:hover {
    filter: brightness(1.04);
  }
  .connect:active {
    transform: scale(0.98);
  }
  .connect.on {
    background: var(--primary);
    color: var(--on-primary);
    box-shadow: 0 3px 10px rgba(0, 0, 0, 0.3);
  }
  .connect:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .speed {
    gap: 20px;
    margin-top: 4px;
  }
  .speed .row {
    gap: 4px;
  }
  .cards {
    width: 100%;
    margin-top: 20px;
    display: flex;
    flex-direction: column;
  }
  .server-card {
    display: block;
    width: 100%;
    text-align: left;
    margin-bottom: 12px;
  }
  .server-row {
    padding: 16px;
    gap: 14px;
  }
  .account,
  .update,
  .announce {
    margin-bottom: 12px;
  }
  .account .progress,
  .update .progress {
    margin: 12px 0 6px;
  }
  .used {
    margin-top: 6px;
  }
  .renew {
    margin-top: 12px;
  }
  .update {
    padding: 16px 16px 6px;
  }
  .gap12 {
    gap: 12px;
  }
  .changelog {
    padding: 6px 0 0 36px;
    white-space: pre-wrap;
  }
  .update .actions {
    justify-content: flex-end;
  }
  .announce {
    gap: 12px;
    align-items: flex-start;
  }
  .quick {
    width: 100%;
    margin-top: 8px;
  }
  .tg-proxies {
    width: 100%;
    gap: 16px;
    margin-top: 8px;
    padding: 14px 16px;
    border-radius: 24px;
    background: var(--surface-container);
    color: var(--on-surface);
    text-align: left;
  }
  .action {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 8px 0;
    border-radius: 16px;
    text-align: center;
  }
  .circle {
    width: 52px;
    height: 52px;
    border-radius: 50%;
    background: var(--surface-container-high);
    color: var(--primary);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .sheet-servers {
    height: min(70vh, 640px);
    display: flex;
    flex-direction: column;
    --pane-bg: var(--surface-container-low);
  }
</style>
