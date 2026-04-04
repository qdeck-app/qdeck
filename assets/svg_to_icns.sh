#!/usr/bin/env bash
# Generates appicon.icns from qdeck_logo.svg (macOS only).
# Requires: rsvg-convert (librsvg) and iconutil.
# CI installs librsvg via: brew install librsvg
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SVG="$SCRIPT_DIR/qdeck_logo.svg"
ICNS="$SCRIPT_DIR/appicon.icns"
ICONSET_DIR=$(mktemp -d)/appicon.iconset

LOGO_PERCENT=80
SIZES=(16 32 64 128 256 512 1024)

for cmd in rsvg-convert iconutil; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "Error: $cmd is required but not found." >&2
        exit 1
    fi
done

mkdir -p "$ICONSET_DIR"

# SVG dimensions for aspect-ratio-aware centering
SVG_W=1247.03
SVG_H=1448.37

for size in "${SIZES[@]}"; do
    logo_size=$(( size * LOGO_PERCENT / 100 ))
    (( logo_size < 1 )) && logo_size=1

    # Fit logo into logo_size box preserving aspect ratio
    # rsvg-convert --keep-aspect-ratio handles this, but we need to
    # composite onto a square transparent canvas afterwards.
    tmp_logo=$(mktemp /tmp/icon_XXXX.png)

    rsvg-convert -w "$logo_size" -h "$logo_size" --keep-aspect-ratio \
        -b none "$SVG" -o "$tmp_logo"

    # Composite onto transparent square canvas
    if [ "$size" -eq 1024 ]; then
        # 1024 is only used for icon_512x512@2x
        out="$ICONSET_DIR/icon_512x512@2x.png"
    elif [ "$size" -eq 64 ]; then
        out="$ICONSET_DIR/icon_32x32@2x.png"
        # Also used as icon_64x64 (not standard but some tools expect it)
    else
        out="$ICONSET_DIR/icon_${size}x${size}.png"
    fi

    # Use sips (macOS built-in) to pad to square canvas
    cp "$tmp_logo" "$out"
    sips -z "$size" "$size" --padToHeightWidth "$size" "$size" \
        --padColor 000000 "$out" >/dev/null 2>&1 || true
    # sips padColor doesn't support transparency well, fall back to rsvg full canvas
    # Better approach: render at full canvas size directly
    rsvg-convert -w "$size" -h "$size" -a -b none "$SVG" -o "$out"

    rm -f "$tmp_logo"

    # Generate @2x variants where needed
    case "$size" in
        16)
            retina_size=$(( size * 2 ))
            retina_logo=$(( retina_size * LOGO_PERCENT / 100 ))
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
