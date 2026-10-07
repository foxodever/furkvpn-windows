#!/usr/bin/env bash
# Publishes the Windows installer on furkapp.net (the same server as the Android app) and announces
# the update to installed apps: the "windows.update" block of /opt/furkapp/config/config.json, which
# the Android app ignores.
#   build/publish.sh "Что нового"
# Run on the furkapp.net server after build/build.sh. Only when a release is really meant for users.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=$(tr -d ' \n' < VERSION)
CODE=$(tr -d ' \n' < VERSIONCODE)
CHANGELOG=${1:-}
SRC="dist/FurkVPN-Setup-$VERSION.exe"
DL=/var/www/furkapp/download
BASE=https://furkapp.net/download
CONFIG=/opt/furkapp/config/config.json

[ -f "$SRC" ] || { echo "no $SRC: run build/build.sh first"; exit 1; }
install -d "$DL"
install -m 644 "$SRC" "$DL/FurkVPN-Setup-$VERSION.exe" # versioned file for the in-app updater
install -m 644 "$SRC" "$DL/FurkVPN-Setup.exe"          # stable link for the download page
SHA=$(sha256sum "$SRC" | cut -d' ' -f1)

tmp=$(mktemp)
jq --argjson vc "$CODE" --arg vn "$VERSION" --arg url "$BASE/FurkVPN-Setup-$VERSION.exe" --arg sha "$SHA" --arg log "$CHANGELOG" '
  .app.windows = ((.app.windows // {}) + {
    update: {
      versionCode: $vc, versionName: $vn, url: $url, sha256: $sha,
      changelog: (if $log == "" then null else $log end)
    }
  })' "$CONFIG" > "$tmp"
install -m 644 "$tmp" "$CONFIG" && rm -f "$tmp"
echo "published Windows $VERSION ($CODE), sha256 $SHA"
