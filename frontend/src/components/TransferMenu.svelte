<script lang="ts">
  // ⋮ in the top bar: save the settings to a file, copy the file to paste it into Telegram, or load
  // a file someone sent.
  import Icon from "./Icon.svelte";
  import Menu from "./Menu.svelte";
  import { call, system } from "../lib/bridge";
  import { inspectSettingsFile, snackbar } from "../lib/state.svelte";
  import { t } from "../lib/i18n";

  let { kind }: { kind: "routing" | "apps" } = $props();
  let menu = $state<{ x: number; y: number } | null>(null);

  async function exported() {
    return call<{ fileName: string; content: string }>("exportSettings", { kind });
  }

  async function save() {
    try {
      const f = await exported();
      if (await system.saveTextFile(f.fileName, f.content)) snackbar(t("export_saved"));
    } catch {
      snackbar(t("export_failed"));
    }
  }

  async function copy() {
    try {
      const f = await exported();
      await system.copyFileToClipboard(f.fileName, f.content);
      snackbar(t("transfer_share_hint"));
    } catch {
      snackbar(t("export_failed"));
    }
  }

  async function load() {
    try {
      const content = await system.openTextFile();
      if (content) inspectSettingsFile(content);
    } catch {
      snackbar(t("import_error_read"));
    }
  }
</script>

<button
  class="icon-btn interactive"
  title={t("transfer_menu")}
  onclick={(e) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    menu = { x: r.right - 220, y: r.bottom };
  }}
>
  <Icon name="more_vert" />
</button>

{#if menu}
  <Menu
    x={menu.x}
    y={menu.y}
    onclose={() => (menu = null)}
    items={[
      { label: t("transfer_save"), icon: "save", action: save },
      { label: t("transfer_share"), icon: "content_copy", action: copy },
      { label: t("transfer_load"), icon: "file_open", action: load },
    ]}
  />
{/if}
