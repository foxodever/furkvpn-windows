<script lang="ts">
  // "Раздача интернета": an HTTP and a SOCKS5 proxy for other devices on the local network, with
  // the computer's addresses and how to set the proxy up on each kind of device.
  import TopBar from "../components/TopBar.svelte";
  import Switch from "../components/Switch.svelte";
  import Icon from "../components/Icon.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import { app, act, setSetting, snackbar } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t, type StringKey } from "../lib/i18n";
  import type { IconName } from "../lib/icons";

  const s = $derived(app.settings?.share);
  const running = $derived(app.vpn.status === "connected" ? app.vpn.share : undefined);
  const httpPort = $derived(running?.httpPort ?? s?.httpPort ?? 8080);
  const socksPort = $derived(running?.socksPort ?? s?.socksPort ?? 1080);
  const addresses = $derived(
    [...(app.lan ?? [])].sort((a, b) => order(a.kind) - order(b.kind)),
  );
  const shownAddress = $derived(addresses[0]?.ip ?? t("share_phone_ip"));

  function order(kind: string) {
    return ["hotspot", "wifi", "ethernet"].indexOf(kind) + 1 || 9;
  }

  const kindName: Record<string, StringKey> = { wifi: "share_if_wifi", hotspot: "share_if_hotspot", ethernet: "share_if_ethernet" };

  let http = $state("");
  let socks = $state("");
  $effect(() => {
    http = String(s?.httpPort ?? 8080);
    socks = String(s?.socksPort ?? 1080);
  });
  const hp = $derived(Number(http));
  const sp = $derived(Number(socks));
  const valid = $derived(Number.isInteger(hp) && Number.isInteger(sp) && hp >= 1024 && hp <= 65535 && sp >= 1024 && sp <= 65535 && hp !== sp);
  const changed = $derived(hp !== s?.httpPort || sp !== s?.socksPort);

  let showPassword = $state(false);
  let openGuide = $state<string | null>(null);

  const guides: { title: StringKey; text: StringKey; icon: IconName | "telegram" }[] = [
    { title: "guide_windows", text: "guide_windows_text", icon: "desktop_windows" },
    { title: "guide_macos", text: "guide_macos_text", icon: "laptop_mac" },
    { title: "guide_ios", text: "guide_ios_text", icon: "mobile" },
    { title: "guide_android", text: "guide_android_text", icon: "android" },
    { title: "guide_tv", text: "guide_tv_text", icon: "tv" },
    { title: "guide_apps", text: "guide_apps_text", icon: "telegram" },
  ];

  async function copy(value: string) {
    await system.copyText(value);
    snackbar(t("share_copied"));
  }

  const dots = (p: string) => "•".repeat(Math.min(p.length, 14));
</script>

<div class="page">
  <TopBar title={t("share_title")} />
  <div class="scroll">
    <div class="content stack">
      <button class="card pad row switch-card interactive" onclick={() => setSetting("shareEnabled", !s?.enabled)}>
        <span class="grow col">
          <span class="title-medium">{t("share_switch")}</span>
          <span class="body-medium muted">{t("share_switch_hint")}</span>
        </span>
        <Switch checked={!!s?.enabled} onchange={(v) => setSetting("shareEnabled", v)} />
      </button>

      {#if s?.enabled}
        {#if !running}
          <div class="card secondary pad body-medium">{t("share_waiting")}</div>
        {:else if addresses.length === 0}
          <div class="card secondary pad body-medium">{t("share_no_network")}</div>
        {:else}
          <div class="card primary details">
            {#each addresses as a}
              {@render copyRow(t(kindName[a.kind] ?? "share_if_ethernet"), a.ip)}
            {/each}
            {@render copyRow(t("share_http_port"), String(httpPort))}
            {@render copyRow(t("share_socks_port"), String(socksPort))}
          </div>
        {/if}

        <div class="card pad">
          <div class="title-medium">{t("share_ports")}</div>
          <div class="row ports">
            <label class="field grow">
              <span class="field-label">{t("share_port_http")}</span>
              <input bind:value={http} inputmode="numeric" maxlength="5" oninput={() => (http = http.replace(/\D/g, ""))} />
            </label>
            <label class="field grow">
              <span class="field-label">{t("share_port_socks")}</span>
              <input bind:value={socks} inputmode="numeric" maxlength="5" oninput={() => (socks = socks.replace(/\D/g, ""))} />
            </label>
          </div>
          <div class="body-small" class:muted={valid} class:error-text={!valid}>
            {t(valid ? "share_ports_hint" : "share_ports_invalid")}
          </div>
          {#if changed}
            <div class="row end">
              <button class="btn text interactive" disabled={!valid} onclick={() => act("setSharePorts", { http: hp, socks: sp })}>{t("share_save")}</button>
            </div>
          {/if}
        </div>

        <div class="card">
          <button class="row auth interactive" onclick={() => setSetting("shareAuth", !s.auth)}>
            <span class="primary-text"><Icon name="lock" /></span>
            <span class="grow col">
              <span class="title-medium">{t("share_auth")}</span>
              <span class="body-small muted">{t("share_auth_hint")}</span>
            </span>
            <Switch checked={s.auth} onchange={(v) => setSetting("shareAuth", v)} />
          </button>
          {#if s.auth && s.user}
            <hr class="divider" />
            {@render copyRow(t("share_login"), s.user)}
            <div class="row secret">
              <span class="grow col">
                <span class="label-medium">{t("share_password")}</span>
                <span class="title-medium mono selectable">{showPassword ? s.password : dots(s.password)}</span>
              </span>
              <button class="icon-btn interactive" title={showPassword ? t("hide") : t("show")} onclick={() => (showPassword = !showPassword)}>
                <Icon name={showPassword ? "visibility_off" : "visibility"} size={20} />
              </button>
              <button class="icon-btn interactive" title={t("copy")} onclick={() => copy(s.password)}>
                <Icon name="content_copy" size={20} />
              </button>
            </div>
            <hr class="divider" />
            <button class="row reset interactive" onclick={() => act("resetShareCredentials")}>
              <span class="primary-text"><Icon name="refresh" /></span>
              <span class="body-large">{t("share_reset")}</span>
            </button>
          {/if}
        </div>
      {/if}

      <div class="title-medium step">{t("share_step1_title")}</div>
      <div class="body-medium muted">{t("share_step1_text")}</div>
      <div><button class="btn outlined interactive" onclick={() => system.openHotspotSettings()}>{t("share_hotspot_settings")}</button></div>

      <div class="title-medium step">{t("share_step2_title")}</div>
      <div class="body-medium muted">{t("share_step2_text")}</div>
      {#if s?.auth && s.user}
        <!-- The password stays hidden here too: it is shown and copied in the block above. -->
        <div class="body-medium muted">{t("share_guide_auth", s.user, dots(s.password))}</div>
      {/if}
      <div class="card">
        {#each guides as g, i}
          {#if i > 0}<hr class="divider" />{/if}
          <button class="guide interactive" onclick={() => (openGuide = openGuide === g.title ? null : g.title)}>
            <span class="row">
              <span class="primary-text">{#if g.icon === "telegram"}<TelegramIcon size={22} />{:else}<Icon name={g.icon} size={22} />{/if}</span>
              <span class="body-large grow">{t(g.title)}</span>
              <Icon name={openGuide === g.title ? "keyboard_arrow_up" : "keyboard_arrow_down"} />
            </span>
            {#if openGuide === g.title}
              <span class="body-medium guide-text">{t(g.text, shownAddress, String(httpPort), String(socksPort))}</span>
            {/if}
          </button>
        {/each}
      </div>

      <div class="title-medium step">{t("share_step3_title")}</div>
      <div class="body-medium muted">{t("share_step3_text")}</div>
      <div class="row warning">
        <span class="muted"><Icon name="info" size={18} /></span>
        <span class="body-small muted">{t("share_warning")}</span>
      </div>
    </div>
  </div>
</div>

{#snippet copyRow(label: string, value: string)}
  <button class="row copy interactive" onclick={() => copy(value)}>
    <span class="grow col">
      <span class="label-medium">{label}</span>
      <span class="title-large mono">{value}</span>
    </span>
    <Icon name="content_copy" size={20} />
  </button>
{/snippet}

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .switch-card {
    width: 100%;
    gap: 12px;
    text-align: left;
  }
  .details {
    padding: 8px 0;
  }
  .copy {
    width: 100%;
    padding: 8px 16px;
    gap: 12px;
    text-align: left;
  }
  .ports {
    gap: 12px;
    margin: 16px 0 6px;
    --field-bg: var(--surface-container);
  }
  .end {
    justify-content: flex-end;
  }
  .auth {
    width: 100%;
    padding: 16px;
    gap: 16px;
    text-align: left;
  }
  .secret {
    padding: 4px 4px 4px 16px;
  }
  .reset {
    width: 100%;
    gap: 16px;
    padding: 14px 16px;
  }
  .step {
    padding-top: 8px;
  }
  .guide {
    display: flex;
    flex-direction: column;
    width: 100%;
    padding: 14px 16px;
    text-align: left;
  }
  .guide .row {
    gap: 14px;
  }
  .guide-text {
    padding: 8px 0 0 36px;
    user-select: text;
  }
  .warning {
    gap: 8px;
    align-items: flex-start;
    padding: 4px 0 24px;
  }
</style>
