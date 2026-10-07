<script lang="ts">
  // Simple routing: two ready-made switches and three lists (directly, through VPN, blocked) of
  // sites or IP addresses, applied on top of the server's routing. Long lists are folded.
  import TopBar from "../components/TopBar.svelte";
  import Switch from "../components/Switch.svelte";
  import Icon from "../components/Icon.svelte";
  import Dialog from "../components/Dialog.svelte";
  import TransferMenu from "../components/TransferMenu.svelte";
  import { app, act, setSetting } from "../lib/state.svelte";
  import { call } from "../lib/bridge";
  import { t, type StringKey } from "../lib/i18n";
  import { PREFIXES, insertPrefix, kindLabel } from "../lib/rules";
  import type { IconName } from "../lib/icons";
  import type { RouteAction, RuleView } from "../lib/types";

  const FOLDED = 5;
  const SEARCH_FROM = 10;

  const groups: { action: RouteAction; icon: IconName; title: StringKey; hint: StringKey }[] = [
    { action: "direct", icon: "public", title: "routing_direct", hint: "routing_direct_hint" },
    { action: "proxy", icon: "shield", title: "routing_proxy", hint: "routing_proxy_hint" },
    { action: "block", icon: "block", title: "routing_block", hint: "routing_block_hint" },
  ];

  let expanded = $state<Record<RouteAction, boolean>>({ direct: false, proxy: false, block: false });
  let query = $state<Record<RouteAction, string>>({ direct: "", proxy: "", block: "" });
  let adding = $state<RouteAction | null>(null);
  let editing = $state<{ action: RouteAction; rule: RuleView } | null>(null);
  let clearing = $state<RouteAction | null>(null);

  // Add / edit dialog.
  let text = $state("");
  let invalid = $state<string[]>([]);
  let area = $state<HTMLTextAreaElement | HTMLInputElement | null>(null);

  const r = $derived(app.routing);
  const titleOf = (a: RouteAction) => t(groups.find((g) => g.action === a)!.title);
  const sorted = (list: RuleView[]) => [...list].sort((a, b) => a.display.localeCompare(b.display));

  function visible(action: RouteAction, list: RuleView[]) {
    const open = expanded[action] && list.length > FOLDED;
    if (!open) return list.slice(0, FOLDED);
    const q = query[action].trim().toLowerCase();
    return q ? list.filter((e) => e.display.toLowerCase().includes(q)) : list;
  }

  function startAdd(action: RouteAction) {
    text = "";
    invalid = [];
    adding = action;
  }

  function startEdit(action: RouteAction, rule: RuleView) {
    text = rule.editable;
    invalid = [];
    editing = { action, rule };
  }

  async function submit() {
    const parsed = await call<{ valid: RuleView[]; invalid: string[] }>("parseRules", { text });
    if (parsed.invalid.length > 0) {
      invalid = parsed.invalid;
      return;
    }
    if (parsed.valid.length === 0) return;
    if (adding) {
      await act("addRules", { action: adding, text });
      adding = null;
    } else if (editing) {
      await act("replaceRule", { action: editing.action, old: editing.rule.entry, text });
      editing = null;
    }
  }

  function prefix(p: string) {
    const el = area;
    const start = el?.selectionStart ?? text.length;
    const end = el?.selectionEnd ?? text.length;
    const [next, cursor] = insertPrefix(text, start, end, p);
    text = next;
    invalid = [];
    queueMicrotask(() => {
      el?.focus();
      el?.setSelectionRange(cursor, cursor);
    });
  }
</script>

<div class="page">
  <TopBar title={t("routing_title")}>
    {#snippet actions()}<TransferMenu kind="routing" />{/snippet}
  </TopBar>
  <div class="scroll">
    <div class="content">
      <button class="card pad row switch-card interactive" onclick={() => setSetting("routingEnabled", !r.enabled)}>
        <span class="grow col">
          <span class="title-medium">{t("routing_enabled")}</span>
          <span class="body-medium muted">{t("routing_enabled_hint")}</span>
        </span>
        <Switch checked={r.enabled} onchange={(v) => setSetting("routingEnabled", v)} />
      </button>

      <div class:dim={!r.enabled}>
        <div class="label-large primary-text group-label">{t("routing_presets")}</div>
        <div class="card">
          {@render preset("lan", "routing_lan", "routing_lan_hint", r.lanDirect, "routingLanDirect")}
          <hr class="divider" />
          {@render preset("language", "routing_ru", "routing_ru_hint", r.ruDirect, "routingRuDirect")}
        </div>

        {#each groups as g (g.action)}
          {@const list = sorted(r[g.action])}
          {@const foldable = list.length > FOLDED}
          {@const open = expanded[g.action] && foldable}
          {@const shown = visible(g.action, list)}
          <div class="card rules">
            <div class="group-head" class:sticky={open} class:alone={list.length === 0}>
              <div class="row head">
                <span class="primary-text"><Icon name={g.icon} /></span>
                <span class="grow col">
                  <span class="title-medium">{t(g.title)} · {list.length}</span>
                  <span class="body-small muted">{t(g.hint)}</span>
                </span>
                <button class="btn text with-icon interactive" disabled={!r.enabled} onclick={() => startAdd(g.action)}>
                  <Icon name="add" size={18} />{t("routing_add")}
                </button>
              </div>
              {#if list.length === 0}
                <div class="body-medium muted empty">{t("routing_empty")}</div>
              {/if}
              {#if open}
                {#if list.length > SEARCH_FROM}
                  <label class="field round small search">
                    <Icon name="search" />
                    <input bind:value={query[g.action]} placeholder={t("routing_search")} />
                    {#if query[g.action]}
                      <button class="icon-btn interactive" title={t("action_close")} onclick={() => (query[g.action] = "")}><Icon name="close" /></button>
                    {/if}
                  </label>
                {/if}
                <div class="row fold-row">
                  <button class="btn text interactive" onclick={() => (expanded[g.action] = false)}>
                    {t("routing_fold")}<Icon name="keyboard_arrow_up" size={18} />
                  </button>
                  <span class="grow"></span>
                  <!-- Thousands of imported addresses must not have to be removed one by one. -->
                  <button class="btn text with-icon interactive" disabled={!r.enabled} onclick={() => (clearing = g.action)}>
                    <Icon name="delete_sweep" size={18} />{t("routing_clear")}
                  </button>
                </div>
              {/if}
            </div>
            {#each shown as rule (rule.entry)}
              {@const label = kindLabel(rule.kind)}
              <!-- A click opens the entry for editing (to fix a typo without retyping it). -->
              <div
                class="rule interactive"
                role="button"
                tabindex="0"
                aria-disabled={!r.enabled}
                onclick={() => r.enabled && startEdit(g.action, rule)}
                onkeydown={(e) => e.key === "Enter" && r.enabled && startEdit(g.action, rule)}
              >
                <span class="row grow">
                  <span class="body-large ellipsis">{rule.display}</span>
                  {#if label}<span class="badge kind">{t(label)}</span>{/if}
                </span>
                <button
                  class="icon-btn interactive"
                  title={t("routing_remove")}
                  disabled={!r.enabled}
                  onclick={(e) => {
                    e.stopPropagation();
                    act("removeRule", { action: g.action, entry: rule.entry });
                  }}
                >
                  <Icon name="close" size={18} />
                </button>
              </div>
            {/each}
            {#if open && shown.length === 0}
              <div class="body-medium muted empty">{t("routing_nothing_found")}</div>
            {/if}
            {#if foldable && !open}
              <div class="fold-row">
                <button class="btn text interactive" onclick={() => (expanded[g.action] = true)}>
                  {t("routing_show_all", list.length)}<Icon name="keyboard_arrow_down" size={18} />
                </button>
              </div>
            {:else if list.length > 0}
              <div class="bottom"></div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  </div>
</div>

{#snippet preset(icon: IconName, title: StringKey, hint: StringKey, checked: boolean, key: string)}
  <button class="list-item three interactive" disabled={!r.enabled} onclick={() => setSetting(key, !checked)}>
    <span class="lead primary"><Icon name={icon} /></span>
    <span class="grow col">
      <span class="body-large">{t(title)}</span>
      <span class="body-medium muted">{t(hint)}</span>
    </span>
    <Switch {checked} disabled={!r.enabled} onchange={(v) => setSetting(key, v)} />
  </button>
{/snippet}

{#if adding || editing}
  {@const action = (adding ?? editing?.action)!}
  <Dialog
    title={adding ? t("routing_add_title", titleOf(action)) : t("routing_edit_title", titleOf(action))}
    onclose={() => ((adding = null), (editing = null))}
  >
    <label class="field" class:error={invalid.length > 0}>
      {#if adding}<span class="field-label">{t("routing_add_label")}</span>{/if}
      {#if adding}
        <!-- svelte-ignore a11y_autofocus -->
        <textarea bind:this={area} bind:value={text} rows="3" autofocus spellcheck="false" oninput={() => (invalid = [])}></textarea>
      {:else}
        <!-- svelte-ignore a11y_autofocus -->
        <input
          bind:this={area}
          bind:value={text}
          autofocus
          spellcheck="false"
          oninput={() => (invalid = [])}
          onkeydown={(e) => e.key === "Enter" && submit()}
        />
      {/if}
    </label>
    <div class="field-hint" class:error={invalid.length > 0}>
      {invalid.length > 0 ? t("routing_invalid", invalid.join(", ")) : t("routing_add_hint")}
    </div>
    {#if adding}
      <div class="row chips">
        {#each PREFIXES as p}
          <button class="chip assist interactive" onclick={() => prefix(p.prefix)}>
            <Icon name="add" size={18} />{t(kindLabel(p.kind)!)}
          </button>
        {/each}
      </div>
    {/if}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => ((adding = null), (editing = null))}>{t("action_cancel")}</button>
      <button class="btn text interactive" disabled={!text.trim()} onclick={submit}>
        {adding ? t("routing_add") : t("routing_save")}
      </button>
    {/snippet}
  </Dialog>
{/if}

{#if clearing}
  {@const action = clearing}
  <Dialog title={t("routing_clear_title", titleOf(action))} onclose={() => (clearing = null)}>
    {t("routing_clear_text", r[action].length)}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (clearing = null)}>{t("action_cancel")}</button>
      <button
        class="btn text interactive"
        onclick={() => {
          act("clearRules", { action });
          clearing = null;
        }}>{t("routing_clear")}</button
      >
    {/snippet}
  </Dialog>
{/if}

<style>
  .switch-card {
    width: 100%;
    gap: 12px;
    text-align: left;
  }
  .dim :global(.card) {
    opacity: 0.5;
  }
  .group-label {
    padding: 16px 4px 12px;
  }
  .rules {
    margin-top: 12px;
    overflow: visible;
  }
  .group-head {
    background: var(--surface-container);
    border-radius: 24px 24px 0 0;
  }
  .group-head.alone {
    border-radius: 24px;
  }
  .group-head.sticky {
    position: sticky;
    top: 0;
    z-index: 2;
  }
  .head {
    gap: 16px;
    padding: 8px 4px 4px 16px;
  }
  .empty {
    padding: 4px 16px 16px 56px;
  }
  .search {
    margin: 4px 16px 0 56px;
  }
  .search input {
    padding: 10px 4px;
  }
  .fold-row {
    display: flex;
    align-items: center;
    padding: 0 4px 4px 44px;
  }
  .rule {
    display: flex;
    align-items: center;
    min-height: 44px;
    padding: 0 4px 0 56px;
    content-visibility: auto;
    contain-intrinsic-size: auto 44px;
  }
  .rule[aria-disabled="true"] {
    cursor: default;
  }
  .kind {
    margin-left: 8px;
  }
  .bottom {
    height: 8px;
  }
  textarea {
    min-height: 96px;
  }
  .chips {
    gap: 8px;
    padding-top: 12px;
    flex-wrap: wrap;
  }
</style>
