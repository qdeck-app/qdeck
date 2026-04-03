package widget

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	notificationBarHeight unit.Dp = 28
	notificationPaddingH  unit.Dp = 16
	notificationPaddingV  unit.Dp = 4
	cursorWidth           unit.Dp = 8
	cursorSpacing         unit.Dp = 4

	cursorCycleMs       int64 = 1000
	cursorOnMs          int64 = 500
	autoDismissDuration       = 10 * time.Second
)

// NotificationBar renders a fixed-height notification area with a blinking
// block cursor in front of the message text.
type NotificationBar struct{}

// Layout renders the notification bar. It always returns a fixed height
// regardless of whether a notification is active, preventing layout shifts.
func (n *NotificationBar) Layout(gtx layout.Context, th *material.Theme, notif *state.NotificationState) layout.Dimensions {
	barH := gtx.Dp(notificationBarHeight)
	fixedSize := image.Pt(gtx.Constraints.Max.X, barH)

	if !notif.Active {
		return layout.Dimensions{Size: fixedSize}
	}

	// Schedule next redraw at the next cursor blink transition or auto-dismiss, whichever is sooner.
	cyclePos := gtx.Now.UnixMilli() % cursorCycleMs

	var nextMs int64
	if cyclePos < cursorOnMs {
		nextMs = cursorOnMs - cyclePos
	} else {
		nextMs = cursorCycleMs - cyclePos
	}

	blinkAt := gtx.Now.Add(time.Duration(nextMs) * time.Millisecond)
	dismissAt := notif.ShowAt.Add(autoDismissDuration)
	invalidateAt := blinkAt

	if dismissAt.Before(blinkAt) {
		invalidateAt = dismissAt
	}

	gtx.Execute(op.InvalidateCmd{At: invalidateAt})

	// Auto-dismiss after timeout.
	if notif.IsExpired(gtx.Now, autoDismissDuration) {
		notif.Clear()

		return layout.Dimensions{Size: fixedSize}
	}

	textColor := colorForLevel(notif.Level)

	// Constrain to fixed height.
	gtx.Constraints.Min = fixedSize
	gtx.Constraints.Max = fixedSize

	return layout.Inset{Left: notificationPaddingH, Top: notificationPaddingV}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			// Measure the text first to get its actual line height.
			lbl := material.Body2(th, notif.Message)
			lbl.Color = textColor

			m := op.Record(gtx.Ops)
			lblDims := lbl.Layout(gtx)
			lblCall := m.Stop()

			// Size cursor to text line height minus half the descent for visual balance.
			cW := gtx.Dp(cursorWidth)
			sW := gtx.Dp(cursorSpacing)
			descent := lblDims.Size.Y - lblDims.Baseline
			cursorH := lblDims.Size.Y - descent/2 //nolint:mnd // half-descent visual trim
			cursorSize := image.Pt(cW, cursorH)

			visible := gtx.Now.UnixMilli()%cursorCycleMs < cursorOnMs
			if visible {
				rect := clip.Rect{Max: cursorSize}.Push(gtx.Ops)
				paint.ColorOp{Color: textColor}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				rect.Pop()
			}

			// Offset past cursor + spacer, then replay text.
			defer op.Offset(image.Pt(cW+sW, 0)).Push(gtx.Ops).Pop()

			lblCall.Add(gtx.Ops)

			totalW := cW + sW + lblDims.Size.X

			return layout.Dimensions{
				Size:     image.Pt(totalW, lblDims.Size.Y),
				Baseline: lblDims.Baseline,
			}
		})
}

func colorForLevel(level state.NotificationLevel) color.NRGBA {
	if level == state.NotificationSuccess {
		return theme.ColorStatsAdded
	}

	return theme.ColorError
}
