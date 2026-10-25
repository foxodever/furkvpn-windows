<script lang="ts">
  import Spinner from "./Spinner.svelte";
  import { t } from "../lib/i18n";
  import type { Ping, ServerKind } from "../lib/types";

  let { kind, ping }: { kind?: ServerKind; ping?: Ping } = $props();

  // Real delay: a full HTTPS request through the server, not an ICMP ping.
  function color(p?: Ping) {
    if (!p) return "var(--on-surface-variant)";
    if (p.state === "failed") return "var(--error)";
    if (p.state === "ok") {
      if ((p.ms ?? 0) <= 550) return "var(--tertiary)";
      if ((p.ms ?? 0) <= 1100) return "var(--warning)";
      return "var(--error)";
    }
    return "var(--on-surface-variant)";
  }
</script>

{#if ping?.state === "testing"}
  <Spinner size={16} stroke={2} />
{:else if ping?.state === "ok"}
  <span class="label-large ping" style="color:{color(ping)}">{t("ping_ms", ping.ms)}</span>
{:else if ping?.state === "failed"}
  <span class="label-large ping" style="color:{color(ping)}">{t("ping_failed")}</span>
{:else if kind === "auto"}
  <span class="label-large primary-text ping">{t("server_auto")}</span>
{/if}

<style>
  .ping {
    padding-right: 4px;
    white-space: nowrap;
  }
</style>
