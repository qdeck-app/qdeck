package widget

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/platform"
	"github.com/qdeck-app/qdeck/ui/theme"
)

func (v *viewerWindow) layoutSearchRow(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{
		Top: viewerSearchPadV, Bottom: viewerSearchPadV,
		Left: viewerSearchPadH, Right: viewerSearchPadH,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		prevHint := platform.ShortcutLabel("\u2318\u2191", "Ctrl+\u2191")
		nextHint := platform.ShortcutLabel("\u2318\u2193", "Ctrl+\u2193")
		saveHint := platform.ShortcutLabel("\u2318+S", "Ctrl+S")

		actionBtn := func(click *widget.Clickable, label string) layout.FlexChild {
			return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: viewerSearchPadH}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return v.layoutActionButton(gtx, th, click, label)
					})
			})
		}

		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return v.layoutSearchInput(gtx, th)
			}),
			actionBtn(&v.prevFileButton, "\u25B2 ("+prevHint+")"),
			actionBtn(&v.nextFileButton, "\u25BC ("+nextHint+")"),
			actionBtn(&v.saveButton, "Save ("+saveHint+")"),
			actionBtn(&v.copyButton, "Copy"),
			// Esc hint.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: viewerSearchPadH}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(th, "Esc \u2715")
						lbl.Color = theme.ColorMuted

						return LayoutLabel(gtx, lbl)
					})
			}),
		)
	})
}

func (v *viewerWindow) layoutActionButton(
	gtx layout.Context,
	th *material.Theme,
	click *widget.Clickable,
	label string,
) layout.Dimensions {
	hovered := click.Hovered()

	m := op.Record(gtx.Ops)

	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Left: viewerNavBtnPadH, Right: viewerNavBtnPadH,
			Top: viewerNavBtnPadV, Bottom: viewerNavBtnPadV,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(th, label)
			lbl.Color = theme.ColorAccent

			if hovered {
				lbl.Color = theme.ColorAccentHover
			}

			return LayoutLabel(gtx, lbl)
		})
	})

	c := m.Stop()

	if hovered {
		radius := gtx.Dp(viewerNavBtnRadius)
		bg := clip.UniformRRect(image.Rectangle{Max: dims.Size}, radius).Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorHover}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bg.Pop()
	}

	c.Add(gtx.Ops)

	// Pointer cursor.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, click)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()

	return dims
}

// layoutSearchInput renders the search editor inside a bordered input box
// with match count and navigation buttons.
func (v *viewerWindow) layoutSearchInput(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Lay out content first to measure size.
	m := op.Record(gtx.Ops)

	dims := layout.Inset{
		Left: viewerInputPadH, Right: viewerInputPadH,
		Top: viewerInputPadV, Bottom: viewerInputPadV,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { //nolint:dupl // mirrors the file-filter editor above; see note there
				searchHint := "Search (" + platform.ShortcutLabel("⌘F", "Ctrl+F") + ")"
				ed := material.Editor(th, &v.searchEditor, searchHint)
				ed.TextSize = viewerEditorTextSize

				return LayoutEditor(gtx, th.Shaper, ed)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutMatchCount(gtx, th)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutNavButtons(gtx, th)
			}),
		)
	})

	c := m.Stop()

	// Input background and border.
	bounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(viewerInputRadius)
	borderW := gtx.Dp(viewerInputBorderW)

	// Background.
	bgRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorCardBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bgRect.Pop()

	// Border edges.
	for _, edge := range EdgeBorders(bounds, borderW) {
		r := clip.Rect(edge).Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		r.Pop()
	}

	// Replay content on top.
	c.Add(gtx.Ops)

	return dims
}

func (v *viewerWindow) layoutMatchCount(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if v.lastQuery == "" {
		return layout.Dimensions{}
	}

	var text string
	if len(v.matchLines) > 0 {
		text = strconv.Itoa(v.currentMatch+1) + "/" + strconv.Itoa(len(v.matchLines))
	} else {
		text = "0/" + strconv.Itoa(len(v.matchLines))
	}

	lbl := material.Caption(th, text)
	lbl.Color = theme.ColorSecondary

	return layout.Inset{Left: viewerSearchPadH}.Layout(gtx, lbl.Layout)
}

func (v *viewerWindow) layoutNavButtons(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if len(v.matchLines) == 0 {
		return layout.Dimensions{}
	}

	return layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutActionButton(gtx, th, &v.prevMatch, "\u25b2")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutActionButton(gtx, th, &v.nextMatch, "\u25bc")
		}),
	)
}

func (v *viewerWindow) recomputeMatches(query string) {
	v.matchLines = v.matchLines[:0]
	v.currentMatch = 0

	if query == "" {
		if v.matchSet != nil {
			clear(v.matchSet)
		}

		return
	}

	for i, lower := range v.lowerLines {
		if strings.Contains(lower, query) {
			v.matchLines = append(v.matchLines, i)
		}
	}

	if v.matchSet == nil {
		v.matchSet = make(map[int]struct{}, len(v.matchLines))
	} else {
		clear(v.matchSet)
	}

	for _, idx := range v.matchLines {
		v.matchSet[idx] = struct{}{}
	}
}

func (v *viewerWindow) advanceMatch(delta int) {
	if len(v.matchLines) == 0 {
		return
	}

	v.currentMatch += delta

	// Wrap around: if past end, go to 0; if before 0, go to last match.
	if v.currentMatch >= len(v.matchLines) || v.currentMatch < 0 {
		v.currentMatch = (v.currentMatch%len(v.matchLines) + len(v.matchLines)) % len(v.matchLines)
	}

	// Scroll to the matched line.
	v.lineList.Position.First = v.matchLines[v.currentMatch]
	v.lineList.Position.Offset = 0
}
