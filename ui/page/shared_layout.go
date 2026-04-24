package page

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const (
	editorFieldPadH        unit.Dp = 8
	editorFieldPadV        unit.Dp = 6
	editorFieldRadius      unit.Dp = 4
	editorFieldBorderWidth unit.Dp = 1

	presetChipRadius unit.Dp = 12
)

// layoutEditorField renders a single-line editor inside a rounded, bordered
// input box with a configurable bottom inset. Used by the repo add form and
// other panel inputs to keep field styling uniform.
func layoutEditorField(gtx layout.Context, th *material.Theme, editor *widget.Editor, hint string, bottomPad unit.Dp) layout.Dimensions {
	return layout.Inset{Bottom: bottomPad}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th, editor, hint)
		ed.Editor.SingleLine = true

		// Fill available width so all fields are the same length.
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		// Record editor layout to measure size for border.
		m := op.Record(gtx.Ops)
		dims := layout.Inset{
			Left: editorFieldPadH, Right: editorFieldPadH,
			Top: editorFieldPadV, Bottom: editorFieldPadV,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutEditor(gtx, th.Shaper, ed)
		})
		c := m.Stop()

		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(editorFieldRadius)
		bw := gtx.Dp(editorFieldBorderWidth)

		paintRoundedBorder(gtx, bounds, radius, bw, theme.ColorInputBorder, theme.ColorDropdownBg)

		c.Add(gtx.Ops)

		return dims
	})
}

// pushPointerCursor registers a pointer-hand cursor over the given area using
// a PassOp so it takes precedence over parent scroll regions.
func pushPointerCursor(gtx layout.Context, dims layout.Dimensions, tag event.Tag) {
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, tag)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()
}

func layoutPanelLabel(gtx layout.Context, th *material.Theme, text string, top, bottom unit.Dp) layout.Dimensions {
	return layout.Inset{Top: top, Bottom: bottom}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return customwidget.LayoutLabel(gtx, material.Body2(th, text))
	})
}

// layoutHelpHint renders a single-line muted Body2 caption used by per-page
// LayoutShortcutsHelp methods to fill the notification bar's idle slot.
func layoutHelpHint(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	lbl := material.Body2(th, text)
	lbl.Color = theme.ColorSecondary
	lbl.MaxLines = 1

	return customwidget.LayoutLabel(gtx, lbl)
}

// layoutCappedHeight constrains Max.Y to the given height and lays out the widget.
func layoutCappedHeight(gtx layout.Context, maxHeight unit.Dp, w layout.Widget) layout.Dimensions {
	maxH := gtx.Dp(maxHeight)
	if gtx.Constraints.Max.Y > maxH {
		gtx.Constraints.Max.Y = maxH
	}

	return w(gtx)
}
