// draw.go — 그리기 전부. Draw는 Cmd를 보내지 않는다.
//
// 프레임당 드로잉 예산: DrawImage ≤ 208회(패널 1 + 라벨 레이어 1 + 노브 55 — P4-scroll
// 믹서 12·fx2 6·P5-poly 폴리 8 추가 + LED 36 + 랙 blit 1 + 인디케이터 ≤ 1 — P4-scroll +
// 표시창 3 + 오버레이 ≤ 14 + 코드 트랙 띠 채움 ≤ 2·글리프 ≈ 24 + VU 세그먼트 ≤ 44 + 패드
// LED 점 ≤ 6 — P3-meters, 레벨 0이면 미터 0 + 폴리 트리거 점 ≤ 1 — P5-poly), vector 호출
// 1회(스코프 폴리라인). 정적 라벨·스텝 버튼 면 16개·이름판 밴드 패치·코드 트랙 셀 외곽선
// 8개(1px×4변)는 첫 프레임에 레이아웃 크기(720×2000, v4) 오프스크린 한 장(labelLayer)로
// 합성해 매 프레임 1회 blit한다(프레임당 추가 비용 0). 옵션·버퍼는 전부 재사용.
package device

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// 스코프 스트로크 수치(스펙).
const (
	scopeStrokeWidth = 1.5
	scopeAmpRatio    = 0.42   // 진폭 = rect 높이 × 0.42(자동 이득 후 창의 peak가 채우는 목표)
	scopeMaxGain     = 8      // 자동 이득 상한(및 peak < 0.05 창의 고정 이득)
	scopeGainMinPeak = 0.05   // 이 이하는 노이즈 바닥으로 보고 ×8
	scopeHPA         = 0.9974 // 1차 하이패스 계수 y[n]=x[n]−x[n−1]+a·y[n−1] ≈ 20Hz @48kHz(2차 비전 처방)
)

// LED 상태 인덱스(ledImg).
const (
	ledOn = iota
	ledMid
	ledOff
)

// Draw — 랙(rack 오프스크린, 레이아웃 크기)에 본문 전체를 그린 뒤 scrollY만큼 올려 화면에
// blit한다(§13.3 스크롤 랙). 화면 밖은 GPU가 클립한다 — 잘라내기용 SubImage는 쓰지 않는다
// (할당이다). rack이 없는 newView(헤드리스 테스트) 경로는 예전처럼 화면에 직접 그린다.
// 본문 순서: 패널 → 패드 lit/뮤트(라벨 아래 — 비전 FIX 2026-09-06) → 라벨 → LED → 노브 →
// 오버레이 → 코드 트랙 띠 → 표시창 → 라인 미터 → 스코프. 인디케이터만 화면 좌표(랙 밖).
func (v *View) Draw(screen *ebiten.Image, ctx *core.Ctx) {
	dst := screen
	if v.rack != nil {
		dst = v.rack
	}
	v.drawRack(dst, ctx)
	if dst == screen {
		return
	}
	v.op.GeoM.Reset()
	v.op.GeoM.Translate(0, -v.scrollY)
	v.op.ColorScale.Reset()
	screen.DrawImage(v.rack, &v.op)
	v.drawScrollInd(screen, ctx)
}

// drawRack — Draw 본문. dst는 제품 경로 rack(레이아웃 크기, v4 720×2000) 또는 헤드리스 폴백 screen.
func (v *View) drawRack(dst *ebiten.Image, ctx *core.Ctx) {
	v.ensureLayers(ctx)
	v.op.GeoM.Reset()
	v.op.ColorScale.Reset()
	dst.DrawImage(v.panel, &v.op)
	v.drawPadLit(dst, ctx)
	v.op.GeoM.Reset()
	v.op.ColorScale.Reset()
	dst.DrawImage(v.labelLayer, &v.op)
	v.drawLEDs(dst, ctx)
	v.drawKnobs(dst, ctx)
	v.drawOverlays(dst, ctx)
	v.drawPolyTrig(dst, ctx)
	v.drawChordTrack(dst, ctx)
	v.drawDisplays(dst, ctx)
	v.drawMeters(dst, ctx)
	v.drawScope(dst, ctx)
}

// ensureLayers — 첫 Draw에서 정적 라벨 레이어를 합성한다(폰트가 ctx로 오므로 여기서).
// 스텝 버튼 면 16개와 이름판 좌측 밴드 패치도 이 레이어에 베이크한다 — 프레임당 blit는 1회 그대로.
func (v *View) ensureLayers(ctx *core.Ctx) {
	if v.layersOK {
		return
	}
	v.layersOK = true
	v.labelLayer = ebiten.NewImage(int(v.layout.Size[0]), int(v.layout.Size[1]))
	// 이름판 좌측 밴드(게이트 2의 검사 밴드, 폭 plateBandW): 페인팅 잔글자(bassA 상단 잔흔 — 패널 실측
	// dark_ratio 0.36)를 판 내부 중앙색으로 덮는다. 테두리(x≈+6..7)는 밴드 밖이라 남는다.
	for s := 0; s < 2; s++ {
		if v.hasBassPlate[s] {
			r := v.bassPlates[s]
			v.fillRect(v.labelLayer, core.Rect{r[0], r[1], plateBandW, r[3]}, colPlateBand[s])
		}
	}
	for s := 0; s < len(v.sectionPlates); s++ { // drums·fx·mixer·fx2(P4-scroll)
		if v.hasSection[s] {
			r := v.sectionPlates[s]
			v.fillRect(v.labelLayer, core.Rect{r[0], r[1], plateBandW, r[3]}, colPlateBand[s+2])
		}
	}
	// 스텝 버튼 면 16개(rect 안쪽 stepFaceInset): 16번 자리가 스크럽으로 지워져 1..15 패널 면의
	// 중앙값(colStepFace)을 앱이 그린다(2차 비전 처방). 숫자 라벨은 이 위에 온다.
	for i := range v.buttons {
		b := &v.buttons[i]
		if b.kind != bkStep {
			continue
		}
		r := b.rect
		v.fillRect(v.labelLayer, core.Rect{r[0] + stepFaceInset, r[1] + stepFaceInset,
			r[2] - 2*stepFaceInset, r[3] - 2*stepFaceInset}, colStepFace)
	}
	// 코드 트랙 셀 외곽선 8개(1px, colChordEdge α0.35) — 기하가 정적이므로 여기에 베이크.
	for i := range v.chordCells {
		r := v.chordCells[i]
		v.fillRectA(v.labelLayer, core.Rect{r[0], r[1], r[2], 1}, colChordEdge)
		v.fillRectA(v.labelLayer, core.Rect{r[0], r[1] + r[3] - 1, r[2], 1}, colChordEdge)
		v.fillRectA(v.labelLayer, core.Rect{r[0], r[1], 1, r[3]}, colChordEdge)
		v.fillRectA(v.labelLayer, core.Rect{r[0] + r[2] - 1, r[1], 1, r[3]}, colChordEdge)
	}
	f := ctx.Font
	if f == nil {
		return
	}
	for i := range v.knobs {
		k := &v.knobs[i]
		dy := float64(knobDyMain)
		if k.sec == secDrums || k.sec == secMixer || k.sec == secPoly { // r25 노브는 행 간격이 좁아 라벨을 붙인다
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
			col = colInk // 스텝 숫자는 (앱이 그린) 주황 면 위 — 어두운 잉크
		}
		sc := labelScale(f, b.label, labelBtnScale, b.rect[2]-labelFitPad)
		if b.kind == bkPlay || b.kind == bkRec {
			// 패널 그림의 트랜스포트 아이콘(가로 막대·삼각)이 버튼 중앙에 칠해져 있어 라벨과
			// 겹친다(비전 FIX 2026-09-06) — 라벨을 아이콘 아래로 내린다.
			cy += labelTransportDy
		}
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
	// 섹션 이름판: 왼쪽 정렬(+plateInset), 세로 중앙(y = cy − h/2). 폰트가 ASCII라 구분자는 asciiSep로 대체.
	// 크림판 위 크림 라벨은 안 보였다 — 어두운 잉크(비전 처방). mixer·fx2 판은 P4-scroll, poly는 P5-poly 추가.
	for i, txt := range [5]string{"DRUMS", "FX" + asciiSep + "SEQ", "MIXER", "FX 2", "POLY"} {
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

// fillRect — 불투명 단색 사각(정적 레이어·표시창 캔버스용). 1×1 흰 텍스처를 ColorScale로 물들여
// 확대한다(white1은 프리멀티플라이드 (1,1,1,1) — rgb×알파 1 곱은 불투명 단색이 된다).
func (v *View) fillRect(dst *ebiten.Image, r core.Rect, c color.NRGBA) {
	if v.white1 == nil {
		return // newView(테스트) 경로 — 이미지 없음
	}
	v.op.GeoM.Reset()
	v.op.GeoM.Scale(r[2], r[3])
	v.op.GeoM.Translate(r[0], r[1])
	v.op.ColorScale.Reset()
	v.op.ColorScale.Scale(float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, 1)
	dst.DrawImage(v.white1, &v.op)
}

// fillRectA — 반투명 단색 사각(코드 트랙 채움·외곽선). overlayRect의 색 일반화:
// 프리멀티플라이드 흰 1×1을 (r·a, g·a, b·a, a)로 축소해 source-over가 알파를 지키게 한다.
func (v *View) fillRectA(dst *ebiten.Image, r core.Rect, c color.NRGBA) {
	if v.white1 == nil {
		return
	}
	a := float32(c.A) / 255
	v.op.GeoM.Reset()
	v.op.GeoM.Scale(r[2], r[3])
	v.op.GeoM.Translate(r[0], r[1])
	v.op.ColorScale.Reset()
	v.op.ColorScale.Scale(float32(c.R)/255*a, float32(c.G)/255*a, float32(c.B)/255*a, a)
	dst.DrawImage(v.white1, &v.op)
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

// drawPadLit — 패드 뮤트 dim(ColorScale 0.55 상당)과 lit 워시(탭 120ms·라인 레벨 합성 max —
// P3-meters). 라벨 레이어 **아래**에 그린다(비전 FIX 2026-09-06: 워시가 글리프 위에 얹히면
// 울리는 패드의 이름이 가장 안 읽혔다 — 대비 1.93:1). 워시 상한도 0.22로 내렸다(잉크 라벨 겹침은
// 글리프가 얇아 실측 2.05:1에 그쳐 폐기). 이번 프레임 알파를 pad.litA에 남긴다(테스트·디버그).
// 반투명 사각 1×1 텍스처 확대(옵션 재사용, 할당 0).
func (v *View) drawPadLit(screen *ebiten.Image, ctx *core.Ctx) {
	for i := range v.pads {
		p := &v.pads[i]
		if ctx.Bridge.Muted(p.part) {
			v.overlayRect(screen, p.rect, v.black1, 1-padMuteScale)
		}
		// 라인 레벨 lit 합성: α = max(탭 lit, 0.12+0.5·vu) 상한 0.62 — 레벨 0이면 탭 lit만.
		vu := v.meters.disp[p.part] // 패드는 드럼 파트(2..7) — Part 값이 Levels 인덱스
		tapA := float32(0)
		if ctx.Now < p.litUntil {
			tapA = overlayLitA
		}
		p.litA = padLitAlpha(tapA, vu)
		if p.litA > 0 {
			v.overlayRect(screen, p.rect, v.white1, p.litA)
		}
	}
}

// drawOverlays — 라벨 레이어 위: 패드 LED 점, PLAY 상시 lit(재생 중 또는 제스처 전 가짜 시계), Build 페이즈 중 DROP
// 펄스. RESUME lit은 폐지(§12.3 — RESUME은 30초 무접촉 자동).
func (v *View) drawOverlays(screen *ebiten.Image, ctx *core.Ctx) {
	for i := range v.pads {
		p := &v.pads[i]
		if vu := v.meters.disp[p.part]; vu > 0 {
			v.drawPadLED(screen, p.rect, vu)
		}
	}
	if v.fxPlay >= 0 && transportLit(ctx.Tick) {
		v.overlayRect(screen, v.buttons[v.fxPlay].rect, v.white1, overlayLitA)
	}
	if v.fxRec >= 0 && ctx.Phase == 1 {
		a := float32(overlayLitA + dropPulseAmp*(0.5+0.5*math.Sin(2*math.Pi*dropPulseHz*ctx.Now)))
		v.overlayRect(screen, v.buttons[v.fxRec].rect, v.white1, a)
	}
}

// 폴리 트리거 점 수치(P5-poly). 감쇠율은 150ms(테스트 계약)에서 α < 0.1이 되는 값:
// e^(−20·0.15) = e^−3 ≈ 0.0498.
const (
	polyTrigR     = 4.0  // 점 반지름(px)
	polyTrigInset = 10.0 // 이름판 오른쪽 끝에서 안쪽 오프셋(px)
	polyTrigDecay = 20.0 // 지수 감쇠율(/초)
)

// polyTrigA — 트리거 점 알파. 미점화(−1) 0, 점화 프레임 1, 이후 e^(−20·경과초).
// 순수 함수 — 단언 대상(점화·150ms 감쇠·미점화).
func (v *View) polyTrigA(now float64) float32 {
	if v.polyTrigT < 0 {
		return 0
	}
	e := now - v.polyTrigT
	if e <= 0 {
		return 1
	}
	return float32(math.Exp(-polyTrigDecay * e))
}

// drawPolyTrig — 폴리 이름판 오른쪽 끄트막의 트리거 점(코드 건반 반응 — §14.1 FlagPoly).
// 점화 시각은 Update가 래치하고 여기선 감쇠만 계산한다. drawPadLED와 같은 계약: New에서
// 만든 점 스프라이트(ledCircle r4, colLCD)를 ColorScale로 페이드 — 프리멀티플라이드 색에
// 알파만 걸면 순색이 되므로 rgb도 함께 접는다(overlayRect 주석). 구 레이아웃(poly 판 없음)·
// newView(테스트, 스프라이트 nil)는 미그림. α ≤ 1/255이면 DrawImage 생략(예산 절약).
func (v *View) drawPolyTrig(dst *ebiten.Image, ctx *core.Ctx) {
	if !v.hasSection[secPoly-2] || v.polyDotImg == nil {
		return
	}
	a := v.polyTrigA(ctx.Now)
	if a <= 1.0/255 {
		return
	}
	r := v.sectionPlates[secPoly-2]
	d := float64(v.polyDotImg.Bounds().Dx())
	v.op.GeoM.Reset()
	v.op.GeoM.Translate(r[0]+r[2]-polyTrigInset-d/2, r[1]+r[3]/2-d/2)
	v.op.ColorScale.Reset()
	v.op.ColorScale.Scale(a, a, a, a)
	dst.DrawImage(v.polyDotImg, &v.op)
}

// drawChordTrack — 코드 트랙 띠(§12.3). 보통 상태: 현재 마디 셀 colLEDMid 채움(텍스트 아래) +
// 캐시된 도수 라벨 8개. 선택기 열림: 같은 띠에 다시 그린다 — 셀 0..6 = 도수 후보(현재 값 셀은
// colChordSel 채움), 셀 7 = 7th 토글(켜짐이면 채움) + 마디 라벨("B<n> 7"). 외곽선은 labelLayer에
// 베이크돼 있으므로 여기선 채움 2 + 글리프 ≈ 24만 쓴다.
func (v *View) drawChordTrack(screen *ebiten.Image, ctx *core.Ctx) {
	if ctx.Font == nil || v.chordRect[2] <= 0 {
		return
	}
	f := ctx.Font
	if v.chord.open {
		// 열림 표시(비전 FIX 2026-09-06): 띠 아래 화면 감광 + 띠 둘레 2px 액센트 외곽선.
		rr := v.chordRingRect
		below := rr[1] + rr[3]
		v.fillRectA(screen, core.Rect{0, below, v.layout.Size[0], v.layout.Size[1] - below}, colChordDim)
		v.fillRectA(screen, core.Rect{rr[0], rr[1], rr[2], chordOpenStroke}, colLEDOn)
		v.fillRectA(screen, core.Rect{rr[0], below - chordOpenStroke, rr[2], chordOpenStroke}, colLEDOn)
		v.fillRectA(screen, core.Rect{rr[0], rr[1], chordOpenStroke, rr[3]}, colLEDOn)
		v.fillRectA(screen, core.Rect{rr[0] + rr[2] - chordOpenStroke, rr[1], chordOpenStroke, rr[3]}, colLEDOn)
		deg, flags := v.bridgeChord(ctx.Bridge, v.chord.bar)
		deg %= engine.NumDegrees
		v.fillRectA(screen, v.chordCells[deg], colChordSel)
		for i := 0; i < engine.NumDegrees; i++ {
			cx, cy := v.chordCells[i].Center()
			f.Draw(screen, romanDeg[i], cx, cy, chordLabelScale, colLabel, core.AlignCenter)
		}
		// 7th 토글 셀 — 왼쪽 14px 비우고 표시창 필드색으로 채워 도수 후보와 분리; 켜짐이면 액센트 채움.
		c7 := v.chordTogRect
		v.fillRectA(screen, c7, colChordTog)
		if flags&engine.ChordSeventh != 0 {
			v.fillRectA(screen, c7, colChordSel)
		}
		cx, cy := c7.Center()
		f.Draw(screen, v.chord.barLbl, cx, cy, labelScale(f, v.chord.barLbl, chordLabelScale, c7[2]-labelFitPad), colLabel, core.AlignCenter)
		return
	}
	cur := int(ctx.Tick.Bar) & int(engine.ChordBars-1)
	v.fillRectA(screen, v.chordCells[cur], colLEDMid)
	for i := range v.chordCells {
		cx, cy := v.chordCells[i].Center()
		f.Draw(screen, v.chord.cells[i].text, cx, cy, labelScale(f, v.chord.cells[i].text, chordLabelScale, v.chordCells[i][2]-labelFitPad), colLabel, core.AlignCenter)
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
// 베이스라인 2개는 페인팅 잔글자가 창 안에 남아 있어(패널 실측: 창 상단 ~10px에 녹색 잔흔)
// 창색(colDispWin)을 불투명하게 깔고 그 위에 앱 폰트 텍스트만 올린다(2차 비전 처방).
// 라인 미터 VU 띠는 이 캐시 밖(drawMeters) — 문자열과 무관하게 매 프레임 화면에 직접.
func (v *View) drawDisplays(screen *ebiten.Image, ctx *core.Ctx) {
	if ctx.Font == nil {
		return
	}
	for s := 0; s < 2; s++ {
		if !v.hasDisp[s] {
			continue
		}
		v.blitDisplay(screen, ctx, s, v.dispRects[s], &v.disp[s].text, &v.disp[s].dirty, dispBassScale, colDispWin[s], labelFitPad)
	}
	v.blitDisplay(screen, ctx, 2, v.botRect, &v.bottom.text, &v.bottom.dirty, dispBottomScale, color.NRGBA{}, botDispPad)
}

func (v *View) blitDisplay(screen *ebiten.Image, ctx *core.Ctx, slot int, r core.Rect, text *string, dirty *bool, scale float64, bg color.NRGBA, pad float64) {
	img := v.dispImg[slot]
	if img == nil {
		img = ebiten.NewImage(int(r[2]), int(r[3]))
		v.dispImg[slot] = img
		*dirty = true
	}
	if *dirty {
		img.Clear()
		if bg.A != 0 {
			v.fillRect(img, core.Rect{0, 0, r[2], r[3]}, bg)
		}
		if *text != "" && ctx.Font != nil {
			// img은 rect와 같은 크기의 로컬 캔버스 — 스크린 좌표(r.Center)를 넣으면 글줄이
			// 캔버스 밖에 놓여 아무 것도 안 그려진다(하단 표시창 미표시의 원인). 로컬 중심.
			// 텍스트 폭은 rect−pad 안으로 축소(2차 비전 처방 — 창 밖 넘침 방지).
			// 하단 표시창은 botDispPad(8)로 더 좁게 — "Am 120 B3 BUILD"가 창에 맞는다(§12.3).
			// 세로는 중앙에서 위로 — 라인 미터 띠(베이스 4px·하단 3px)와 겹치지 않게(P3-meters).
			// slot별 오프셋: 공용 경로라 같이 올리면 하단(띠 3px)이 과하게 뜬다.
			dy := -vuTextDy
			if slot == 2 {
				dy = -vuTextDyBot
			}
			sc := labelScale(ctx.Font, *text, scale, r[2]-pad)
			ctx.Font.Draw(img, *text, r[2]/2, r[3]/2+dy, sc, colLCD, core.AlignCenter)
		}
		*dirty = false
	}
	v.op.GeoM.Reset()
	v.op.GeoM.Translate(r[0], r[1])
	v.op.ColorScale.Reset()
	screen.DrawImage(img, &v.op)
}

// drawScope — 스코프 rect에 Bridge.Scope 256샘플 폴리라인. 순서: ① 클램프 ② 1차 하이패스(≈20Hz,
// 상태는 프레임 간 유지 — DC·저주파를 지워 자동 이득이 램프(경사선)를 증폭하지 않게 한다, 2차 비전
// 처방) ③ 창 평균 제거 ④ 상승 제로크로싱에 시작점 정렬(원형 회전 — 파형이 프레임마다 흔들리지 않게)
// ⑤ 자동 이득(상한 ×8) ⑥ 중앙선 = rect 세로 중앙. 파형이 없으면 중앙 수평선.
func (v *View) drawScope(screen *ebiten.Image, ctx *core.Ctx) {
	r := v.scopeRect
	mid := r[1] + r[3]/2
	amp := r[3] * scopeAmpRatio
	v.wave.Reset()
	if ctx.Bridge.Scope(v.scopeBytes[:]) {
		for i := 0; i < scopeSamples; i++ {
			s := math.Float32frombits(binary.LittleEndian.Uint32(v.scopeBytes[4*i:]))
			if s > 1 {
				s = 1
			} else if s < -1 {
				s = -1
			}
			v.scopeF32[i] = s
		}
		for i := 0; i < scopeSamples; i++ {
			x := v.scopeF32[i]
			y := hpStep(x, v.scopeHPX, v.scopeHPY)
			v.scopeF32[i] = y
			v.scopeHPX, v.scopeHPY = x, y
		}
		peak := float32(0)
		removeMean(v.scopeF32[:])
		for i := 0; i < scopeSamples; i++ {
			if s := v.scopeF32[i]; s > peak {
				peak = s
			} else if -s > peak {
				peak = -s
			}
		}
		z := scopeTrigger(v.scopeF32[:])
		gain := scopeGain(peak)
		for k := 0; k < scopeSamples; k++ {
			x := r[0] + r[2]*float64(k)/float64(scopeSamples-1)
			y := mid - amp*float64(v.scopeF32[(z+k)%scopeSamples]*gain)
			if k == 0 {
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

// hpStep — 스코프 표시 전 1차 하이패스 1샘플: y[n] = x[n] − x[n−1] + a·y[n−1](a = scopeHPA ≈ 20Hz
// @48kHz). 순수 함수(단언 대상) — 상태는 호출자(View.scopeHPX/Y)가 프레임 간 유지한다.
func hpStep(x, x1, y1 float32) float32 {
	return x - x1 + scopeHPA*y1
}

// removeMean — 창(256샘플 ≈ 5.3ms) 평균을 뺀다. 하이패스(≈20Hz)만으로는 중앙 정렬이 안 된다:
// 베이스 기본파(~55Hz, 주기 ≈18ms)가 창보다 길어 창마다 평균(오프셋)이 남고, 자동 이득이 그것을
// 증폭해 파형이 한쪽으로 치우친다(실측 2026-09-06: HPF만으로 녹색 y-평균 47.1 vs 게이트 중앙 63±8).
// 창 평균 제거는 표시 샘플의 평균을 0으로 못 박는다 — 중앙선 계약의 구조적 닫힘. 순수 함수(단언 대상).
func removeMean(buf []float32) {
	sum := float32(0)
	for i := range buf {
		sum += buf[i]
	}
	m := sum / float32(len(buf))
	for i := range buf {
		buf[i] -= m
	}
}

// scopeTrigger — 상승 제로크로싱(≤0 → >0) 인덱스. 파형 시작점 정렬용, 없으면 0. 순수 함수.
func scopeTrigger(buf []float32) int {
	for i := 1; i < len(buf); i++ {
		if buf[i-1] <= 0 && buf[i] > 0 {
			return i
		}
	}
	return 0
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
