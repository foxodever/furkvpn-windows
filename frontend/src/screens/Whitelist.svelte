<script lang="ts">
  // "Белые списки" on the main screen: the status, and the details of the last check.
  import Icon from "../components/Icon.svelte";
  import Spinner from "../components/Spinner.svelte";
  import Dialog from "../components/Dialog.svelte";
  import { app, act } from "../lib/state.svelte";
  import { t, type StringKey } from "../lib/i18n";
  import { formatTime, relativeTime } from "../lib/format";
  import type { ProbeView, WhitelistStatus } from "../lib/types";

  let details = $state(false);
  let now = $state(Date.now());

  $effect(() => {
    if (!details) return;
    const timer = setInterval(() => (now = Date.now()), 15_000);
    return () => clearInterval(timer);
  });

  const statusText: Record<WhitelistStatus, StringKey> = {
    all_ok: "whitelist_all_ok",
    whitelist: "whitelist_on",
    no_internet: "whitelist_none",
    unknown: "whitelist_checking",
  };
  const statusColor: Record<WhitelistStatus, string> = {
    all_ok: "var(--tertiary)",
    whitelist: "var(--warning)",
    no_internet: "var(--error)",
    unknown: "var(--outline)",
  };
  const networkName: Record<string, StringKey> = {
    wifi: "network_wifi",
    mobile: "network_mobile",
    ethernet: "network_ethernet",
    hotspot: "network_wifi",
    other: "network_other",
    none: "network_none",
  };
  const probeErrors: Record<string, StringKey> = {
    timeout: "probe_timeout",
    dns: "probe_dns",
    connect: "probe_connect",
    tls: "probe_tls",
    other: "probe_error",
  };

  const wl = $derived(app.whitelist);
  const check = $derived(wl.lastCheck);
  const groups = $derived(
    check
      ? [
          { title: "whitelist_probes_blocked" as StringKey, probes: check.blocked },
          { title: "whitelist_probes_allowed" as StringKey, probes: check.allowed },
        ]
      : [],
  );

  function probeText(p: ProbeView) {
    return p.error ? t(probeErrors[p.error] ?? "probe_error") : t("probe_ok", p.ms ?? 0);
  }
</script>

{#if app.settings?.whitelistDetect}
  <button class="card wl interactive" onclick={() => (details = true)}>
    <div class="row top">
      <span class="grow col">
        <span class="title-medium">{t("whitelist_title")}</span>
        <span class="body-medium muted">{t(statusText[wl.status])}</span>
      </span>
      <span class="state">
        {#if wl.checking}<Spinner size={18} stroke={2} />{:else}<span class="dot" style="background:{statusColor[wl.status]}"></span>{/if}
      </span>
    </div>
    <div class="row end">
      <span class="btn text">{t("whitelist_details")}</span>
    </div>
  </button>
{/if}

{#if details}
  <Dialog sheet title={t("whitelist_title")} onclose={() => (details = false)}>
    <div class="sheet">
      <div class="row status">
        <span class="dot" style="background:{statusColor[wl.status]}"></span>
        <span class="body-large">{t(statusText[wl.status])}</span>
      </div>
      <div class="body-medium muted">
        {#if check}
          {t("whitelist_last_check", formatTime(check.time), relativeTime(check.time, now))}<br />
          {t("whitelist_network", t(networkName[check.network] ?? "network_other"))}
        {:else}
          {t("whitelist_never_checked")}
        {/if}
      </div>
      {#if check}
        {#each groups as group}
          <div class="label-large primary-text group">{t(group.title)}</div>
          {#each group.probes as probe}
            <div class="row probe">
              <span style="color:{probe.error ? 'var(--error)' : 'var(--tertiary)'}">
                <Icon name={probe.error ? "cancel" : "check_circle"} size={20} />
              </span>
              <span class="body-large grow">{probe.host}</span>
              <span class="body-medium" style="color:{probe.error ? 'var(--error)' : 'var(--on-surface-variant)'}">{probeText(probe)}</span>
            </div>
          {/each}
        {/each}
      {/if}
      <div class="note row">
        <span class="primary-text"><Icon name="info" /></span>
        <span class="col gap">
          <span class="body-medium">{t("whitelist_direct_note")}</span>
          <span class="body-small muted">{t("whitelist_how")}</span>
        </span>
      </div>
      <button class="btn tonal wide large interactive" disabled={wl.checking} onclick={() => act("checkWhitelist")}>
        {#if wl.checking}<Spinner size={18} stroke={2} />{:else}<Icon name="refresh" />{/if}
        {t("whitelist_check_again")}
      </button>
    </div>
  </Dialog>
{/if}

<style>
  .wl {
    display: block;
    width: 100%;
    text-align: left;
    padding: 16px 8px 4px 16px;
    margin-bottom: 12px;
  }
  .top {
    gap: 12px;
  }
  .state {
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 8px;
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .end {
    justify-content: flex-end;
  }
  .sheet {
    padding: 0 24px;
    display: flex;
    flex-direction: column;
  }
  .status {
    gap: 12px;
    padding-bottom: 8px;
  }
  .group {
    padding: 20px 0 4px;
  }
  .probe {
    gap: 12px;
    padding: 6px 0;
  }
  .note {
    align-items: flex-start;
    gap: 12px;
    padding: 16px;
    margin: 20px 0 16px;
    border-radius: 16px;
    background: var(--surface-container-high);
  }
  .gap {
    gap: 6px;
  }
</style>
