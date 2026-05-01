package widget

import (
	"image"
	"image/color"
	"io"
	"net/url"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	giowidget "gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	dropSpacerHeight unit.Dp = 8
	dropCornerRadius unit.Dp = 6
	dropZonePaddingH unit.Dp = 12
	dropZonePaddingV unit.Dp = 8
	pickBtnPaddingH  unit.Dp = 6

	// maxDropDataSize limits the data read from a drag-and-drop transfer event.
	// URI lists are typically a few hundred bytes; 1 MB is a generous safety cap.
	maxDropDataSize = 1 << 20 // 1 MB
)

//nolint:mnd // Color component values are design tokens.
var (
	dropActiveColor = color.NRGBA{R: 235, G: 240, B: 250, A: 255}
	dropTitleColor  = color.NRGBA{R: 150, G: 150, B: 165, A: 255}
)

// FileDropZone renders a file picker area with an optional button.
// On platforms that support OS-level drag-and-drop (not macOS as of Gio v0.9.0),
// it also registers as a transfer target for file drops.
type FileDropZone struct {
	Active      bool
	HideTitle   bool
	FilePaths   []string
	Title       string
	Hint        string
	PickButton  *giowidget.Clickable
	ButtonLabel string
	AlignLeft   bool
	Compact     bool // reduce vertical padding for inline use in headers
	ExtraWidget layout.Widget
}

// Layout renders the file picker area, registers the transfer target, and processes events.
func (z *FileDropZone) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Process transfer events (for platforms that support OS-level drops).
	z.processEvents(gtx)

	if z.Active {
		bounds := image.Rectangle{Max: gtx.Constraints.Max}
		radius := gtx.Dp(dropCornerRadius)

		bgRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
		paint.ColorOp{Color: dropActiveColor}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgRect.Pop()
	}

	title := z.Title
	if title == "" {
		title = "Drop values file"
	}

	// Stack-allocated scratch buffer sized to the maximum child count the
	// layout can produce: title (1) + pick button's spacer+button (2) + extra
	// widget (1). Adding a new optional child requires bumping this constant.
	const maxFlexChildren = 4

	var buf [maxFlexChildren]layout.FlexChild

	children := buf[:0]

	if !z.HideTitle {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(th, title)
			lbl.Color = dropTitleColor

			return LayoutLabel(gtx, lbl)
		}))
	}

	if z.PickButton != nil {
		label := z.ButtonLabel
		if label == "" {
			label = "Open file"
		}

		children = append(children,
			layout.Rigid(layout.Spacer{Width: dropSpacerHeight}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutPickButton(gtx, th, z.PickButton, label)
			}),
		)
	}

	if z.ExtraWidget != nil {
		extra := z.ExtraWidget

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return extra(gtx)
		}))
	}

	padV := dropZonePaddingV
	if z.Compact {
		padV = 0
	}

	dims := layout.Inset{
		Left: dropZonePaddingH, Right: dropZonePaddingH,
		Top: padV, Bottom: padV,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if z.AlignLeft {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		}

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		})
	})

	// Register transfer target within the visual bounds.
	// PassOp so pointer events pass through to the Browse button.
	pass := pointer.PassOp{}.Push(gtx.Ops)

	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)
	event.Op(gtx.Ops, z)
	area.Pop()

	pass.Pop()

	return dims
}

func (z *FileDropZone) processEvents(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(
			transfer.TargetFilter{Target: z, Type: "application/octet-stream"},
			transfer.TargetFilter{Target: z, Type: "text/uri-list"},
		)
		if !ok {
			break
		}

		switch e := ev.(type) {
		case transfer.InitiateEvent:
			z.Active = true
		case transfer.CancelEvent:
			z.Active = false
		case transfer.DataEvent:
			z.Active = false
			z.handleDrop(e)
		}
	}
}

func (z *FileDropZone) handleDrop(e transfer.DataEvent) {
	reader := e.Open()

	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(io.LimitReader(reader, maxDropDataSize))
	if err != nil {
		return
	}

	raw := string(data)
	lines := strings.Split(raw, "\n")
	paths := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimPrefix(line, "file://")
		line = strings.TrimSpace(line)

		if unescaped, err := url.PathUnescape(line); err == nil {
			line = unescaped
		}

		if line != "" {
			paths = append(paths, line)
		}
	}

	if len(paths) > 0 {
		z.FilePaths = paths
	}
}

// layoutPickButton renders a clickable text button with hover background
// that spans the full parent height.
func layoutPickButton(gtx layout.Context, th *material.Theme, click *giowidget.Clickable, label string) layout.Dimensions {
	hovered := click.Hovered()
	parentH := gtx.Constraints.Max.Y

	lbl := material.Body2(th, label)
	lbl.Color = theme.Default.Override

	m := op.Record(gtx.Ops)

	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Left: pickBtnPaddingH, Right: pickBtnPaddingH,
		}.Layout(gtx, LabelWidget(lbl))
	})

	c := m.Stop()

	// Use the full parent height for the hover background.
	btnH := max(dims.Size.Y, parentH)

	if hovered {
		bounds := image.Rectangle{Max: image.Pt(dims.Size.X, btnH)}
		bg := clip.Rect(bounds).Push(gtx.Ops)

		paint.ColorOp{Color: theme.Default.RowHover}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bg.Pop()
	}

	// Center content vertically within the full-height area.
	yOff := (btnH - dims.Size.Y) / 2 //nolint:mnd // vertical center
	off := op.Offset(image.Pt(0, yOff)).Push(gtx.Ops)
	c.Add(gtx.Ops)
	off.Pop()

	// Hand cursor.
	sz := image.Pt(dims.Size.X, btnH)
	area := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()

	return layout.Dimensions{Size: sz}
}
