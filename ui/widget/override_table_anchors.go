package widget

import (
	"image"
	"strings"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
)

// propagateAnchoredValueEdit mirrors a cell edit into every alias that
// resolves through the same anchor. YAML semantics say aliases take their
// value from the anchor, so editing the anchor's scalar (or a leaf under an
// anchored mapping) must visually update the parallel alias cells — without
// this the UI drifts from what the saved file will contain.
//
// The nearest anchored ancestor (or the edited cell itself) determines the
// alias name; the suffix after that ancestor's flat key is appended to each
// alias's flat key to locate the corresponding editor.
func (t *OverrideTable) propagateAnchoredValueEdit(
	gtx layout.Context,
	col int,
	changedKey string,
	entries []service.FlatValueEntry,
	editors []widget.Editor,
) {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil || len(cs.CustomValues.Anchors) == 0 {
		return
	}

	anchors := cs.CustomValues.Anchors

	anchorKey, anchorName := findAnchoredAncestor(anchors, changedKey)
	if anchorName == "" {
		return
	}

	rawSuffix := strings.TrimPrefix(strings.TrimPrefix(changedKey, anchorKey), ".")

	// Cache the dot-prefixed suffix once — each alias target is aliasKey+suffix
	// with no branch or extra concat inside the loop.
	var suffix string
	if rawSuffix != "" {
		suffix = "." + rawSuffix
	}

	changedIdx := indexOfEntry(entries, changedKey)
	if changedIdx < 0 || changedIdx >= len(editors) {
		return
	}

	newText := editors[changedIdx].Text()

	for aliasKey, info := range anchors {
		if info.Role != service.AnchorRoleAlias || info.Name != anchorName {
			continue
		}

		targetKey := aliasKey + suffix

		idx := indexOfEntry(entries, targetKey)
		if idx < 0 || idx >= len(editors) {
			continue
		}

		if editors[idx].Text() == newText {
			continue
		}

		editors[idx].SetText(newText)
		t.drainEditorEvents(gtx, &editors[idx])
		cs.MarkOverride(idx, state.StripYAMLComments(newText) != "")
	}
}

// findAnchoredAncestor returns the flat key of the nearest ancestor (or the
// key itself) annotated with role=Anchor in anchors, along with the anchor
// name. Returns ("", "") when no anchored ancestor is found.
func findAnchoredAncestor(anchors map[string]service.AnchorInfo, key string) (string, string) {
	bestKey := ""
	bestName := ""

	for k, info := range anchors {
		if info.Role != service.AnchorRoleAnchor {
			continue
		}

		if k == key || strings.HasPrefix(key, k+".") {
			if len(k) > len(bestKey) {
				bestKey = k
				bestName = info.Name
			}
		}
	}

	return bestKey, bestName
}

func indexOfEntry(entries []service.FlatValueEntry, key string) int {
	for i, e := range entries {
		if e.Key == key {
			return i
		}
	}

	return -1
}

// columnAnchorInfo returns anchor/alias metadata for a flat key in a specific
// override column, or the zero value when that column did not load an anchored
// file or the key has no anchor annotation.
func (t *OverrideTable) columnAnchorInfo(col int, key string) service.AnchorInfo {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil {
		return service.AnchorInfo{}
	}

	return cs.CustomValues.Anchors[key]
}

// aliasTextMatchesEffective reports whether text equals the scalar the alias
// at key in col currently resolves to. Used by the alias-edit guard to
// distinguish a programmatic sync (text already matches) from a real user
// divergence that should prompt for unlock.
func (t *OverrideTable) aliasTextMatchesEffective(col int, key, text string) bool {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil || cs.CustomValues.NodeTree == nil {
		return false
	}

	resolved, ok := service.EffectiveScalarAt(cs.CustomValues.NodeTree, key)

	return ok && resolved == text
}

// columnAnchors returns the full anchor map for a column, or nil when the
// column has no loaded file. Used to resolve an alias badge's jump target.
func (t *OverrideTable) columnAnchors(col int) map[string]service.AnchorInfo {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil {
		return nil
	}

	return cs.CustomValues.Anchors
}

// layoutAnchorBadge renders a small pill marking a YAML anchor definition
// (`&name`, green) or alias usage (`*name`, blue). Returns zero-size
// dimensions when info is empty so the surrounding Flex.Rigid collapses with
// no visible gap.
//
// Both roles are clickable when their respective handler is wired:
//   - Alias badges fire OnJumpToFlatKey with the anchor's source flat key so
//     the user jumps to the anchor definition.
//   - Anchor badges fire OnAnchorBadgeClicked with the column, flat key, and
//     anchor name so the page can show the reverse menu (aliases → jump).
//
// The badge registers a click region and a pointer cursor only for the role
// whose handler is available; non-clickable badges remain purely decorative.
//
// col and flatKey are only used for the anchor-badge handler; aliases derive
// their jump target from the anchors map.
func (t *OverrideTable) layoutAnchorBadge(
	gtx layout.Context,
	info service.AnchorInfo,
	anchors map[string]service.AnchorInfo,
	click *gesture.Click,
	col int,
	flatKey string,
) layout.Dimensions {
	if info.Role == service.AnchorRoleNone || info.Name == "" {
		return layout.Dimensions{}
	}

	var (
		sigil string
		bg    = theme.ColorAccent
	)

	switch info.Role {
	case service.AnchorRoleAnchor:
		sigil = "&"
		bg = theme.ColorStatsAdded
	case service.AnchorRoleAlias:
		sigil = "*"
		bg = theme.ColorAccent
	case service.AnchorRoleNone:
		return layout.Dimensions{}
	}

	clickable := click != nil && t.badgeHandler(info.Role) != nil
	if clickable {
		for {
			ev, ok := click.Update(gtx.Source)
			if !ok {
				break
			}

			if ev.Kind != gesture.KindClick {
				continue
			}

			switch info.Role {
			case service.AnchorRoleAlias:
				if target, found := findAnchorSourceKey(anchors, info.Name); found {
					t.OnJumpToFlatKey(gtx, target)
				}
			case service.AnchorRoleAnchor:
				t.OnAnchorBadgeClicked(gtx, col, flatKey, info.Name)
			case service.AnchorRoleNone:
			}
		}
	}

	lbl := material.Caption(t.Theme, sigil+info.Name)
	lbl.Color = theme.ColorWhite
	lbl.MaxLines = 1

	gap := gtx.Dp(overrideBadgeGap)
	radius := gtx.Dp(overrideBadgeRadius)
	innerInset := layout.Inset{
		Left:   overrideBadgePaddingH,
		Right:  overrideBadgePaddingH,
		Top:    overrideBadgePaddingV,
		Bottom: overrideBadgePaddingV,
	}

	innerGtx := gtx
	innerGtx.Constraints.Min = image.Point{}

	macro := op.Record(gtx.Ops)
	pillDims := innerInset.Layout(innerGtx, func(gtx layout.Context) layout.Dimensions {
		return LayoutLabel(gtx, lbl)
	})
	pillCall := macro.Stop()

	offset := op.Offset(image.Pt(gap, 0)).Push(gtx.Ops)
	shape := clip.UniformRRect(image.Rectangle{Max: pillDims.Size}, radius).Push(gtx.Ops)
	paint.ColorOp{Color: bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	shape.Pop()
	pillCall.Add(gtx.Ops)

	if clickable {
		area := clip.Rect{Max: pillDims.Size}.Push(gtx.Ops)
		click.Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		area.Pop()
	}

	offset.Pop()

	return layout.Dimensions{Size: image.Point{X: gap + pillDims.Size.X, Y: pillDims.Size.Y}}
}

// badgeHandler reports whether a handler is wired for a badge role — used to
// decide whether to register a click region and a pointer cursor. Returns a
// non-nil func when the role has a callback; nil otherwise.
func (t *OverrideTable) badgeHandler(role service.AnchorRole) any {
	switch role {
	case service.AnchorRoleAlias:
		if t.OnJumpToFlatKey != nil {
			return t.OnJumpToFlatKey
		}
	case service.AnchorRoleAnchor:
		if t.OnAnchorBadgeClicked != nil {
			return t.OnAnchorBadgeClicked
		}
	case service.AnchorRoleNone:
	}

	return nil
}

// findAnchorSourceKey scans anchors for an entry with role=Anchor and name=n
// and returns the flat key where it is defined. Returns ("", false) when no
// matching anchor is in the map — the alias either points at an anchor
// defined outside the file (not representable here) or the map is nil.
func findAnchorSourceKey(anchors map[string]service.AnchorInfo, n string) (string, bool) {
	for k, info := range anchors {
		if info.Role == service.AnchorRoleAnchor && info.Name == n {
			return k, true
		}
	}

	return "", false
}
