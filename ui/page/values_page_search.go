package page

import (
	"strconv"
	"unicode"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

// handleSearchNavigation steps the active match cursor for the prev/next buttons,
// Enter, and query edits (a query change snaps to the first hit).
func (p *ValuesPage) handleSearchNavigation(gtx layout.Context) {
	// Enter in the search field jumps to the next match.
	for {
		ev, ok := p.Search.Editor.Update(gtx)
		if !ok {
			break
		}

		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			p.navigateMatch(gtx, 1)
		}
	}

	// A click grabs focus onto the button, so pull it back to the field to keep typing and keep the jump search-driven.
	if p.State.SearchPrevButton.Clicked(gtx) {
		p.navigateMatch(gtx, -1)
		gtx.Execute(key.FocusCmd{Tag: p.Search.Editor})
	}

	if p.State.SearchNextButton.Clicked(gtx) {
		p.navigateMatch(gtx, 1)
		gtx.Execute(key.FocusCmd{Tag: p.Search.Editor})
	}

	// On a query change, land on the first match (or clear the cursor) once per edit.
	query := p.Search.Editor.Text()
	if query != p.lastSearchQuery {
		p.lastSearchQuery = query

		if len(p.State.SearchMatches) > 0 {
			p.State.CurrentMatch = 0
			p.scrollToCurrentMatch(gtx)
		} else {
			p.State.CurrentMatch = -1
		}

		return
	}

	// Re-anchor the cursor when the match set changes under a stable query: -1 with no matches, else clamped to [0, n-1].
	switch n := len(p.State.SearchMatches); {
	case n == 0:
		p.State.CurrentMatch = -1
	case p.State.CurrentMatch < 0:
		p.State.CurrentMatch = 0
	case p.State.CurrentMatch >= n:
		p.State.CurrentMatch = n - 1
	}
}

// navigateMatch advances the cursor by delta with wraparound and scrolls the match
// into view (a parked -1 starts at the first match forward, the last backward).
func (p *ValuesPage) navigateMatch(gtx layout.Context, delta int) {
	n := len(p.State.SearchMatches)
	if n == 0 {
		p.State.CurrentMatch = -1

		return
	}

	switch {
	case p.State.CurrentMatch < 0 && delta > 0:
		p.State.CurrentMatch = 0
	case p.State.CurrentMatch < 0:
		p.State.CurrentMatch = n - 1
	default:
		p.State.CurrentMatch = (p.State.CurrentMatch + delta + n) % n
	}

	p.scrollToCurrentMatch(gtx)
}

// scrollToCurrentMatch reuses the flat-key jump machinery to uncollapse, scroll to, and highlight the entry under the active match cursor.
func (p *ValuesPage) scrollToCurrentMatch(gtx layout.Context) {
	entryIdx := p.currentMatchEntry()
	if entryIdx < 0 || entryIdx >= len(p.State.Entries) {
		return
	}

	p.jumpToFlatKey(gtx, p.State.Entries[entryIdx].Key)
}

// handleSearchEditorKeys applies Ctrl/Cmd word-wise caret movement, deletion, and
// Shift+Enter to the focused search field, polled before the editor's Update since
// Gio swallows these chords on Windows/Linux where ModShortcut == ModShortcutAlt.
func (p *ValuesPage) handleSearchEditorKeys(gtx layout.Context) {
	ed := p.Search.Editor

	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: ed, Name: key.NameDeleteBackward, Required: key.ModShortcut, Optional: key.ModShift},
			key.Filter{Focus: ed, Name: key.NameDeleteForward, Required: key.ModShortcut, Optional: key.ModShift},
			key.Filter{Focus: ed, Name: key.NameLeftArrow, Required: key.ModShortcut, Optional: key.ModShift},
			key.Filter{Focus: ed, Name: key.NameRightArrow, Required: key.ModShortcut, Optional: key.ModShift},
			// Shift+Enter steps to the previous match (plain Enter → next comes via the editor's SubmitEvent).
			key.Filter{Focus: ed, Name: key.NameReturn, Required: key.ModShift},
			key.Filter{Focus: ed, Name: key.NameEnter, Required: key.ModShift},
		)
		if !ok {
			break
		}

		e, isKey := ev.(key.Event)
		if !isKey || e.State != key.Press {
			continue
		}

		extend := e.Modifiers.Contain(key.ModShift)

		switch e.Name {
		case key.NameLeftArrow:
			p.searchMoveWord(-1, extend)
		case key.NameRightArrow:
			p.searchMoveWord(1, extend)
		case key.NameDeleteBackward:
			p.searchDeleteWord(-1)
		case key.NameDeleteForward:
			p.searchDeleteWord(1)
		case key.NameReturn, key.NameEnter:
			p.navigateMatch(gtx, -1)
		}
	}
}

// searchMoveWord moves the search caret one word in dir (-1 back, +1 forward), extending the selection from the anchor when extend is set.
func (p *ValuesPage) searchMoveWord(dir int, extend bool) {
	ed := p.Search.Editor
	caret, anchor := ed.Selection()

	target := wordBoundary([]rune(ed.Text()), caret, dir)
	if extend {
		ed.SetCaret(target, anchor)
	} else {
		ed.SetCaret(target, target)
	}
}

// searchDeleteWord deletes one word from the search caret in dir (-1 back, +1 forward), or the current selection if there is one.
func (p *ValuesPage) searchDeleteWord(dir int) {
	ed := p.Search.Editor

	caret, anchor := ed.Selection()
	if caret != anchor {
		ed.Insert("")

		return
	}

	target := wordBoundary([]rune(ed.Text()), caret, dir)
	if target == caret {
		return
	}

	ed.SetCaret(caret, target)
	ed.Insert("")
}

// wordBoundary returns the word-boundary offset from pos in dir (-1 back, +1 forward).
func wordBoundary(runes []rune, pos, dir int) int {
	if dir < 0 {
		return prevWordBoundary(runes, pos)
	}

	return nextWordBoundary(runes, pos)
}

// isWordRune reports whether r is part of a word (letter, digit, or underscore); everything else splits words.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// prevWordBoundary returns the rune offset of the start of the word preceding pos, skipping a leading run of spaces first.
func prevWordBoundary(runes []rune, pos int) int {
	i := pos
	for i > 0 && unicode.IsSpace(runes[i-1]) {
		i--
	}

	if i == 0 {
		return i
	}

	if isWordRune(runes[i-1]) {
		for i > 0 && isWordRune(runes[i-1]) {
			i--
		}
	} else {
		for i > 0 && !isWordRune(runes[i-1]) && !unicode.IsSpace(runes[i-1]) {
			i--
		}
	}

	return i
}

// nextWordBoundary returns the rune offset of the end of the word following pos, mirroring prevWordBoundary forward.
func nextWordBoundary(runes []rune, pos int) int {
	i := pos
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}

	if i == len(runes) {
		return i
	}

	if isWordRune(runes[i]) {
		for i < len(runes) && isWordRune(runes[i]) {
			i++
		}
	} else {
		for i < len(runes) && !isWordRune(runes[i]) && !unicode.IsSpace(runes[i]) {
			i++
		}
	}

	return i
}

// currentMatchEntry returns the entry index of the active match, or -1 when the cursor is parked or there are no matches.
func (p *ValuesPage) currentMatchEntry() int {
	if p.State.CurrentMatch < 0 || p.State.CurrentMatch >= len(p.State.SearchMatches) {
		return -1
	}

	return p.State.SearchMatches[p.State.CurrentMatch]
}

// layoutSearchNav renders the trailing match counter and prev/next jump buttons, or nothing when the query is empty.
func (p *ValuesPage) layoutSearchNav(gtx layout.Context) layout.Dimensions {
	if p.Search.Editor.Text() == "" {
		return layout.Dimensions{}
	}

	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(p.layoutSearchCounter),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutSearchNavButton(gtx, &p.State.SearchPrevButton, "↑")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutSearchNavButton(gtx, &p.State.SearchNextButton, "↓")
		}),
	)
}

// layoutSearchCounter renders the "current / total" match readout, or "No results" in the danger color when there are none.
func (p *ValuesPage) layoutSearchCounter(gtx layout.Context) layout.Dimensions {
	n := len(p.State.SearchMatches)

	txt := strconv.Itoa(p.State.CurrentMatch+1) + " / " + strconv.Itoa(n)
	txtColor := theme.Default.Muted

	if n == 0 {
		txt = "No results"
		txtColor = theme.Default.Danger
	}

	lbl := material.Caption(p.Theme, txt)
	lbl.Color = txtColor

	return layout.Inset{Right: textBtnPaddingH}.Layout(gtx, customwidget.LabelWidget(lbl))
}

// layoutSearchNavButton renders one compact prev/next jump button with horizontal-only padding so the search bar keeps the editor's line height.
func (p *ValuesPage) layoutSearchNavButton(gtx layout.Context, click *widget.Clickable, glyph string) layout.Dimensions {
	return layoutClickablePointer(gtx, click, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body2(p.Theme, glyph)
		lbl.Color = theme.Default.Override

		return layout.Inset{Left: textBtnPaddingH, Right: textBtnPaddingH}.Layout(gtx, customwidget.LabelWidget(lbl))
	})
}
