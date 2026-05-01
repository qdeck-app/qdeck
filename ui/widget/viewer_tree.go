package widget

import (
	"cmp"
	"image"
	"image/color"
	"path"
	"slices"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/platform"
	"github.com/qdeck-app/qdeck/ui/theme"
)

// parseTemplateFiles extracts per-file manifest boundaries from a helm-template
// output stream keyed by "# Source: …" comment lines.
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

	// One path is a prefix of the other: shorter (parent dir) sorts first.
	return cmp.Compare(len(aParts), len(bParts))
}

func (v *viewerWindow) layoutFilePanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Top: viewerSearchPadV, Bottom: viewerSearchPadV,
				Left: viewerFilePadH, Right: viewerFilePadH,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { //nolint:dupl // same shape as the content search editor below but a shared helper would take more args than it saves lines
				filterHint := "Filter files (" + platform.ShortcutLabel("⌘O", "Ctrl+O") + ")"
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

				bgColor := theme.Default.RowHover
				if isSelected {
					bgColor = color.NRGBA{
						R: theme.Default.Override.R,
						G: theme.Default.Override.G,
						B: theme.Default.Override.B,
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
				paint.ColorOp{Color: theme.Default.Guide}.Add(gtx.Ops)
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
			lbl.Color = theme.Default.Muted

			return layout.Inset{Right: treeToggleWidth / 4}.Layout(gtx, LabelWidget(lbl)) //nolint:mnd // quarter spacing
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(th, node.name)
			lbl.MaxLines = 1
			lbl.Color = theme.Default.Muted

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
		lbl.Color = theme.Default.Override
	}

	return LayoutLabel(gtx, lbl)
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
