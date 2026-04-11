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

## License

Copyright © 2026 bosiakov

Licensed under MIT (see [LICENSE](LICENSE)).

The font `ui/font/FiraCode-Medium.ttf` is licensed under the OFL-1.1. See [ui/font/LICENSE](ui/font/LICENSE) for details.

All fonts are legally licensed to Yauheni Basiakou via MyFonts.
