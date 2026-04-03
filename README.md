# QDeck

Helm Values Editor.

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

## License

Copyright © 2026 bosiakov

Licensed under MIT (see [LICENSE](LICENSE)).

The font `ui/font/FiraCode-Regular.ttf` is licensed under the OFL-1.1. See [ui/font/LICENSE](ui/font/LICENSE) for details.
