package page

import (
	"image"
	"path/filepath"
	"strconv"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/platform"
	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

func (p *ValuesPage) layoutColumnHeaders(gtx layout.Context) layout.Dimensions {
	ratio := p.Table.ColumnRatio
	if ratio <= 0 {
		ratio = valuesDefaultRatio
	}

	// material.List subtracts the scrollbar width from child constraints,
	// so the table's totalW = viewport - scrollbar. Match that here.
	viewportW := gtx.Constraints.Max.X
	scrollW := gtx.Dp(valuesScrollbarWidth)
	totalW := viewportW - scrollW
	divW := gtx.Dp(valuesDividerWidth)
	leftW := int(ratio * float32(totalW))
	rightW := max(totalW-leftW-divW, 0)

	// 1. Measure trailing buttons (Recent ▾, +Values) via op.Record so we know their width.
	// perColW approximates the last override column's width; used to decide compact labels.
	perColW := rightW / max(p.State.ColumnCount, 1)
	btnMacro := op.Record(gtx.Ops)
	btnDims := p.layoutTrailingButtons(gtx, perColW)
	btnCall := btnMacro.Stop()
	p.trailingBtnW = btnDims.Size.X

	// 2. Record the main header row to measure its height.
	m := op.Record(gtx.Ops)

	dims := layout.Inset{Top: valuesHeaderPadV, Bottom: valuesHeaderPadV}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			// Cap Max.Y so the header height is stable across empty/loaded states.
			contentH := gtx.Dp(valuesHeaderMinH)
			gtx.Constraints.Max.Y = contentH

			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Zero-width spacer that sets the flex cross-axis height to contentH.
				// Children keep crossMin=0, so they return natural heights and
				// get properly centered by layout.Middle.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(0, contentH)}
				}),
				// Left: Key + Default Value with explicit pixel width matching the table.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = leftW
					gtx.Constraints.Max.X = leftW

					return layout.Inset{Left: valuesSpacing, Right: valuesSpacing}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions { //nolint:mnd // key proportion
									lbl := material.Body2(p.Theme, "Key")
									lbl.Font.Weight = valuesHeaderWeight

									return customwidget.LayoutLabel(gtx, lbl)
								}),
								layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions { //nolint:mnd // value proportion
									lbl := material.Body2(p.Theme, "Default Value")
									lbl.Font.Weight = valuesHeaderWeight
									lbl.Alignment = text.End

									return customwidget.LayoutLabel(gtx, lbl)
								}),
							)
						})
				}),
				// Divider spacer (matches table's rigid divider width).
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(divW, 0)}
				}),
				// Right: per-column file statuses only (no buttons — they are overlaid).
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return p.layoutColumnFileStatuses(gtx)
				}),
			)
		})

	c := m.Stop()
	c.Add(gtx.Ops)

	// 3. Overlay trailing buttons at the right edge, aligned to content area.
	if btnDims.Size.X > 0 {
		padV := gtx.Dp(valuesHeaderPadV)
		contentH := dims.Size.Y - 2*padV //nolint:mnd // top + bottom padding
		btnX := viewportW - btnDims.Size.X
		btnY := padV + (contentH-btnDims.Size.Y)/2 //nolint:mnd // center in content area

		btnOff := op.Offset(image.Pt(btnX, btnY)).Push(gtx.Ops)
		btnCall.Add(gtx.Ops)
		btnOff.Pop()
	}

	// 4. Draw divider lines at table-aligned positions.
	sepH := gtx.Dp(valuesSeparatorHeight)

	// Vertical divider between left and right panels.
	vDiv := clip.Rect{
		Min: image.Pt(leftW, 0),
		Max: image.Pt(leftW+divW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	vDiv.Pop()

	// Sub-column dividers between override columns in the header.
	colCount := p.State.ColumnCount
	if colCount > 1 {
		subDivW := gtx.Dp(headerSubDividerW)
		subTotalDivW := subDivW * (colCount - 1)
		subColW := (rightW - subTotalDivW) / colCount
		rightStart := leftW + divW

		for i := 1; i < colCount; i++ {
			x := rightStart + i*subColW + (i-1)*subDivW

			subDiv := clip.Rect{
				Min: image.Pt(x, 0),
				Max: image.Pt(x+subDivW, dims.Size.Y),
			}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			subDiv.Pop()
		}
	}

	// Horizontal line above header.
	topLine := clip.Rect{
		Max: image.Pt(viewportW, sepH),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	topLine.Pop()

	// Horizontal line below header.
	botLine := clip.Rect{
		Min: image.Pt(0, dims.Size.Y-sepH),
		Max: image.Pt(viewportW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	botLine.Pop()

	return dims
}

// layoutColumnFileStatuses renders per-column file statuses (no trailing buttons).
// Each column gets equal Flexed(1) weight with sub-divider spacers between them.
func (p *ValuesPage) layoutColumnFileStatuses(gtx layout.Context) layout.Dimensions {
	colCount := p.State.ColumnCount

	var children [state.MaxCustomColumns*2 - 1]layout.FlexChild //nolint:mnd // cols + dividers

	n := 0

	for c := range colCount {
		col := c

		children[n] = layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return p.layoutSingleColumnStatus(gtx, col)
		})
		n++

		if c < colCount-1 {
			subDivW := gtx.Dp(headerSubDividerW)

			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(subDivW, 0)}
			})
			n++
		}
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
}

// layoutTrailingButtons renders the "Recent ▾" and "+Values" buttons.
// Called via op.Record to measure width, then overlaid at the right edge.
// perColW is the pixel width of the last override column; when the buttons
// would consume more than half of it, "Recent ▾" collapses to just "▾".
func (p *ValuesPage) layoutTrailingButtons(gtx layout.Context, perColW int) layout.Dimensions {
	showRecent := len(p.State.RecentValuesFiles) > 0
	showAddCol := p.State.CanAddColumn()

	if !showRecent && !showAddCol {
		return layout.Dimensions{}
	}

	recentLabel := recentBtnLabel

	// Measure with full label first; if it exceeds half the last column, use compact.
	if showRecent && perColW > 0 {
		probe := op.Record(gtx.Ops)
		fullW := p.layoutTrailingButtonsInner(gtx, recentLabel, showRecent, showAddCol)

		probe.Stop()

		if fullW.Size.X > perColW/3 { //nolint:mnd // leave room for Browse button
			recentLabel = recentBtnLabelCompact
		}
	}

	return p.layoutTrailingButtonsInner(gtx, recentLabel, showRecent, showAddCol)
}

func (p *ValuesPage) layoutTrailingButtonsInner(
	gtx layout.Context, recentLabel string, showRecent, showAddCol bool,
) layout.Dimensions {
	var children [2]layout.FlexChild //nolint:mnd // recent + addcol

	n := 0

	if showRecent {
		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &p.State.RecentDropdownToggle, recentLabel, valuesPaddingSmall)
		})
		n++
	}

	if showAddCol {
		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &p.State.AddColumnButton, addColumnLabel, valuesPaddingSmall)
		})
		n++
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
}

// layoutSingleColumnStatus renders the file status for one column:
// empty → drop zone / browse, loaded → filename + save + x.
func (p *ValuesPage) layoutSingleColumnStatus(gtx layout.Context, colIdx int) layout.Dimensions {
	col := &p.State.Columns[colIdx]
	hasFile := len(col.CustomFilePaths) > 0
	hasOverrides := hasFile || col.HasOverrides()

	var inner func(layout.Context) layout.Dimensions
	if !hasOverrides {
		inner = func(gtx layout.Context) layout.Dimensions {
			return p.layoutColumnDropZone(gtx, colIdx)
		}
	} else {
		inner = func(gtx layout.Context) layout.Dimensions {
			return p.layoutColumnFileStatus(gtx, colIdx)
		}
	}

	// Columns after the first get an extra remove-column button.
	if colIdx == 0 {
		return inner(gtx)
	}

	idx := colIdx

	if col.RemoveColumnButton.Clicked(gtx) && p.OnRemoveColumn != nil {
		p.OnRemoveColumn(idx)
		gtx.Execute(op.InvalidateCmd{})
	}

	// Reserve space: trailing buttons (Recent, +Values) on the last column,
	// or a small gap before the sub-divider on middle columns.
	var trailingPad int
	if colIdx == p.State.ColumnCount-1 {
		trailingPad = p.trailingBtnW
	} else {
		trailingPad = gtx.Dp(valuesSpacing)
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, inner),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &col.RemoveColumnButton, "x", valuesPaddingSmall)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(trailingPad, 0)}
		}),
	)
}

// layoutColumnFileStatus renders the filename, close, and save buttons for a loaded column.
func (p *ValuesPage) layoutColumnFileStatus(gtx layout.Context, colIdx int) layout.Dimensions {
	col := &p.State.Columns[colIdx]
	hasFile := len(col.CustomFilePaths) > 0

	if col.SaveValuesButton.Clicked(gtx) && p.OnSaveColumnValues != nil {
		p.OnSaveColumnValues(colIdx)
	}

	label := "New Values File"
	if hasFile {
		label = filepath.Base(col.CustomFilePaths[0])

		if len(col.CustomFilePaths) > 1 {
			label += " (+" + strconv.Itoa(len(col.CustomFilePaths)-1) + " merged)"
		}
	}

	if col.ValuesModified {
		label += "*"
	}

	idx := colIdx

	// Handle filename click → reveal in Finder.
	if col.FileNameButton.Clicked(gtx) && hasFile && p.OnRevealFile != nil {
		p.OnRevealFile(idx)
	}

	// Handle open-in-editor click.
	if col.OpenInEditorButton.Clicked(gtx) && hasFile && p.OnOpenInEditor != nil {
		p.OnOpenInEditor(idx)
	}

	const maxChildren = 4

	var (
		children [maxChildren]layout.FlexChild
		n        int
	)

	children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: valuesSpacing}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				if !hasFile {
					lbl := material.Body2(p.Theme, label)
					lbl.Font.Weight = valuesHeaderWeight
					lbl.MaxLines = 1

					return customwidget.LayoutLabel(gtx, lbl)
				}

				return layoutClickablePointer(gtx, &col.FileNameButton,
					func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(p.Theme, label)
						lbl.Font.Weight = valuesHeaderWeight
						lbl.MaxLines = 1

						return customwidget.LayoutLabel(gtx, lbl)
					})
			})
	})
	n++

	// Open in VS Code button (only when a file is loaded).
	if hasFile {
		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: valuesPaddingSmall}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					iconColor := theme.ColorAccent
					if col.OpenInEditorButton.Hovered() {
						iconColor = theme.ColorAccentHover
					}

					return layoutIconButton(gtx, p.Theme, &col.OpenInEditorButton,
						func(gtx layout.Context) layout.Dimensions {
							return customwidget.LayoutVSCodeIcon(gtx, editorIconSize, iconColor)
						})
				})
		})
		n++
	}

	// Close button right next to filename: always clears the file/overrides.
	children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		if col.CloseButton.Clicked(gtx) && p.OnClearColumn != nil {
			p.OnClearColumn(idx)

			// Force re-render: column state changed mid-frame after Table was wired.
			gtx.Execute(op.InvalidateCmd{})
		}

		return LayoutTextButton(gtx, p.Theme, &col.CloseButton, "x", valuesPaddingSmall)
	})
	n++

	if col.ValuesModified {
		saveHint := platform.ShortcutLabel("\u2318+S", "Ctrl+S")

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &col.SaveValuesButton, "Save ("+saveHint+")", valuesPaddingSmall)
		})
		n++
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
}

// layoutColumnDropZone renders a compact drop zone / browse button for a single column.
func (p *ValuesPage) layoutColumnDropZone(gtx layout.Context, colIdx int) layout.Dimensions {
	col := &p.State.Columns[colIdx]

	if col.PickFileButton.Clicked(gtx) && p.OnOpenColumnFile != nil {
		p.OnOpenColumnFile(colIdx)
	}

	dz := &p.DropZones[colIdx]
	dz.Active = col.FileDropActive

	// Hide the "Drop values file" title when the column is too narrow,
	// accounting for trailing buttons (Recent, +Values) overlaid on the last column.
	effectiveW := gtx.Constraints.Max.X
	if colIdx == p.State.ColumnCount-1 {
		effectiveW -= p.trailingBtnW
	}

	dz.HideTitle = !p.State.DropSupported || effectiveW < gtx.Dp(dropZoneCompactThreshold)
	dz.PickButton = &col.PickFileButton
	dz.ButtonLabel = browseLabel
	dz.AlignLeft = true
	dz.Compact = true

	return dz.Layout(gtx, p.Theme)
}

// overrideColumnOffset returns the pixel offset and width of the override (right) column.
// Subtracts scrollbar width to match layoutColumnHeaders alignment.
func (p *ValuesPage) overrideColumnOffset(gtx layout.Context) (leftPx int, rightPx int) {
	ratio := p.Table.ColumnRatio
	if ratio <= 0 {
		ratio = valuesDefaultRatio
	}

	totalW := gtx.Constraints.Max.X - gtx.Dp(valuesScrollbarWidth)
	divW := gtx.Dp(valuesDividerWidth)
	left := int(ratio * float32(totalW))
	right := max(totalW-left-divW, 0)

	return left + divW, right
}
