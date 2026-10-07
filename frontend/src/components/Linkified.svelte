<script lang="ts">
  // Makes URLs, @telegram handles and bare domains in announcement text clickable.
  import { system } from "../lib/bridge";

  let { text }: { text: string } = $props();
  const pattern = /(https?:\/\/[^\s]+)|(@[A-Za-z0-9_]{4,})|(\b[A-Za-z0-9-]+\.(?:info|ru|com|net|app|me)\b(?:\/[^\s]*)?)/g;

  type Part = { text: string; url?: string };
  const parts = $derived.by(() => {
    const out: Part[] = [];
    let last = 0;
    for (const m of text.matchAll(pattern)) {
      const raw = m[0];
      const value = raw.replace(/[.,)!]+$/, "");
      out.push({ text: text.slice(last, m.index) });
      const url = value.startsWith("@") ? "https://t.me/" + value.slice(1) : value.startsWith("http") ? value : "https://" + value;
      out.push({ text: value, url });
      out.push({ text: raw.slice(value.length) });
      last = (m.index ?? 0) + raw.length;
    }
    out.push({ text: text.slice(last) });
    return out;
  });
</script>

<span class="text">{#each parts as p}{#if p.url}<a
        href={p.url}
        onclick={(e) => {
          e.preventDefault();
          system.openURL(p.url!);
        }}>{p.text}</a
      >{:else}{p.text}{/if}{/each}</span>

<style>
  .text {
    white-space: pre-wrap;
    user-select: text;
  }
  a {
    color: var(--primary);
  }
</style>
