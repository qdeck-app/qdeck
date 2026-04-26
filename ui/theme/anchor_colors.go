package theme

import (
	"hash/fnv"
	"image/color"
	"math"
)

// Anchor color tokens. Each anchor name maps deterministically to a hue via
// FNV-1a, with fixed saturation and lightness so badges and stripes for the
// same anchor render identically across runs. Hues that fall too close to the
// git-indicator bar colors are rotated out so anchor stripes never blur into
// "this row has uncommitted git changes". Both anchor (`&name`) and alias
// (`*name`) badges use the same color — the sigil already disambiguates role,
// and matching colors group an anchor and its aliases at a glance.
//
//nolint:mnd // HSL constants and forbidden-hue band centers are design tokens.
const (
	anchorSaturation = 0.55
	anchorLightness  = 0.55

	anchorForbiddenHueGreen = 120.0 // matches ColorGitAddedBar hue
	anchorForbiddenHueBlue  = 215.0 // matches ColorGitModifiedBar hue
	anchorForbiddenHalfBand = 15.0  // ± degrees around each forbidden center
	anchorHueRotation       = 30.0  // rotation applied when hue lands inside a forbidden band
	anchorHueWheel          = 360.0
)

// AnchorColor returns a saturated NRGBA derived from name. Used for both
// anchor and alias badge backgrounds and the membership stripe drawn on every
// row inside the anchor's subtree (or the subtree of one of its aliases).
func AnchorColor(name string) color.NRGBA {
	return hslToNRGBA(hueFromName(name), anchorSaturation, anchorLightness)
}

// hueFromName maps a string to a hue in [0, 360) using FNV-1a, then rotates it
// out of the forbidden bands around git-indicator hues so anchor stripes can't
// be confused with git status. Pure, allocation-free.
//
// Invariant: anchorHueRotation must exceed every forbidden band's full width
// (2 × anchorForbiddenHalfBand) AND each rotation must land outside every
// other forbidden band. With the current bands (120°, 215°) separated by
// 95° and rotation 30°, a single pass clears both. If a new band is added
// or rotation is reduced, replace the single rotation with a re-check loop.
func hueFromName(name string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))

	hue := math.Mod(float64(h.Sum32()), anchorHueWheel)

	if hueInForbiddenBand(hue) {
		hue = math.Mod(hue+anchorHueRotation, anchorHueWheel)
	}

	return hue
}

func hueInForbiddenBand(hue float64) bool {
	return hueDistance(hue, anchorForbiddenHueGreen) < anchorForbiddenHalfBand ||
		hueDistance(hue, anchorForbiddenHueBlue) < anchorForbiddenHalfBand
}

func hueDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > anchorHueWheel/2 {
		d = anchorHueWheel - d
	}

	return d
}

// hslToNRGBA converts HSL (h in [0,360), s and l in [0,1]) to an opaque NRGBA.
// Uses the standard chroma/X/match decomposition.
//
//nolint:mnd // The 60°/2/255 magic numbers are part of the HSL→RGB formula.
func hslToNRGBA(h, s, l float64) color.NRGBA {
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))

	var r, g, b float64

	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	m := l - c/2

	return color.NRGBA{
		R: uint8(math.Round((r + m) * 255)),
		G: uint8(math.Round((g + m) * 255)),
		B: uint8(math.Round((b + m) * 255)),
		A: 255,
	}
}
