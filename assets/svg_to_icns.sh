#!/usr/bin/env bash
# Generates appicon.icns from qdeck_logo.svg (macOS only).
# Requires: rsvg-convert (librsvg) and iconutil.
# CI installs librsvg via: brew install librsvg
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SVG="$SCRIPT_DIR/qdeck_logo_square.svg"
ICNS="$SCRIPT_DIR/appicon.icns"
ICONSET_DIR=$(mktemp -d)/appicon.iconset

SIZES=(16 32 64 128 256 512 1024)

for cmd in rsvg-convert iconutil; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "Error: $cmd is required but not found." >&2
        exit 1
    fi
done

mkdir -p "$ICONSET_DIR"

for size in "${SIZES[@]}"; do
    if [ "$size" -eq 1024 ]; then
        # 1024 is only used for icon_512x512@2x
        out="$ICONSET_DIR/icon_512x512@2x.png"
    elif [ "$size" -eq 64 ]; then
        out="$ICONSET_DIR/icon_32x32@2x.png"
        # Also used as icon_64x64 (not standard but some tools expect it)
    else
        out="$ICONSET_DIR/icon_${size}x${size}.png"
    fi

    rsvg-convert -w "$size" -h "$size" -a -b none "$SVG" -o "$out"

    # Generate @2x variants where needed
    case "$size" in
        16)
            retina_size=$(( size * 2 ))
            rsvg-convert -w "$retina_size" -h "$retina_size" -a -b none "$SVG" \
                -o "$ICONSET_DIR/icon_16x16@2x.png"
            ;;
        128)
            retina_size=$(( size * 2 ))
            rsvg-convert -w "$retina_size" -h "$retina_size" -a -b none "$SVG" \
                -o "$ICONSET_DIR/icon_128x128@2x.png"
            ;;
        256)
            retina_size=$(( size * 2 ))
            rsvg-convert -w "$retina_size" -h "$retina_size" -a -b none "$SVG" \
                -o "$ICONSET_DIR/icon_256x256@2x.png"
            ;;
    esac
done

echo "Creating appicon.icns..."
iconutil -c icns "$ICONSET_DIR" -o "$ICNS"

rm -rf "$(dirname "$ICONSET_DIR")"

echo "Done: $ICNS"
