<script lang="ts">
  // A QR code: black on a white card in both themes, with the empty border scanners need.
  import qrcode from "qrcode-generator";

  let { text, size = 208, label = "" }: { text: string; size?: number; label?: string } = $props();

  const border = 4; // modules, as the standard asks

  const code = $derived.by(() => {
    const qr = qrcode(0, "M");
    qr.addData(text);
    qr.make();
    const n = qr.getModuleCount();
    // One rectangle per run of dark modules in a row.
    let d = "";
    for (let row = 0; row < n; row++) {
      for (let col = 0; col < n; col++) {
        if (!qr.isDark(row, col)) continue;
        let run = 1;
        while (col + run < n && qr.isDark(row, col + run)) run++;
        d += `M${col + border} ${row + border}h${run}v1h-${run}z`;
        col += run;
      }
    }
    return { d, side: n + border * 2 };
  });
</script>

<svg class="qr" width={size} height={size} viewBox="0 0 {code.side} {code.side}" shape-rendering="crispEdges" role="img" aria-label={label}>
  <rect width={code.side} height={code.side} fill="#fff" />
  <path d={code.d} fill="#000" />
</svg>

<style>
  .qr {
    display: block;
    border-radius: 16px;
    flex: none;
  }
</style>
