// Package theme — design tokens.
//
// Tokens are authored in OKLCH (perceptually uniform) and resolved to NRGBA
// at init() so the design intent stays readable in source and so a future
// dark mode can swap a single Default value.
//
// 1 pt in the design spec maps to 1 dp / 1 sp in Gio (they track the
// platform's logical density automatically).
package theme

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/unit"
)

// Tokens groups every paintable color used by the app.
//
// Field naming mirrors the design spec sections (surfaces, ink, lines,
// semantic accents, type-tag hues, traffic-light dots) so a designer can
// cross-reference tokens.go and the spec line-by-line.
type Tokens struct {
	// Surfaces — warm neutrals, slight yellow tint, never pure white.
	Bg          color.NRGBA // grid body
	Bg2         color.NRGBA // toolbars, header strip, statusbar, version chip
	Bg3         color.NRGBA // app shell, kbd background
	RowHover    color.NRGBA // amber-tinted row wash
	RowZebra    color.NRGBA // alternate row when zebra mode is on
	RowSelected color.NRGBA // selected row wash

	// Ink — text colors at descending importance.
	Ink    color.NRGBA // primary text, button text, brand wordmark
	Ink2   color.NRGBA // secondary text, breadcrumb middle, install code
	Muted  color.NRGBA // column headers, default-value column, hints
	Muted2 color.NRGBA // placeholders, separator glyphs, leaf disclosure

	// Lines — borders and dividers.
	Border       color.NRGBA // toolbar / section dividers, button outlines, kbd outline
	BorderStrong color.NRGBA // scrollbar thumb, button hover border
	Guide        color.NRGBA // row separators, indent guides, cell separators

	// Semantic accents — all chroma ≈ 0.13, lightness ≈ 68% so no single
	// status visually dominates. Each has a saturated stripe color, a
	// pale wash for cell backgrounds, and a strong text color for use on
	// the wash.
	Override       color.NRGBA // amber stripe
	OverrideBg     color.NRGBA // wash behind overridden cell
	OverrideStrong color.NRGBA // text on override wash
	Added          color.NRGBA // git-added stripe (green)
	AddedBg        color.NRGBA // green wash
	AddedStrong    color.NRGBA // text on green wash
	Modified       color.NRGBA // git-modified stripe (blue)
	ModifiedBg     color.NRGBA // blue wash
	ModifiedStrong color.NRGBA // text on blue wash

	// Danger — error / destructive accent. Used for failure notifications,
	// destructive button labels (Delete), and stats showing removed keys.
	// Saturated red at the same chroma envelope (~0.13) and slightly lower
	// lightness than the warm semantic accents so it reads as a hard stop.
	Danger color.NRGBA

	// Extra — cyan-teal accent for keys defined ONLY in the overlay file
	// with no chart-defaults counterpart. Independent axis from override:
	// a row can be "extra" without yet having a value (`podAnnotations: {}`).
	// Cyan-teal sits in the unused slot of the wheel (we already use
	// amber/green/blue/purple) and reads as "additive" without claiming a
	// git-staged meaning.
	Extra       color.NRGBA // strip color, key chip border
	ExtraBg     color.NRGBA // wash behind override cell when row is extra
	ExtraStrong color.NRGBA // text on the wash, key chip text
	ExtraFaint  color.NRGBA // wash behind key cell when descendant of an extra branch

	// Traffic-light dots — titlebar (mac-style).
	TrafficRed   color.NRGBA
	TrafficAmber color.NRGBA
	TrafficGreen color.NRGBA

	// Utility — pure values needed for compositing.
	Transparent color.NRGBA
	White       color.NRGBA // ONLY for Gio framebuffer fill at frame start, never UI
}

// Metrics groups every dp- or sp-denominated size used by the chrome and
// grid. Defining them here keeps widget files free of magic numbers (mnd
// linter happy) and keeps the layout spec auditable in one place.
type Metrics struct {
	// Chrome bands.
	TitleBarHeight   unit.Dp // 32
	ToolbarRowHeight unit.Dp // 32 — toolbar has two rows
	SearchBarHeight  unit.Dp // 32
	GridHeaderHeight unit.Dp // 26

	// Grid rows.
	RowHeightDefault unit.Dp // 22
	RowHeightMin     unit.Dp // 20
	RowHeightMax     unit.Dp // 32

	// Grid cell.
	CellPaddingH        unit.Dp // 12
	OverrideCellPadL    unit.Dp // 12
	OverrideCellPadR    unit.Dp // 18 — room for reset glyph
	IndentColumnWidth   unit.Dp // 14
	DisclosureColWidth  unit.Dp // 14
	DisclosureGlyphSize unit.Dp // 8

	// Override cell decoration.
	OverrideStripWidth unit.Dp // 2
	ResetButtonSize    unit.Dp // 16
	ResetButtonInsetR  unit.Dp // 6

	// Buttons.
	ButtonHeight       unit.Dp // 26
	ButtonPaddingH     unit.Dp // 10
	ButtonRadius       unit.Dp // 4
	ButtonContentGap   unit.Dp // 6 — between glyph / label / hint
	ButtonIconSize     unit.Dp // 12 — leading-icon edge length (vector icons, not glyphs)
	ButtonFocusOutline unit.Dp // 2 — focus-visible ring width
	ButtonFocusOffset  unit.Dp // 1 — outset of focus ring from button edge

	// Small chip — used by the extras "+" badge in the grid key cell.
	// Naming preserved as TypeTag* for back-compat; geometry is shared
	// with any other tiny pill we might add.
	TypeTagPadH   unit.Dp // 4
	TypeTagPadV   unit.Dp // 1
	TypeTagRadius unit.Dp // 2

	// Inline switch (bool editor).
	SwitchTrackW    unit.Dp // 22
	SwitchTrackH    unit.Dp // 12
	SwitchTrackR    unit.Dp // 7
	SwitchKnobSize  unit.Dp // 10
	SwitchKnobShift unit.Dp // 10

	// Generic strokes.
	HairlineWidth unit.Dp // 1 — every chrome divider
	StripeWidth   unit.Dp // 2 — selected row left edge, override stripe
}

// TypeScale groups every sp-denominated text size and the three weights the
// design uses. The spec is "all-monospace, weights 400/500/600". Body is
// 11sp; Material's Body1/Body2 helpers stay available for migration.
//
//nolint:mnd // Size constants are design tokens, not magic numbers.
type TypeScale struct {
	// Sizes.
	SizeXXS unit.Sp //  9.5 — type tags
	SizeXS  unit.Sp // 10   — kbd glyphs, version chip
	SizeSM  unit.Sp // 10.5 — column headers, status bar, legend
	SizeMD  unit.Sp // 11   — toolbar hints, kbd labels
	SizeLG  unit.Sp // 11.5 — cell text, toolbar buttons
	SizeXL  unit.Sp // 12   — app body, search input, brand wordmark

	// Weights — 400 is the default body, 500 emphasizes section keys and
	// override values, 600 is reserved for the brand wordmark.
	WeightRegular  font.Weight
	WeightMedium   font.Weight
	WeightSemiBold font.Weight
}

// Default is the single resolved token set for the app. Future dark mode can
// swap this at startup before NewTheme is called.
var Default = newDefaultTokens()

//nolint:mnd // OKLCH coordinates are the design spec — they ARE the documentation.
func newDefaultTokens() (t struct {
	Tokens
	Metrics
	TypeScale
},
) {
	t.Tokens = Tokens{
		Bg:          oklchOpaque(0.99, 0.003, 90),
		Bg2:         oklchOpaque(0.975, 0.004, 90),
		Bg3:         oklchOpaque(0.95, 0.005, 90),
		RowHover:    oklchOpaque(0.96, 0.012, 75),
		RowZebra:    oklchOpaque(0.985, 0.004, 90),
		RowSelected: oklchOpaque(0.95, 0.025, 75),

		Ink:    oklchOpaque(0.22, 0.01, 90),
		Ink2:   oklchOpaque(0.40, 0.008, 90),
		Muted:  oklchOpaque(0.58, 0.008, 90),
		Muted2: oklchOpaque(0.72, 0.006, 90),

		Border:       oklchOpaque(0.91, 0.005, 90),
		BorderStrong: oklchOpaque(0.84, 0.006, 90),
		Guide:        oklchOpaque(0.93, 0.005, 90),

		Override:       oklchOpaque(0.72, 0.13, 75),
		OverrideBg:     oklchOpaque(0.96, 0.04, 80),
		OverrideStrong: oklchOpaque(0.58, 0.14, 60),
		Added:          oklchOpaque(0.68, 0.12, 145),
		AddedBg:        oklchOpaque(0.96, 0.035, 145),
		AddedStrong:    oklchOpaque(0.40, 0.10, 145),
		Modified:       oklchOpaque(0.63, 0.13, 240),
		ModifiedBg:     oklchOpaque(0.96, 0.025, 240),
		ModifiedStrong: oklchOpaque(0.40, 0.12, 240),

		Danger: oklchOpaque(0.58, 0.20, 25),

		Extra:       oklchOpaque(0.66, 0.13, 195),
		ExtraBg:     oklchOpaque(0.96, 0.03, 195),
		ExtraStrong: oklchOpaque(0.48, 0.14, 195),
		ExtraFaint:  oklchOpaque(0.985, 0.012, 195),

		TrafficRed:   oklchOpaque(0.70, 0.15, 25),
		TrafficAmber: oklchOpaque(0.82, 0.13, 85),
		TrafficGreen: oklchOpaque(0.72, 0.16, 145),

		Transparent: color.NRGBA{},
		White:       color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	}

	t.Metrics = Metrics{
		TitleBarHeight:   32,
		ToolbarRowHeight: 32,
		SearchBarHeight:  32,
		GridHeaderHeight: 26,

		RowHeightDefault: 22,
		RowHeightMin:     20,
		RowHeightMax:     32,

		CellPaddingH:        12,
		OverrideCellPadL:    12,
		OverrideCellPadR:    18,
		IndentColumnWidth:   14,
		DisclosureColWidth:  14,
		DisclosureGlyphSize: 8,

		OverrideStripWidth: 2,
		ResetButtonSize:    16,
		ResetButtonInsetR:  6,

		ButtonHeight:       26,
		ButtonPaddingH:     10,
		ButtonRadius:       4,
		ButtonContentGap:   6,
		ButtonIconSize:     12,
		ButtonFocusOutline: 2,
		ButtonFocusOffset:  1,
		TypeTagPadH:        4,
		TypeTagPadV:        1,
		TypeTagRadius:      2,

		SwitchTrackW:    22,
		SwitchTrackH:    12,
		SwitchTrackR:    7,
		SwitchKnobSize:  10,
		SwitchKnobShift: 10,

		HairlineWidth: 1,
		StripeWidth:   2,
	}

	t.TypeScale = TypeScale{
		SizeXXS: 9.5,
		SizeXS:  10,
		SizeSM:  10.5,
		SizeMD:  11,
		SizeLG:  11.5,
		SizeXL:  12,

		WeightRegular:  font.Normal,
		WeightMedium:   font.Medium,
		WeightSemiBold: font.SemiBold,
	}

	return t
}
