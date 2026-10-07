#!/usr/bin/env bash
# Builds FurkVPN for Windows (x64) on Linux: the interface, both programs, the installer.
#   build/build.sh            → dist/FurkVPN-Setup-<version>.exe
# Version: VERSION and VERSIONCODE in the repository root (VERSIONCODE grows with every release).
# Needs Go, Node.js, go-winres (go install github.com/tc-hib/go-winres@latest), makensis (nsis),
# osslsigncode, curl.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"

VERSION=$(tr -d ' \n' < VERSION)
CODE=$(tr -d ' \n' < VERSIONCODE)
DIST=dist
CACHE=build/.cache
WINTUN_ZIP_SHA256=07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51
mkdir -p "$DIST" "$CACHE"

echo "== interface"
(cd frontend && npm ci --no-audit --no-fund --silent && npm run build --silent)

echo "== resources"
(cd build && go-winres make --in winres-app.json --arch amd64 --out ../cmd/furkvpn/rsrc --product-version "$VERSION.0" --file-version "$VERSION.0")
(cd build && go-winres make --in winres-service.json --arch amd64 --out ../cmd/furkvpn-service/rsrc --product-version "$VERSION.0" --file-version "$VERSION.0")

echo "== programs $VERSION ($CODE)"
LDFLAGS="-s -w -buildid= -X furkvpn/internal/version.Name=$VERSION -X furkvpn/internal/version.Code=$CODE"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -tags desktop,production -ldflags "$LDFLAGS -H windowsgui" -o "$DIST/FurkVPN.exe" ./cmd/furkvpn
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/FurkVPNService.exe" ./cmd/furkvpn-service

echo "== wintun"
if [ ! -f "$CACHE/wintun-0.14.1.zip" ]; then
  curl -fsSL -o "$CACHE/wintun-0.14.1.zip" https://www.wintun.net/builds/wintun-0.14.1.zip
fi
echo "$WINTUN_ZIP_SHA256  $CACHE/wintun-0.14.1.zip" | sha256sum -c --quiet
unzip -o -q -j "$CACHE/wintun-0.14.1.zip" wintun/bin/amd64/wintun.dll -d "$DIST"
unzip -o -q -p "$CACHE/wintun-0.14.1.zip" wintun/LICENSE.txt > "$DIST/LICENSE-wintun.txt"

echo "== WebView2 bootstrapper"
if [ ! -f "$CACHE/MicrosoftEdgeWebview2Setup.exe" ]; then
  curl -fsSL -o "$CACHE/MicrosoftEdgeWebview2Setup.exe" "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
fi
signers=$(osslsigncode verify -in "$CACHE/MicrosoftEdgeWebview2Setup.exe" 2>&1 || true)
grep -q "O=Microsoft Corporation/CN=Microsoft Corporation" <<<"$signers" || { echo "WebView2 bootstrapper is not signed by Microsoft"; exit 1; }
cp "$CACHE/MicrosoftEdgeWebview2Setup.exe" "$DIST/"
cp build/THIRD_PARTY.txt "$DIST/"

echo "== installer"
(cd installer && makensis -V2 -DVERSION="$VERSION" -DVERSIONCODE="$CODE" -DDIST=../dist -DOUT="../dist/FurkVPN-Setup-$VERSION.exe" furkvpn.nsi)
sha256sum "$DIST/FurkVPN-Setup-$VERSION.exe"
ls -la "$DIST"
