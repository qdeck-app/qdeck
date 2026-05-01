package widget

import (
	"image"
	"image/color"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
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
	viewerTitleWeight            = 700
	viewerDoubleClick            = 2
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

	// Custom window decorations (Linux/Windows).
	customDecor bool
	winButtons  WinButtons
	titleClick  gesture.Click
}

// OpenViewerWindow spawns a new Gio window in a goroutine displaying the content.
// saveFileName is the default filename for the save dialog (falls back to "rendered.yaml").
func OpenViewerWindow(title, content, saveFileName string, customDecor bool) *ViewerLink {
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
		customDecor:  customDecor,
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

func (v *viewerWindow) run() {
	v.window = new(app.Window)
	v.window.Option(app.Title(v.title))
	v.window.Option(app.Size(viewerWindowWidth, viewerWindowHeight))

	if v.customDecor {
		v.window.Option(app.Decorated(false))
	}

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
		case app.ConfigEvent:
			v.winButtons.Maximized = e.Config.Mode == app.Maximized
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
	v.handleWinButtons(gtx)

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

func (v *viewerWindow) handleWinButtons(gtx layout.Context) {
	if !v.customDecor {
		return
	}

	if v.winButtons.Minimize.Clicked(gtx) {
		v.window.Perform(system.ActionMinimize)
	}

	if v.winButtons.Maximize.Clicked(gtx) {
		v.toggleMaximize()
	}

	if v.winButtons.Close.Clicked(gtx) {
		v.window.Perform(system.ActionClose)
	}
}

func (v *viewerWindow) toggleMaximize() {
	if v.winButtons.Maximized {
		v.window.Perform(system.ActionUnmaximize)
	} else {
		v.window.Perform(system.ActionMaximize)
	}
}

func (v *viewerWindow) layoutTitleBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Process double-click on title area to toggle maximize.
	for {
		ev, ok := v.titleClick.Update(gtx.Source)
		if !ok {
			break
		}

		if ev.Kind == gesture.KindClick && ev.NumClicks == viewerDoubleClick {
			v.toggleMaximize()
		}
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			// The title area doubles as a drag handle for moving the window.
			dims := layout.Inset{
				Left: viewerPadH, Right: viewerPadH,
				Top: viewerPadV, Bottom: viewerPadV,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, v.title)
				lbl.Font.Weight = viewerTitleWeight

				return LayoutLabel(gtx, lbl)
			})

			area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
			system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
			v.titleClick.Add(gtx.Ops)
			area.Pop()

			return dims
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.winButtons.Layout(gtx)
		}),
	)
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

	children := make([]layout.FlexChild, 0, 5) //nolint:mnd

	if v.customDecor {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutTitleBar(gtx, th)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutSeparator(gtx)
			}),
		)
	}

	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSearchRow(gtx, th)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSeparator(gtx)
		}),
		content,
	)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (v *viewerWindow) layoutSeparator(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(viewerSeparatorH))
	rect := clip.Rect{Max: size}.Push(gtx.Ops)

	paint.ColorOp{Color: theme.Default.Border}.Add(gtx.Ops)
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
