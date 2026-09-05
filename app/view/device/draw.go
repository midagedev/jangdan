// draw.go — 그리기 전부. Draw는 Cmd를 보내지 않는다.
//
// 프레임당 드로잉 예산: DrawImage ≤ 120회(패널 1 + 라벨 레이어 1 + 노브 29 + LED 36 +
// 표시창 3 + 오버레이 ≤ 14), vector 호출 1회(스코프 폴리라인). 정적 라벨은 첫 프레임에
// 720×1280 오프스크린 한 장으로 합성해 매 프레임 1회 blit한다. 옵션·버퍼는 전부 재사용.
package device

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/midagedev/revirth/app/core"
	"github.com/midagedev/revirth/engine"
)

// 스코프 스트로크 수치(스펙).
const (
	scopeStrokeWidth = 1.5
	scopeAmpRatio    = 0.42 // 진폭 = rect 높이 × 0.42(자동 이득 후 창의 peak가 채우는 목표)
	scopeMaxGain     = 8    // 자동 이득 상한(및 peak < 0.05 창의 고정 이득)
	scopeGainMinPeak = 0.05 // 이 이하는 노이즈 바닥으로 보고 ×8
)

// LED 상태 인덱스(ledImg).
const (
	ledOn = iota
	ledMid
	ledOff
)

// Draw — 패널 → 라벨 → LED → 노브 → 오버레이 → 표시창 → 스코프.
func (v *View) Draw(screen *ebiten.Image, ctx *core.Ctx) {
	v.ensureLayers(ctx)
	v.op.GeoM.Reset()
	v.op.ColorScale.Reset()
	screen.DrawImage(v.panel, &v.op)
	screen.DrawImage(v.labelLayer, &v.op)
	v.drawLEDs(screen, ctx)
	v.drawKnobs(screen, ctx)
	v.drawOverlays(screen, ctx)
	v.drawDisplays(screen, ctx)
	v.drawScope(screen, ctx)
}

// ensureLayers — 첫 Draw에서 정적 라벨 레이어를 합성한다(폰트가 ctx로 오므로 여기서).
func (v *View) ensureLayers(ctx *core.Ctx) {
	if v.layersOK {
		return
	}
	v.layersOK = true
	v.labelLayer = ebiten.NewImage(int(v.layout.Size[0]), int(v.layout.Size[1]))
	f := ctx.Font
	if f == nil {
		return
	}
	for i := range v.knobs {
		k := &v.knobs[i]
		dy := float64(knobDyMain)
		if k.sec == secDrums {
			dy = knobDyDrums
		}
		// 어두운 잉크: 라벨판이 밝은 크림이라 크림 라벨은 안 보였다(비전 처방).
		f.Draw(v.labelLayer, k.label, k.cx, k.cy+k.r+dy, labelKnobScale, colInk, core.AlignCenter)
	}
	for i := range v.buttons {
		b := &v.buttons[i]
		cx, cy := b.rect.Center()
		col := colLabel
		if b.kind == bkStep {
			col = colInk // 스텝 숫자는 밝은 알약 위 — 어두운 잉크
		}
		sc := labelScale(f, b.label, labelBtnScale, b.rect[2]-labelFitPad)
		f.Draw(v.labelLayer, b.label, cx, cy, sc, col, core.AlignCenter)
	}
	for i := range v.pads {
		p := &v.pads[i]
		cx, cy := p.rect.Center()
		f.Draw(v.labelLayer, p.name, cx, cy, labelPadScale, colLabel, core.AlignCenter)
	}
	if v.hasTitle {
		cx, cy := v.titlePlate.Center()
		// 어두운 잉크 + 1.6배 + x+40(좌상단 DOM 시드 입력 회피) — 비전 처방.
		f.Draw(v.labelLayer, "JANGDAN", cx+titleShiftX, cy, labelTitleScale, colInk, core.AlignCenter)
	}
	// 섹션 이름판: 왼쪽 정렬(+6px), 세로 중앙. 폰트가 ASCII라 구분자는 asciiSep로 대체.
	// 크림판 위 크림 라벨은 안 보였다 — 어두운 잉크(비전 처방).
	for i, txt := range [2]string{"DRUMS", "FX" + asciiSep + "SEQ"} {
		if !v.hasSection[i] {
			continue
		}
		r := v.sectionPlates[i]
		_, lh := f.Measure(txt, labelSectionScale)
		f.Draw(v.labelLayer, txt, r[0]+plateInset, r[1]+r[3]/2-lh/2, labelSectionScale, colInk, core.AlignLeft)
	}
	for s, txt := range [2]string{"BASSLINE A", "BASSLINE B"} {
		if !v.hasBassPlate[s] {
			continue
		}
		r := v.bassPlates[s]
		_, lh := f.Measure(txt, labelSectionScale)
		f.Draw(v.labelLayer, txt, r[0]+plateInset, r[1]+r[3]/2-lh/2, labelSectionScale, colInk, core.AlignLeft)
	}
}

// shrinkScale — 측정 폭 w가 예산 maxW를 넘으면 base에서 비례 축소(하한 labelFloor).
// 순수 함수 — 단언은 이 함수에 걸고 폰트 실측은 labelScale이 잇는다.
func shrinkScale(base, w, maxW float64) float64 {
	if w <= maxW || w <= 0 {
		return base
	}
	s := base * maxW / w
	if s < labelFloor {
		return labelFloor
	}
	return s
}

// labelScale — 라벨 문자열의 폰트 실측 폭으로 shrinkScale을 적용한 스케일.
func labelScale(f *core.FontSet, s string, base, maxW float64) float64 {
	w, _ := f.Measure(s, base)
	return shrinkScale(base, w, maxW)
}

// drawLEDs — 베이스라인 섹션 10개(파형·모드·옥타브·슬롯) + fx 16개(현재 스텝=밝음,
// 게이트 켜진 스텝=중간, 나머지=꺼짐).
func (v *View) drawLEDs(screen *ebiten.Image, ctx *core.Ctx) {
	for s := 0; s < 2; s++ {
		base := bassParam(uint8(s))
		wave := ctx.Bridge.Param(base + engine.BWave)
		oct := ctx.Bridge.Param(base + engine.BOct)
		slot := ctx.Bridge.Slot(engine.Part(s))
		for j := 0; j < numBassSecBtns; j++ {
			on := false
			switch j {
			case 0: // saw
				on = wave < 0.5
			case 1: // sqr
				on = wave >= 0.5
			case 2: // slide 편집 모드
				on = v.mode == emSlide
			case 3: // acc 편집 모드
				on = v.mode == emAcc
			case 4: // oct-
				on = oct < 1.0/3
			case 5: // oct+
				on = oct >= 2.0/3
			default: // patA..D
				on = int(slot) == j-6
			}
			if id := v.secLEDs[s][j]; id >= 0 {
				v.drawLEDMid(screen, v.leds[id], on, false)
			}
		}
	}
	cur := -1
	if ctx.Tick.Started {
		cur = ctx.Tick.Step
	}
	for i := 0; i < engine.Steps; i++ {
		if id := v.fxLEDs[i]; id >= 0 {
			v.drawLEDMid(screen, v.leds[id], i == cur, v.stepGate(ctx, i))
		}
	}
}

// drawLEDMid — on=켜짐, mid=중간, 그 외 꺼짐. 스프라이트는 ledR 기준으로 축소.
func (v *View) drawLEDMid(screen *ebiten.Image, led core.LED, on, mid bool) {
	img := v.ledImg[ledOff]
	if mid {
		img = v.ledImg[ledMid]
	}
	if on {
		img = v.ledImg[ledOn]
	}
	s := led.R / v.ledR
	w := float64(img.Bounds().Dx()) * s
	v.op.GeoM.Reset()
	v.op.GeoM.Scale(s, s)
	v.op.GeoM.Translate(led.CX-w/2, led.CY-w/2)
	v.op.ColorScale.Reset()
	screen.DrawImage(img, &v.op)
}

// drawKnobs — 반지름에 가장 가까운 스프라이트 클래스를 표시 지름 2r로 그리고
// −135°..+135° = 0..1 회전. 잡힌 노브는 밝기 ×1.12.
func (v *View) drawKnobs(screen *ebiten.Image, ctx *core.Ctx) {
	for i := range v.knobs {
		k := &v.knobs[i]
		img := v.spriteFor(k.r)
		if img == nil {
			continue
		}
		size := float64(img.Bounds().Dx())
		s := (2 * k.r) / size
		ang := (-135 + 270*float64(v.knobValue(ctx, k))) * math.Pi / 180
		v.op.GeoM.Reset()
		v.op.GeoM.Translate(-size/2, -size/2)
		v.op.GeoM.Scale(s, s)
		v.op.GeoM.Rotate(ang)
		v.op.GeoM.Translate(k.cx, k.cy)
		v.op.ColorScale.Reset()
		if k.held {
			v.op.ColorScale.Scale(knobLitBoost, knobLitBoost, knobLitBoost, 1)
		}
		screen.DrawImage(img, &v.op)
	}
}

// spriteFor — 반지름 클래스(25/32/42) 중 가장 가까운 것.
func (v *View) spriteFor(r float64) *ebiten.Image {
	best := 0
	bd := math.MaxFloat64
	for i, c := range v.spriteCls {
		if d := math.Abs(c - r); d < bd {
			best, bd = i, d
		}
	}
	if len(v.spriteImg) == 0 {
		return nil
	}
	return v.spriteImg[best]
}

// drawOverlays — 패드 lit(120ms)·뮤트 dim(ColorScale 0.55 상당), Build 중 DROP 펄스,
// MANUAL 잠금 중 RESUME lit. 반투명 사각 1×1 텍스처 확대(옵션 재사용, 할당 0).
func (v *View) drawOverlays(screen *ebiten.Image, ctx *core.Ctx) {
	for i := range v.pads {
		p := &v.pads[i]
		if ctx.Bridge.Muted(p.part) {
			v.overlayRect(screen, p.rect, v.black1, 1-padMuteScale)
		}
		if ctx.Now < p.litUntil {
			v.overlayRect(screen, p.rect, v.white1, overlayLitA)
		}
	}
	if v.fxPlay >= 0 && ctx.Phase == 1 {
		a := float32(overlayLitA + dropPulseAmp*(0.5+0.5*math.Sin(2*math.Pi*dropPulseHz*ctx.Now)))
		v.overlayRect(screen, v.buttons[v.fxPlay].rect, v.white1, a)
	}
	if v.fxRec >= 0 && ctx.ManualLocked {
		v.overlayRect(screen, v.buttons[v.fxRec].rect, v.white1, overlayLitA)
	}
}

func (v *View) overlayRect(screen *ebiten.Image, r core.Rect, tex *ebiten.Image, alpha float32) {
	if tex == nil {
		return
	}
	v.op.GeoM.Reset()
	v.op.GeoM.Scale(r[2], r[3])
	v.op.GeoM.Translate(r[0], r[1])
	v.op.ColorScale.Reset()
	// ColorScale은 프리멀티플라이드 색에 곱해진다(ebiten 문서: ColorM.Scale(r,g,b,a) ≡
	// ColorScale.Scale(r*a,g*a,b*a,a)). 불투명 원본에서 rgb을 1로 두면 source-over의
	// src.rgb + dst×(1−a)가 상한 클램프돼 알파 1 순백 사각이 된다 — rgb도 알파만큼.
	v.op.ColorScale.Scale(alpha, alpha, alpha, alpha)
	screen.DrawImage(tex, &v.op)
}

// drawDisplays — 표시창 3개. 문자열이 변화했을 때만 오프스크린에 다시 찍는다.
func (v *View) drawDisplays(screen *ebiten.Image, ctx *core.Ctx) {
	if ctx.Font == nil {
		return
	}
	for s := 0; s < 2; s++ {
		if !v.hasDisp[s] {
			continue
		}
		v.blitDisplay(screen, ctx, s, v.dispRects[s], &v.disp[s].text, &v.disp[s].dirty, dispBassScale)
	}
	v.blitDisplay(screen, ctx, 2, v.botRect, &v.bottom.text, &v.bottom.dirty, dispBottomScale)
}

func (v *View) blitDisplay(screen *ebiten.Image, ctx *core.Ctx, slot int, r core.Rect, text *string, dirty *bool, scale float64) {
	img := v.dispImg[slot]
	if img == nil {
		img = ebiten.NewImage(int(r[2]), int(r[3]))
		v.dispImg[slot] = img
		*dirty = true
	}
	if *dirty {
		img.Clear()
		if *text != "" && ctx.Font != nil {
			// img은 rect와 같은 크기의 로컬 캔버스 — 스크린 좌표(r.Center)를 넣으면 글줄이
			// 캔버스 밖에 놓여 아무 것도 안 그려진다(하단 표시창 미표시의 원인). 로컬 중심.
			ctx.Font.Draw(img, *text, r[2]/2, r[3]/2, scale, colLCD, core.AlignCenter)
		}
		*dirty = false
	}
	v.op.GeoM.Reset()
	v.op.GeoM.Translate(r[0], r[1])
	v.op.ColorScale.Reset()
	screen.DrawImage(img, &v.op)
}

// drawScope — 스코프 rect에 Bridge.Scope 256샘플 폴리라인. 진폭은 자동 이득: 현재 창의
// peak가 rect 높이 0.42를 채우도록 스케일한다(상한 ×8, peak < 0.05는 ×8) — 소리 피크가
// 0.3~0.4라 고정 스케일에서 선이 납작했던 수정(비전 처방). 파형이 없으면 중앙 수평선.
func (v *View) drawScope(screen *ebiten.Image, ctx *core.Ctx) {
	r := v.scopeRect
	mid := r[1] + r[3]/2
	amp := r[3] * scopeAmpRatio
	v.wave.Reset()
	if ctx.Bridge.Scope(v.scopeBytes[:]) {
		peak := float32(0)
		for i := 0; i < scopeSamples; i++ {
			s := math.Float32frombits(binary.LittleEndian.Uint32(v.scopeBytes[4*i:]))
			if s > 1 {
				s = 1
			} else if s < -1 {
				s = -1
			}
			v.scopeF32[i] = s
			if s > peak {
				peak = s
			} else if -s > peak {
				peak = -s
			}
		}
		gain := scopeGain(peak)
		for i := 0; i < scopeSamples; i++ {
			x := r[0] + r[2]*float64(i)/float64(scopeSamples-1)
			y := mid - amp*float64(v.scopeF32[i]*gain)
			if i == 0 {
				v.wave.MoveTo(float32(x), float32(y))
			} else {
				v.wave.LineTo(float32(x), float32(y))
			}
		}
	} else {
		v.wave.MoveTo(float32(r[0]), float32(mid))
		v.wave.LineTo(float32(r[0]+r[2]), float32(mid))
	}
	vector.StrokePath(screen, &v.wave, &v.strokeOpts, &v.drawOpts)
}

// scopeGain — 자동 이득: peak ≥ 0.05면 1/peak(상한 8), 그 밑은 8. 순수 함수(단언 대상).
func scopeGain(peak float32) float32 {
	if peak < scopeGainMinPeak {
		return scopeMaxGain
	}
	g := 1 / peak
	if g > scopeMaxGain {
		return scopeMaxGain
	}
	return g
}

// initStrokeOpts — 스코프 스트로크 옵션(불변, New에서 1회).
func (v *View) initStrokeOpts() {
	v.strokeOpts = vector.StrokeOptions{Width: scopeStrokeWidth, LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound}
	v.drawOpts = vector.DrawPathOptions{AntiAlias: true}
	v.drawOpts.ColorScale.ScaleWithColor(colLCD)
}

// ledCircle — LED 원 스프라이트(4×4 supersample 커버리지, straight alpha NRGBA).
func ledCircle(r float64, c color.NRGBA) *image.NRGBA {
	d := int(2*r + 0.5)
	img := image.NewNRGBA(image.Rect(0, 0, d, d))
	fr := float64(d) / 2
	for y := 0; y < d; y++ {
		for x := 0; x < d; x++ {
			cov := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px := float64(x) + (float64(sx)+0.5)/4 - fr
					py := float64(y) + (float64(sy)+0.5)/4 - fr
					if px*px+py*py <= fr*fr {
						cov++
					}
				}
			}
			if cov == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8(uint32(c.A) * uint32(cov) / 16)})
		}
	}
	return img
}

func solid1x1(c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, c)
	return img
}
