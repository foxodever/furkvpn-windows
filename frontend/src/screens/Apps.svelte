<script lang="ts">
  // "Программы без VPN": programs from the Start menu and the running ones; the checked ones go
  // around the VPN (Xray routes them by their file name).
  import TopBar from "../components/TopBar.svelte";
  import Checkbox from "../components/Checkbox.svelte";
  import Icon from "../components/Icon.svelte";
  import Spinner from "../components/Spinner.svelte";
  import TransferMenu from "../components/TransferMenu.svelte";
  import { app, act, snackbar } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t } from "../lib/i18n";
  import type { Program } from "../lib/types";
  import { onMount } from "svelte";

  let programs = $state<Program[] | null>(null);
  let icons = $state<Record<string, string>>({});
  let query = $state("");

  const lower = (s: string) => s.toLowerCase();
  const excluded = $derived(new Set(app.exclusions.effective.map(lower)));
  const recommended = $derived(new Set(app.exclusions.useRecommended ? app.exclusions.recommended.map(lower) : []));

  onMount(async () => {
    try {
      programs = await system.programs();
    } catch {
      programs = [];
    }
    const paths = programs.map((p) => p.path).filter(Boolean);
    for (let i = 0; i < paths.length; i += 40) {
      const batch = await system.programIcons(paths.slice(i, i + 40)).catch(() => ({}));
      icons = { ...icons, ...batch };
    }
  });

  // Excluded programs that are not on this computer stay visible, so they can be taken off the list.
  const list = $derived.by(() => {
    if (!programs) return null;
    const known = new Set(programs.map((p) => lower(p.name)));
    const missing: Program[] = app.exclusions.effective
      .filter((n) => !known.has(lower(n)))
      .map((n) => ({ name: n, title: n.replace(/\.exe$/i, ""), path: "", running: false }));
    const q = lower(query.trim());
    return [...programs, ...missing]
      .filter((p) => !q || lower(p.title).includes(q) || lower(p.name).includes(q))
      .sort((a, b) => {
        const ea = excluded.has(lower(a.name)) ? 0 : 1;
        const eb = excluded.has(lower(b.name)) ? 0 : 1;
        return ea - eb || a.title.localeCompare(b.title);
      });
  });

  function toggle(p: Program) {
    act("setAppExcluded", { name: p.name, excluded: !excluded.has(lower(p.name)) });
  }

  async function pick() {
    try {
      const p = await system.pickProgram();
      if (!p) return;
      if (programs && !programs.some((x) => lower(x.name) === lower(p.name))) {
        programs = [p, ...programs];
        system.programIcons([p.path]).then((m) => (icons = { ...icons, ...m })).catch(() => {});
      }
      if (!excluded.has(lower(p.name))) act("setAppExcluded", { name: p.name, excluded: true });
    } catch (e) {
      snackbar(t("error_generic", e instanceof Error ? e.message : String(e)));
    }
  }
</script>

<div class="page">
  <TopBar title={t("split_title")}>
    {#snippet actions()}<TransferMenu kind="apps" />{/snippet}
  </TopBar>
  <div class="head">
    <div class="body-medium muted hint">{t("split_hint")}</div>
    <div class="row tools">
      <label class="field round small grow">
        <Icon name="search" />
        <input bind:value={query} placeholder={t("split_search")} />
      </label>
      <button class="btn tonal interactive" onclick={pick}><Icon name="add" size={18} />{t("apps_add_file")}</button>
    </div>
  </div>
  <div class="scroll">
    <div class="content flat">
      {#if list === null}
        <div class="loading row"><Spinner /><span class="body-medium muted">{t("apps_loading")}</span></div>
      {:else}
        {#each list as p (p.name.toLowerCase())}
          {@const checked = excluded.has(lower(p.name))}
          {@const isRecommended = recommended.has(lower(p.name))}
          <div class="list-item three interactive program" role="button" tabindex="0" onclick={() => toggle(p)} onkeydown={(e) => e.key === "Enter" && toggle(p)}>
            <span class="icon">
              {#if icons[p.path]}<img src={icons[p.path]} alt="" width="32" height="32" />{:else}<Icon name="deployed_code" size={28} />{/if}
            </span>
            <span class="grow col">
              <span class="body-large ellipsis">{p.title}</span>
              <span class="body-small ellipsis" class:primary-text={isRecommended} class:muted={!isRecommended}>
                {#if isRecommended}{t("split_recommended")}{:else if !p.path}{t("apps_not_found")}{:else}{p.name}{p.running ? " · " + t("apps_running") : ""}{/if}
              </span>
            </span>
            <Checkbox {checked} onchange={() => toggle(p)} />
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .head {
    max-width: 720px;
    width: 100%;
    margin: 0 auto;
    padding: 0 16px 8px;
  }
  .hint {
    padding-bottom: 12px;
  }
  .tools {
    gap: 12px;
  }
  .tools input {
    padding: 10px 4px;
  }
  .flat {
    padding: 0 0 24px;
  }
  .program {
    content-visibility: auto;
    contain-intrinsic-size: auto 72px;
  }
  .icon {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--on-surface-variant);
    flex-shrink: 0;
  }
  .loading {
    gap: 16px;
    justify-content: center;
    padding: 48px 0;
  }
</style>
