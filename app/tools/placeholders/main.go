// 커맨드 placeholders — UI 스프라이트 플레이스홀더 PNG 생성기.
//
// 룩은 확정됐지만 실제 스프라이트는 리드가 fal.ai로 뽑아 같은 파일명으로 교체한다.
// 이 생성기는 회색 단색 도형만 만든다(룩 흉내 금지 — CLAUDE.md "UI 스프라이트").
// 기존 파일은 덮지 않는다(-force로만). -still은 panel.png를 540×960으로 축소한
// 정지 첫 화면(app/web/still.png)을 별도로 쓴다 — 파생 산출물이라 매번 갱신.
//
// 순수 Go(image/png, image/draw)만 사용. 실행: go run ./app/tools/placeholders
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// 회색 플레이스홀더 팔레트 — 새 룩·새 색 발명 금지 계약의 하한 구현.
var (
	grayPanel   = color.RGBA{115, 115, 115, 255}
	grayKnob    = color.RGBA{72, 72, 72, 255}
	grayKnobRim = color.RGBA{96, 96, 96, 255}
	grayPointer = color.RGBA{208, 208, 208, 255}
	grayPad     = color.RGBA{88, 88, 88, 255}
	grayPadLit  = color.RGBA{196, 196, 196, 255}
	grayStep    = color.RGBA{76, 76, 76, 255}
	grayStepLit = color.RGBA{206, 206, 206, 255}
	grayFrame   = color.RGBA{36, 36, 36, 255}
	grayGlass   = color.RGBA{36, 36, 36, 150} // 스코프 안쪽 반투명(파형은 UI가 vector로 그림)
)

type asset struct {
	name string
	w, h int
	draw func(m *image.RGBA)
}

func assets() []asset {
	return []asset{
		{"panel.png", 1080, 1920, func(m *image.RGBA) { fill(m, image.Rect(0, 0, 1080, 1920), grayPanel) }},
		{"knob.png", 256, 256, drawKnob},
		{"pad.png", 192, 192, func(m *image.RGBA) { drawPad(m, grayPad) }},
		{"pad-lit.png", 192, 192, func(m *image.RGBA) { drawPad(m, grayPadLit) }},
		{"step.png", 96, 96, func(m *image.RGBA) { drawStep(m, grayStep) }},
		{"step-lit.png", 96, 96, func(m *image.RGBA) { drawStep(m, grayStepLit) }},
		{"scope-frame.png", 512, 192, drawScopeFrame},
	}
}

func main() {
	out := flag.String("out", "app/assets", "플레이스홀더 출력 디렉터리")
	still := flag.String("still", "", "panel.png 축소본(540×960)을 쓸 경로. 빈 문자열이면 생략")
	force := flag.Bool("force", false, "기존 파일을 덮어쓴다")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	for _, a := range assets() {
		p := filepath.Join(*out, a.name)
		if _, err := os.Stat(p); err == nil && !*force {
			fmt.Printf("skip   %s (있음, -force로만 덮음)\n", p)
			continue
		}
		m := image.NewRGBA(image.Rect(0, 0, a.w, a.h))
		a.draw(m)
		writePNG(p, notOpaque{m})
	}

	if *still != "" {
		panel := readPNG(filepath.Join(*out, "panel.png"))
		writePNG(*still, downscale2x(panel, 540, 960))
	}
}

// notOpaque — png.Encode는 불투명 이미지를 RGB(컬러 타입 2)로 줄인다.
// 자산 계약은 1080×1920 RGBA라서 Opaque()를 false로 보고해 RGBA(컬러 타입 6)로 쓴다.
type notOpaque struct{ image.Image }

func (notOpaque) Opaque() bool { return false }

// drawKnob — 정면 로터리 노브: 원 + 테두리 링 + 12시 방향 포인터.
// 실제 회전은 UI가 이 이미지를 -135°..+135°로 돌린다(피벗은 중앙).
func drawKnob(m *image.RGBA) {
	cx, cy := 128.0, 128.0
	fillCircle(m, cx, cy, 124, grayKnob)
	ring(m, cx, cy, 118, 8, grayKnobRim)
	// 포인터: 중앙에서 위로(12시)
	fill(m, image.Rect(int(cx)-7, 18, int(cx)+7, int(cy)-30), grayPointer)
}

func drawPad(m *image.RGBA, c color.RGBA) {
	roundRect(m, 14, 14, 178, 178, 28, c)
}

func drawStep(m *image.RGBA, c color.RGBA) {
	roundRect(m, 10, 10, 86, 86, 16, c)
}

func drawScopeFrame(m *image.RGBA) {
	fill(m, image.Rect(0, 0, 512, 192), grayFrame)
	fill(m, image.Rect(10, 10, 502, 182), grayGlass)
}

// --- 도형 헬퍼 (정수·비교만, 안티에일리어싱 없음) ---

func fill(m *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(m, r, image.NewUniform(c), image.Point{}, draw.Src)
}

func fillCircle(m *image.RGBA, cx, cy, r float64, c color.RGBA) {
	r2 := r * r
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r2 {
				m.SetRGBA(x, y, c)
			}
		}
	}
}

func ring(m *image.RGBA, cx, cy, r, w float64, c color.RGBA) {
	ro, ri := r+w/2, r-w/2
	ro2, ri2 := ro*ro, ri*ri
	for y := int(cy - ro); y <= int(cy+ro); y++ {
		for x := int(cx - ro); x <= int(cx+ro); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if d2 := dx*dx + dy*dy; d2 <= ro2 && d2 >= ri2 {
				m.SetRGBA(x, y, c)
			}
		}
	}
}

func roundRect(m *image.RGBA, x0, y0, x1, y1, r int, c color.RGBA) {
	fill(m, image.Rect(x0+r, y0, x1-r, y1), c)
	fill(m, image.Rect(x0, y0+r, x0+r, y1-r), c)
	fill(m, image.Rect(x1-r, y0+r, x1, y1-r), c)
	fillCircleI(m, x0+r, y0+r, r, c)
	fillCircleI(m, x1-r, y0+r, r, c)
	fillCircleI(m, x0+r, y1-r, r, c)
	fillCircleI(m, x1-r, y1-r, r, c)
}

func fillCircleI(m *image.RGBA, cx, cy, r int, c color.RGBA) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy <= r2 {
				m.SetRGBA(x, y, c)
			}
		}
	}
}

// downscale2x — 2×2 박스 평균으로 축소한다(1080×1920 → 540×960 전제).
func downscale2x(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	if b.Dx() != w*2 || b.Dy() != h*2 {
		fmt.Fprintf(os.Stderr, "warn: panel 크기 %dx%d — 2× 축소 전제 위반\n", b.Dx(), b.Dy())
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, a uint32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					pr, pg, pb, pa := src.At(b.Min.X+x*2+dx, b.Min.Y+y*2+dy).RGBA()
					r += pr >> 8
					g += pg >> 8
					bl += pb >> 8
					a += pa >> 8
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / 4), uint8(g / 4), uint8(bl / 4), uint8(a / 4)})
		}
	}
	return dst
}

func readPNG(p string) image.Image {
	b, err := os.ReadFile(p)
	if err != nil {
		fail(err)
	}
	m, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		fail(err)
	}
	return m
}

func writePNG(p string, m image.Image) {
	f, err := os.Create(p)
	if err != nil {
		fail(err)
	}
	if err := png.Encode(f, m); err != nil {
		f.Close()
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
	fmt.Printf("create %s (%dx%d)\n", p, m.Bounds().Dx(), m.Bounds().Dy())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "placeholders:", err)
	os.Exit(1)
}
