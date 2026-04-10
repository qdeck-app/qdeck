package theme

import "image/color"

// Shared UI colors used across pages and widgets.
//
//nolint:mnd // Color component values are inherently numeric design tokens.
var (
	ColorError        = color.NRGBA{R: 200, A: 255}
	ColorSecondary    = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	ColorAccent       = color.NRGBA{R: 80, G: 120, B: 200, A: 255}
	ColorAccentHover  = color.NRGBA{R: 50, G: 90, B: 240, A: 255}
	ColorDanger       = color.NRGBA{R: 200, G: 50, B: 50, A: 255}
	ColorMuted        = color.NRGBA{R: 120, G: 120, B: 120, A: 255}
	ColorHover        = color.NRGBA{R: 0, G: 0, B: 0, A: 20}
	ColorCardBg       = color.NRGBA{R: 246, G: 246, B: 250, A: 255}
	ColorStickyHeader = color.NRGBA{R: 240, G: 240, B: 240, A: 245}
	ColorSeparator    = color.NRGBA{R: 230, G: 230, B: 230, A: 255}
	ColorTreeGuide    = color.NRGBA{R: 210, G: 210, B: 210, A: 255}
	ColorIndentTick   = color.NRGBA{R: 195, G: 195, B: 195, A: 255}
	ColorOverride     = color.NRGBA{R: 255, G: 255, B: 200, A: 255}
	ColorScrollMarker = color.NRGBA{R: 200, G: 170, B: 50, A: 200}
	ColorDropdownBg   = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	ColorBtnCancel    = color.NRGBA{R: 200, G: 200, B: 200, A: 255}
	ColorInputBorder  = color.NRGBA{R: 200, G: 200, B: 200, A: 255}
	ColorTextPrimary  = color.NRGBA{A: 255}

	// Git change highlights for values table (pastel row backgrounds).
	ColorGitAdded    = color.NRGBA{R: 200, G: 235, B: 200, A: 255}
	ColorGitModified = color.NRGBA{R: 200, G: 220, B: 245, A: 255}

	// Git indicator bar colors (saturated, for left-edge stripe).
	ColorGitAddedBar    = color.NRGBA{R: 50, G: 180, B: 50, A: 255}
	ColorGitModifiedBar = color.NRGBA{R: 60, G: 130, B: 220, A: 255}

	// Focus highlight for keyboard navigation.
	ColorFocus = color.NRGBA{R: 80, G: 120, B: 200, A: 40}

	// Stats colors for diff summary.
	ColorStatsAdded     = color.NRGBA{R: 50, G: 150, B: 50, A: 255}
	ColorStatsChanged   = color.NRGBA{R: 200, G: 150, A: 255}
	ColorStatsRemoved   = ColorDanger
	ColorStatsUnchanged = ColorSecondary
)
