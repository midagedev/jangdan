// 장단(Jangdan) — Ebitengine 기기 뷰 UI 스파이크.
//
// 빌드(브라우저): bash app/build.sh → 표준 Go wasm(GOOS=js GOARCH=wasm).
// 데스크톱(darwin 등)에서도 같은 게임이 돈다 — JS 의존은 bridge_js.go로만
// 격리돼 있어 `go vet ./app/...`이 그대로 통과한다.
// 좌표의 단일 소유자는 layout.go. 스프라이트는 assets 계약 파일명(플레이스홀더 →
// fal.ai 생성물로 교체, 코드 무수정). 엔진 워클릿은 spike/worklet 재사용.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"runtime"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/midagedev/revirth/app/assets"
)

// Bridge — 브라우저 계측·오디오 다리. 구현은 bridge_js.go(js/wasm)와
// bridge_desktop.go(그 외, no-op)뿐이다.
type Bridge interface {
	SetParam(id int, v float32) // 엔진 워클릿으로 파라미터 전달
	Scope(dst []byte) bool      // AnalyserNode 파형 256 Float32(1024바이트) 복사
	Clock() float64             // 오디오 시계(초) — 스텝 lit 이동용
	Frame(ms float64)           // Update 시작~Draw 끝 시간
	FirstFrame()
	AllocPerFrame(bytes float64) // 60프레임 평균 힙 할당
	KnobDrag(id int, v float32)  // 드래그 실측(계측 검증용)
}

// 플레이스홀더 그레이 — 새 룩 발명 금지 계약. 벡터 보조 요소(파형·미연결 점)에만 쓴다.
var (
	colScopeWave = color.RGBA{210, 210, 210, 255}
	colUnconnDot = color.RGBA{150, 150, 150, 255}
)

type game struct {
	panel, knob, pad, padLit, step, stepLit, scopeFrame *ebiten.Image

	knobVal  [KnobCount]float32
	padLitMs [PadCount]float64
	curStep  int

	mouseKnob int // 마우스가 캡처 중인 노브(-1 없음)
	mouseY    int
	touchKnob map[ebiten.TouchID]int
	touchY    map[ebiten.TouchID]int
	touchJust [16]ebiten.TouchID

	scopeBytes [ScopeSamples * 4]byte
	scopeValid bool

	path       vector.Path
	strokeOpts vector.StrokeOptions

	// 그리기 옵션 재사용 — DrawImage는 op를 호출 시점에 동기로 읽으므로 안전하다.
	// 프레임당 &DrawImageOptions{} 45개가 새로 생기면 힙 할당 목표(≤2KB/프레임)를 깬다.
	op     ebiten.DrawImageOptions // 패드·스텝·스코프 프레임(이동만)
	knobOp ebiten.DrawImageOptions // 노브(스케일 없음, 회전만)
	pathOp vector.DrawPathOptions  // 스코프 파형

	frame          int
	frameStart     time.Time
	lastFrameMs    float64
	firstFrameSent bool

	memBase  uint64
	memValid bool

	bridge Bridge
}

func newGame() (*game, error) {
	g := &game{
		mouseKnob: -1,
		curStep:   -1,
		bridge:    newBridge(),
		touchKnob: map[ebiten.TouchID]int{},
		touchY:    map[ebiten.TouchID]int{},
	}
	var err error
	for _, s := range []struct {
		dst  **ebiten.Image
		src  []byte
		w, h int
	}{
		{&g.panel, assets.PanelPNG, LogicalW, LogicalH},
		{&g.knob, assets.KnobPNG, KnobSize, KnobSize},
		{&g.pad, assets.PadPNG, PadSize, PadSize},
		{&g.padLit, assets.PadLitPNG, PadSize, PadSize},
		{&g.step, assets.StepPNG, StepSize, StepSize},
		{&g.stepLit, assets.StepLitPNG, StepSize, StepSize},
		{&g.scopeFrame, assets.ScopeFramePNG, ScopeW, ScopeH},
	} {
		if *s.dst, err = scaleTo(s.src, s.w, s.h); err != nil {
			return nil, err
		}
	}
	for i := range g.knobVal {
		g.knobVal[i] = 0.5 // 엔진 기본값과 동일(세트 초기 전송 불필요)
	}
	return g, nil
}

// scaleTo — 소스 스프라이트를 표시 크기로 미리 스케일해 둔다(프레임당 변환 최소화).
func scaleTo(src []byte, w, h int) (*ebiten.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	dst := ebiten.NewImage(w, h)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()))
	dst.DrawImage(ebiten.NewImageFromImage(img), op)
	return dst, nil
}

func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return LogicalW, LogicalH
}

func (g *game) Update() error {
	g.frameStart = time.Now()
	g.handleInput()

	// 스텝 시계: UI 자체 시계(오디오 컨텍스트). 정확 동기는 다음 라운드.
	if s := int(math.Floor(g.bridge.Clock()/StepSecond)) % StepCount; s != g.curStep {
		g.curStep = s
	}
	for i := range g.padLitMs {
		if g.padLitMs[i] > 0 {
			g.padLitMs[i] -= g.lastFrameMs
		}
	}
	g.scopeValid = g.bridge.Scope(g.scopeBytes[:])

	// 힙 할당 계측: 60프레임마다 한 번(첫 표본은 워밍업 제외 베이스라인).
	if g.frame%60 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if g.memValid {
			g.bridge.AllocPerFrame(float64(m.TotalAlloc-g.memBase) / 60)
		}
		g.memBase, g.memValid = m.TotalAlloc, true
	}
	return nil
}

func (g *game) handleInput() {
	// 마우스: 좌클릭 드래그(노브) 또는 탭(패드).
	cx, cy := ebiten.CursorPosition()
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		if k := g.knobAt(cx, cy); k >= 0 {
			g.mouseKnob, g.mouseY = k, cy
		} else if p := g.padAt(cx, cy); p >= 0 {
			g.padLitMs[p] = PadLitMs
		}
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && g.mouseKnob >= 0 {
		g.dragKnob(g.mouseKnob, g.mouseY-cy) // 위로 드래그 = 증가
		g.mouseY = cy
	}
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
		g.mouseKnob = -1
	}

	// 터치: ID별 캡처로 멀티터치 2개 동시 노브를 지원한다.
	just := inpututil.AppendJustPressedTouchIDs(g.touchJust[:0])
	for _, id := range just {
		x, y := ebiten.TouchPosition(id)
		if k := g.knobAt(x, y); k >= 0 {
			g.touchKnob[id], g.touchY[id] = k, y
		} else if p := g.padAt(x, y); p >= 0 {
			g.padLitMs[p] = PadLitMs
		}
	}
	for id := range g.touchKnob {
		x, y := ebiten.TouchPosition(id)
		if x < 0 && y < 0 { // 뗀 터치 — TouchPosition이 (-1,-1)
			delete(g.touchKnob, id)
			delete(g.touchY, id)
			continue
		}
		g.dragKnob(g.touchKnob[id], g.touchY[id]-y)
		g.touchY[id] = y
	}
}

// knobAt — 점(논리 좌표)이 닿는 노브 번호, 없으면 -1.
func (g *game) knobAt(x, y int) int {
	const reach = KnobSize/2 + 8
	for i := 0; i < KnobCount; i++ {
		kx, ky := KnobCenter(i)
		if math.Abs(float64(x)-kx) <= reach && math.Abs(float64(y)-ky) <= reach {
			return i
		}
	}
	return -1
}

// padAt — 점이 닿는 패드 번호, 없으면 -1.
func (g *game) padAt(x, y int) int {
	const reach = PadSize/2 + 8
	for r := 0; r < PadRows; r++ {
		for c := 0; c < PadCols; c++ {
			px, py := PadCenter(c, r)
			if math.Abs(float64(x)-px) <= reach && math.Abs(float64(y)-py) <= reach {
				return r*PadCols + c
			}
		}
	}
	return -1
}

// knobParam — 노브 i가 연결된 엔진 ParamID(engine.go 순서). -1 = 미연결.
// 행 0 = Cutoff..Tempo(0..5), 행 1은 베이스라인 B 자리라 BDLevel·CHLevel만 연결.
func knobParam(i int) int {
	switch i {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		return i
	default:
		return -1
	}
}

func (g *game) dragKnob(i, dy int) {
	v := g.knobVal[i] + float32(dy)/KnobDragPx
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	if v == g.knobVal[i] {
		return
	}
	g.knobVal[i] = v
	if p := knobParam(i); p >= 0 {
		g.bridge.SetParam(p, v)
	}
	g.bridge.KnobDrag(i, v)
}

func (g *game) Draw(screen *ebiten.Image) {
	screen.DrawImage(g.panel, nil)
	g.drawScope(screen)
	g.drawKnobs(screen)
	g.drawPads(screen)
	g.drawSteps(screen)

	if !g.firstFrameSent {
		g.firstFrameSent = true
		g.bridge.FirstFrame()
	}
	g.lastFrameMs = float64(time.Since(g.frameStart).Microseconds()) / 1000.0
	g.bridge.Frame(g.lastFrameMs)
}

func (g *game) drawScope(screen *ebiten.Image) {
	g.op.GeoM.Reset()
	g.op.GeoM.Translate(float64(ScopeCX-ScopeW/2), float64(ScopeCY-ScopeH/2))
	screen.DrawImage(g.scopeFrame, &g.op)

	if !g.scopeValid {
		return
	}
	g.path.Reset()
	x0 := float32(ScopeCX - ScopeW/2)
	for i := 0; i < ScopeSamples; i++ {
		s := math.Float32frombits(binary.LittleEndian.Uint32(g.scopeBytes[i*4:]))
		if s < -1 {
			s = -1
		} else if s > 1 {
			s = 1
		}
		x := x0 + float32(i)*float32(ScopeW)/ScopeSamples
		y := float32(ScopeCY) + s*ScopeAmp
		if i == 0 {
			g.path.MoveTo(x, y)
		} else {
			g.path.LineTo(x, y)
		}
	}
	g.strokeOpts.Width = 1.25
	g.pathOp.ColorScale.SetR(210.0 / 255)
	g.pathOp.ColorScale.SetG(210.0 / 255)
	g.pathOp.ColorScale.SetB(210.0 / 255)
	g.pathOp.ColorScale.SetA(1)
	vector.StrokePath(screen, &g.path, &g.strokeOpts, &g.pathOp)
}

func (g *game) drawKnobs(screen *ebiten.Image) {
	for i := 0; i < KnobCount; i++ {
		x, y := KnobCenter(i)
		g.knobOp.GeoM.Reset()
		g.knobOp.GeoM.Translate(-KnobSize/2, -KnobSize/2)
		g.knobOp.GeoM.Rotate((-135 + 270*float64(g.knobVal[i])) * math.Pi / 180)
		g.knobOp.GeoM.Translate(x, y)
		screen.DrawImage(g.knob, &g.knobOp)
		if knobParam(i) < 0 { // 미연결 표시
			vector.FillCircle(screen, float32(x)+KnobUnconnDotDX, float32(y)+KnobUnconnDotDX, KnobUnconnDotR, colUnconnDot, false)
		}
	}
}

func (g *game) drawPads(screen *ebiten.Image) {
	for r := 0; r < PadRows; r++ {
		for c := 0; c < PadCols; c++ {
			i := r*PadCols + c
			x, y := PadCenter(c, r)
			img := g.pad
			if g.padLitMs[i] > 0 {
				img = g.padLit
			}
			g.drawCentered(screen, img, x, y)
		}
	}
}

func (g *game) drawSteps(screen *ebiten.Image) {
	for s := 0; s < StepCount; s++ {
		x, y := StepCenter(s)
		img := g.step
		if s == g.curStep {
			img = g.stepLit
		}
		g.drawCentered(screen, img, x, y)
	}
}

func (g *game) drawCentered(screen *ebiten.Image, img *ebiten.Image, x, y float64) {
	g.op.GeoM.Reset()
	g.op.GeoM.Translate(x-float64(img.Bounds().Dx())/2, y-float64(img.Bounds().Dy())/2)
	screen.DrawImage(img, &g.op)
}

func main() {
	ebiten.SetWindowSize(LogicalW, LogicalH)
	ebiten.SetWindowTitle("장단 / Jangdan")
	// TPS를 FPS에 동기 — 120Hz 화면에서 기본 TPS 60이면 Update가 프레임을 건너뛰고,
	// frameMs(Update 시작~Draw 끝)가 갱신 대기 시간까지 삼켜 측정이 부풀어진다.
	ebiten.SetTPS(ebiten.SyncWithFPS)
	g, err := newGame()
	if err != nil {
		panic(err)
	}
	if err := ebiten.RunGame(g); err != nil {
		panic(err)
	}
}
