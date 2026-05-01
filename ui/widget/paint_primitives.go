package widget

import (
	"image"
	"image/color"

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

// paintRect fills a single rectangle with the supplied color. Convenience
// helper for the four-edge border idiom (paint top, bottom, left, right
// rects of one-pixel width).
func paintRect(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	stack := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
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

// paintRowBgTo fills the row background from x=0 to xEnd, the mirror of
// paintRowBgFrom — used for the key+default-value side. Lets the key
// column carry a faint extras tint while the override side gets the
// stronger extras-bg wash.
func paintRowBgTo(gtx layout.Context, xEnd, height int, c color.NRGBA) {
	rect := clip.Rect{
		Min: image.Pt(0, 0),
		Max: image.Pt(xEnd, height),
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

// paintOverrideStrip draws a saturated vertical bar (StripeWidth wide) flush
// against the override panel's left edge, full row height. The color
// communicates the row's modification status: amber for manual override,
// green for git-added, blue for git-modified.
func paintOverrideStrip(gtx layout.Context, xStart, height int, c color.NRGBA) {
	w := gtx.Dp(theme.Default.StripeWidth)
	rect := clip.Rect{
		Min: image.Pt(xStart, 0),
		Max: image.Pt(xStart+w, height),
	}.Push(gtx.Ops)
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
