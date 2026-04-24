package platform

import "runtime"

// IsMac reports whether the current platform is macOS.
var IsMac = runtime.GOOS == "darwin" //nolint:gochecknoglobals // platform detection at init

// ShortcutLabel returns the Mac or non-Mac shortcut label.
func ShortcutLabel(mac, other string) string {
	if IsMac {
		return mac
	}

	return other
}
