#!/bin/bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
app="$PWD/build/Garcon Usage.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp Info.plist "$app/Contents/Info.plist"
# Keep the icon opaque and square; macOS supplies its own rounded mask.
cp UsageIcon.icns "$app/Contents/Resources/UsageIcon.icns"
xcrun swiftc -O -target "$(uname -m)-apple-macosx12.0" usage-widget.swift \
  -framework AppKit -framework WebKit -o "$app/Contents/MacOS/GarconUsage"
codesign --force --sign - "$app"
echo "$app"
