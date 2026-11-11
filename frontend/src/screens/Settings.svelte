<script lang="ts">
  import TopBar from "../components/TopBar.svelte";
  import SettingsRow from "../components/SettingsRow.svelte";
  import Switch from "../components/Switch.svelte";
  import Spinner from "../components/Spinner.svelte";
  import Dialog from "../components/Dialog.svelte";
  import Radio from "../components/Radio.svelte";
  import Icon from "../components/Icon.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import { app, ui, open, act, setSetting, refreshSubscription, snackbar, sendTvKey } from "../lib/state.svelte";
  import { call, system } from "../lib/bridge";
  import { t, plural } from "../lib/i18n";
  import { relativeTime } from "../lib/format";

  let chooseInterval = $state(false);
  let confirmLogout = $state(false);
  let tvCode = $state<string | null>(null);
  // What was typed off the TV's screen: 6 Latin letters and digits, spaces and dashes don't count,
  // and letters typed with the Russian layout on count as the Latin ones on the same keys.
  const russianKeys: Record<string, string> = { "й": "q", "ц": "w", "у": "e", "к": "r", "е": "t", "н": "y", "г": "u", "ш": "i", "щ": "o", "з": "p", "ф": "a", "ы": "s", "в": "d", "а": "f", "п": "g", "р": "h", "о": "j", "л": "k", "д": "l", "я": "z", "ч": "x", "с": "c", "м": "v", "и": "b", "т": "n", "ь": "m" };
  const latin = (text: string) => Array.from(text, (c) => russianKeys[c.toLowerCase()] ?? c).join("");
  const tvCodeClean = $derived(latin(tvCode ?? "").toUpperCase().replace(/[\s-]/g, ""));

  const s = $derived(app.settings);
  const subHours = $derived(app.subscription.info?.updateIntervalHours || 6);
  const intervals = [0, 1, 3, 6, 12, 24, -1];
  const excludedCount = $derived(app.exclusions.effective.length);

  function intervalLabel(h: number) {
    if (h === 0) return t("update_interval_subscription", subHours);
    if (h === -1) return t("update_interval_manual");
    return plural("update_every_hours", h);
  }

  async function checkUpdates() {
    try {
      const r = await call<{ available: boolean }>("checkForUpdates");
      if (!r.available) snackbar(t("settings_up_to_date"));
    } catch {
      snackbar(t("error_download"));
    }
  }

  async function setAutostart(on: boolean) {
    try {
      await system.setAutostart(on);
      ui.autostart = on;
    } catch (e) {
      snackbar(t("error_generic", e instanceof Error ? e.message : String(e)));
    }
  }
</script>

<div class="page">
  <TopBar title={t("settings")} />
  <div class="scroll">
    <div class="content flat">
      <div class="section-title">{t("settings_subscription")}</div>
      <SettingsRow
        icon="refresh"
        title={t("settings_update_subscription")}
        subtitle={app.subscription.info ? t("settings_updated_at", relativeTime(app.subscription.info.fetchedAt)) : ""}
        onclick={() => refreshSubscription()}
      >
        {#snippet trailing()}
          {#if app.subscription.refreshing}<Spinner size={20} stroke={2} />{/if}
        {/snippet}
      </SettingsRow>
      <SettingsRow
        icon="schedule"
        title={t("settings_auto_update")}
        subtitle={intervalLabel(s?.subscriptionUpdateHours ?? 0)}
        onclick={() => (chooseInterval = true)}
      />
      <SettingsRow icon="key" title={t("settings_change_key")} onclick={() => open("changeKey")} />
      <SettingsRow icon="logout" title={t("settings_logout")} onclick={() => (confirmLogout = true)} />

      <div class="section-title">{t("settings_vpn")}</div>
      <SettingsRow
        icon="apps"
        title={t("settings_split")}
        subtitle={excludedCount === 0 ? t("settings_split_none") : t("settings_split_count", excludedCount)}
        onclick={() => open("apps")}
      />
      <SettingsRow
        icon="wand_stars"
        title={t("settings_recommended")}
        subtitle={t("settings_recommended_hint")}
        onclick={() => setSetting("useRecommendedExclusions", !app.exclusions.useRecommended)}
      >
        {#snippet trailing()}
          <Switch checked={app.exclusions.useRecommended} onchange={(v) => setSetting("useRecommendedExclusions", v)} />
        {/snippet}
      </SettingsRow>
      <SettingsRow icon="alt_route" title={t("settings_routing")} subtitle={t("settings_routing_hint")} onclick={() => open("routing")} />
      <SettingsRow
        icon="fact_check"
        title={t("settings_whitelist")}
        subtitle={t("settings_whitelist_hint")}
        onclick={() => setSetting("whitelistDetect", !s?.whitelistDetect)}
      >
        {#snippet trailing()}
          <Switch checked={!!s?.whitelistDetect} onchange={(v) => setSetting("whitelistDetect", v)} />
        {/snippet}
      </SettingsRow>
      <SettingsRow
        icon="wifi_tethering"
        title={t("settings_share")}
        subtitle={t(s?.share.enabled ? "share_on" : "share_off")}
        onclick={() => open("share")}
      />
      <SettingsRow icon="tune" title={t("settings_advanced")} subtitle={t("settings_advanced_hint")} onclick={() => open("advanced")} />
      <SettingsRow icon="connected_tv" title={t("settings_connect_tv")} subtitle={t("settings_connect_tv_hint")} onclick={() => (tvCode = "")} />

      <div class="section-title">{t("settings_windows")}</div>
      <SettingsRow
        icon="computer"
        title={t("settings_autostart")}
        subtitle={t("settings_autostart_hint")}
        onclick={() => setAutostart(!ui.autostart)}
      >
        {#snippet trailing()}
          <Switch checked={ui.autostart} onchange={setAutostart} />
        {/snippet}
      </SettingsRow>
      <SettingsRow
        icon="bolt"
        title={t("settings_autoconnect")}
        subtitle={t("settings_autoconnect_hint")}
        onclick={() => setSetting("autoConnect", !s?.autoConnect)}
      >
        {#snippet trailing()}
          <Switch checked={!!s?.autoConnect} onchange={(v) => setSetting("autoConnect", v)} />
        {/snippet}
      </SettingsRow>

      <div class="section-title">{t("settings_interface")}</div>
      <SettingsRow
        icon="swap_horiz"
        title={t("settings_servers_left")}
        subtitle={t("settings_servers_left_hint")}
        onclick={() => setSetting("serversOnLeft", !s?.serversOnLeft)}
      >
        {#snippet trailing()}
          <Switch checked={!!s?.serversOnLeft} onchange={(v) => setSetting("serversOnLeft", v)} />
        {/snippet}
      </SettingsRow>

      <div class="section-title">{t("settings_diagnostics")}</div>
      <SettingsRow
        icon="receipt_long"
        title={t("settings_connection_log")}
        subtitle={t(s?.connectionLog ? "settings_connection_log_on" : "settings_connection_log_off")}
        onclick={() => open("connlog")}
      />
      <SettingsRow icon="folder_open" title={t("settings_open_log")} onclick={() => system.openLogFolder()} />

      <div class="section-title">{t("settings_support")}</div>
      {#each app.config.support as contact}
        <button class="list-item interactive" onclick={() => system.openURL(contact.url)}>
          <span class="lead primary">
            {#if contact.type === "telegram"}<TelegramIcon />{:else if contact.type === "vk"}<Icon name="forum" />{:else if contact.type === "email"}<Icon name="mail" />{:else}<Icon name="link" />{/if}
          </span>
          <span class="body-large grow">{contact.title}</span>
        </button>
      {/each}

      <div class="section-title">{t("settings_about")}</div>
      <SettingsRow icon="info" title={t("settings_version", app.meta.version)} subtitle={"Xray-core " + app.meta.xray} />
      <SettingsRow icon="install_desktop" title={t("settings_check_update")} onclick={checkUpdates} />
      <SettingsRow icon="policy" title={t("settings_privacy")} onclick={() => system.openURL(`https://${app.config.site.domain}/privacy`)} />
      <SettingsRow icon="description" title={t("settings_terms")} onclick={() => system.openURL(`https://${app.config.site.domain}/terms`)} />
      <div class="body-small muted licenses">{t("settings_licenses")}</div>
    </div>
  </div>
</div>

{#if chooseInterval}
  <Dialog title={t("settings_auto_update")} onclose={() => (chooseInterval = false)}>
    {#each intervals as h}
      <button
        class="option interactive"
        onclick={() => {
          setSetting("subscriptionUpdateHours", h);
          chooseInterval = false;
        }}
      >
        <Radio checked={h === (s?.subscriptionUpdateHours ?? 0)} />
        <span class="body-large">{intervalLabel(h)}</span>
      </button>
    {/each}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (chooseInterval = false)}>{t("action_close")}</button>
    {/snippet}
  </Dialog>
{/if}

{#if tvCode !== null}
  <Dialog title={t("settings_connect_tv")} onclose={() => (tvCode = null)}>
    <div class="body-medium muted">{t("tv_code_text")}</div>
    <form
      onsubmit={(e) => {
        e.preventDefault();
        const code = tvCodeClean; // read before the dialog closes: it comes from the field
        if (code.length !== 6) return;
        tvCode = null;
        sendTvKey(code);
      }}
    >
      <label class="field tv-code">
        <span class="field-label">{t("tv_code_label")}</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input
          bind:value={tvCode}
          oninput={() => (tvCode = latin(tvCode ?? ""))}
          placeholder="ABC 123"
          maxlength="7"
          autofocus
          spellcheck="false"
          autocomplete="off"
        />
      </label>
    </form>
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (tvCode = null)}>{t("action_cancel")}</button>
      <button
        class="btn text interactive"
        disabled={!/^[A-Z0-9]{6}$/.test(tvCodeClean)}
        onclick={() => {
          const code = tvCodeClean;
          tvCode = null;
          sendTvKey(code);
        }}>{t("tv_send_action")}</button
      >
    {/snippet}
  </Dialog>
{/if}

{#if confirmLogout}
  <Dialog title={t("settings_logout")} onclose={() => (confirmLogout = false)}>
    {t("settings_logout_text")}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (confirmLogout = false)}>{t("action_cancel")}</button>
      <button
        class="btn text interactive"
        onclick={() => {
          confirmLogout = false;
          act("logout");
          ui.stack = [];
        }}>{t("settings_logout")}</button
      >
    {/snippet}
  </Dialog>
{/if}

<style>
  .flat {
    padding: 0 0 24px;
  }
  .option {
    display: flex;
    align-items: center;
    gap: 16px;
    width: 100%;
    padding: 10px 8px;
    border-radius: 8px;
    color: var(--on-surface);
    text-align: left;
  }
  .tv-code {
    margin-top: 16px;
  }
  .tv-code input {
    font-family: var(--mono);
    font-size: 20px;
    letter-spacing: 0.15em;
    text-transform: uppercase;
  }
  .licenses {
    padding: 8px 16px 0 56px;
  }
</style>
