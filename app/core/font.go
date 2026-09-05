// font.go — 라벨 폰트(글리프 아틀라스). diffusion은 글자를 망치므로 라벨·숫자는 앱이 폰트로 올린다.
//
// text/v2 + go-text 셰이퍼 + 비트맵 폰트는 앱 wasm gzip을 +2.5MB 키워(실측 2026-09-05) 예산 3.0MB를 깬다.
// 대신 tools/font/atlas.py가 만든 PNG 아틀라스(Go Bold 22px, ASCII 32..126, BSD-3)를 DrawImage 서브이미지로
// 그린다. ASCII 밖 문자는 '?'로 대체 — 한글 시드 단어는 DOM 오버레이가 그린다(기획 결정).
package core

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	_ "image/png"

	"github.com/hajimehoshi/ebiten/v2"
)

type Align uint8

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

type glyph struct {
	X, Y, W, H int
	Adv        float64
	OX, OY     int
	sub        *ebiten.Image
}

// FontSet — 아틀라스 하나. Draw는 프레임당 힙 할당 0(옵션 재사용, 룬 순회).
type FontSet struct {
	px, lineH float64
	glyphs    [128]glyph
	op        ebiten.DrawImageOptions
	ok        bool
}

// NewFontSet — 아틀라스 PNG/JSON에서 만든다. 실패하면 그리기가 무동작인 빈 세트(패닉 없음).
func NewFontSet(pngData, jsonData []byte) *FontSet {
	f := &FontSet{}
	var meta struct {
		Px         int `json:"px"`
		LineHeight int `json:"lineHeight"`
		Glyphs     map[string]struct {
			X   int     `json:"x"`
			Y   int     `json:"y"`
			W   int     `json:"w"`
			H   int     `json:"h"`
			Adv float64 `json:"adv"`
			OX  int     `json:"ox"`
			OY  int     `json:"oy"`
		} `json:"glyphs"`
	}
	if err := json.Unmarshal(jsonData, &meta); err != nil {
		return f
	}
	img, _, err := image.Decode(bytes.NewReader(pngData))
	if err != nil {
		return f
	}
	atlas := ebiten.NewImageFromImage(img)
	f.px = float64(meta.Px)
	f.lineH = float64(meta.LineHeight)
	for s, g := range meta.Glyphs {
		if len(s) != 1 {
			continue
		}
		c := s[0]
		f.glyphs[c] = glyph{X: g.X, Y: g.Y, W: g.W, H: g.H, Adv: g.Adv, OX: g.OX, OY: g.OY,
			sub: atlas.SubImage(image.Rect(g.X, g.Y, g.X+g.W, g.Y+g.H)).(*ebiten.Image)}
	}
	f.ok = true
	return f
}

// Measure — scale 배율에서의 (w, h). h는 줄 높이.
func (f *FontSet) Measure(s string, scale float64) (float64, float64) {
	w := 0.0
	for i := 0; i < len(s); i++ {
		w += f.glyphs[f.idx(s[i])].Adv
	}
	return w * scale, f.lineH * scale
}

func (f *FontSet) idx(c byte) byte {
	if c < 32 || c > 126 {
		return '?'
	}
	return c
}

// Draw — s를 (x,y)에 그린다. AlignLeft/Right: (x,y)는 글줄 상단. AlignCenter: (x,y)는 글줄 중심.
// 비ASCII 바이트는 '?'. 프레임당 할당 0.
func (f *FontSet) Draw(dst *ebiten.Image, s string, x, y, scale float64, c color.Color, a Align) {
	if !f.ok {
		return
	}
	w, h := f.Measure(s, scale)
	switch a {
	case AlignCenter:
		x -= w / 2
		y -= h / 2
	case AlignRight:
		x -= w
	}
	f.op.ColorScale.Reset()
	f.op.ColorScale.ScaleWithColor(c)
	for i := 0; i < len(s); i++ {
		g := &f.glyphs[f.idx(s[i])]
		if g.sub != nil {
			f.op.GeoM.Reset()
			f.op.GeoM.Scale(scale, scale)
			f.op.GeoM.Translate(x+float64(g.OX)*scale, y+float64(g.OY)*scale)
			dst.DrawImage(g.sub, &f.op)
		}
		x += g.Adv * scale
	}
}
