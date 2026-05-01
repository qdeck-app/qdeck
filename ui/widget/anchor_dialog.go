package widget

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// AnchorDialogMode selects the dialog body. Create prompts for a name; Alias
// lists the anchor names declared in the file so the user can pick one to
// alias to; AliasesOf lists every flat key using a specific anchor so the
// user can jump to its usage.
type AnchorDialogMode uint8

const (
	AnchorDialogNone AnchorDialogMode = iota
	AnchorDialogCreate
	AnchorDialogAlias
	AnchorDialogAliasesOf
)

// AnchorDialogAction describes the user's decision on a dialog frame.
// Rename and Delete are only produced in AliasesOf mode via the action
// buttons at the bottom; they carry no submit value (the target anchor name
// is already known to the caller).
type AnchorDialogAction uint8

const (
	AnchorActionNone AnchorDialogAction = iota
	AnchorActionSubmit
	AnchorActionCancel
	AnchorActionRename
	AnchorActionDelete
)

const (
	anchorDialogOverlayAlpha         = 128
	anchorDialogMaxWidth     unit.Dp = 380
	anchorDialogMinWidth     unit.Dp = 280
	anchorDialogRadius       unit.Dp = 8
	anchorDialogPadding      unit.Dp = 18
	anchorDialogGap          unit.Dp = 12
	anchorDialogSmallGap     unit.Dp = 6
	anchorDialogListMaxH     unit.Dp = 220
	anchorDialogItemPad      unit.Dp = 6
	anchorDialogInputH       unit.Dp = 32
	anchorDialogInputRadius  unit.Dp = 4
	anchorDialogInputBorderW         = 1
)

// AnchorDialog renders a small modal used for anchor operations: Create
// prompts for a name, Alias/AliasesOf show a filterable list picker. The
// owning page sets the mode (via the matching Setup* call) and repeatedly
// invokes Update + Layout each frame until an action fires.
type AnchorDialog struct {
	NameEditor   widget.Editor
	SearchEditor widget.Editor
	OKButton     widget.Clickable
	CancelBtn    widget.Clickable
	RenameBtn    widget.Clickable
	DeleteBtn    widget.Clickable

	// Items populated by SetupForAlias / SetupForAliasesOf. itemPrefix is
	// prepended to each item when rendered (e.g. "*" for alias names so the
	// list visually matches the badge style). Empty for AliasesOf which
	// shows raw flat keys.
	Items      []string
	itemPrefix string
	itemClicks []widget.Clickable

	// scratchVisible stores the indexes of Items passing the current search
	// filter. Reused across frames (see rebuildVisible) to avoid per-frame
	// allocation per the no-UI-alloc convention.
	scratchVisible []int

	// selectedIdx is the index into Items of the keyboard-selected row.
	// Advanced by Up/Down arrows; used to highlight a row and to drive
	// Enter-to-submit in list modes. -1 when no items are visible.
	selectedIdx int

	// focusTarget names the editor to auto-focus on the next Layout call.
	// Cleared after the focus command is issued so the user can Tab away
	// without being snapped back each frame.
	focusTarget *widget.Editor

	// Populated by Update/Layout so the page can read the chosen value via
	// Result on an AnchorActionSubmit frame.
	submittedValue string
}

// SetupForCreate prepares the dialog to prompt for a new anchor name, seeding
// the editor with an initial suggestion (typically the flat key's last
// segment) and requesting focus on the name field.
func (d *AnchorDialog) SetupForCreate(initial string) {
	d.NameEditor.SetText(initial)
	// Place the caret at the end with an empty selection. Passing a different
	// start/end would create a selection, and passing (0, 0) would send the
	// caret home — we want end-of-text with no selection so typing appends.
	d.NameEditor.SetCaret(len(initial), len(initial))
	d.NameEditor.SingleLine = true
	d.NameEditor.Submit = true
	d.SearchEditor.SetText("")
	d.submittedValue = ""
	d.focusTarget = &d.NameEditor
}

// SetupForAlias prepares the dialog to pick an anchor name to alias to. items
// is the list of declared anchor names in the target file.
func (d *AnchorDialog) SetupForAlias(items []string) {
	d.setupListMode(items, "*")
}

// SetupForAliasesOf prepares the dialog to pick a flat key where the given
// anchor is used, for jumping to a specific usage site. items is the list of
// flat keys; itemPrefix is empty because these aren't anchor names.
func (d *AnchorDialog) SetupForAliasesOf(items []string) {
	d.setupListMode(items, "")
}

func (d *AnchorDialog) setupListMode(items []string, prefix string) {
	d.Items = items
	d.itemPrefix = prefix

	if len(d.itemClicks) < len(items) {
		d.itemClicks = append(d.itemClicks, make([]widget.Clickable, len(items)-len(d.itemClicks))...)
	}

	d.SearchEditor.SetText("")
	d.SearchEditor.SingleLine = true
	d.SearchEditor.Submit = true
	d.submittedValue = ""
	d.focusTarget = &d.SearchEditor
	d.scratchVisible = d.scratchVisible[:0]

	if len(items) > 0 {
		d.selectedIdx = 0
	} else {
		d.selectedIdx = -1
	}
}

// Result returns the submitted value on AnchorActionSubmit frames: anchor
// name for Create/Alias, flat key for AliasesOf. Empty on other frames.
func (d *AnchorDialog) Result() string {
	return d.submittedValue
}

// Update returns the dialog's action for this frame. Call before Layout.
func (d *AnchorDialog) Update(gtx layout.Context, mode AnchorDialogMode) AnchorDialogAction {
	if d.CancelBtn.Clicked(gtx) {
		return AnchorActionCancel
	}

	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}

		if e, isKey := ev.(key.Event); isKey && e.State == key.Press {
			return AnchorActionCancel
		}
	}

	switch mode {
	case AnchorDialogCreate:
		return d.updateCreate(gtx)
	case AnchorDialogAlias, AnchorDialogAliasesOf:
		return d.updateList(gtx, mode)
	case AnchorDialogNone:
	}

	return AnchorActionNone
}

func (d *AnchorDialog) updateCreate(gtx layout.Context) AnchorDialogAction {
	if d.OKButton.Clicked(gtx) {
		return d.submitFromNameEditor()
	}

	for {
		ev, ok := d.NameEditor.Update(gtx)
		if !ok {
			break
		}

		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			return d.submitFromNameEditor()
		}
	}

	return AnchorActionNone
}

func (d *AnchorDialog) updateList(gtx layout.Context, mode AnchorDialogMode) AnchorDialogAction {
	if mode == AnchorDialogAliasesOf {
		if d.RenameBtn.Clicked(gtx) {
			return AnchorActionRename
		}

		if d.DeleteBtn.Clicked(gtx) {
			return AnchorActionDelete
		}
	}

	// Arrow-key navigation and Escape scoped to the search editor's focus
	// so the editor's own key handling doesn't shadow them. Escape cancels;
	// Up/Down move the selection through scratchVisible.
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &d.SearchEditor, Name: key.NameEscape},
			key.Filter{Focus: &d.SearchEditor, Name: key.NameUpArrow},
			key.Filter{Focus: &d.SearchEditor, Name: key.NameDownArrow},
		)
		if !ok {
			break
		}

		ke, isKey := ev.(key.Event)
		if !isKey || ke.State != key.Press {
			continue
		}

		switch ke.Name {
		case key.NameEscape:
			return AnchorActionCancel
		case key.NameUpArrow:
			d.moveSelection(-1)
		case key.NameDownArrow:
			d.moveSelection(1)
		}
	}

	// Drain search-editor events: Submit picks the selected item (or the
	// first visible if the selection has been filtered out).
	for {
		ev, ok := d.SearchEditor.Update(gtx)
		if !ok {
			break
		}

		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			if action := d.submitSelected(); action != AnchorActionNone {
				return action
			}
		}
	}

	// Click on any item submits.
	for i := range d.itemClicks {
		if i >= len(d.Items) {
			break
		}

		if d.itemClicks[i].Clicked(gtx) {
			d.submittedValue = d.Items[i]

			return AnchorActionSubmit
		}
	}

	if d.OKButton.Clicked(gtx) {
		if action := d.submitSelected(); action != AnchorActionNone {
			return action
		}
	}

	return AnchorActionNone
}

// moveSelection advances d.selectedIdx by delta through scratchVisible. If
// the current selection isn't in the visible set (e.g. filtered out), the
// move snaps to either the first or last visible item depending on
// direction. No-op when no items are visible.
func (d *AnchorDialog) moveSelection(delta int) {
	d.rebuildVisible()

	if len(d.scratchVisible) == 0 {
		d.selectedIdx = -1

		return
	}

	cur := -1

	for i, origIdx := range d.scratchVisible {
		if origIdx == d.selectedIdx {
			cur = i

			break
		}
	}

	next := cur + delta
	if cur == -1 {
		if delta > 0 {
			next = 0
		} else {
			next = len(d.scratchVisible) - 1
		}
	}

	if next < 0 {
		next = 0
	}

	if next >= len(d.scratchVisible) {
		next = len(d.scratchVisible) - 1
	}

	d.selectedIdx = d.scratchVisible[next]
}

// submitSelected attempts to submit the currently selected item. Falls back
// to the first visible item when the selection is invalid. Returns
// AnchorActionNone when nothing is selectable (empty filter result).
func (d *AnchorDialog) submitSelected() AnchorDialogAction {
	d.rebuildVisible()

	if len(d.scratchVisible) == 0 {
		return AnchorActionNone
	}

	target := d.selectedIdx
	inVisible := false

	for _, origIdx := range d.scratchVisible {
		if origIdx == target {
			inVisible = true

			break
		}
	}

	if !inVisible {
		target = d.scratchVisible[0]
	}

	if target < 0 || target >= len(d.Items) {
		return AnchorActionNone
	}

	d.submittedValue = d.Items[target]

	return AnchorActionSubmit
}

func (d *AnchorDialog) submitFromNameEditor() AnchorDialogAction {
	text := strings.TrimSpace(d.NameEditor.Text())
	if text == "" {
		return AnchorActionNone
	}

	d.submittedValue = text

	return AnchorActionSubmit
}

// rebuildVisible refreshes scratchVisible to match items passing the current
// search filter. Reuses the backing array across frames to avoid per-frame
// allocation (per the codebase's no-UI-alloc convention). Also re-anchors
// selectedIdx so it always points at a visible item after filter changes —
// without this, the highlight would disappear after typing a query that
// filters out the previous selection.
func (d *AnchorDialog) rebuildVisible() {
	d.scratchVisible = d.scratchVisible[:0]

	query := strings.ToLower(strings.TrimSpace(d.SearchEditor.Text()))

	for i, item := range d.Items {
		if query == "" || strings.Contains(strings.ToLower(item), query) {
			d.scratchVisible = append(d.scratchVisible, i)
		}
	}

	if len(d.scratchVisible) == 0 {
		d.selectedIdx = -1

		return
	}

	for _, origIdx := range d.scratchVisible {
		if origIdx == d.selectedIdx {
			return
		}
	}

	d.selectedIdx = d.scratchVisible[0]
}

// Layout renders the dialog. mode controls the body; title is the header label.
func (d *AnchorDialog) Layout(
	gtx layout.Context, th *material.Theme, mode AnchorDialogMode, title string,
) layout.Dimensions {
	// Issue a one-shot focus command if Setup* requested one. Clearing the
	// target after dispatch lets the user Tab away without being snapped back.
	if d.focusTarget != nil {
		gtx.Execute(key.FocusCmd{Tag: d.focusTarget})
		d.focusTarget = nil
	}

	overlay := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	paint.ColorOp{Color: color.NRGBA{A: anchorDialogOverlayAlpha}}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	event.Op(gtx.Ops, d)
	overlay.Pop()

	for {
		_, ok := gtx.Event(pointer.Filter{
			Target: d,
			Kinds:  pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll,
		})
		if !ok {
			break
		}
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(anchorDialogMaxWidth)
		gtx.Constraints.Min.X = gtx.Dp(anchorDialogMinWidth)

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(dialogBackground(anchorDialogRadius)),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(anchorDialogPadding).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return LayoutLabel(gtx, material.Body1(th, title))
						}),
						layout.Rigid(layout.Spacer{Height: anchorDialogGap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutBody(gtx, th, mode)
						}),
						layout.Rigid(layout.Spacer{Height: anchorDialogGap}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutButtons(gtx, th, mode)
						}),
					)
				})
			}),
		)
	})
}

func (d *AnchorDialog) layoutBody(gtx layout.Context, th *material.Theme, mode AnchorDialogMode) layout.Dimensions {
	switch mode {
	case AnchorDialogCreate:
		return d.layoutTextInput(gtx, th, &d.NameEditor, "anchor-name")
	case AnchorDialogAlias, AnchorDialogAliasesOf:
		return d.layoutSearchableList(gtx, th, mode)
	case AnchorDialogNone:
	}

	return layout.Dimensions{}
}

// layoutTextInput renders the editor wrapped in a bordered rounded rect so
// the input visually matches the rest of the app's inputs. Used by Create
// mode and as the search-field inside list modes.
func (d *AnchorDialog) layoutTextInput(
	gtx layout.Context, th *material.Theme, editor *widget.Editor, hint string,
) layout.Dimensions {
	ed := material.Editor(th, editor, hint)
	inputH := gtx.Dp(anchorDialogInputH)
	inputRadius := gtx.Dp(anchorDialogInputRadius)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			border := clip.UniformRRect(
				image.Rectangle{Max: image.Pt(gtx.Constraints.Max.X, inputH)},
				inputRadius,
			).Push(gtx.Ops)
			paint.ColorOp{Color: theme.Default.Border}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			border.Pop()

			inner := clip.UniformRRect(
				image.Rect(
					anchorDialogInputBorderW,
					anchorDialogInputBorderW,
					gtx.Constraints.Max.X-anchorDialogInputBorderW,
					inputH-anchorDialogInputBorderW,
				),
				inputRadius,
			).Push(gtx.Ops)
			paint.ColorOp{Color: theme.Default.White}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			inner.Pop()

			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, inputH)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(anchorDialogItemPad).Layout(gtx, ed.Layout)
		}),
	)
}

// layoutSearchableList renders a search field above a filterable clickable
// list. The list is bounded in height and populated from d.Items, filtered
// through scratchVisible. Used by both Alias (pick an anchor name) and
// AliasesOf (pick a usage site) modes.
func (d *AnchorDialog) layoutSearchableList(
	gtx layout.Context, th *material.Theme, mode AnchorDialogMode,
) layout.Dimensions {
	d.rebuildVisible()

	if len(d.Items) == 0 {
		return LayoutLabel(gtx, material.Caption(th, d.emptyHint(mode)))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.layoutTextInput(gtx, th, &d.SearchEditor, "Search…")
		}),
		layout.Rigid(layout.Spacer{Height: anchorDialogSmallGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(d.scratchVisible) == 0 {
				return LayoutLabel(gtx, material.Caption(th, "No matches."))
			}

			gtx.Constraints.Max.Y = gtx.Dp(anchorDialogListMaxH)

			children := d.buildListChildren(th)

			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
	)
}

func (d *AnchorDialog) emptyHint(mode AnchorDialogMode) string {
	if mode == AnchorDialogAliasesOf {
		return "No aliases reference this anchor."
	}

	return "No anchors declared in this file."
}

func (d *AnchorDialog) buildListChildren(th *material.Theme) []layout.FlexChild {
	children := make([]layout.FlexChild, 0, len(d.scratchVisible))

	for _, origIdx := range d.scratchVisible {
		idx := origIdx

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			click := &d.itemClicks[idx]
			// Force the row to span the full width so both the hover tint
			// and the click-hit area cover the whole row, not just the
			// label glyphs.
			gtx.Constraints.Min.X = gtx.Constraints.Max.X

			selected := idx == d.selectedIdx

			return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				pointer.CursorPointer.Add(gtx.Ops)

				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						bg := theme.Default.Transparent

						switch {
						case selected:
							bg = theme.Default.RowSelected
						case click.Hovered():
							bg = theme.Default.RowHover
						}

						// Inside Stack's Expanded pass, Constraints.Min equals
						// the Stacked children's max size (labelDims). Use its
						// height so the row stays single-line tall, but widen
						// to the full row with Max.X.
						size := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)

						rect := clip.Rect{Max: size}.Push(gtx.Ops)
						paint.ColorOp{Color: bg}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						rect.Pop()

						return layout.Dimensions{Size: size}
					}),
					layout.Stacked(func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(anchorDialogItemPad).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return LayoutLabel(gtx, material.Body2(th, d.itemPrefix+d.Items[idx]))
						})
					}),
				)
			})
		}))
	}

	return children
}

func (d *AnchorDialog) layoutButtons(gtx layout.Context, th *material.Theme, mode AnchorDialogMode) layout.Dimensions {
	cancel := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(th, &d.CancelBtn, "Cancel")
		btn.Background = theme.Default.Bg3
		btn.Color = theme.Default.Ink

		return btn.Layout(gtx)
	})

	gap := layout.Rigid(layout.Spacer{Width: anchorDialogGap}.Layout)

	if mode == AnchorDialogAliasesOf {
		// Replace OK with Rename/Delete: the user's primary action in this
		// mode is to pick an alias (click a list row) or manage the anchor,
		// not "submit first visible" — so the OK button is redundant.
		del := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &d.DeleteBtn, "Delete")
			btn.Background = theme.Default.TrafficRed

			return btn.Layout(gtx)
		})
		rename := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			btn := material.Button(th, &d.RenameBtn, "Rename")
			btn.Background = theme.Default.Ink

			return btn.Layout(gtx)
		})

		return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx, cancel, gap, del, gap, rename)
	}

	ok := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(th, &d.OKButton, "OK")
		btn.Background = theme.Default.Ink

		return btn.Layout(gtx)
	})

	return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx, cancel, gap, ok)
}
