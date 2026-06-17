package service

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

// renderOptions holds everything the unified renderer needs to draw a QR matrix
// to either a raster image or an SVG document. Both renderers read the same
// options so styling stays identical across output formats.
type renderOptions struct {
	Width, Height int
	CellSize      int
	OffsetX       int // top-left pixel of the module grid on the canvas
	OffsetY       int

	FG, BG, EyeColor color.RGBA
	Transparent      bool // bg has no fill (alpha 0)

	ModuleStyle string // square|rounded|dot|circle
	EyeStyle    string // square|rounded|circle

	Gradient      string // none|linear|radial
	GradientFrom  color.RGBA
	GradientTo    color.RGBA
	GradientAngle int // degrees, for linear
}

// eyeRegions returns the three 7x7 finder-pattern boxes (module coordinates) for
// a matrix with `cells` modules per side. Returned as [minX,minY,maxX,maxY) boxes.
func eyeRegions(cells int) [3][4]int {
	return [3][4]int{
		{0, 0, 7, 7},                 // top-left
		{cells - 7, 0, cells, 7},     // top-right
		{0, cells - 7, 7, cells},     // bottom-left
	}
}

func inEye(x, y int, regions [3][4]int) bool {
	for _, r := range regions {
		if x >= r[0] && x < r[2] && y >= r[1] && y < r[3] {
			return true
		}
	}
	return false
}

// lerpColor linearly interpolates between a and b at t in [0,1].
func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

// gradientAt returns the foreground color at canvas pixel (px,py).
func (o *renderOptions) gradientAt(px, py int) color.RGBA {
	if o.Gradient != "linear" && o.Gradient != "radial" {
		return o.FG
	}
	w, h := float64(o.Width), float64(o.Height)
	if o.Gradient == "radial" {
		cx, cy := w/2, h/2
		max := math.Hypot(cx, cy)
		if max == 0 {
			return o.GradientFrom
		}
		t := math.Hypot(float64(px)-cx, float64(py)-cy) / max
		return lerpColor(o.GradientFrom, o.GradientTo, t)
	}
	// linear: project pixel onto the gradient direction, normalized over the
	// canvas extent along that direction.
	rad := float64(o.GradientAngle) * math.Pi / 180
	dx, dy := math.Cos(rad), math.Sin(rad)
	// extent = projection span of the four canvas corners
	proj := func(x, y float64) float64 { return x*dx + y*dy }
	min := math.Min(math.Min(proj(0, 0), proj(w, 0)), math.Min(proj(0, h), proj(w, h)))
	max := math.Max(math.Max(proj(0, 0), proj(w, 0)), math.Max(proj(0, h), proj(w, h)))
	span := max - min
	if span == 0 {
		return o.GradientFrom
	}
	t := (proj(float64(px), float64(py)) - min) / span
	return lerpColor(o.GradientFrom, o.GradientTo, t)
}

// insideShape reports whether the local pixel (lx,ly) within a box of side
// `size` falls inside the given module shape.
func insideShape(lx, ly, size int, style string) bool {
	fsize := float64(size)
	cx, cy := fsize/2-0.5, fsize/2-0.5
	flx, fly := float64(lx), float64(ly)
	switch style {
	case "dot":
		r := fsize * 0.42
		return (flx-cx)*(flx-cx)+(fly-cy)*(fly-cy) <= r*r
	case "circle":
		r := fsize * 0.5
		return (flx-cx)*(flx-cx)+(fly-cy)*(fly-cy) <= r*r
	case "rounded":
		r := fsize * 0.3
		// corners: distance to the nearest corner-arc center
		minX, maxX := r, fsize-1-r
		minY, maxY := r, fsize-1-r
		ccx, ccy := flx, fly
		if flx < minX {
			ccx = minX
		} else if flx > maxX {
			ccx = maxX
		}
		if fly < minY {
			ccy = minY
		} else if fly > maxY {
			ccy = maxY
		}
		if ccx == flx && ccy == fly {
			return true // straight edge / interior
		}
		return (flx-ccx)*(flx-ccx)+(fly-ccy)*(fly-ccy) <= r*r
	default: // square
		return true
	}
}

// fillShape sets every pixel of `style` shape within the box at (x0,y0,size) to
// col. It clips to the image bounds. Used for both drawing (opaque col) and
// punching holes (transparent/bg col), so it always overwrites.
func fillShape(img *image.RGBA, x0, y0, size int, col color.RGBA, style string) {
	b := img.Bounds()
	for ly := 0; ly < size; ly++ {
		py := y0 + ly
		if py < b.Min.Y || py >= b.Max.Y {
			continue
		}
		for lx := 0; lx < size; lx++ {
			px := x0 + lx
			if px < b.Min.X || px >= b.Max.X {
				continue
			}
			if insideShape(lx, ly, size, style) {
				img.SetRGBA(px, py, col)
			}
		}
	}
}

// renderRaster draws the QR matrix onto a fresh RGBA canvas using the styling
// options. Logo overlay is applied separately by the caller.
func renderRaster(matrix [][]bool, o renderOptions) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, o.Width, o.Height))
	// Fill background (transparent leaves alpha 0).
	if !o.Transparent {
		for y := 0; y < o.Height; y++ {
			for x := 0; x < o.Width; x++ {
				canvas.SetRGBA(x, y, o.BG)
			}
		}
	}

	cells := len(matrix)
	if cells == 0 {
		return canvas
	}
	regions := eyeRegions(cells)
	cs := o.CellSize

	// Body modules (skip eye regions; eyes drawn as cohesive shapes below).
	for y := 0; y < cells; y++ {
		for x := 0; x < len(matrix[y]); x++ {
			if !matrix[y][x] || inEye(x, y, regions) {
				continue
			}
			px := o.OffsetX + x*cs
			py := o.OffsetY + y*cs
			col := o.FG
			if o.Gradient == "linear" || o.Gradient == "radial" {
				col = o.gradientAt(px+cs/2, py+cs/2)
			}
			fillShape(canvas, px, py, cs, col, o.ModuleStyle)
		}
	}

	// Eyes: outer ring + inner dot, drawn cohesively.
	for _, r := range regions {
		ex := o.OffsetX + r[0]*cs
		ey := o.OffsetY + r[1]*cs
		drawEyeRaster(canvas, ex, ey, cs, o)
	}
	return canvas
}

// drawEyeRaster renders one 7x7 finder pattern at pixel (ex,ey): a 7x7 outer
// shape, a 5x5 punch back to background, and a 3x3 inner shape.
func drawEyeRaster(img *image.RGBA, ex, ey, cs int, o renderOptions) {
	punch := o.BG
	if o.Transparent {
		punch = color.RGBA{} // alpha 0
	}
	// outer 7x7
	fillShape(img, ex, ey, 7*cs, o.EyeColor, o.EyeStyle)
	// punch 5x5
	fillShape(img, ex+cs, ey+cs, 5*cs, punch, o.EyeStyle)
	// inner 3x3
	fillShape(img, ex+2*cs, ey+2*cs, 3*cs, o.EyeColor, o.EyeStyle)
}

// --- SVG ---

func rgb(c color.RGBA) string { return fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B) }

// renderSVG builds an SVG document for the matrix with the same styling options.
func renderSVG(matrix [][]bool, o renderOptions) string {
	cells := len(matrix)
	cs := o.CellSize

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`,
		o.Width, o.Height, o.Width, o.Height)

	// Gradient def + the fill reference used for module/eye color.
	fgFill := rgb(o.FG)
	if o.Gradient == "linear" || o.Gradient == "radial" {
		sb.WriteString(svgGradientDef(o))
		fgFill = "url(#qrgrad)"
	}
	eyeFill := rgb(o.EyeColor)

	// Background
	if !o.Transparent {
		fmt.Fprintf(&sb, `<rect width="%d" height="%d" fill="%s"/>`, o.Width, o.Height, rgb(o.BG))
	}
	if cells == 0 {
		sb.WriteString(`</svg>`)
		return sb.String()
	}
	regions := eyeRegions(cells)

	// Body modules
	for y := 0; y < cells; y++ {
		for x := 0; x < len(matrix[y]); x++ {
			if !matrix[y][x] || inEye(x, y, regions) {
				continue
			}
			px := o.OffsetX + x*cs
			py := o.OffsetY + y*cs
			sb.WriteString(svgModule(px, py, cs, fgFill, o.ModuleStyle))
		}
	}

	// Eyes
	for _, r := range regions {
		ex := o.OffsetX + r[0]*cs
		ey := o.OffsetY + r[1]*cs
		sb.WriteString(svgEye(ex, ey, cs, eyeFill, o.EyeStyle))
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}

func svgGradientDef(o renderOptions) string {
	from, to := rgb(o.GradientFrom), rgb(o.GradientTo)
	if o.Gradient == "radial" {
		return fmt.Sprintf(`<defs><radialGradient id="qrgrad"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></radialGradient></defs>`, from, to)
	}
	// linear: convert angle to x1,y1,x2,y2 on the unit square
	rad := float64(o.GradientAngle) * math.Pi / 180
	dx, dy := math.Cos(rad), math.Sin(rad)
	x1, y1 := 0.5-dx/2, 0.5-dy/2
	x2, y2 := 0.5+dx/2, 0.5+dy/2
	return fmt.Sprintf(`<defs><linearGradient id="qrgrad" x1="%.4f" y1="%.4f" x2="%.4f" y2="%.4f"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></linearGradient></defs>`,
		x1, y1, x2, y2, from, to)
}

func svgModule(x, y, cs int, fill, style string) string {
	switch style {
	case "circle":
		return fmt.Sprintf(`<circle cx="%g" cy="%g" r="%g" fill="%s"/>`,
			float64(x)+float64(cs)/2, float64(y)+float64(cs)/2, float64(cs)/2, fill)
	case "dot":
		return fmt.Sprintf(`<circle cx="%g" cy="%g" r="%g" fill="%s"/>`,
			float64(x)+float64(cs)/2, float64(y)+float64(cs)/2, float64(cs)*0.42, fill)
	case "rounded":
		return fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="%g" fill="%s"/>`,
			x, y, cs, cs, float64(cs)*0.3, fill)
	default:
		return fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`, x, y, cs, cs, fill)
	}
}

// svgEye draws a 7x7 finder pattern as an outer ring (stroke) + inner block,
// avoiding background "punching" so it works with transparent backgrounds.
func svgEye(ex, ey, cs int, fill, style string) string {
	var sb strings.Builder
	fcs := float64(cs)
	if style == "circle" {
		// outer ring: stroked circle, centerline radius 3 modules
		cx, cy := float64(ex)+3.5*fcs, float64(ey)+3.5*fcs
		fmt.Fprintf(&sb, `<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="%g"/>`,
			cx, cy, 3*fcs, fill, fcs)
		// inner 3x3 disc, radius 1.5 modules
		fmt.Fprintf(&sb, `<circle cx="%g" cy="%g" r="%g" fill="%s"/>`, cx, cy, 1.5*fcs, fill)
		return sb.String()
	}
	rx := 0.0
	if style == "rounded" {
		rx = fcs * 1.2
	}
	// outer ring: stroked rounded rect, inset half a module, span 6 modules
	fmt.Fprintf(&sb, `<rect x="%g" y="%g" width="%g" height="%g" rx="%g" fill="none" stroke="%s" stroke-width="%g"/>`,
		float64(ex)+fcs/2, float64(ey)+fcs/2, 6*fcs, 6*fcs, rx, fill, fcs)
	// inner 3x3 block
	innerRx := 0.0
	if style == "rounded" {
		innerRx = fcs * 0.6
	}
	fmt.Fprintf(&sb, `<rect x="%g" y="%g" width="%g" height="%g" rx="%g" fill="%s"/>`,
		float64(ex)+2*fcs, float64(ey)+2*fcs, 3*fcs, 3*fcs, innerRx, fill)
	return sb.String()
}
