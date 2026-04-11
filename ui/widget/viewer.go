package widget

import (
	"cmp"
	"image"
	"image/color"
	"io"
	"log/slog"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	viewerWindowWidth    unit.Dp = 1100
	viewerWindowHeight   unit.Dp = 750
	viewerPadH           unit.Dp = 12
	viewerPadV           unit.Dp = 4
	viewerSearchPadH     unit.Dp = 8
	viewerSearchPadV     unit.Dp = 6
	viewerMatchAlpha             = 80
	viewerNavBtnPadH     unit.Dp = 4
	viewerNavBtnPadV     unit.Dp = 2
	viewerNavBtnRadius   unit.Dp = 4
	viewerSeparatorH     unit.Dp = 1
	viewerFilePadH       unit.Dp = 8
	viewerFilePadV       unit.Dp = 4
	viewerFileRadius     unit.Dp = 4
	viewerSplitRatio     float32 = -0.5 // ~25% left panel
	viewerActiveAlpha            = 30
	viewerEditorTextSize unit.Sp = 14
	viewerInputRadius    unit.Dp = 6
	viewerInputPadH      unit.Dp = 10
	viewerInputPadV      unit.Dp = 6
	viewerInputBorderW   unit.Dp = 1
)

const (
	sourcePrefix               = "# Source: "
	treeIndentPerLevel unit.Dp = 16
	treeToggleWidth    unit.Dp = 16
	treeGuideW         unit.Dp = 1
	treeExpandedIcon           = "\u25BC" // ▼
	treeCollapsedIcon          = "\u25B6" // ▶
	treeDirFileIdx             = -1       // sentinel for directory nodes
)

// treeNode represents one row in the flattened directory tree.
type treeNode struct {
	name    string // display name (base name of dir or file)
	path    string // full relative path (after common prefix stripped)
	depth   int    // nesting level for indentation
	isDir   bool   // directory vs file
	fileIdx int    // index into viewerWindow.files (-1 for dirs)
}

// templateFile represents one rendered template file in the manifest.
type templateFile struct {
	sourcePath string // "mychart/templates/deployment.yaml"
	fileName   string // "deployment.yaml"
	startLine  int    // first line index in lines (the "---" line)
	endLine    int    // exclusive end line index
}

// ViewerLink is the handle returned by OpenViewerWindow, allowing the main app
// to push updated content to an already-open viewer window.
type ViewerLink struct {
	ch     chan string
	closed atomic.Bool
	window atomic.Pointer[app.Window]
}

// Send pushes new content to the viewer (non-blocking).
// Returns false if the viewer has been closed or the channel is full.
func (l *ViewerLink) Send(content string) bool {
	if l.closed.Load() {
		return false
	}

	select {
	case l.ch <- content:
		// Wake the viewer's event loop so it processes the new content.
		if w := l.window.Load(); w != nil {
			w.Invalidate()
		}

		return true
	default:
		return false
	}
}

// viewerWindow displays rendered YAML in a scrollable, searchable window
// with a left panel for navigating between rendered template files.
type viewerWindow struct {
	window     *app.Window
	explorer   *explorer.Explorer
	title      string
	content    string
	lines      []string
	lowerLines []string // pre-computed lowercase lines for search

	// Content search state.
	searchEditor widget.Editor
	lineList     widget.List
	nextMatch    widget.Clickable
	prevMatch    widget.Clickable
	lastQuery    string
	matchLines   []int
	matchSet     map[int]struct{}
	currentMatch int

	// Action buttons.
	copyButton     widget.Clickable
	saveButton     widget.Clickable
	prevFileButton widget.Clickable
	nextFileButton widget.Clickable
	saveFileName   string

	// File navigation state.
	files        []templateFile
	selectedFile int
	fileFilter   widget.Editor

	// Directory tree state.
	treeNodes     []treeNode
	visibleNodes  []int
	dirExpanded   map[string]bool
	nodeClicks    []widget.Clickable
	nodeList      widget.List
	lastFileQuery string

	// Reusable maps for file filter to avoid per-query allocations.
	filterAncestorDirs map[string]struct{}
	filterMatchedFiles map[int]struct{}

	// Split panel.
	split SplitView

	// Link for receiving updated content from the main app.
	link *ViewerLink
}

// OpenViewerWindow spawns a new Gio window in a goroutine displaying the content.
// saveFileName is the default filename for the save dialog (falls back to "rendered.yaml").
func OpenViewerWindow(title, content, saveFileName string) *ViewerLink {
	if saveFileName == "" {
		saveFileName = "rendered.yaml"
	}

	link := &ViewerLink{ch: make(chan string, 1)}

	lines := strings.Split(content, "\n")
	lowerLines := make([]string, len(lines))

	for i, line := range lines {
		lowerLines[i] = strings.ToLower(line)
	}

	v := &viewerWindow{
		title:        title,
		content:      content,
		lines:        lines,
		lowerLines:   lowerLines,
		saveFileName: saveFileName,
		link:         link,
	}
	v.lineList.Axis = layout.Vertical
	v.nodeList.Axis = layout.Vertical
	v.searchEditor.SingleLine = true
	v.searchEditor.Submit = true
	v.fileFilter.SingleLine = true
	v.split.Ratio = viewerSplitRatio

	// Parse template files from the manifest.
	v.files = parseTemplateFiles(v.lines)
	v.treeNodes, v.dirExpanded = buildTreeNodes(v.files)
	v.visibleNodes = make([]int, 0, len(v.treeNodes))
	v.recomputeVisibleNodes("")

	go v.run()

	return link
}

// parseTemplateFiles walks the lines and extracts template file boundaries.
// Helm manifests use "---" separators followed by "# Source: <path>" comments.
func parseTemplateFiles(lines []string) []templateFile {
	count := 0

	for _, line := range lines {
		if strings.HasPrefix(line, sourcePrefix) {
			count++
		}
	}

	files := make([]templateFile, 0, count)

	for i, line := range lines {
		if !strings.HasPrefix(line, sourcePrefix) {
			continue
		}

		sourcePath := strings.TrimSpace(line[len(sourcePrefix):])
		fileName := path.Base(sourcePath)

		startLine := i
		// Include the "---" separator if it's on the line before.
		if i > 0 && strings.TrimSpace(lines[i-1]) == "---" {
			startLine = i - 1
		}

		// Close previous file section at the start of the new one.
		if len(files) > 0 {
			files[len(files)-1].endLine = startLine
		}

		files = append(files, templateFile{
			sourcePath: sourcePath,
			fileName:   fileName,
			startLine:  startLine,
		})
	}

	// Close last file section.
	if len(files) > 0 {
		files[len(files)-1].endLine = len(lines)
	}

	return files
}

// commonPathPrefix finds the longest shared directory prefix across all template file paths.
func commonPathPrefix(files []templateFile) string {
	if len(files) == 0 {
		return ""
	}

	refParts := strings.Split(files[0].sourcePath, "/")
	commonLen := len(refParts) - 1 // exclude the file name itself

	for _, f := range files[1:] {
		parts := strings.Split(f.sourcePath, "/")

		n := len(parts) - 1 // exclude file name
		if n < commonLen {
			commonLen = n
		}

		for i := range commonLen {
			if parts[i] != refParts[i] {
				commonLen = i

				break
			}
		}

		if commonLen == 0 {
			return ""
		}
	}

	if commonLen == 0 {
		return ""
	}

	return strings.Join(refParts[:commonLen], "/") + "/"
}

// buildTreeNodes constructs a flattened pre-order DFS tree from template files.
func buildTreeNodes(files []templateFile) ([]treeNode, map[string]bool) {
	if len(files) == 0 {
		return nil, nil
	}

	prefix := commonPathPrefix(files)

	// Collect unique directories.
	dirSet := make(map[string]struct{})

	for _, f := range files {
		stripped := strings.TrimPrefix(f.sourcePath, prefix)
		dir := path.Dir(stripped)

		for dir != "." && dir != "" {
			if _, exists := dirSet[dir]; exists {
				break // ancestors already added
			}

			dirSet[dir] = struct{}{}
			dir = path.Dir(dir)
		}
	}

	// Build combined node slice.
	nodes := make([]treeNode, 0, len(dirSet)+len(files))

	for d := range dirSet {
		nodes = append(nodes, treeNode{
			name:    path.Base(d),
			path:    d,
			depth:   strings.Count(d, "/"),
			isDir:   true,
			fileIdx: treeDirFileIdx,
		})
	}

	for i, f := range files {
		stripped := strings.TrimPrefix(f.sourcePath, prefix)
		nodes = append(nodes, treeNode{
			name:    f.fileName,
			path:    stripped,
			depth:   strings.Count(stripped, "/"),
			isDir:   false,
			fileIdx: i,
		})
	}

	slices.SortFunc(nodes, compareTreeNodes)

	// All directories start expanded.
	expanded := make(map[string]bool, len(dirSet))
	for d := range dirSet {
		expanded[d] = true
	}

	return nodes, expanded
}

// compareTreeNodes sorts tree nodes in pre-order DFS: by path segments,
// directories before files at the same parent level.
func compareTreeNodes(a, b treeNode) int {
	aParts := strings.Split(a.path, "/")
	bParts := strings.Split(b.path, "/")

	minLen := min(len(aParts), len(bParts))

	for i := range minLen {
		if aParts[i] == bParts[i] {
			continue
		}

		// At the same depth, directories (with children) sort before files.
		aIsParent := i < len(aParts)-1
		bIsParent := i < len(bParts)-1

		if aIsParent && !bIsParent {
			return -1
		}

		if !aIsParent && bIsParent {
			return 1
		}

		return cmp.Compare(aParts[i], bParts[i])
	}

	return cmp.Compare(len(aParts), len(bParts))
}

func (v *viewerWindow) run() {
	v.window = new(app.Window)
	v.window.Option(app.Title(v.title))
	v.window.Option(app.Size(viewerWindowWidth, viewerWindowHeight))
	v.explorer = explorer.NewExplorer(v.window)
	v.link.window.Store(v.window)

	w := v.window

	th := theme.NewTheme()

	var ops op.Ops

	for {
		e := w.Event()
		v.explorer.ListenEvents(e)

		switch e := e.(type) {
		case app.DestroyEvent:
			v.link.closed.Store(true)

			return
		case app.FrameEvent:
			select {
			case newContent := <-v.link.ch:
				v.refreshContent(newContent)
			default:
			}

			gtx := app.NewContext(&ops, e)
			v.update(gtx)
			v.layout(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// refreshContent replaces the viewer's content and rebuilds the file tree,
// preserving the currently selected file position where possible.
func (v *viewerWindow) refreshContent(content string) {
	v.content = content
	v.lines = strings.Split(content, "\n")

	v.lowerLines = slices.Grow(v.lowerLines[:0], len(v.lines))[:len(v.lines)]
	for i, line := range v.lines {
		v.lowerLines[i] = strings.ToLower(line)
	}

	v.files = parseTemplateFiles(v.lines)
	v.treeNodes, v.dirExpanded = buildTreeNodes(v.files)
	v.nodeClicks = v.nodeClicks[:0]

	v.selectedFile = max(min(v.selectedFile, len(v.files)-1), 0)

	v.recomputeVisibleNodes(strings.ToLower(v.fileFilter.Text()))

	// Re-run search against new content; scroll position is preserved.
	v.lastQuery = ""
}

func (v *viewerWindow) update(gtx layout.Context) {
	// Content search.
	query := strings.ToLower(v.searchEditor.Text())
	if query != v.lastQuery {
		v.lastQuery = query
		v.recomputeMatches(query)
	}

	// Enter in search field jumps to next match.
	for {
		ev, ok := v.searchEditor.Update(gtx)
		if !ok {
			break
		}

		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.advanceMatch(1)
		}
	}

	if v.nextMatch.Clicked(gtx) {
		v.advanceMatch(1)
	}

	if v.prevMatch.Clicked(gtx) {
		v.advanceMatch(-1)
	}

	if v.copyButton.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{
			Type: "application/text",
			Data: io.NopCloser(strings.NewReader(v.content)),
		})
	}

	if v.saveButton.Clicked(gtx) {
		v.saveContent()
	}

	if v.prevFileButton.Clicked(gtx) {
		v.jumpFile(-1)
	}

	if v.nextFileButton.Clicked(gtx) {
		v.jumpFile(1)
	}

	// File filter.
	fileQuery := strings.ToLower(v.fileFilter.Text())
	if fileQuery != v.lastFileQuery {
		v.lastFileQuery = fileQuery
		v.recomputeVisibleNodes(fileQuery)
	}

	// Tree node clicks.
	v.ensureNodeClicks(len(v.visibleNodes))

	for i := range v.visibleNodes {
		if i >= len(v.nodeClicks) {
			break
		}

		if !v.nodeClicks[i].Clicked(gtx) {
			continue
		}

		node := v.treeNodes[v.visibleNodes[i]]

		if node.isDir {
			v.dirExpanded[node.path] = !v.dirExpanded[node.path]
			v.recomputeVisibleNodes(v.lastFileQuery)

			// recomputeVisibleNodes overwrites the backing array via [:0]+append,
			// corrupting the range-captured slice. Break to avoid stale indices.
			break
		}

		v.selectedFile = node.fileIdx
		v.lineList.Position.First = v.files[node.fileIdx].startLine
		v.lineList.Position.Offset = 0
	}

	// Scroll sync: update selected file based on scroll position.
	v.syncSelectedFile()

	// Keyboard shortcuts.
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, v)
	area.Pop()

	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameEscape},
			key.Filter{Name: "S", Required: key.ModShortcut},
			key.Filter{Name: key.NameUpArrow, Required: key.ModShortcut},
			key.Filter{Name: key.NameDownArrow, Required: key.ModShortcut},
			key.Filter{Name: "F", Required: key.ModShortcut},
			key.Filter{Name: "O", Required: key.ModShortcut},
		)
		if !ok {
			break
		}

		e, isKey := ev.(key.Event)
		if !isKey || e.State != key.Press {
			continue
		}

		switch e.Name {
		case key.NameEscape:
			v.window.Perform(system.ActionClose)
		case "S":
			v.saveContent()
		case key.NameUpArrow:
			v.jumpFile(-1)
		case key.NameDownArrow:
			v.jumpFile(1)
		case "F":
			gtx.Execute(key.FocusCmd{Tag: &v.searchEditor})
		case "O":
			gtx.Execute(key.FocusCmd{Tag: &v.fileFilter})
		}
	}
}

func (v *viewerWindow) syncSelectedFile() {
	if len(v.files) == 0 {
		return
	}

	firstVisible := v.lineList.Position.First

	// Binary search: find last file where startLine <= firstVisible.
	sel := max(sort.Search(len(v.files), func(i int) bool {
		return v.files[i].startLine > firstVisible
	})-1, 0)

	v.selectedFile = sel
}

func (v *viewerWindow) saveContent() {
	// Capture content before launching the goroutine to avoid a data race
	// with refreshContent, which can reassign v.content on the event loop.
	content := v.content

	go func() {
		writer, err := v.explorer.CreateFile(v.saveFileName)
		if err != nil {
			return // user cancelled or error
		}

		defer func() { _ = writer.Close() }()

		if _, err := writer.Write([]byte(content)); err != nil {
			slog.Error("viewer: save failed", "error", err)
		}
	}()
}

func (v *viewerWindow) jumpFile(delta int) {
	if len(v.files) == 0 {
		return
	}

	v.selectedFile += delta

	// Wrap around: if past end, go to 0; if before 0, go to last file.
	if v.selectedFile >= len(v.files) || v.selectedFile < 0 {
		v.selectedFile = (v.selectedFile%len(v.files) + len(v.files)) % len(v.files)
	}

	v.lineList.Position.First = v.files[v.selectedFile].startLine
	v.lineList.Position.Offset = 0
}

func (v *viewerWindow) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	var content layout.FlexChild

	if len(v.files) == 0 {
		content = layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return v.layoutContent(gtx, th)
		})
	} else {
		content = layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return v.split.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return v.layoutFilePanel(gtx, th)
				},
				func(gtx layout.Context) layout.Dimensions {
					return v.layoutContent(gtx, th)
				},
			)
		})
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSearchRow(gtx, th)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSeparator(gtx)
		}),
		content,
	)
}

func (v *viewerWindow) layoutFilePanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Top: viewerSearchPadV, Bottom: viewerSearchPadV,
				Left: viewerFilePadH, Right: viewerFilePadH,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				filterHint := "Filter files (" + ShortcutLabel("⌘O", "Ctrl+O") + ")"
				ed := material.Editor(th, &v.fileFilter, filterHint)
				ed.TextSize = viewerEditorTextSize

				return LayoutEditor(gtx, th.Shaper, ed)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSeparator(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return v.layoutTreeList(gtx, th)
		}),
	)
}

func (v *viewerWindow) layoutTreeList(gtx layout.Context, th *material.Theme) layout.Dimensions {
	count := len(v.visibleNodes)

	return material.List(th, &v.nodeList).Layout(gtx, count,
		func(gtx layout.Context, index int) layout.Dimensions {
			if index >= len(v.visibleNodes) || index >= len(v.nodeClicks) {
				return layout.Dimensions{}
			}

			nodeIdx := v.visibleNodes[index]
			node := v.treeNodes[nodeIdx]
			click := &v.nodeClicks[index]
			hovered := click.Hovered()

			isSelected := !node.isDir && node.fileIdx == v.selectedFile

			indent := unit.Dp(node.depth) * treeIndentPerLevel

			m := op.Record(gtx.Ops)

			dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{
					Left: viewerFilePadH + indent, Right: viewerFilePadH,
					Top: viewerFilePadV, Bottom: viewerFilePadV,
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if node.isDir {
						return v.layoutDirNode(gtx, th, node)
					}

					return v.layoutFileNode(gtx, th, node, isSelected)
				})
			})

			c := m.Stop()

			// Selected or hover background.
			if isSelected || hovered {
				bounds := image.Rectangle{Max: dims.Size}
				radius := gtx.Dp(viewerFileRadius)
				bg := clip.UniformRRect(bounds, radius).Push(gtx.Ops)

				bgColor := theme.ColorHover
				if isSelected {
					bgColor = color.NRGBA{
						R: theme.ColorAccent.R,
						G: theme.ColorAccent.G,
						B: theme.ColorAccent.B,
						A: viewerActiveAlpha, //nolint:mnd // subtle active highlight
					}
				}

				paint.ColorOp{Color: bgColor}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				bg.Pop()
			}

			// Tree guide lines.
			guideW := gtx.Dp(treeGuideW)

			for d := 1; d <= node.depth; d++ {
				x := gtx.Dp(viewerFilePadH + unit.Dp(d-1)*treeIndentPerLevel +
					treeIndentPerLevel/2) //nolint:mnd // midpoint of indent level

				guide := clip.Rect{
					Min: image.Pt(x, 0),
					Max: image.Pt(x+guideW, dims.Size.Y),
				}.Push(gtx.Ops)
				paint.ColorOp{Color: theme.ColorTreeGuide}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				guide.Pop()
			}

			c.Add(gtx.Ops)

			// Pointer cursor.
			pass := pointer.PassOp{}.Push(gtx.Ops)
			btnArea := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

			event.Op(gtx.Ops, click)
			pointer.CursorPointer.Add(gtx.Ops)

			btnArea.Pop()
			pass.Pop()

			return dims
		})
}

func (v *viewerWindow) layoutDirNode(
	gtx layout.Context,
	th *material.Theme,
	node treeNode,
) layout.Dimensions {
	icon := treeExpandedIcon
	if !v.dirExpanded[node.path] {
		icon = treeCollapsedIcon
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(th, icon)
			lbl.Color = theme.ColorSecondary

			return layout.Inset{Right: treeToggleWidth / 4}.Layout(gtx, lbl.Layout) //nolint:mnd // quarter spacing
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(th, node.name)
			lbl.MaxLines = 1
			lbl.Color = theme.ColorSecondary

			return LayoutLabel(gtx, lbl)
		}),
	)
}

func (v *viewerWindow) layoutFileNode(
	gtx layout.Context,
	th *material.Theme,
	node treeNode,
	isSelected bool,
) layout.Dimensions {
	lbl := material.Caption(th, node.name)
	lbl.MaxLines = 1

	if isSelected {
		lbl.Color = theme.ColorAccent
	}

	return LayoutLabel(gtx, lbl)
}

func (v *viewerWindow) layoutSearchRow(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{
		Top: viewerSearchPadV, Bottom: viewerSearchPadV,
		Left: viewerSearchPadH, Right: viewerSearchPadH,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		prevHint := ShortcutLabel("\u2318\u2191", "Ctrl+\u2191")
		nextHint := ShortcutLabel("\u2318\u2193", "Ctrl+\u2193")
		saveHint := ShortcutLabel("\u2318+S", "Ctrl+S")

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
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				searchHint := "Search (" + ShortcutLabel("⌘F", "Ctrl+F") + ")"
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

func (v *viewerWindow) layoutSeparator(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(viewerSeparatorH))
	rect := clip.Rect{Max: size}.Push(gtx.Ops)

	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()

	return layout.Dimensions{Size: size}
}

func (v *viewerWindow) layoutContent(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return material.List(th, &v.lineList).Layout(gtx, len(v.lines),
		func(gtx layout.Context, index int) layout.Dimensions {
			_, isMatch := v.matchSet[index]

			// Record text layout to measure actual line height.
			m := op.Record(gtx.Ops)

			dims := layout.Inset{
				Left: viewerPadH, Right: viewerPadH,
				Top: viewerPadV, Bottom: viewerPadV,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, v.lines[index])

				return LayoutLabel(gtx, lbl)
			})

			c := m.Stop()

			// Paint highlight at the measured size, not Constraints.Max.
			if isMatch {
				rect := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
				paint.ColorOp{Color: color.NRGBA{
					R: 255, G: 255, B: 100, A: viewerMatchAlpha, //nolint:mnd // yellow highlight
				}}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				rect.Pop()
			}

			// Replay text on top.
			c.Add(gtx.Ops)

			return dims
		})
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

func (v *viewerWindow) recomputeVisibleNodes(query string) {
	v.visibleNodes = v.visibleNodes[:0]

	if query == "" {
		// No filter: show nodes whose ancestors are all expanded.
		for i, node := range v.treeNodes {
			if v.isNodeVisible(node) {
				v.visibleNodes = append(v.visibleNodes, i)
			}
		}

		return
	}

	// With filter: find matching files and collect their ancestor directories.
	if v.filterAncestorDirs == nil {
		v.filterAncestorDirs = make(map[string]struct{})
	} else {
		clear(v.filterAncestorDirs)
	}

	if v.filterMatchedFiles == nil {
		v.filterMatchedFiles = make(map[int]struct{})
	} else {
		clear(v.filterMatchedFiles)
	}

	ancestorDirs := v.filterAncestorDirs
	matchedFiles := v.filterMatchedFiles

	for i, node := range v.treeNodes {
		if node.isDir {
			continue
		}

		lowerName := strings.ToLower(node.name)
		lowerPath := strings.ToLower(node.path)

		if !strings.Contains(lowerName, query) && !strings.Contains(lowerPath, query) {
			continue
		}

		matchedFiles[i] = struct{}{}

		dir := path.Dir(node.path)
		for dir != "." && dir != "" {
			if _, exists := ancestorDirs[dir]; exists {
				break
			}

			ancestorDirs[dir] = struct{}{}
			dir = path.Dir(dir)
		}
	}

	for i, node := range v.treeNodes {
		if node.isDir {
			if _, ok := ancestorDirs[node.path]; ok {
				v.visibleNodes = append(v.visibleNodes, i)
			}
		} else if _, ok := matchedFiles[i]; ok {
			v.visibleNodes = append(v.visibleNodes, i)
		}
	}
}

// isNodeVisible checks that all ancestor directories of a node are expanded.
func (v *viewerWindow) isNodeVisible(node treeNode) bool {
	dir := path.Dir(node.path)
	for dir != "." && dir != "" {
		if !v.dirExpanded[dir] {
			return false
		}

		dir = path.Dir(dir)
	}

	return true
}

func (v *viewerWindow) ensureNodeClicks(count int) {
	for len(v.nodeClicks) < count {
		v.nodeClicks = append(v.nodeClicks, widget.Clickable{})
	}
}
