//go:build !windows

package screen

// workAreaDp is unsupported on this platform; the caller falls back to a fixed
// default window size.
func workAreaDp() (widthDp, heightDp int, ok bool) {
	return 0, 0, false
}
