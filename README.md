<p align="center">
  <img src="assets/qdeck.png" alt="QDeck" width="400">
</p>

<p align="center">The Quarterdeck is the rear section of a ship's upper deck, traditionally reserved for the commanding officer.</p>

# QDeck
[![Go](https://img.shields.io/github/go-mod/go-version/qdeck-app/qdeck)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/qdeck-app/qdeck)](https://goreportcard.com/report/github.com/qdeck-app/qdeck)
[![License](https://img.shields.io/github/license/qdeck-app/qdeck)](LICENSE)

Helm values are structured data, not plain text. 

QDeck renders them as navigable tables, not walls of YAML.

* See how your values differ from chart defaults — additions, removals, and overrides at a glance
* Preview rendered templates as you edit values. No more save-run-check cycles.
* Navigate sub-charts and their values in a single view. No need to juggle multiple files.
* Works with local charts on disk
* macOS, Windows, Linux


## Development

QDeck is a Gio-based Go GUI for visually exploring Helm chart values, layered on top of the helm.sh/helm/v3 library.

Project follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#specification) naming.

Prerequisites:

- [Go](https://go.dev/) 1.25.0
- [golangci-lint](https://golangci-lint.run/)


```bash
# build binary
# on macOS you may need CC=/usr/bin/clang explicitly
go run .

# lint
golangci-lint run --fix
```

### Linux Text Rendering

Gio's GPU text renderer produces grey/fuzzy text on Linux (Mesa OpenGL) due to three framework-level issues: transparent framebuffer compositing, gamma-incorrect alpha blending ([gio#70](https://todo.sr.ht/~eliasnaur/gio/70)), and a 32-glyph batch boundary that creates visible vertical seams with monospace fonts. These do not reproduce on macOS (Metal) or Windows (Direct3D 11) because their GPU rasterisers handle sub-pixel positioning deterministically.

Workaround applied:

- **Custom label/editor renderer** (`ui/widget/label.go`) with a 256-glyph buffer and integer pixel-grid snapping — all text must be rendered through `widget.LayoutLabel` / `widget.LayoutEditor`, never via Gio's `lbl.Layout(gtx)` directly

### Vendored Gio (Windows click reliability)

`third_party/gio/` holds a local fork of `gioui.org v0.9.0`. `go.mod` replaces the upstream import with this fork. The patch we apply on top is in [`third_party/0001-gesture-refresh-PointerID-on-Press-and-Enter.patch`](third_party/0001-gesture-refresh-PointerID-on-Press-and-Enter.patch).

**Upstream submission:** [lists.sr.ht/~eliasnaur/gio-patches/patches/69089](https://lists.sr.ht/~eliasnaur/gio-patches/patches/69089). Drop the fork once this lands in a Gio release.

**Why:** upstream `gesture.Click` and `gesture.Hover` lock their internal `pid` field to the first `PointerID` they ever see — both refresh it only when `!c.hovered` / `!h.entered`. On Windows, Gio enables `EnableMouseInPointer(1)` (`app/os_windows.go`), which causes the OS to assign different `PointerID`s to the same physical mouse across focus changes, window leave/re-enter, and similar events. Once the gesture is "stuck" with a stale pid, every subsequent `Press` whose `PointerID` doesn't match is silently dropped (the `pid != e.PointerID` check breaks out before reaching `c.pressed = true` and the event return). Most visibly this made clicks no-op on multi-line override editors, where `widget.Editor`'s internal `clicker` (a `gesture.Click`) ate the press without positioning the caret.

**Patch:** refresh `pid` on every `Enter` / `Press` so the gesture tracks whichever pointer is currently interacting. 14 lines added, 12 removed. No behavioural change on macOS/Linux.

**Removing the fork:** when the upstream patch is merged and released, drop the `replace` directive in [`go.mod`](go.mod), bump `gioui.org` to the release, delete `third_party/gio/`, both `.patch` files, the drift-check step in CI, and this section.

## License

Copyright © 2026 bosiakov

Licensed under MIT (see [LICENSE](LICENSE)).

The font `ui/platform/font/FiraCode-Medium.ttf` is licensed under the OFL-1.1. See [ui/platform/font/LICENSE](ui/platform/font/LICENSE) for details.

All fonts are legally licensed to Yauheni Basiakou via MyFonts.
