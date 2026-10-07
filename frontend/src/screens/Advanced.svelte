<script lang="ts">
  // "Расширенные настройки": how the computer's traffic gets into the VPN. The TUN adapter and its
  // parameters, or, with it off, a proxy on 127.0.0.1 that the app sets as Windows' proxy.
  import TopBar from "../components/TopBar.svelte";
  import SettingsRow from "../components/SettingsRow.svelte";
  import Switch from "../components/Switch.svelte";
  import Dialog from "../components/Dialog.svelte";
  import Icon from "../components/Icon.svelte";
  import { app, act, setSetting, snackbar } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t } from "../lib/i18n";

  const DEFAULT_MTU = 1500;

  const tun = $derived(app.settings?.tun);
  // The port the proxy actually got (the next free one when the chosen one is busy).
  const port = $derived(app.vpn.status === "connected" && app.vpn.localPort ? app.vpn.localPort : (tun?.proxyPort ?? 10808));

  let editing = $state<"mtu" | "port" | null>(null);
  let value = $state("");
  let confirmReset = $state(false);

  const number = $derived(Number(value));
  const valid = $derived(
    Number.isInteger(number) && (editing === "mtu" ? number >= 1280 && number <= 9000 : number >= 1024 && number <= 65535),
  );

  function edit(what: "mtu" | "port") {
    editing = what;
    value = String(what === "mtu" ? (tun?.mtu ?? DEFAULT_MTU) : (tun?.proxyPort ?? 10808));
  }

  function save() {
    if (!valid || !editing) return;
    setSetting(editing === "mtu" ? "tunMtu" : "proxyPort", number);
    editing = null;
  }

  async function copyAddress() {
    await system.copyText("127.0.0.1:" + port);
    snackbar(t("share_copied"));
  }
</script>

<div class="page">
  <TopBar title={t("advanced_title")} />
  <div class="scroll">
    <div class="content flat">
      <div class="body-medium muted intro">{t("advanced_intro")}</div>

      <div class="section-title">TUN</div>
      <SettingsRow
        icon="vpn_lock"
        title={t("tun_mode")}
        subtitle={t(tun?.enabled ? "tun_mode_on" : "tun_mode_off")}
        onclick={() => setSetting("tunEnabled", !tun?.enabled)}
      >
        {#snippet trailing()}
          <Switch checked={!!tun?.enabled} onchange={(v) => setSetting("tunEnabled", v)} />
        {/snippet}
      </SettingsRow>
      {#if tun?.enabled}
        <SettingsRow
          icon="straighten"
          title="MTU"
          subtitle={tun.mtu === DEFAULT_MTU ? t("tun_mtu_default", String(tun.mtu)) : String(tun.mtu)}
          onclick={() => edit("mtu")}
        />
        <SettingsRow
          icon="looks_6"
          title={t("tun_ipv6")}
          subtitle={t(tun.ipv6 ? "tun_ipv6_on" : "tun_ipv6_off")}
          onclick={() => setSetting("tunIpv6", !tun.ipv6)}
        >
          {#snippet trailing()}
            <Switch checked={tun.ipv6} onchange={(v) => setSetting("tunIpv6", v)} />
          {/snippet}
        </SettingsRow>
        <SettingsRow
          icon="dns"
          title={t("tun_strict_dns")}
          subtitle={t("tun_strict_dns_hint")}
          onclick={() => setSetting("tunStrictDns", !tun.strictDns)}
        >
          {#snippet trailing()}
            <Switch checked={tun.strictDns} onchange={(v) => setSetting("tunStrictDns", v)} />
          {/snippet}
        </SettingsRow>
      {:else if tun}
        <div class="section-title">{t("advanced_proxy")}</div>
        <SettingsRow
          icon="travel_explore"
          title={t("system_proxy")}
          subtitle={t("system_proxy_hint")}
          onclick={() => setSetting("systemProxy", !tun.systemProxy)}
        >
          {#snippet trailing()}
            <Switch checked={tun.systemProxy} onchange={(v) => setSetting("systemProxy", v)} />
          {/snippet}
        </SettingsRow>
        <SettingsRow icon="settings_ethernet" title={t("proxy_port")} subtitle={t("proxy_port_value", String(port))} onclick={() => edit("port")}>
          {#snippet trailing()}
            <button
              class="icon-btn interactive"
              title={t("copy")}
              onclick={(e) => {
                e.stopPropagation();
                copyAddress();
              }}
            >
              <Icon name="content_copy" size={20} />
            </button>
          {/snippet}
        </SettingsRow>
      {/if}

      <div class="divider-space"></div>
      <SettingsRow icon="restart_alt" title={t("advanced_reset")} onclick={() => (confirmReset = true)} />
    </div>
  </div>
</div>

{#if editing}
  <Dialog title={editing === "mtu" ? "MTU" : t("proxy_port")} onclose={() => (editing = null)}>
    <div class="body-medium muted">{t(editing === "mtu" ? "tun_mtu_dialog_text" : "proxy_port_dialog_text")}</div>
    <label class="field number">
      <span class="field-label">{editing === "mtu" ? "MTU" : t("proxy_port")}</span>
      <input
        bind:value
        inputmode="numeric"
        maxlength="5"
        oninput={() => (value = value.replace(/\D/g, ""))}
        onkeydown={(e) => e.key === "Enter" && save()}
      />
    </label>
    {#if !valid}
      <div class="body-small error-text">{t(editing === "mtu" ? "tun_mtu_invalid" : "proxy_port_invalid")}</div>
    {/if}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (editing = null)}>{t("action_cancel")}</button>
      <button class="btn text interactive" disabled={!valid} onclick={save}>{t("share_save")}</button>
    {/snippet}
  </Dialog>
{/if}

{#if confirmReset}
  <Dialog title={t("advanced_reset")} onclose={() => (confirmReset = false)}>
    {t("advanced_reset_text")}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (confirmReset = false)}>{t("action_cancel")}</button>
      <button
        class="btn text interactive"
        onclick={() => {
          confirmReset = false;
          act("resetTun");
        }}>{t("action_reset")}</button
      >
    {/snippet}
  </Dialog>
{/if}

<style>
  .flat {
    padding: 0 0 24px;
  }
  .intro {
    padding: 0 16px 8px;
  }
  .number {
    margin-top: 16px;
  }
  .divider-space {
    height: 8px;
  }
</style>
