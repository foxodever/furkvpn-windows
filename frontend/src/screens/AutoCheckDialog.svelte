<script lang="ts">
  // The servers inside auto-select: fastest first, the one it would pick now marked.
  import Dialog from "../components/Dialog.svelte";
  import FlagAvatar from "../components/FlagAvatar.svelte";
  import Spinner from "../components/Spinner.svelte";
  import { act } from "../lib/state.svelte";
  import { t } from "../lib/i18n";
  import type { AutoCheck } from "../lib/types";

  let { check }: { check: AutoCheck } = $props();

  const best = $derived(
    check.servers.filter((s) => s.alive === true).sort((a, b) => (a.delay ?? 1e9) - (b.delay ?? 1e9))[0]?.tag,
  );
  const order = (alive: boolean | null) => (alive === true ? 0 : alive === null ? 1 : 2);
  const ordered = $derived(
    [...check.servers].sort((a, b) => order(a.alive) - order(b.alive) || (a.delay ?? 1e9) - (b.delay ?? 1e9)),
  );

  function color(ms: number) {
    return ms <= 550 ? "var(--tertiary)" : ms <= 1100 ? "var(--warning)" : "var(--error)";
  }

  const close = () => act("closeAutoCheck");
</script>

<Dialog title={check.name} onclose={close}>
  <p class="body-medium">{check.live ? t("auto_check_live") : t("auto_check_direct")}</p>
  {#if ordered.length === 0}
    <p class="body-medium on">{t("auto_check_empty")}</p>
  {/if}
  {#each ordered as server (server.tag)}
    <div class="row line">
      <FlagAvatar flag={server.flag} size={32} />
      <span class="grow col">
        <span class="body-large on ellipsis">{server.title}</span>
        {#if server.tag === best}<span class="label-small primary-text">{t("auto_check_best")}</span>{/if}
      </span>
      {#if server.alive === null}
        {#if check.checking}<Spinner size={16} stroke={2} />{:else}<span class="on">—</span>{/if}
      {:else if server.alive === false}
        <span class="label-large error-text">{t("auto_server_down")}</span>
      {:else}
        <span class="label-large" style="color:{color(server.delay ?? 0)}">{t("ping_ms", server.delay ?? 0)}</span>
      {/if}
    </div>
  {/each}
  {#snippet actions()}
    <button class="btn text with-icon interactive" disabled={check.checking} onclick={() => act("recheckAuto")}>
      {#if check.checking}<Spinner size={16} stroke={2} />{/if}
      {t("auto_check_now")}
    </button>
    <button class="btn text interactive" onclick={close}>{t("action_close")}</button>
  {/snippet}
</Dialog>

<style>
  p {
    margin: 0 0 12px;
  }
  .line {
    gap: 12px;
    padding: 6px 0;
  }
  .on {
    color: var(--on-surface);
  }
</style>
