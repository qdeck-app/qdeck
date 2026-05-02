#!/usr/bin/env bash
# Verify third_party/gio/ matches upstream Gio at the pinned version with
# our patches applied on top, in order.
#
# Catches two failure modes:
#   1) Someone hand-edited a vendored file without updating a .patch.
#   2) A .patch was changed but the vendor tree wasn't re-rolled.
#
# Both produce silent drift that's invisible in code review until something
# breaks at runtime. This script is wired into CI so PRs that introduce
# drift fail loudly.
#
# Pinned version + patch list live in the constants below — bump them
# together when you upgrade Gio.

set -euo pipefail

GIO_VERSION="v0.9.0"
PATCH_FILES=(
    "third_party/0001-gesture-refresh-PointerID-on-Press-and-Enter.patch"
    "third_party/0002-app-windows-stable-PointerID-for-mouse.patch"
)
VENDOR_DIR="third_party/gio"
PROXY_URL="https://proxy.golang.org/gioui.org/@v/${GIO_VERSION}.zip"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

if [ ! -d "$VENDOR_DIR" ]; then
    echo "ERROR: $VENDOR_DIR does not exist. Did the replace directive in go.mod fall out of sync with the vendor tree?"
    exit 1
fi

for patch in "${PATCH_FILES[@]}"; do
    if [ ! -f "$patch" ]; then
        echo "ERROR: $patch not found"
        exit 1
    fi
done

# Resolve vendor path to absolute up front so `cd` into the temp dir later
# doesn't break it.
VENDOR_DIR_ABS="$REPO_ROOT/$VENDOR_DIR"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading upstream Gio $GIO_VERSION from the Go module proxy..."
curl --fail --silent --show-error --location "$PROXY_URL" -o "$TMPDIR/gio.zip"

echo "Extracting upstream zip..."
unzip -q "$TMPDIR/gio.zip" -d "$TMPDIR/extract"

UPSTREAM_DIR="$TMPDIR/extract/gioui.org@$GIO_VERSION"
if [ ! -d "$UPSTREAM_DIR" ]; then
    echo "ERROR: expected upstream directory not found at $UPSTREAM_DIR after extraction"
    echo "Extracted layout:"
    find "$TMPDIR/extract" -maxdepth 2 -type d
    exit 1
fi

chmod -R u+w "$UPSTREAM_DIR"
# cd into the upstream dir and apply from there. `git apply --directory=...`
# rejects paths containing `@` on Windows (msys git treats them as remote
# refs); the cd workaround is portable across all platforms CI runs on.
# Patches must apply in array order — each one's context lines assume the
# previous patch has already landed.
for patch in "${PATCH_FILES[@]}"; do
    echo "Applying $patch to a fresh upstream copy..."
    (cd "$UPSTREAM_DIR" && git apply "$REPO_ROOT/$patch")
done

echo "Comparing $VENDOR_DIR against upstream + patches..."
DRIFT_FILE="$TMPDIR/drift.diff"
if diff --recursive --brief "$VENDOR_DIR_ABS" "$UPSTREAM_DIR" >"$DRIFT_FILE"; then
    echo "OK: $VENDOR_DIR matches upstream Gio $GIO_VERSION + ${#PATCH_FILES[@]} patch(es)"
    exit 0
fi

echo ""
echo "ERROR: $VENDOR_DIR has drifted from upstream Gio $GIO_VERSION + the patches in third_party/"
echo ""
echo "Differences (first 30 lines):"
head -30 "$DRIFT_FILE"

total=$(wc -l <"$DRIFT_FILE")
if [ "$total" -gt 30 ]; then
    echo ""
    echo "($((total - 30)) more lines truncated; see $DRIFT_FILE inside the runner if needed)"
fi

echo ""
echo "----------------------------------------------------------------"
echo "How to resolve:"
echo ""
echo "  - If you intentionally changed vendored files, regenerate the relevant patch:"
echo "      diff -u --label a/<rel> --label b/<rel> <orig> <yours> > third_party/<NNNN-...>.patch"
echo "    then re-run this script to confirm the diff is empty."
echo ""
echo "  - If a patch was meant to change but the vendor wasn't re-rolled,"
echo "    rebuild the vendor tree:"
echo "      rm -rf $VENDOR_DIR"
echo "      curl -L $PROXY_URL -o /tmp/gio.zip"
echo "      unzip -q /tmp/gio.zip -d /tmp/gio-extract"
echo "      cp -r /tmp/gio-extract/gioui.org@$GIO_VERSION $VENDOR_DIR"
echo "      chmod -R u+w $VENDOR_DIR"
for patch in "${PATCH_FILES[@]}"; do
    echo "      git apply $patch"
done
echo "----------------------------------------------------------------"
exit 1
