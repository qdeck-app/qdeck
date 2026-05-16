package widget

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	nullPillPaddingH unit.Dp = 6
	// nullPillPaddingV stays at zero so the pill's outer height equals
	// the label's line box exactly — same as the editor reports for an
	// empty single-line cell. Adding vertical padding here makes the
	// row grow by 2*padV on nullify, which the user notices as a vertical
	// jump.
	nullPillPaddingV unit.Dp = 0
)

// LayoutNullPill renders the explicit-null chip: an italic muted "null"
// label inside a faint amber pill with a thin border. This is the visual
// signal that the cell (or section) has been nullified and will round-trip
// to a YAML null scalar — distinct from a literal string "null" or "~"
// the user could type, which renders as plain editor text.
//
// compact shrinks the horizontal padding so the chip fits inside narrow
// override columns without horizontal clipping.
func LayoutNullPill(gtx layout.Context, th *material.Theme, compact bool) layout.Dimensions {
	padH := gtx.Dp(nullPillPaddingH)
	if compact {
		padH /= 2 //nolint:mnd // half-width padding for narrow columns.
	}

	padV := gtx.Dp(nullPillPaddingV)

	hairline := gtx.Dp(theme.Default.HairlineWidth)
	if hairline < 1 {
		hairline = 1
	}

	lbl := material.Body2(th, service.TypeNull)
	lbl.Color = theme.Default.Muted
	lbl.Font.Style = font.Italic
	lbl.MaxLines = 1
	// Match the editor's text size so a cell switching between an empty
	// editor and the null pill keeps the same line-height — otherwise
	// the row collapses vertically on nullify and the trailing button
	// visibly hops upward.
	lbl.TextSize = viewerEditorTextSize

	// Measure the label at its natural width by dropping the caller's
	// Min.X for the recording — otherwise the label would expand to
	// fill Min.X and our `contentDims + 2*padH` formula would overflow
	// the slot by 2*padH. The chrome reapplies Min.X below.
	measureGtx := gtx
	measureGtx.Constraints.Min = image.Point{}

	measure := op.Record(gtx.Ops)
	contentDims := LayoutLabel(measureGtx, lbl)
	contentCall := measure.Stop()

	w := contentDims.Size.X + 2*padH //nolint:mnd // 2 sides of horizontal padding.
	h := contentDims.Size.Y + 2*padV //nolint:mnd // 2 sides of vertical padding.

	// Expand the pill's chrome to honor the caller's Min.X. Leaf cells
	// set Min.X = Max.X (the Flexed slot width) before calling so the
	// pill's bg/border spans the same horizontal footprint as the
	// editor would; the trailing badge + nullify button then sit at
	// the identical x in both states. Section rows leave Min.X at the
	// default 0 so the chip keeps its compact width.
	if w < gtx.Constraints.Min.X {
		w = gtx.Constraints.Min.X
	}

	radius := h / 2 //nolint:mnd // half-height = full radius for pill shape.

	bgStack := clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, radius).Push(gtx.Ops)
	paint.ColorOp{Color: theme.Default.OverrideBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bgStack.Pop()

	paintHairlineBorder(gtx, w, h, hairline, theme.Default.Border)

	tr := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
	contentCall.Add(gtx.Ops)
	tr.Pop()

	return layout.Dimensions{Size: image.Pt(w, h)}
}
