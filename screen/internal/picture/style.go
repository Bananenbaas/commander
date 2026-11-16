package picture

import (
	_ "embed"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/BarlowCondensed-SemiBold.ttf
var fontData []byte

//go:embed fonts/OFL.txt
var fontLicence string

// FontLicence is the licence text that travels with the embedded font.
func FontLicence() string { return fontLicence }

// ErrTitleTooLong is returned when a title does not fit at the smallest size.
var ErrTitleTooLong = errors.New("picture: title does not fit on the button")

// Style describes a built-in button picture.
type Style struct {
	Color  color.NRGBA // frame, accents and symbol colour
	Symbol image.Image // optional, white on transparent; tinted with Color
	Title  string
}

const (
	ss        = 4 // supersampling factor
	maxTitle  = 23
	minTitle  = 17
	titleMaxW = 136
	bandTop   = 114
	bandBot   = 146
	outlineR  = 2
)

var (
	fontOnce sync.Once
	fontFace *opentype.Font
	fontErr  error
)

func loadFont() (*opentype.Font, error) {
	fontOnce.Do(func() { fontFace, fontErr = opentype.Parse(fontData) })
	return fontFace, fontErr
}

func newFace(f *opentype.Font, px int) (font.Face, error) {
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size: float64(px * ss), DPI: 72, Hinting: font.HintingNone,
	})
}

// Render draws the built-in button style at 4x and box-filters it down.
func Render(s Style) (*image.NRGBA, error) {
	big := image.NewRGBA(image.Rect(0, 0, W*ss, H*ss))
	const k = 130 + 111 - 20
	inset := 4 * math.Sqrt2

	// Shapes are tested at the centre of each supersampled pixel.
	inFrame := func(x, y float64) bool {
		return x >= 12 && x <= 130 && y >= 3 && y <= 111 && x+y <= k
	}
	inInner := func(x, y float64) bool {
		return x >= 16 && x <= 126 && y >= 7 && y <= 107 && x+y <= k-inset
	}
	inAccent := func(x, y float64) bool {
		if x > 130 || y > 111 {
			return false
		}
		d := x + y - k
		return (d >= 5 && d <= 9) || (d >= 13 && d <= 17)
	}

	black82 := color.RGBA{0, 0, 0, uint8(math.Round(0.82 * 255))}
	col := premul(s.Color)
	for py := 0; py < H*ss; py++ {
		y := (float64(py) + 0.5) / ss
		for px := 0; px < W*ss; px++ {
			x := (float64(px) + 0.5) / ss
			switch {
			case inInner(x, y):
				big.SetRGBA(px, py, black82)
			case inFrame(x, y), inAccent(x, y):
				big.SetRGBA(px, py, col)
			}
		}
	}

	if s.Symbol != nil {
		box := image.Rect(24*ss, 15*ss, 118*ss, 99*ss)
		sym := fitInto(s.Symbol, box.Dx(), box.Dy())
		tint := image.NewRGBA(sym.Bounds())
		for i := 0; i < len(sym.Pix); i += 4 {
			a := uint32(sym.Pix[i+3]) * uint32(s.Color.A) / 255
			tint.Pix[i] = uint8(uint32(s.Color.R) * a / 255)
			tint.Pix[i+1] = uint8(uint32(s.Color.G) * a / 255)
			tint.Pix[i+2] = uint8(uint32(s.Color.B) * a / 255)
			tint.Pix[i+3] = uint8(a)
		}
		draw.Draw(big, box, tint, image.Point{}, draw.Over)
	}

	if s.Title != "" {
		if err := drawTitle(big, s.Title); err != nil {
			return nil, err
		}
	}
	return downsample(big), nil
}

func premul(c color.NRGBA) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func drawTitle(dst *image.RGBA, title string) error {
	f, err := loadFont()
	if err != nil {
		return err
	}
	var face font.Face
	for px := maxTitle; px >= minTitle; px-- {
		fc, err := newFace(f, px)
		if err != nil {
			return err
		}
		w := font.MeasureString(fc, title)
		if w <= fixed.I(titleMaxW*ss) {
			face = fc
			break
		}
		fc.Close()
	}
	if face == nil {
		return ErrTitleTooLong
	}
	defer face.Close()

	w := font.MeasureString(face, title)
	capH := face.Metrics().CapHeight
	centreY := fixed.I((bandTop + bandBot) * ss / 2)
	x := (fixed.I(W*ss) - w) / 2
	y := centreY + capH/2

	mask := image.NewAlpha(dst.Bounds())
	d := &font.Drawer{Dst: mask, Src: image.Opaque, Face: face, Dot: fixed.Point26_6{X: x, Y: y}}
	d.DrawString(title)

	// Outline: dilate the mask by a disc of radius 2 px at 4x.
	r := outlineR * ss
	var offs []image.Point
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				offs = append(offs, image.Pt(dx, dy))
			}
		}
	}
	out := image.NewAlpha(dst.Bounds())
	b := dst.Bounds()
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			a := mask.AlphaAt(px, py).A
			if a == 0 {
				continue
			}
			for _, o := range offs {
				qx, qy := px+o.X, py+o.Y
				if qx < b.Min.X || qx >= b.Max.X || qy < b.Min.Y || qy >= b.Max.Y {
					continue
				}
				if out.AlphaAt(qx, qy).A < a {
					out.SetAlpha(qx, qy, color.Alpha{a})
				}
			}
		}
	}
	// 90% black through the dilated mask, then the white text through its own.
	draw.DrawMask(dst, dst.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{},
		scaleAlpha(out, 0.9), image.Point{}, draw.Over)
	draw.DrawMask(dst, dst.Bounds(), image.White, image.Point{}, mask, image.Point{}, draw.Over)
	return nil
}

func scaleAlpha(a *image.Alpha, f float64) *image.Alpha {
	r := image.NewAlpha(a.Bounds())
	for i, v := range a.Pix {
		r.Pix[i] = uint8(math.Round(float64(v) * f))
	}
	return r
}

// downsample box-filters a 4x premultiplied image to 1x straight alpha.
func downsample(big *image.RGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, W, H))
	const n = ss * ss
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			var r, g, b, a uint32
			for j := 0; j < ss; j++ {
				row := big.Pix[(y*ss+j)*big.Stride+x*ss*4:]
				for i := 0; i < ss; i++ {
					r += uint32(row[i*4])
					g += uint32(row[i*4+1])
					b += uint32(row[i*4+2])
					a += uint32(row[i*4+3])
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			if a == 0 {
				continue
			}
			o[0] = uint8((r*255 + a/2) / a)
			o[1] = uint8((g*255 + a/2) / a)
			o[2] = uint8((b*255 + a/2) / a)
			o[3] = uint8((a + n/2) / n)
		}
	}
	return out
}
