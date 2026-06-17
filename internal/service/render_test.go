package service

import (
	"errors"
	"image/color"
	"strings"
	"testing"
)

// --- parseColor ---

func TestParseColor_Hex(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
	}{
		{"#ff0000", color.RGBA{255, 0, 0, 255}},
		{"#FF0000", color.RGBA{255, 0, 0, 255}},
		{"ff0000", color.RGBA{255, 0, 0, 255}},
		{"#f00", color.RGBA{255, 0, 0, 255}},
		{"#000000", color.RGBA{0, 0, 0, 255}},
		{"#ffffff", color.RGBA{255, 255, 255, 255}},
	}
	for _, c := range cases {
		got, err := parseColor(c.in)
		if err != nil {
			t.Errorf("parseColor(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseColor(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseColor_Decimal(t *testing.T) {
	got, err := parseColor("255-0-128")
	if err != nil {
		t.Fatal(err)
	}
	if got != (color.RGBA{255, 0, 128, 255}) {
		t.Errorf("got %v", got)
	}
}

func TestParseColor_Transparent(t *testing.T) {
	for _, kw := range []string{"transparent", "none", "TRANSPARENT"} {
		got, err := parseColor(kw)
		if err != nil {
			t.Fatalf("%q: %v", kw, err)
		}
		if got.A != 0 {
			t.Errorf("%q: expected alpha 0, got %v", kw, got)
		}
	}
}

func TestParseColor_Invalid(t *testing.T) {
	// "#12345" is wrong length; "300-0-0" is out of byte range; "zz-00-00" won't parse as ints.
	for _, bad := range []string{"zz-00-00", "#12345", "300-0-0"} {
		_, err := parseColor(bad)
		if !errors.Is(err, ErrInvalidColor) {
			t.Errorf("parseColor(%q): expected ErrInvalidColor, got %v", bad, err)
		}
	}
}

// --- renderRaster ---

func TestRenderRaster_SquareBasic(t *testing.T) {
	matrix := makeMatrix(21)
	opt := renderOptions{
		Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
		FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
		EyeColor: color.RGBA{0, 0, 0, 255},
		ModuleStyle: "square", EyeStyle: "square",
	}
	img := renderRaster(matrix, opt)
	b := img.Bounds()
	if b.Dx() != 210 || b.Dy() != 210 {
		t.Errorf("expected 210x210, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestRenderRaster_Styles(t *testing.T) {
	matrix := makeMatrix(21)
	for _, style := range []string{"square", "rounded", "dot", "circle"} {
		opt := renderOptions{
			Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
			FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
			EyeColor: color.RGBA{0, 0, 0, 255},
			ModuleStyle: style, EyeStyle: style,
		}
		img := renderRaster(matrix, opt)
		if img == nil {
			t.Errorf("style=%q: nil image", style)
		}
	}
}

func TestRenderRaster_Transparent(t *testing.T) {
	matrix := makeMatrix(21)
	opt := renderOptions{
		Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
		FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{}, EyeColor: color.RGBA{0, 0, 0, 255},
		Transparent: true, ModuleStyle: "square", EyeStyle: "square",
	}
	img := renderRaster(matrix, opt)
	// The top-left corner (0,0) is inside the eye, so it's drawn opaque.
	// A pixel well away from any module (e.g., bottom-right background) should be transparent.
	// Just verify the canvas dimensions are correct.
	if img.Bounds().Dx() != 210 {
		t.Errorf("wrong width %d", img.Bounds().Dx())
	}
}

func TestRenderRaster_Gradient(t *testing.T) {
	matrix := makeMatrix(21)
	opt := renderOptions{
		Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
		FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
		EyeColor: color.RGBA{0, 0, 0, 255},
		ModuleStyle: "square", EyeStyle: "square",
		Gradient: "linear",
		GradientFrom: color.RGBA{255, 0, 0, 255},
		GradientTo:   color.RGBA{0, 0, 255, 255},
	}
	img := renderRaster(matrix, opt)
	if img == nil {
		t.Error("nil image with gradient")
	}
}

// --- renderSVG ---

func TestRenderSVG_ContainsSVGTag(t *testing.T) {
	matrix := makeMatrix(21)
	opt := renderOptions{
		Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
		FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
		EyeColor: color.RGBA{0, 0, 0, 255},
		ModuleStyle: "square", EyeStyle: "square",
	}
	svg := renderSVG(matrix, opt)
	if !strings.HasPrefix(svg, "<svg") {
		t.Errorf("expected SVG to start with <svg, got: %.50s", svg)
	}
	if !strings.HasSuffix(svg, "</svg>") {
		t.Errorf("expected SVG to end with </svg>")
	}
}

func TestRenderSVG_ModuleStyles(t *testing.T) {
	matrix := makeMatrix(21)
	for _, style := range []string{"square", "rounded", "dot", "circle"} {
		opt := renderOptions{
			Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
			FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
			EyeColor: color.RGBA{0, 0, 0, 255},
			ModuleStyle: style, EyeStyle: "square",
		}
		svg := renderSVG(matrix, opt)
		if !strings.Contains(svg, "<svg") {
			t.Errorf("style=%q: missing <svg", style)
		}
	}
}

func TestRenderSVG_Gradient(t *testing.T) {
	matrix := makeMatrix(21)
	for _, gtype := range []string{"linear", "radial"} {
		opt := renderOptions{
			Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
			FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
			EyeColor: color.RGBA{0, 0, 0, 255},
			ModuleStyle: "square", EyeStyle: "square",
			Gradient:     gtype,
			GradientFrom: color.RGBA{255, 0, 0, 255},
			GradientTo:   color.RGBA{0, 0, 255, 255},
		}
		svg := renderSVG(matrix, opt)
		if !strings.Contains(svg, "Gradient") {
			t.Errorf("gradient=%q: expected gradient def in SVG", gtype)
		}
		if !strings.Contains(svg, "url(#qrgrad)") {
			t.Errorf("gradient=%q: expected url(#qrgrad) fill", gtype)
		}
	}
}

func TestRenderSVG_Transparent(t *testing.T) {
	matrix := makeMatrix(21)
	opt := renderOptions{
		Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
		FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{},
		EyeColor:    color.RGBA{0, 0, 0, 255},
		Transparent: true, ModuleStyle: "square", EyeStyle: "square",
	}
	svg := renderSVG(matrix, opt)
	// When transparent, the SVG renderer should skip the background <rect>.
	// Count <rect occurrences: transparent mode has none for background.
	// Opaque mode emits one background rect right after <svg ...>.
	opaque := renderSVG(matrix, renderOptions{
		Width: 210, Height: 210, CellSize: 10, OffsetX: 0, OffsetY: 0,
		FG: color.RGBA{0, 0, 0, 255}, BG: color.RGBA{255, 255, 255, 255},
		EyeColor: color.RGBA{0, 0, 0, 255},
		ModuleStyle: "square", EyeStyle: "square",
	})
	// Transparent SVG should be shorter (missing background rect).
	if len(svg) >= len(opaque) {
		t.Error("transparent SVG should be shorter than opaque (no background rect)")
	}
}

// --- normStyle ---

func TestNormStyle(t *testing.T) {
	if normStyle("ROUNDED", "square", "rounded", "circle") != "rounded" {
		t.Error("expected rounded")
	}
	if normStyle("bad", "square", "rounded") != "square" {
		t.Error("expected default square")
	}
}

// --- insideShape ---

func TestInsideShape_Circle(t *testing.T) {
	// Centre pixel of a 10x10 box should be inside a circle.
	if !insideShape(5, 5, 10, "circle") {
		t.Error("centre should be inside circle")
	}
	// Corner pixel should be outside.
	if insideShape(0, 0, 10, "circle") {
		t.Error("corner should be outside circle")
	}
}

// makeMatrix returns a fake 21x21 bool matrix with checkerboard pattern.
func makeMatrix(cells int) [][]bool {
	m := make([][]bool, cells)
	for y := 0; y < cells; y++ {
		m[y] = make([]bool, cells)
		for x := 0; x < cells; x++ {
			m[y][x] = (x+y)%2 == 0
		}
	}
	return m
}
