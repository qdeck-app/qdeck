package widget

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// AnchorMenuAction identifies which right-click menu item was chosen.
type AnchorMenuAction uint8

const (
	AnchorMenuNone AnchorMenuAction = iota
	AnchorMenuCreate
	AnchorMenuAlias
	AnchorMenuDismiss
)

const (
	anchorMenuMinWidth   unit.Dp = 160
	anchorMenuRowHeight  unit.Dp = 28
	anchorMenuPadding    unit.Dp = 4
	anchorMenuRowPadH    unit.Dp = 10
	anchorMenuRadius     unit.Dp = 6
	anchorMenuShadowA            = 40
	anchorMenuShadowOffX         = 1
	anchorMenuShadowOffY         = 2
)

// AnchorContextMenu is a small popup menu opened by right-clicking an editor
// cell. It renders two items (Create anchor…, Alias to…) as clickable rows.
// The caller sets Origin (in page-local coords) each frame the menu is open;
// Layout returns zero-size dims and paints a floating panel.
type AnchorContextMenu struct {
	createBtn widget.Clickable
	aliasBtn  widget.Clickable

	// DisableAlias dims the "Alias to…" row when the cell's file has no
	// anchors declared (picker would be empty). The row is still rendered so
	// users understand the action exists.
	DisableAlias bool

	// selected is the keyboard-highlighted row index (0 = Create, 1 = Alias).
	// Arrow keys update it, Enter activates. Also drives the focus tint so
	// mouse and keyboard affordances share a visual cue.
	selected int

	// focusRequested asks Layout to transfer keyboard focus to this menu on
	// the next frame, so arrow keys get delivered here instead of the
	// previously-focused editor. Cleared after the FocusCmd dispatches.
	focusRequested bool
}

// Reset prepares the menu for a fresh open: resets the keyboard selection
// to the first item and schedules a one-shot focus command. Call whenever
// the page sets AnchorMenuOpen = true.
func (m *AnchorContextMenu) Reset() {
	m.selected = 0
	if m.DisableAlias {
		// Keep selection on Create when the second row is inert.
		m.selected = 0
	}

	m.focusRequested = true
}

// moveSelection advances the selected index by delta (+1/-1) within the
// visible/enabled rows. Wraps at the ends and skips the disabled Alias row.
// The loop is bounded by `count` iterations so the degenerate case — only
// the Create row is enabled and already selected — is a silent no-op
// rather than looping forever.
func (m *AnchorContextMenu) moveSelection(delta int) {
	const count = 2

	next := m.selected
	for range count {
		next = (next + delta + count) % count
		if next == 1 && m.DisableAlias {
			continue
		}

		m.selected = next

		return
	}
}

// Update returns the action chosen on this frame. Press outside the menu or
// Escape produce AnchorMenuDismiss so the caller can close it.
func (m *AnchorContextMenu) Update(gtx layout.Context) AnchorMenuAction {
	if m.createBtn.Clicked(gtx) {
		return AnchorMenuCreate
	}

	if !m.DisableAlias && m.aliasBtn.Clicked(gtx) {
		return AnchorMenuAlias
	}

	// Keyboard navigation — arrows move selection, Enter activates. The
	// FocusFilter registration is critical: Gio only marks a tag as
	// keyboard-focusable when a FocusFilter is present for it, so without
	// it FocusCmd silently fails and the scoped filters below never match.
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: m},
			key.Filter{Focus: m, Name: key.NameUpArrow},
			key.Filter{Focus: m, Name: key.NameDownArrow},
			key.Filter{Focus: m, Name: key.NameReturn},
			key.Filter{Focus: m, Name: key.NameEnter},
			key.Filter{Focus: m, Name: key.NameEscape},
		)
		if !ok {
			break
		}

		ke, isKey := ev.(key.Event)
		if !isKey || ke.State != key.Press {
			continue
		}

		switch ke.Name {
		case key.NameUpArrow:
			m.moveSelection(-1)
		case key.NameDownArrow:
			m.moveSelection(1)
		case key.NameEscape:
			return AnchorMenuDismiss
		case key.NameReturn, key.NameEnter:
			switch m.selected {
			case 0:
				return AnchorMenuCreate
			case 1:
				if !m.DisableAlias {
					return AnchorMenuAlias
				}
			}
		}
	}

	// Click anywhere outside the menu dismisses it — the overlay below
	// absorbs those presses and turns them into dismiss signals.
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: m,
			Kinds:  pointer.Press,
		})
		if !ok {
			break
		}

		if pe, isPtr := ev.(pointer.Event); isPtr && pe.Kind == pointer.Press {
			return AnchorMenuDismiss
		}
	}

	return AnchorMenuNone
}

// Layout paints a full-screen invisible overlay that catches outside clicks,
// then the menu panel at origin (top-left of the menu, in the gtx's own
// coordinate system). Returns dims covering the full constraints so the
// overlay blocks everything under it.
func (m *AnchorContextMenu) Layout(gtx layout.Context, th *material.Theme, origin image.Point) layout.Dimensions {
	// Invisible overlay for outside-click dismissal.
	overlay := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, m)
	overlay.Pop()

	// One-shot focus transfer so arrow keys route to the menu, not the
	// editor that was focused before the menu opened. Reset sets the flag;
	// we clear it after dispatch so the user can tab/click away later.
	if m.focusRequested {
		gtx.Execute(key.FocusCmd{Tag: m})
		m.focusRequested = false
	}

	panelMinW := gtx.Dp(anchorMenuMinWidth)
	rowH := gtx.Dp(anchorMenuRowHeight)
	pad := gtx.Dp(anchorMenuPadding)
	height := 2*rowH + 2*pad //nolint:mnd // two rows + two paddings.

	// Clamp origin so the menu stays on screen.
	origin.X = clamp(origin.X, 0, gtx.Constraints.Max.X-panelMinW)
	origin.Y = clamp(origin.Y, 0, gtx.Constraints.Max.Y-height)

	// Offset the panel using op.Offset directly. layout.Inset converts via
	// DP round-trip which was introducing subtle misalignment; op.Offset
	// operates in pixels, matching the pixel-exact origin from the caller.
	transform := op.Offset(origin).Push(gtx.Ops)
	panelGtx := gtx
	panelGtx.Constraints.Min = image.Pt(panelMinW, height)
	panelGtx.Constraints.Max = image.Pt(panelMinW, height)
	m.layoutPanel(panelGtx, th)
	transform.Pop()

	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func (m *AnchorContextMenu) layoutPanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// Drop shadow for depth cue.
			shadow := clip.UniformRRect(
				image.Rect(
					anchorMenuShadowOffX,
					anchorMenuShadowOffY,
					gtx.Constraints.Min.X+anchorMenuShadowOffX,
					gtx.Constraints.Min.Y+anchorMenuShadowOffY,
				),
				gtx.Dp(anchorMenuRadius),
			).Push(gtx.Ops)
			paint.ColorOp{Color: color.NRGBA{A: anchorMenuShadowA}}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			shadow.Pop()

			// Panel background.
			bg := clip.UniformRRect(
				image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(anchorMenuRadius),
			).Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorDropdownBg}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			bg.Pop()

			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(anchorMenuPadding).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return m.layoutItem(gtx, th, &m.createBtn, "Create anchor…", false, m.selected == 0)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return m.layoutItem(gtx, th, &m.aliasBtn, "Alias to…", m.DisableAlias, m.selected == 1)
					}),
				)
			})
		}),
	)
}

func (m *AnchorContextMenu) layoutItem(
	gtx layout.Context, th *material.Theme, btn *widget.Clickable, label string, disabled, selected bool,
) layout.Dimensions {
	if disabled {
		// Render non-clickable dim row.
		return layout.Inset{Left: anchorMenuRowPadH, Right: anchorMenuRowPadH}.Layout(
			gtx,
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(anchorMenuRowHeight)
				lbl := material.Body2(th, label)
				lbl.Color = theme.ColorMuted

				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return LayoutLabel(gtx, lbl)
						}),
					)
				})
			})
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = gtx.Dp(anchorMenuRowHeight)
		pointer.CursorPointer.Add(gtx.Ops)

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				// Keyboard selection takes priority over hover so an arrow-key
				// user sees a stable highlight that doesn't flicker while the
				// cursor happens to sit over a different row.
				bg := theme.ColorTransparent

				switch {
				case selected:
					bg = theme.ColorFocus
				case btn.Hovered():
					bg = theme.ColorHover
				}

				if bg != theme.ColorTransparent {
					r := clip.Rect{Max: gtx.Constraints.Min}.Push(gtx.Ops)
					paint.ColorOp{Color: bg}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					r.Pop()
				}

				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: anchorMenuRowPadH, Right: anchorMenuRowPadH}.Layout(
					gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return LayoutLabel(gtx, material.Body2(th, label))
							}),
						)
					})
			}),
		)
	})
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}

	if v > hi {
		return hi
	}

	return v
}
