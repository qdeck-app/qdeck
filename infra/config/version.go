package config

// AppVersion is set at build time via ldflags:
// -X github.com/qdeck-app/qdeck/infra/config.AppVersion=<tag>
var AppVersion = "dev" //nolint:gochecknoglobals // injected by ldflags
