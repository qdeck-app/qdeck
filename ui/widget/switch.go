package widget

import (
	"image"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	giowidget "gioui.org/widget"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// SwitchState carries the click target for a single switch instance plus a
// cached previous value used for click-debouncing (single click → single
// toggle even if the underlying widget.Clickable observes multiple frames
// of the same press). Construct one per cell that hosts a switch.
type SwitchState struct {
	Click giowidget.Clickable
}

// LayoutSwitch renders a 22×12dp pill toggle. on is the current value;
// returns the requested new value if the user clicked the switch this
// frame (caller must persist), otherwise returns on unchanged.
//
// Click area extends across the entire pill rect — caller doesn't need
// to wrap LayoutSwitch in another clickable. The switch reserves its
// fixed 22×12dp footprint regardless of constraints.
func LayoutSwitch(gtx layout.Context, s *SwitchState, on bool) (newValue bool, dims layout.Dimensions) {
	trackW := gtx.Dp(theme.Default.SwitchTrackW)
	trackH := gtx.Dp(theme.Default.SwitchTrackH)
	radius := gtx.Dp(theme.Default.SwitchTrackR)
	knobSize := gtx.Dp(theme.Default.SwitchKnobSize)
	shift := gtx.Dp(theme.Default.SwitchKnobShift)

	// Track click events. .Clicked drains the queued click and returns true
	// once per actual click — toggle in response.
	if s.Click.Clicked(gtx) {
		on = !on
	}

	gtx.Constraints.Min = image.Pt(trackW, trackH)
	gtx.Constraints.Max = image.Pt(trackW, trackH)

	dims = s.Click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Track background.
		trackColor := theme.Default.BorderStrong
		if on {
			trackColor = theme.Default.Override
		}

		track := clip.UniformRRect(image.Rectangle{Max: image.Pt(trackW, trackH)}, radius).Push(gtx.Ops)
		paint.ColorOp{Color: trackColor}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		track.Pop()

		// Knob — 1dp inset from track top so it sits inside the pill.
		knobInset := (trackH - knobSize) / 2 //nolint:mnd // vertical centering.
		knobX := knobInset

		if on {
			knobX += shift
		}

		knobRect := image.Rectangle{
			Min: image.Pt(knobX, knobInset),
			Max: image.Pt(knobX+knobSize, knobInset+knobSize),
		}
		knob := clip.UniformRRect(knobRect, knobSize/2).Push(gtx.Ops) //nolint:mnd // half-size = full radius for circle.
		paint.ColorOp{Color: theme.Default.Bg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		knob.Pop()

		pointer.CursorPointer.Add(gtx.Ops)

		return layout.Dimensions{Size: image.Pt(trackW, trackH)}
	})

	return on, dims
}

const (
	boolLiteralTrue  = "true"
	boolLiteralFalse = "false"
)

// ParseBoolValue extracts the boolean value of a YAML scalar. Mirrors yaml.v3's
// loose parsing: "true"/"True"/"TRUE"/"y"/"yes"/"on"/"1" → true.
// Anything else (including empty string) → false.
func ParseBoolValue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case boolLiteralTrue, "y", "yes", "on", "1":
		return true
	default:
		return false
	}
}

// FormatBoolValue serializes a boolean as the lowercase YAML scalar form
// (the form yaml.v3 emits by default).
func FormatBoolValue(b bool) string {
	if b {
		return boolLiteralTrue
	}

	return boolLiteralFalse
}
