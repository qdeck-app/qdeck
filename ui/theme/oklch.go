// Package theme — OKLCH → sRGB conversion. Tokens in tokens.go are authored
// in OKLCH (perceptually uniform) and resolved to NRGBA at init() so the
// design intent stays readable in source.
//
// References:
//   - Björn Ottosson, "A perceptual color space for image processing"
//     https://bottosson.github.io/posts/oklab/
//   - CSS Color Module Level 4, OKLCH definition.
package theme

import (
	"image/color"
	"math"
)

// oklch builds a sRGB color from OKLCH coordinates.
//
//	l: lightness in [0, 1]   (CSS authors usually write 0–100%; pass as 0.0–1.0).
//	c: chroma   in [0, ~0.4] (most in-gamut colors live below 0.3).
//	h: hue      in degrees [0, 360).
//	a: alpha    in [0, 1].
//
// Out-of-gamut sRGB triples are clamped per channel after gamma encoding.
//
//nolint:mnd // OKLab/sRGB conversion matrices are mathematical constants.
func oklch(l, c, h, a float64) color.NRGBA {
	hr := h * math.Pi / 180.0
	labA := c * math.Cos(hr)
	labB := c * math.Sin(hr)

	// OKLab → LMS' (linear after cube).
	lp := l + 0.3963377774*labA + 0.2158037573*labB
	mp := l - 0.1055613458*labA - 0.0638541728*labB
	sp := l - 0.0894841775*labA - 1.2914855480*labB

	lc := lp * lp * lp
	mc := mp * mp * mp
	sc := sp * sp * sp

	// LMS → linear sRGB.
	rl := 4.0767416621*lc - 3.3077115913*mc + 0.2309699292*sc
	gl := -1.2684380046*lc + 2.6097574011*mc - 0.3413193965*sc
	bl := -0.0041960863*lc - 0.7034186147*mc + 1.7076147010*sc

	return color.NRGBA{
		R: gammaEncode8(rl),
		G: gammaEncode8(gl),
		B: gammaEncode8(bl),
		A: alpha8(a),
	}
}

// oklchOpaque is shorthand for fully opaque OKLCH colors (the common case).
func oklchOpaque(l, c, h float64) color.NRGBA {
	return oklch(l, c, h, 1.0)
}

// gammaEncode8 converts a linear-light component in [0, 1] to gamma-encoded
// sRGB in [0, 255]. Out-of-range inputs are clamped (typical when an OKLCH
// triple falls outside the sRGB gamut).
//
//nolint:mnd // sRGB transfer-function constants are part of the standard.
func gammaEncode8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	case v <= 0.0031308:
		return uint8(math.Round(v * 12.92 * 255))
	default:
		enc := 1.055*math.Pow(v, 1.0/2.4) - 0.055
		if enc <= 0 {
			return 0
		}

		if enc >= 1 {
			return 255
		}

		return uint8(math.Round(enc * 255))
	}
}

//nolint:mnd // 255 is the uint8 ceiling, not a magic number.
func alpha8(a float64) uint8 {
	switch {
	case a <= 0:
		return 0
	case a >= 1:
		return 255
	default:
		return uint8(math.Round(a * 255))
	}
}
