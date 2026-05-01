package theme

import (
	"image/color"
	"testing"
)

func TestOKLCHReferenceValues(t *testing.T) {
	tests := []struct {
		name      string
		l, c, h   float64
		want      color.NRGBA
		tolerance uint8 // per-channel ± tolerance
	}{
		{
			name: "white",
			l:    1.0, c: 0, h: 0,
			want:      color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			tolerance: 0,
		},
		{
			name: "black",
			l:    0, c: 0, h: 0,
			want:      color.NRGBA{R: 0, G: 0, B: 0, A: 255},
			tolerance: 0,
		},
		{
			// OKLCH lightness is perceptual, not linear: L=0.5 maps to
			// sRGB ≈ 99 (#636363), not the linear midpoint #808080.
			name: "perceptual mid gray",
			l:    0.5, c: 0, h: 0,
			want:      color.NRGBA{R: 99, G: 99, B: 99, A: 255},
			tolerance: 2,
		},
		{
			// CSS spec: oklch(62.8% 0.258 29.234) ≈ #ff0000.
			name: "red",
			l:    0.628, c: 0.258, h: 29.234,
			want:      color.NRGBA{R: 255, G: 0, B: 0, A: 255},
			tolerance: 2,
		},
		{
			// CSS spec: oklch(86.6% 0.295 142.495) ≈ #00ff00.
			name: "green",
			l:    0.866, c: 0.295, h: 142.495,
			want:      color.NRGBA{R: 0, G: 255, B: 0, A: 255},
			tolerance: 2,
		},
		{
			// CSS spec: oklch(45.2% 0.313 264.052) ≈ #0000ff.
			name: "blue",
			l:    0.452, c: 0.313, h: 264.052,
			want:      color.NRGBA{R: 0, G: 0, B: 255, A: 255},
			tolerance: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oklchOpaque(tt.l, tt.c, tt.h)
			if !channelsEqual(got, tt.want, tt.tolerance) {
				t.Errorf("oklch(%g, %g, %g) = %+v, want %+v (±%d)",
					tt.l, tt.c, tt.h, got, tt.want, tt.tolerance)
			}
		})
	}
}

func TestOKLCHAlpha(t *testing.T) {
	c := oklch(0, 0, 0, 0.5)
	if c.A != 128 {
		t.Errorf("alpha 0.5 → %d, want 128", c.A)
	}
}

func TestOKLCHGamutClamp(t *testing.T) {
	// High-chroma value outside sRGB gamut must clamp, not panic or return
	// out-of-range bytes.
	got := oklchOpaque(0.7, 0.4, 30)
	if got.A != 255 {
		t.Errorf("alpha must stay 255 on clamp, got %d", got.A)
	}
}

func channelsEqual(a, b color.NRGBA, tol uint8) bool {
	return absDiff(a.R, b.R) <= tol &&
		absDiff(a.G, b.G) <= tol &&
		absDiff(a.B, b.B) <= tol &&
		absDiff(a.A, b.A) <= tol
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}

	return b - a
}
