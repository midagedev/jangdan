// Package room — 방 뷰. 다락방 플레이트 한 장 위에 작은 것들이 리듬에 산다.
//
// 계약 원본: docs/impl-plan-2026-09-05.md §7, 기획서(plan-2026-09-05-v2.html) 05절
// "사물 ↔ 엔진 신호" 표와 광과민 규정. 좌표의 단일 소유자는 app/assets/room/layout.json
// (지금은 플레이스홀더 — 별도 비전 라운드가 실제 좌표로 덮어쓴다. 값에 의존한 하드코딩 금지).
//
// 구조: 프레임 로직(state.step — 이미지 없이 단위 테스트)과 렌더링(View.Draw — state를
// 읽어 그리기만)이 분리돼 있다. Draw에서 Cmd를 보내지 않고, ebiten 입력 API 대신
// ctx.Pointers를 쓰고, 프레임당 옵션·버퍼는 전부 View 필드로 재사용한다(할당 ≤ 2KB).
package room

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/midagedev/revirth/app/assets"
	"github.com/midagedev/revirth/app/core"
	"github.com/midagedev/revirth/engine"
)

// 스펙 수치(§8·§12~§14·§18). 색은 스펙 hex와 layout.palette만.
const (
	todFadeSec          = 30.0 // 시간대 전환 크로스페이드
	sessionAfternoonSec = 45 * 60

	scopeSamps = 256 // Bridge.Scope 파형 샘플 수
	scopeSegs  = scopeSamps - 1
	scopeHalfW = 0.5 // 폴리라인 굵기 1px → 반폭

	tapScale       = 1.2 // 첫 힌트 "TAP"
	seedScale      = 0.7 // 시드 단어
	hintRingR      = 40.0
	hintStrokePx   = 2.0
	hintIdleMinSec = 15.0
	hintIdleMaxSec = 60.0
	ledInsetPx     = 4.0 // device rect 하단 안쪽(자동 배치 때)

	gatePollFrames = 4  // CH/OH 게이트 폴 주기(프레임)
	seedPollFrames = 60 // 시드 단어 폴 주기(1초)

	rainMaxAlpha = 0.6 // 비 알파 상한(§15)
)

// 스펙 고정색.
var (
	colScope  = color.NRGBA{R: 0x7F, G: 0xE0, B: 0x8A, A: 204} // #7FE08A α0.8
	colLED    = color.NRGBA{R: 0xFF, G: 0x9A, B: 0x3C, A: 255} // #FF9A3C
	colText   = color.NRGBA{R: 0xE8, G: 0xE2, B: 0xD2, A: 255} // #E8E2D2
	lampHot   = color.NRGBA{R: 0xFF, G: 0xF1, B: 0xD6, A: 255} // 컷오프 q=1 색온도
	dustColor = color.NRGBA{R: 0xE8, G: 0xE2, B: 0xD2, A: 255} // 등빛 속 먼지
)

// Signals — 한 프레임의 입력(브리지·Ctx에서 모은 것). 로직 단위 테스트는 이 값을 합성해
// state.step에 넣는다. 뮤트는 비 공식에 없다(게이트 수만 본다).
type Signals struct {
	DT               float64
	Now              float64
	Started          bool
	Touched          bool // 이 프레임에 눌린 포인터가 하나라도 있다(힌트 소거)
	Step             int
	Bar              uint32
	Flags            uint32 // engine.Flag* + 파트 트리거 비트
	Cutoff           float32
	Tempo            float32 // 파라미터 미러(0..1) — BPM은 엔진 공식
	CHGates, OHGates int
	Phase            uint8 // 0 Intro 1 Build 2 Drop 3 Breakdown
	PomodoroRest     bool
	ResidentHandOn   bool
	ManualLocked     bool
	ReducedMotion    bool
	CleanScreen      bool
}

// state — 방 뷰의 프레임 로직 상태 전부. Draw가 읽는 출력(변환·알파·밝기)까지 여기서
// 계산된다.
type state struct {
	t         float64
	now       float64
	bpm       float64
	beatPhase float64 // 박 위상(꼬리·끄덕임 공용)

	reduced, clean, started bool
	everTouched             bool
	manualLocked            bool

	// 스탠드
	lampAmp      float32 // 테스트 주입용(기본 lampBreathAmp)
	lampPulse    float32
	lampBase     float32
	lampQ        float32
	lampShakeT   float64
	lampShakeOff [2]float64

	// 비
	rainDensity float32
	rainThick   float32

	// 창
	winLit         []bool
	winFade        []float32
	winAlpha       []float32 // 유효 알파(기본 알파 × 페이드 × 강제 점등)
	winArea        []float64
	winAreaFrac    []float32
	winChangedArea float64
	capArea        float64
	lampFrac       float32 // 빛 풀 면적 / 화면 면적(휘도 모델)

	// 머그
	steamT    float64
	steamRise float64
	steamP    [steamCount][3]float32 // x오프셋, 진행(0..1), 알파
	mugJitter bool

	// 라디에이터·레코드
	radLift   bool
	radGlow   float64
	recOrder  []int
	recFadeK  float64
	prevPhase uint8

	cat  catState
	char charState

	reachDX, reachDY float64 // 기기 방향 6px 변위

	// 시간대
	tod      timeOfDay
	prevTod  timeOfDay
	todBlend float64

	// 플래시 게이트(광과민 안전망)
	flash        flashGate
	FlashClamped int
	prevPulse    float32
	prevFades    []float32
	prevBlend    float64

	tapArmed int // 기기 탭 추적 포인터 ID(−1 없음)
	lastStep int
	rng      uint32
}

func newState(l *core.RoomLayout) state {
	s := state{
		lampAmp:  lampBreathAmp,
		tod:      todNight,
		prevTod:  todNight,
		todBlend: 1,
		tapArmed: -1,
		rng:      rngSeedInit,
		recFadeK: 1,
		lastStep: -1,
	}
	w, h := l.Size[0], l.Size[1]
	if w <= 0 || h <= 0 {
		w, h = core.LogicalW, core.LogicalH
	}
	screen := w * h
	s.capArea = screen * screenAreaFrac
	frac := math.Pi * l.Lamp.Radius * l.Lamp.Radius / screen
	if frac > 1 {
		frac = 1
	}
	s.lampFrac = float32(frac)
	if n := len(l.Windows); n > 0 {
		s.winLit = make([]bool, n)
		s.winFade = make([]float32, n)
		s.winAlpha = make([]float32, n)
		s.prevFades = make([]float32, n)
		s.winArea = make([]float64, n)
		s.winAreaFrac = make([]float32, n)
		for i, r := range l.Windows {
			s.winArea[i] = r[2] * r[3]
			s.winAreaFrac[i] = float32(s.winArea[i] / screen)
		}
	}
	if n := len(l.Records); n > 0 {
		s.recOrder = make([]int, n)
		for i := range s.recOrder {
			s.recOrder[i] = i
		}
	}
	s.steamRise = math.Max(1, l.Mug.Rect[3]*steamRiseRatio)
	// reach: 캐릭터 앵커 → 기기 중심 방향 6px
	dcx, dcy := l.Device.Center()
	ax, ay := l.Character.Anchor[0], l.Character.Anchor[1]
	if d := math.Hypot(dcx-ax, dcy-ay); d > 1 {
		s.reachDX = (dcx - ax) / d * charReachPx
		s.reachDY = (dcy - ay) / d * charReachPx
	}
	// idle_look 방향: 고양이 쪽 부호
	s.char.idleDir = 1
	if l.Cat.Anchor[0] < l.Character.Anchor[0] {
		s.char.idleDir = -1
	}
	return s
}

// step — 한 프레임 로직 전부. 마지막에 휘도 모델로 플래시 게이트를 돌리고, 임계 초과면
// 그 프레임의 광원 변화를 되돌린다(스펙 §9).
func (s *state) step(sig Signals, dt float64) {
	// 클램프 되돌림용 직전 프레임 광원 스냅샷
	s.prevPulse = s.lampPulse
	copy(s.prevFades, s.winFade)
	s.prevBlend = s.todBlend

	if dt < 0 {
		dt = 0
	}
	s.t += dt
	s.now = sig.Now
	s.reduced = sig.ReducedMotion
	s.clean = sig.CleanScreen
	s.started = sig.Started
	s.manualLocked = sig.ManualLocked
	if sig.Started && sig.Touched {
		s.everTouched = true
	}
	s.bpm = engine.BPMOf(sig.Tempo)
	s.lastStep = sig.Step
	if !s.reduced {
		s.beatPhase = math.Mod(s.beatPhase+dt/(60/s.bpm), 1)
	}

	s.stepTod(sig, dt)
	s.stepLamp(sig, dt)
	s.stepRain(sig)
	s.stepWindows(sig, dt)
	s.stepMug(sig, dt, s.steamRise)
	s.stepFurnishings(sig, dt, len(s.recOrder))
	s.stepCat(sig, dt)
	s.stepChar(sig, dt)

	if s.flash.observe(s.t, s.screenLum()) {
		s.lampPulse = s.prevPulse
		copy(s.winFade, s.prevFades)
		s.todBlend = s.prevBlend
		s.FlashClamped++
	}
}

// stepTod — 방의 시간: 휴식 → evening, 세션 45분 초과 → afternoon, 그 외 night.
func (s *state) stepTod(sig Signals, dt float64) {
	target := todNight
	if sig.PomodoroRest {
		target = todEvening
	} else if sig.Now > sessionAfternoonSec {
		target = todAfternoon
	}
	if target != s.tod {
		s.prevTod, s.tod = s.tod, target
		s.todBlend = 0
	}
	if s.todBlend < 1 {
		s.todBlend = math.Min(1, s.todBlend+dt/todFadeSec)
	}
}

// tintNow — 시간대 크로스페이드가 적용된 전체 ColorScale.
func (s *state) tintNow() (r, g, b float32) { return lerpTint(s.prevTod, s.tod, s.todBlend) }

// screenLum — 화면 휘도 모델(스펙 §9): 스탠드 빛 풀 면적 × 밝기 + 창 면적 × 켜짐.
// 상수 기반(틴트 베이스라인)이 아니라 변하는 빛만 잰다 — 6% 호흡은 10% 임계 안에
// 들어오고, 그 이상의 요동은 잡히는 감도.
func (s *state) screenLum() float64 {
	lampA := lerpF32(s.prevTod.lampAlpha(), s.tod.lampAlpha(), float32(s.todBlend))
	l := float64(s.lampFrac * s.lampBrightness() * lampA)
	base := lerpF32(s.prevTod.windowAlpha(), s.tod.windowAlpha(), float32(s.todBlend))
	for i := range s.winFade {
		l += float64(s.winAreaFrac[i] * base * s.winFade[i])
	}
	return l
}

// ----------------------------------------------------------------------------
// View

type View struct {
	layout  *core.RoomLayout
	pal     palette
	plates  [3]*ebiten.Image
	plateNm [3]string
	sh      *shaderSet
	white   *ebiten.Image // 1×1 흰(채움·라인·삼각형 소스)

	// 서브이미지(New에서 1회)
	mugSub, mugTop, radSub *ebiten.Image
	recSub                 []*ebiten.Image
	catSub, charSub        *ebiten.Image
	catPoses, charPoses    []*ebiten.Image

	// 캐시 오프스크린(상태 변화 시에만 다시 그린다)
	winImg     *ebiten.Image
	winX, winY float64
	winCached  []float32
	recImg     *ebiten.Image
	recX, recY float64
	recCacheO0 int
	recCacheK  float64

	ledBase            *ebiten.Image
	ledBaseX, ledBaseY float64
	ledLit             []*ebiten.Image
	ledLitX, ledLitY   []float64

	tapImg       *ebiten.Image
	tapCX, tapCY float64
	ringImg      *ebiten.Image
	hintImg      *ebiten.Image
	seedImg      *ebiten.Image
	seedStr      string

	st     state
	tapped bool // 이 프레임 DeviceTapped 값(Update에서 계산)

	// 프레임 재사용(할당 0). 셰이더 드로우 옵션은 shaderSet이 소유(Uniforms 생성 시 바인드).
	op   ebiten.DrawImageOptions
	trop ebiten.DrawTrianglesOptions

	scopeBuf [scopeSamps * 4]byte
	scopeOn  bool
	scopePts [scopeSamps * 2]float32
	scopeV   [scopeSegs * 4]ebiten.Vertex
	scopeI   [scopeSegs * 6]uint16
	frame    int
	gateCH   int
	gateOH   int
	draws    int // 계측: 이 프레임 DrawImage 수
}

type palette struct {
	lampWarm, ink, rain, windowLit color.NRGBA
}

// New — 플레이트 디코드(assets.RoomFiles)·레이아웃 파싱·셰이더 컴파일·서브이미지와
// 캐시 오프스크린 준비. 실패 시 오류를 돌려준다(무음 폴백 없음).
func New(ctx *core.Ctx) (*View, error) {
	l, err := core.LoadRoomLayout(assets.RoomLayoutJSON)
	if err != nil {
		return nil, fmt.Errorf("room: layout: %w", err)
	}
	v := &View{layout: l}
	v.pal = palette{
		lampWarm:  parseHex(l.Palette.LampWarm, color.NRGBA{R: 0xF2, G: 0xB8, B: 0x66, A: 255}),
		ink:       parseHex(l.Palette.Ink, color.NRGBA{R: 0x1C, G: 0x1A, B: 0x24, A: 255}),
		rain:      parseHex(l.Palette.Rain, color.NRGBA{R: 0x9F, G: 0xB3, B: 0xC8, A: 255}),
		windowLit: parseHex(l.Palette.WindowLit, color.NRGBA{R: 0xE8, G: 0xC2, B: 0x7A, A: 255}),
	}
	if v.sh, err = newShaders(); err != nil {
		return nil, err
	}
	v.white = ebiten.NewImage(1, 1)
	v.white.Fill(color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	// 플레이트 3장 — 같은 파일명이면 디코드를 공유한다.
	names := [3]string{l.Plates.Night, l.Plates.Evening, l.Plates.Afternoon}
	decoded := make(map[string]*ebiten.Image, 3)
	for i, nm := range names {
		img, ok := decoded[nm]
		if !ok {
			b, err := assets.Read("room/" + nm)
			if err != nil {
				return nil, fmt.Errorf("room: plate %s: %w", nm, err)
			}
			m, _, err := image.Decode(bytes.NewReader(b))
			if err != nil {
				return nil, fmt.Errorf("room: plate %s decode: %w", nm, err)
			}
			img = ebiten.NewImageFromImage(m)
			decoded[nm] = img
		}
		v.plates[i], v.plateNm[i] = img, nm
	}

	sub := func(r core.Rect) *ebiten.Image {
		if r[2] < 1 || r[3] < 1 {
			return nil
		}
		ir := image.Rect(int(r[0]), int(r[1]), int(r[0]+r[2]), int(r[1]+r[3]))
		return v.plates[0].SubImage(ir).(*ebiten.Image)
	}
	v.mugSub = sub(l.Mug.Rect)
	v.mugTop = sub(core.Rect{l.Mug.Rect[0], l.Mug.Rect[1], l.Mug.Rect[2], 2})
	v.radSub = sub(l.Radiator)
	v.catSub = sub(l.Cat.Rect)
	v.charSub = sub(l.Character.Rect)
	for _, r := range l.Records {
		v.recSub = append(v.recSub, sub(r))
	}
	v.catPoses = v.loadPoses(l.Cat.Poses)
	v.charPoses = v.loadPoses(l.Character.Poses)

	// 창·레코드 캐시 오프스크린(union bbox)
	if n := len(l.Windows); n > 0 {
		x0, y0, x1, y1 := bboxOf(l.Windows)
		v.winImg = newOffscreen(int(x1-x0)+1, int(y1-y0)+1)
		v.winX, v.winY = x0, y0
		v.winCached = make([]float32, n)
		for i := range v.winCached {
			v.winCached[i] = -1
		}
	}
	if len(l.Records) > 0 {
		x0, y0, x1, y1 := bboxOf(l.Records)
		v.recImg = newOffscreen(int(x1-x0)+1, int(y1-y0)+1)
		v.recX, v.recY = x0, y0
		v.recCacheO0, v.recCacheK = -2, -1
	}

	v.buildLEDs()
	v.buildHints(ctx.Font)
	for i := 0; i < scopeSegs; i++ {
		b := i * 4
		for k := 0; k < 4; k++ {
			v.scopeV[b+k].SrcX, v.scopeV[b+k].SrcY = 0.5, 0.5
			v.scopeV[b+k].ColorR, v.scopeV[b+k].ColorG = float32(colScope.R)/255, float32(colScope.G)/255
			v.scopeV[b+k].ColorB, v.scopeV[b+k].ColorA = float32(colScope.B)/255, float32(colScope.A)/255
		}
		o := i * 6
		v.scopeI[o+0], v.scopeI[o+1], v.scopeI[o+2] = uint16(b), uint16(b+1), uint16(b+2)
		v.scopeI[o+3], v.scopeI[o+4], v.scopeI[o+5] = uint16(b+1), uint16(b+3), uint16(b+2)
	}

	v.st = newState(l)
	return v, nil
}

// loadPoses — 포즈 스프라이트(없으면 빈 슬라이스 — 플레이트 서브이미지 변형으로 동작).
func (v *View) loadPoses(names []string) []*ebiten.Image {
	if len(names) == 0 {
		return nil
	}
	out := make([]*ebiten.Image, len(names))
	for i, nm := range names {
		b, err := assets.Read("room/" + nm)
		if err != nil {
			continue
		}
		m, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			continue
		}
		out[i] = ebiten.NewImageFromImage(m)
	}
	return out
}

// buildLEDs — 16스텝 LED. layout device_leds가 비면 device rect 하단 안쪽 4px 줄에
// 균등 배치한다. 전부 꺼진 밑판 + 켜진 칸 하나(스텝) 이미지를 미리 그려 프레임당
// 2장 blit로 끝낸다.
func (v *View) buildLEDs() {
	leds := v.layout.DeviceLEDs
	if len(leds) == 0 {
		r := v.layout.Device
		n := engine.Steps
		rr := r[3] / 12
		y := r[1] + r[3] - ledInsetPx - 2*rr
		sp := r[2] / float64(n)
		syn := make([]core.LED, n)
		for i := range syn {
			syn[i] = core.LED{CX: r[0] + sp*(float64(i)+0.5), CY: y, R: rr}
		}
		leds = syn
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, led := range leds {
		x0 = math.Min(x0, led.CX-led.R-1)
		y0 = math.Min(y0, led.CY-led.R-1)
		x1 = math.Max(x1, led.CX+led.R+1)
		y1 = math.Max(y1, led.CY+led.R+1)
	}
	v.ledBase = newOffscreen(int(x1-x0)+1, int(y1-y0)+1)
	v.ledBaseX, v.ledBaseY = x0, y0
	for _, led := range leds {
		vector.FillCircle(v.ledBase, float32(led.CX-x0), float32(led.CY-y0), float32(led.R),
			color.NRGBA{R: colLED.R, G: colLED.G, B: colLED.B, A: 64}, true) // α0.25
	}
	v.ledLit = make([]*ebiten.Image, len(leds))
	v.ledLitX = make([]float64, len(leds))
	v.ledLitY = make([]float64, len(leds))
	for i, led := range leds {
		w, h := int(2*led.R+3), int(2*led.R+3)
		img := newOffscreen(w, h)
		vector.FillCircle(img, float32(w/2), float32(h/2), float32(led.R), colLED, true)
		v.ledLit[i] = img
		v.ledLitX[i] = led.CX - float64(w)/2
		v.ledLitY[i] = led.CY - float64(h)/2
	}
}

// buildHints — "TAP" 글자·링(r 40)·기기 외곽 힌트를 미리 그린다(알파 펄스는 blit에서).
func (v *View) buildHints(font *core.FontSet) {
	if font != nil {
		w, h := font.Measure("TAP", tapScale)
		if w >= 1 {
			v.tapImg = newOffscreen(int(w)+2, int(h)+2)
			font.Draw(v.tapImg, "TAP", 1, 1, tapScale, colText, core.AlignLeft)
			v.tapCX, v.tapCY = float64(int(w)+2)/2, float64(int(h)+2)/2
		}
	}
	size := int(2 * (hintRingR + hintStrokePx))
	v.ringImg = newOffscreen(size, size)
	vector.StrokeCircle(v.ringImg, float32(size/2), float32(size/2), float32(hintRingR),
		float32(hintStrokePx), colText, true)
	dr := v.layout.Device
	v.hintImg = newOffscreen(int(dr[2])+5, int(dr[3])+5)
	vector.StrokeRect(v.hintImg, 2.5, 2.5, float32(dr[2]), float32(dr[3]),
		float32(hintStrokePx), colText, true)
}

// rebuildSeedCache — 시드 단어가 바뀔 때만(ASCII면 새기고, 아니면 DOM 오버레이 몫).
func (v *View) rebuildSeedCache(font *core.FontSet) {
	v.seedImg = nil
	if !isASCIIWord(v.seedStr) || font == nil {
		return
	}
	r := v.layout.SeedText
	w, _ := font.Measure(v.seedStr, seedScale)
	if w < 1 || r[2] < 1 || r[3] < 1 {
		return
	}
	v.seedImg = newOffscreen(int(r[2]), int(r[3]))
	font.Draw(v.seedImg, v.seedStr, r[2]/2, r[3]/2, seedScale,
		color.NRGBA{R: colText.R, G: colText.G, B: colText.B, A: 178}, core.AlignCenter) // α0.7
}

// Update — 입력(기기 탭)·브리지 폴·프레임 로직. Draw에서 하는 일은 없다.
func (v *View) Update(ctx *core.Ctx) {
	v.tapped = false
	v.draws = 0

	// 기기 탭: rect 안에서 눌렸다가 떼어진 포인터(스펙 공개 API 계약).
	for i := range ctx.Pointers {
		p := &ctx.Pointers[i]
		if p.JustPressed && v.layout.Device.Contains(p.X, p.Y) {
			v.st.tapArmed = p.ID
		}
		if p.JustReleased && v.st.tapArmed == p.ID {
			v.st.tapArmed = -1
			if v.layout.Device.Contains(p.X, p.Y) {
				v.tapped = true
			}
		}
	}

	// CH/OH 게이트 수 — 4프레임마다 폴(맵·JS 호출 비용; 비는 앰비언트 반응이라 즉시성
	// 필요 없다). 뮤트는 공식에 없다.
	if v.frame%gatePollFrames == 0 {
		v.gateCH, v.gateOH = 0, 0
		for s := 0; s < engine.Steps; s++ {
			if ctx.Bridge.DrumStep(engine.CH, s)&engine.StepGate != 0 {
				v.gateCH++
			}
			if ctx.Bridge.DrumStep(engine.OH, s)&engine.StepGate != 0 {
				v.gateOH++
			}
		}
	}
	v.frame++

	v.scopeOn = ctx.Bridge.Scope(v.scopeBuf[:])
	if v.frame%seedPollFrames == 0 {
		if w := ctx.Bridge.SeedWord(); w != v.seedStr {
			v.seedStr = w
			v.rebuildSeedCache(ctx.Font)
		}
	}
	v.st.step(v.signals(ctx), ctx.DT)
}

// DeviceTapped — 이 프레임에 기기 rect 안에서 눌렸다가 떼어진 포인터면 true.
// 갱신은 Update에서: 해제 프레임에만 true, 다음 프레임엔 false다.
func (v *View) DeviceTapped() bool { return v.tapped }

// signals — Ctx/브리지에서 이번 프레임 신호를 모은다.
func (v *View) signals(ctx *core.Ctx) Signals {
	touched := false
	for i := range ctx.Pointers {
		if ctx.Pointers[i].Pressed {
			touched = true
			break
		}
	}
	return Signals{
		DT:             ctx.DT,
		Now:            ctx.Now,
		Started:        ctx.Tick.Started,
		Touched:        touched,
		Step:           ctx.Tick.Step,
		Bar:            ctx.Tick.Bar,
		Flags:          ctx.Tick.Flags,
		Cutoff:         ctx.Bridge.Param(engine.CutoffA),
		Tempo:          ctx.Bridge.Param(engine.Tempo),
		CHGates:        v.gateCH,
		OHGates:        v.gateOH,
		Phase:          ctx.Phase,
		PomodoroRest:   ctx.PomodoroRest,
		ResidentHandOn: ctx.ResidentHandOn,
		ManualLocked:   ctx.ManualLocked,
		ReducedMotion:  ctx.Bridge.ReducedMotion(),
		CleanScreen:    ctx.CleanScreen,
	}
}

// Draw — state를 읽어 그리기만 한다(Cmd·상태 갱신 없음).
func (v *View) Draw(screen *ebiten.Image, ctx *core.Ctx) {
	s := &v.st
	l := v.layout
	tr, tg, tb := s.tintNow()

	// 1) 플레이트. 시간대 전환: 파일이 다르면 두 장 알파 크로스페이드, 같으면 틴트.
	two := s.todBlend < 1 && v.plateNm[s.prevTod] != v.plateNm[s.tod]
	if two {
		v.blit(screen, v.plates[s.prevTod], 0, 0, tr, tg, tb, float32(1-s.todBlend))
	}
	v.blit(screen, v.plates[s.tod], 0, 0, tr, tg, tb, 1)

	// 2) 창밖 마을 불빛(캐시 1장)
	if v.winImg != nil {
		v.rebuildWinCache()
		v.blit(screen, v.winImg, v.winX, v.winY, tr, tg, tb, 1)
	}

	// 3) 천창의 비
	if w, h := rectSize(l.Skylight); w >= 1 && h >= 1 {
		u := v.sh.rainU
		u["Time"] = float32(s.t)
		u["Density"] = s.rainDensity
		u["Thickness"] = s.rainThick
		setColorUniform(v.sh.rainColor, v.pal.rain, tr, tg, tb, rainMaxAlpha)
		op := &v.sh.rainShop
		op.GeoM.Reset()
		op.GeoM.Translate(l.Skylight[0], l.Skylight[1])
		screen.DrawRectShader(w, h, v.sh.rain, op)
	}

	// 4) 벽 레코드(캐시 — Phase 교체 크로스페이드)
	if v.recImg != nil && !v.recIdentityResting() {
		v.rebuildRecCache()
		v.blit(screen, v.recImg, v.recX, v.recY, tr, tg, tb, 1)
	}

	// 5) 라디에이터 — 바마다 1px 리프트 + 60ms 하이라이트
	if v.radSub != nil {
		if s.radLift {
			v.blit(screen, v.radSub, l.Radiator[0], l.Radiator[1]-radLiftPx, tr, tg, tb, 1)
		}
		if s.radGlow > 0 {
			a := float32(radHiAlpha * (s.radGlow / radGlowSec))
			v.fill(screen, l.Radiator[0], l.Radiator[1], l.Radiator[2], l.Radiator[3], tr, tg, tb, a)
		}
	}

	// 6) 머그 — 표면 떨림(1프레임)과 김
	if s.mugJitter && v.mugTop != nil {
		v.blit(screen, v.mugTop, l.Mug.Rect[0], l.Mug.Rect[1]+1, tr, tg, tb, 1)
	}
	if !s.reduced || s.steamT > 0 {
		for i := range s.steamP {
			p := &s.steamP[i]
			if p[2] <= 0.01 {
				continue
			}
			x := l.Mug.Steam[0] + float64(p[0]) - steamSizePx/2
			y := l.Mug.Steam[1] - float64(p[1])*s.steamRise - steamSizePx/2
			v.fill(screen, x, y, steamSizePx, steamSizePx, tr*float32(colText.R)/255,
				tg*float32(colText.G)/255, tb*float32(colText.B)/255, p[2])
		}
	}

	// 7) 스탠드 빛 베일과 먼지(콘 영역)
	if w, h := rectSize(l.Lamp.Cone); w >= 1 && h >= 1 {
		u := v.sh.lampU
		v.sh.lampCenter[0] = float32(l.Lamp.Bulb[0] + s.lampShakeOff[0])
		v.sh.lampCenter[1] = float32(l.Lamp.Bulb[1] + s.lampShakeOff[1])
		u["Radius"] = float32(l.Lamp.Radius)
		lampA := lerpF32(s.prevTod.lampAlpha(), s.tod.lampAlpha(), float32(s.todBlend))
		u["Brightness"] = s.lampBrightness() * lampA
		r, g, b := lampColorAt(v.pal.lampWarm, lampHot, s.lampQ)
		setColorUniform(v.sh.lampColor, color.NRGBA{R: uint8(r * 255), G: uint8(g * 255), B: uint8(b * 255), A: 255}, tr, tg, tb, 1)
		op := &v.sh.lampShop
		op.GeoM.Reset()
		op.GeoM.Translate(l.Lamp.Cone[0], l.Lamp.Cone[1])
		screen.DrawRectShader(w, h, v.sh.lamp, op)

		v.sh.dustU["Time"] = float32(s.t)
		v.sh.dustSize[0] = float32(w)
		v.sh.dustSize[1] = float32(h)
		setColorUniform(v.sh.dustColor, dustColor, tr, tg, tb, 1)
		dop := &v.sh.dustShop
		dop.GeoM.Reset()
		dop.GeoM.Translate(l.Lamp.Cone[0], l.Lamp.Cone[1])
		screen.DrawRectShader(w, h, v.sh.dust, dop)
	}

	// 8) 고양이·캐릭터(포즈 스프라이트가 없으면 플레이트 서브이미지 변형)
	var catPose *ebiten.Image
	if int(s.cat.action) < len(v.catPoses) {
		catPose = v.catPoses[s.cat.action]
	}
	v.drawActor(screen, v.catSub, catPose, l.Cat.Rect, l.Cat.Anchor,
		s.cat.dx, s.cat.dy, s.cat.ang, s.cat.scale, tr, tg, tb)
	var charPose *ebiten.Image
	if len(v.charPoses) > 0 {
		charPose = v.charPoses[0] // 0번 = 기본(RoomActor 계약)
	}
	v.drawActor(screen, v.charSub, charPose, l.Character.Rect, l.Character.Anchor,
		s.char.dx, s.char.dy, s.char.ang, s.char.scale, tr, tg, tb)

	// 9) 기기 정적 표시 — 스코프·스텝 LED(CleanScreen에서는 숨긴다)
	if !s.clean {
		v.drawScope(screen, tr, tg, tb)
		if v.ledBase != nil {
			v.blit(screen, v.ledBase, v.ledBaseX, v.ledBaseY, tr, tg, tb, 1)
			if len(v.ledLit) > 0 && s.lastStep >= 0 {
				i := s.lastStep % len(v.ledLit)
				if img := v.ledLit[i]; img != nil {
					v.blit(screen, img, v.ledLitX[i], v.ledLitY[i], tr, tg, tb, 1)
				}
			}
		}
		// 10) 시드 단어(ASCII만 — 한글 등은 DOM 오버레이가 그린다)
		if v.seedImg != nil {
			v.blit(screen, v.seedImg, l.SeedText[0], l.SeedText[1], tr, tg, tb, 1)
		}
		v.drawHints(screen, tr, tg, tb)
	}
}

// drawHints — 첫 힌트: !Started면 "TAP"+링, Started 뒤 15~60초 무접촉이면 기기 외곽 펄스.
func (v *View) drawHints(dst *ebiten.Image, tr, tg, tb float32) {
	s := &v.st
	if !s.started {
		cx, cy := v.layout.Device.Center()
		a := float32(0.75 + 0.25*math.Sin(2*math.Pi*s.t)) // 1Hz 0.5..1.0
		if v.tapImg != nil {
			v.blit(dst, v.tapImg, cx-v.tapCX, cy-v.tapCY, tr, tg, tb, a)
		}
		if v.ringImg != nil {
			off := hintRingR + hintStrokePx
			v.blit(dst, v.ringImg, cx-off, cy-off, tr, tg, tb, 1)
		}
		return
	}
	if v.hintImg == nil || s.now < hintIdleMinSec || s.now > hintIdleMaxSec ||
		s.everTouched || s.manualLocked {
		return
	}
	a := float32(0.75 + 0.25*math.Sin(2*math.Pi*s.t))
	dr := v.layout.Device
	v.blit(dst, v.hintImg, dr[0]-2.5, dr[1]-2.5, tr, tg, tb, a)
}

// drawScope — 기기의 작은 스코프. Bridge.Scope 256샘플 폴리라인(굵기 1, #7FE08A α0.8).
// 정점 버퍼는 고정 배열 — 프레임 할당 0.
func (v *View) drawScope(dst *ebiten.Image, tr, tg, tb float32) {
	r := v.layout.Scope
	if !v.scopeOn || r[2] < 2 || r[3] < 2 {
		return
	}
	x0, ymid := r[0], r[1]+r[3]/2
	amp := r[3] / 2 * 0.9
	for i := 0; i < scopeSamps; i++ {
		f := math.Float32frombits(binary.LittleEndian.Uint32(v.scopeBuf[i*4 : i*4+4]))
		if f > 1 {
			f = 1
		} else if f < -1 {
			f = -1
		}
		v.scopePts[i*2] = float32(x0 + r[2]*float64(i)/(scopeSamps-1))
		v.scopePts[i*2+1] = float32(ymid - float64(f)*amp)
	}
	// 정점 색은 DrawTrianglesOptions에 ColorScale이 없어 정점에 직접(틴트 곱해 제자리).
	cr := float32(colScope.R) / 255 * tr
	cg := float32(colScope.G) / 255 * tg
	cb := float32(colScope.B) / 255 * tb
	ca := float32(colScope.A) / 255
	for i := 0; i < scopeSegs; i++ {
		ax, ay := v.scopePts[i*2], v.scopePts[i*2+1]
		bx, by := v.scopePts[i*2+2], v.scopePts[i*2+3]
		dx, dy := bx-ax, by-ay
		n := math.Sqrt(float64(dx*dx + dy*dy))
		var nx, ny float32
		if n < 1e-6 {
			nx, ny = 0, scopeHalfW
		} else {
			nx, ny = -dy/float32(n)*scopeHalfW, dx/float32(n)*scopeHalfW
		}
		b := i * 4
		v.scopeV[b].DstX, v.scopeV[b].DstY = ax+nx, ay+ny
		v.scopeV[b+1].DstX, v.scopeV[b+1].DstY = ax-nx, ay-ny
		v.scopeV[b+2].DstX, v.scopeV[b+2].DstY = bx+nx, by+ny
		v.scopeV[b+3].DstX, v.scopeV[b+3].DstY = bx-nx, by-ny
		for k := 0; k < 4; k++ {
			v.scopeV[b+k].ColorR, v.scopeV[b+k].ColorG = cr, cg
			v.scopeV[b+k].ColorB, v.scopeV[b+k].ColorA = cb, ca
		}
	}
	dst.DrawTriangles(v.scopeV[:], v.scopeI[:], v.white, &v.trop)
}

// blit — 옵션 재사용 이미지 그리기. ColorScale은 프리멀티 값에 적용되므로(v2.9 계약,
// image.go "ColorM.Scale(r,g,b,a) == ColorScale.Scale(r·a, g·a, b·a, a)") 알파를 rgb에
// 접어 넣는다 — 안 접으면 src가 투명해지지 않고 dst 위에 그대로 얹혀 순백으로 포화한다.
func (v *View) blit(dst, src *ebiten.Image, x, y float64, tr, tg, tb, a float32) {
	v.op.GeoM.Reset()
	v.op.GeoM.Translate(x, y)
	v.op.ColorScale.Reset()
	v.op.ColorScale.Scale(tr*a, tg*a, tb*a, a)
	dst.DrawImage(src, &v.op)
	v.draws++
}

// fill — 흰 1×1을 확대한 채움 사각. 알파 접기 계약은 blit과 같다.
func (v *View) fill(dst *ebiten.Image, x, y, w, h float64, tr, tg, tb, a float32) {
	v.op.GeoM.Reset()
	v.op.GeoM.Scale(w, h)
	v.op.GeoM.Translate(x, y)
	v.op.ColorScale.Reset()
	v.op.ColorScale.Scale(tr*a, tg*a, tb*a, a)
	dst.DrawImage(v.white, &v.op)
	v.draws++
}

// actorGeoM — 배우 변환(서브이미지·포즈 공용): rect 위치에 놓고 앵커 기준으로
// 회전·스케일한 뒤 앵커+변위로 옮긴다. GeoM 호출 순서 = 점 적용 순서이므로
// Translate(r−a) → Rotate → Scale → Translate(a+d)가 "앵커를 원점에 모아 변형"이다.
func actorGeoM(g *ebiten.GeoM, poseSize [2]int, r core.Rect, anchor [2]float64,
	dx, dy, ang, scale float64) {
	if poseSize[0] > 0 && poseSize[1] > 0 { // 포즈 스프라이트를 rect에 맞춘다
		g.Scale(r[2]/float64(poseSize[0]), r[3]/float64(poseSize[1]))
	}
	g.Translate(r[0]-anchor[0], r[1]-anchor[1])
	if ang != 0 {
		g.Rotate(ang)
	}
	if scale != 1 {
		g.Scale(scale, scale)
	}
	g.Translate(anchor[0]+dx, anchor[1]+dy)
}

// drawActor — 플레이트 서브이미지(또는 포즈 스프라이트)를 앵커 기준 변형해 그린다.
// 포즈가 없고 변환이 항등이면 그리지 않는다(플레이트에 이미 있다). 서브이미지도
// GeoM 원점(=rect 좌상단)에 배치해야 제자리에 그려진다 — 원점을 빼면 (0,0)에 복제된다.
func (v *View) drawActor(dst, sub, pose *ebiten.Image, r core.Rect, anchor [2]float64,
	dx, dy, ang, scale float64, tr, tg, tb float32) {
	src := sub
	if pose != nil {
		src = pose
	}
	if src == nil {
		return
	}
	if pose == nil && dx == 0 && dy == 0 && ang == 0 && scale == 1 {
		return
	}
	var ps [2]int
	if pose != nil {
		w, h := pose.Size()
		ps = [2]int{w, h}
	}
	v.op.GeoM.Reset()
	actorGeoM(&v.op.GeoM, ps, r, anchor, dx, dy, ang, scale)
	v.op.ColorScale.Reset()
	v.op.ColorScale.Scale(tr, tg, tb, 1)
	dst.DrawImage(src, &v.op)
	v.draws++
}

// rebuildWinCache — 창 알파가 변했을 때만 다시 그린다(200ms 페이드 동안만 더럽다).
func (v *View) rebuildWinCache() {
	s := &v.st
	dirty := false
	for i := range s.winAlpha {
		if d := s.winAlpha[i] - v.winCached[i]; d > 1.0/255 || d < -1.0/255 {
			dirty = true
			break
		}
	}
	if !dirty {
		return
	}
	v.winImg.Clear()
	for i, a := range s.winAlpha {
		v.winCached[i] = a
		if a <= 0.004 {
			continue
		}
		r := v.layout.Windows[i]
		v.op.GeoM.Reset()
		v.op.GeoM.Scale(r[2], r[3])
		v.op.GeoM.Translate(r[0]-v.winX, r[1]-v.winY)
		v.op.ColorScale.Reset()
		// 알파 접기(blit 계약) — 캐시 텍스처에 프리멀 값으로 남아야 나중 blit이 옳다.
		v.op.ColorScale.Scale(float32(v.pal.windowLit.R)/255*a, float32(v.pal.windowLit.G)/255*a,
			float32(v.pal.windowLit.B)/255*a, a)
		v.winImg.DrawImage(v.white, &v.op)
	}
}

// recIdentityResting — 순서가 원래대로고 페이드도 끝났으면 플레이트가 이미 옳다.
func (v *View) recIdentityResting() bool {
	s := &v.st
	if s.recFadeK < 1 {
		return false
	}
	for i, o := range s.recOrder {
		if o != i {
			return false
		}
	}
	return true
}

// rebuildRecCache — 레코드 배치가 변할 때만(Phase 교체 + 300ms 크로스페이드 동안).
func (v *View) rebuildRecCache() {
	s := &v.st
	o0 := -1
	if len(s.recOrder) > 0 {
		o0 = s.recOrder[0]
	}
	if o0 == v.recCacheO0 && s.recFadeK == v.recCacheK {
		return
	}
	v.recCacheO0, v.recCacheK = o0, s.recFadeK
	v.recImg.Clear()
	a := float32(1)
	if s.recFadeK < 1 {
		a = float32(s.recFadeK)
	}
	for slot, srcIdx := range s.recOrder {
		src := v.recSub[srcIdx]
		if src == nil {
			continue
		}
		dstR := v.layout.Records[slot]
		srcR := v.layout.Records[srcIdx]
		v.op.GeoM.Reset()
		v.op.GeoM.Translate(dstR[0]-srcR[0], dstR[1]-srcR[1])
		v.op.ColorScale.Reset()
		v.op.ColorScale.Scale(a, a, a, a) // 알파 접기 — 크로스페이드가 프리멀로 감쇠한다
		v.recImg.DrawImage(src, &v.op)
	}
}

// ----------------------------------------------------------------------------
// 잡헬퍼

func rectSize(r core.Rect) (int, int) { return int(r[2]), int(r[3]) }

func bboxOf(rects []core.Rect) (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, r := range rects {
		x0, y0 = math.Min(x0, r[0]), math.Min(y0, r[1])
		x1, y1 = math.Max(x1, r[0]+r[2]), math.Max(y1, r[1]+r[3])
	}
	return
}

func newOffscreen(w, h int) *ebiten.Image {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return ebiten.NewImage(w, h)
}

// parseHex — "#rrggbb" → NRGBA. 못 읽으면 스펙 기본색.
func parseHex(s string, fallback color.NRGBA) color.NRGBA {
	if len(s) != 7 || s[0] != '#' {
		return fallback
	}
	var v [3]byte
	for i := 0; i < 3; i++ {
		hi, ok1 := hexVal(s[1+i*2])
		lo, ok2 := hexVal(s[2+i*2])
		if ok1 || ok2 {
			return fallback
		}
		v[i] = hi<<4 | lo
	}
	return color.NRGBA{R: v[0], G: v[1], B: v[2], A: 255}
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', false
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, false
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, false
	}
	return 0, true
}

// lampColorAt — 컷오프 q에 따른 색온도 보간(warm #F2B866 → hot #FFF1D6, 선형).
func lampColorAt(warm, hot color.NRGBA, q float32) (r, g, b float32) {
	wr, wg, wb := float32(warm.R)/255, float32(warm.G)/255, float32(warm.B)/255
	hr, hg, hb := float32(hot.R)/255, float32(hot.G)/255, float32(hot.B)/255
	return lerpF32(wr, hr, q), lerpF32(wg, hg, q), lerpF32(wb, hb, q)
}

// setColorUniform — [r,g,b,a] 유니폼(틴트 곱해 제자리 갱신).
func setColorUniform(dst []float32, c color.NRGBA, tr, tg, tb, a float32) {
	dst[0] = float32(c.R) / 255 * tr
	dst[1] = float32(c.G) / 255 * tg
	dst[2] = float32(c.B) / 255 * tb
	dst[3] = a
}

// isASCIIWord — 시드 단어를 폰트로 새길 수 있는가(ASCII 인쇄 가능 문자만).
func isASCIIWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] > 126 {
			return false
		}
	}
	return true
}
