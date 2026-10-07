#!/usr/bin/env python3
"""Draws the app's icons: the program icon (the wordmark on the dark square, as in Google Play)
and the tray icons (the shield of the Android notification, in the VPN state's color).
Needs rsvg-convert. Run from the repository root: python3 build/icons.py"""
import os, struct, subprocess, tempfile

WORDMARK = open(os.path.join(os.path.dirname(__file__), "wordmark.path")).read().strip()
SHIELD = "M12,1L3,5v6c0,5.55 3.84,10.74 9,12 5.16,-1.26 9,-6.45 9,-12L21,5l-9,-4zM12,7a2,2 0,0 1,1 3.73L13,15h-2v-4.27A2,2 0,0 1,12 7z"

def app_svg(size):
    r = size * 0.18
    if size >= 48:
        # The wordmark (210x194) takes 62% of the width, centered.
        scale = size * 0.62 / 210
        w, h = 210 * scale, 194 * scale
        body = f'<g transform="translate({(size - w) / 2:.3f},{(size - h) / 2:.3f}) scale({scale:.5f})"><path fill="#ffffff" d="{WORDMARK}"/></g>'
    else:
        scale = size * 0.72 / 24
        off = (size - 24 * scale) / 2
        body = f'<g transform="translate({off:.3f},{off:.3f}) scale({scale:.5f})"><path fill="#ffffff" d="{SHIELD}"/></g>'
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}"><rect width="{size}" height="{size}" rx="{r:.2f}" fill="#1a1c1e"/>{body}</svg>'

def tray_svg(size, color):
    scale = size / 24
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}"><g transform="scale({scale:.5f})"><path fill="{color}" d="{SHIELD}"/></g></svg>'

def png(svg, size):
    with tempfile.NamedTemporaryFile("w", suffix=".svg", delete=False) as f:
        f.write(svg)
        name = f.name
    try:
        return subprocess.check_output(["rsvg-convert", "-w", str(size), "-h", str(size), name])
    finally:
        os.unlink(name)

def ico(images):
    """images: [(size, png bytes)] -> .ico with PNG images."""
    header = struct.pack("<HHH", 0, 1, len(images))
    entries, blobs = b"", b""
    offset = 6 + 16 * len(images)
    for size, data in images:
        s = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", s, s, 0, 0, 1, 32, len(data), offset)
        blobs += data
        offset += len(data)
    return header + entries + blobs

def write(path, data):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "wb").write(data)
    print(path, len(data))

app_sizes = [16, 20, 24, 32, 40, 48, 64, 96, 128, 256]
write("build/appicon.ico", ico([(s, png(app_svg(s), s)) for s in app_sizes]))
write("build/appicon.png", png(app_svg(256), 256))
for name, color in [("off", "#8a8f98"), ("busy", "#e8892b"), ("on", "#2eb067")]:
    write(f"cmd/furkvpn/icons/tray-{name}.ico", ico([(s, png(tray_svg(s, color), s)) for s in [16, 20, 24, 32, 40, 48, 64]]))

# The installer's side picture (welcome and finish pages): 164x314, the wordmark on the dark brand color.
def side_svg(w, h):
    scale = w * 0.6 / 210
    lw, lh = 210 * scale, 194 * scale
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}"><rect width="{w}" height="{h}" fill="#1a1c1e"/>'
            f'<g transform="translate({(w - lw) / 2:.2f},{h * 0.30 - lh / 2:.2f}) scale({scale:.5f})"><path fill="#ffffff" d="{WORDMARK}"/></g></svg>')

from PIL import Image
import io
img = Image.open(io.BytesIO(subprocess.check_output(["rsvg-convert", "-w", "164", "-h", "314", "/dev/stdin"], input=side_svg(164, 314).encode()))).convert("RGB")
img.save("build/installer-side.bmp", format="BMP")
print("build/installer-side.bmp")
