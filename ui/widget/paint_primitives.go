package widget

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// EdgeBorders returns four edge rectangles forming a border inside the given bounds.
func EdgeBorders(bounds image.Rectangle, w int) [4]image.Rectangle {
	return [4]image.Rectangle{
		// Top
		{Min: bounds.Min, Max: image.Pt(bounds.Max.X, bounds.Min.Y+w)},
		// Bottom
		{Min: image.Pt(bounds.Min.X, bounds.Max.Y-w), Max: bounds.Max},
		// Left
		{Min: bounds.Min, Max: image.Pt(bounds.Min.X+w, bounds.Max.Y)},
		// Right
		{Min: image.Pt(bounds.Max.X-w, bounds.Min.Y), Max: bounds.Max},
	}
}

// paintRowBg fills a full-width rectangle of the given height with the specified color.
func paintRowBg(gtx layout.Context, height int, c color.NRGBA) {
	rect := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, height)}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}

// paintRowBgFrom fills the row background from xStart to the row's right edge,
// used for tints that should apply only to the override-editor side of the
// table (not the key+default-value side).
func paintRowBgFrom(gtx layout.Context, xStart, height int, c color.NRGBA) {
	rect := clip.Rect{
		Min: image.Pt(xStart, 0),
		Max: image.Pt(gtx.Constraints.Max.X, height),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}

const gitIndicatorWidth = 4

// paintGitIndicator draws a narrow vertical bar at the given x position.
func paintGitIndicator(gtx layout.Context, x, height int, c color.NRGBA) {
	w := gtx.Dp(gitIndicatorWidth)

	rect := clip.Rect{Min: image.Pt(x, 0), Max: image.Pt(x+w, height)}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}

// customOnlyGradientFade is the fraction of the row width over which the
// section gradient transitions from c to white; past this point the row
// is solid white. Keeping the colored band tight (10%) makes the marker
// readable without dominating the row.
const customOnlyGradientFade = 0.10

// paintCustomOnlySectionGradient fills the row with a horizontal linear
// gradient that runs from c (saturated, on the left) to white over the
// first customOnlyGradientFade of the row width; the remaining width is
// solid white. Used to flag a section header that exists only in an
// override file — calls out the new subtree without obscuring downstream
// content.
func paintCustomOnlySectionGradient(gtx layout.Context, height int, c color.NRGBA) {
	w := gtx.Constraints.Max.X
	rect := clip.Rect{Max: image.Pt(w, height)}.Push(gtx.Ops)

	fadeEnd := float32(w) * customOnlyGradientFade

	paint.LinearGradientOp{
		Stop1:  f32.Pt(0, 0),
		Color1: c,
		Stop2:  f32.Pt(fadeEnd, 0),
		Color2: theme.ColorWhite,
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}

const customOnlyLeafBarWidth = 4

// paintCustomOnlyLeafBar draws a saturated vertical bar on the row's left
// edge marking a leaf key that exists only in an override file. Sections
// with the same status take the wider gradient instead — together they
// cover the legend's "override-only" entry on every affected row.
func paintCustomOnlyLeafBar(gtx layout.Context, height int, c color.NRGBA) {
	w := gtx.Dp(customOnlyLeafBarWidth)
	rect := clip.Rect{Max: image.Pt(w, height)}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}

const anchorStripeWidth = 3

// paintAnchorStripe draws a thick vertical bar at x with the row's height,
// marking membership in a YAML anchor's subtree. The caller picks x from the
// anchor's nesting depth so the stripe lines up with the indent column where
// the anchor's key text begins; nested anchors produce stacked vertical bars
// at the indent levels of the rows where they were defined.
func paintAnchorStripe(gtx layout.Context, x, width, height int, c color.NRGBA) {
	rect := clip.Rect{Min: image.Pt(x, 0), Max: image.Pt(x+width, height)}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}
