// 장단(Jangdan) — Ebitengine 앱 진입점·배선. 리드 소유(docs/impl-plan-2026-09-05.md §7).
//
// 빌드(브라우저): bash app/build.sh → app/web/app.wasm(+gz). 데스크톱에서도 같은 게임이 돈다
// (호스트 없음 → core.NewBridge가 no-op). 뷰는 view/room(기본)과 view/device(기기 탭)이고,
// 뷰는 core만 의존한다. 레지던트·세션 로그 배선은 통합 라운드에서 이 파일에 얹는다.
package main

import (
	"runtime"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/midagedev/revirth/app/assets"
	"github.com/midagedev/revirth/app/core"
	"github.com/midagedev/revirth/app/view/device"
	"github.com/midagedev/revirth/app/view/room"
	"github.com/midagedev/revirth/engine"
	"github.com/midagedev/revirth/resident"
	"github.com/midagedev/revirth/session"
)

type mode uint8

const (
	modeRoom mode = iota
	modeDevice
)

type game struct {
	ctx    core.Ctx
	room   *room.View
	device *device.View
	mode   mode

	start      time.Time
	last       time.Time
	frameStart time.Time
	lastMs     float64
	frame      int
	firstDrawn bool
	memBase    uint64
	memValid   bool
	touchIDs   []ebiten.TouchID
	pointers   []core.Pointer
	audioOn    bool

	in integ
}

// integ — 레지던트·세션 배선 상태(계약: docs/impl-plan-2026-09-05.md §6·§7).
type integ struct {
	res       *resident.Resident
	log       *session.Log // 정본 로그는 호스트(JS)가 갖고, Go 쪽 미러는 URL 인코딩·리플레이 후보 계산용
	started   bool
	startNow  float64 // 오디오 시작 시각(ctx.Now)
	lastBar   uint32
	firstKnob bool
	wallDrop  int // 마지막으로 벽시계 드롭을 보낸 시(hour), -1 = 없음

	// 공유 URL 재생(?s=): 디코드된 엔트리를 블록 순서로 엔진에 보낸다. 재생이 남아 있는 동안
	// 로컬 레지던트는 쉰다(로그 안에 만든 사람의 레지던트 궤적이 이미 들어 있다). 끝나면 레지던트가 이어 튼다.
	shared    []session.Entry
	sharedIdx int
}

func newInteg(word string) integ {
	seed := session.SeedFromWord(word)
	return integ{
		res:      resident.New(seed, resident.DeepFocus, resident.Config{FocusMin: 25, RestMin: 5, DemoFocusMin: 5}),
		log:      &session.Log{Seed: seed, Word: word},
		wallDrop: -1,
	}
}

// send — 엔진에 닿는 모든 Cmd는 이 한 곳을 지난다(브리지 전송 + Go 미러 로그).
func (g *game) send(c engine.Cmd, a core.Author) {
	g.ctx.Bridge.Cmd(c, a)
	g.in.log.Append(uint32(g.ctx.Tick.Block)+2, session.Author(a), c)
}

// updateIntegration — Tick 갱신 직후. 사용자 손→잠금, 레지던트 틱→Cmd, 방 뷰 연출 입력, 벽시계 드롭.
func (g *game) updateIntegration() {
	t := g.ctx.Tick
	if !t.Started {
		return
	}
	if !g.in.started {
		g.in.started = true
		g.in.startNow = g.ctx.Now
		g.in.lastBar = t.Bar
	}
	if id, ok := g.device.JustGrabbed(); ok {
		g.in.res.Lock(id)
		if !g.in.firstKnob {
			g.in.firstKnob = true
			g.ctx.Bridge.Telemetry("first_knob_ms", (g.ctx.Now-g.in.startNow)*1000)
		}
	}
	if g.device.ResumeTapped() {
		g.in.res.Resume()
	}
	if g.device.DropTapped() {
		g.ctx.Bridge.Telemetry("drop", 1)
	}
	barStart := t.Flags&engine.FlagBar != 0 || t.Bar != g.in.lastBar
	g.in.lastBar = t.Bar
	if g.in.sharedIdx < len(g.in.shared) {
		// 공유 로그 재생: 현재 블록 + 8블록(≈21ms) 안에 든 엔트리를 순서대로 보낸다(호스트가 +2로 예약).
		horizon := uint32(t.Block) + 8
		for g.in.sharedIdx < len(g.in.shared) && g.in.shared[g.in.sharedIdx].Block <= horizon {
			e := g.in.shared[g.in.sharedIdx]
			g.ctx.Bridge.Cmd(e.Cmd, core.Replay) // Go 미러 로그에는 넣지 않는다(재공유 시 중복 방지)
			if e.Cmd.Kind == engine.SetParam {
				g.ctx.ResidentHand, g.ctx.ResidentHandOn = engine.ParamID(e.Cmd.A), true
			}
			g.in.sharedIdx++
		}
		if g.in.sharedIdx >= len(g.in.shared) {
			g.in.shared = nil // 끝 — 다음 프레임부터 로컬 레지던트
		}
		return
	}
	for _, c := range g.in.res.Tick(resident.Input{Bar: t.Bar, Step: t.Step, Now: g.ctx.Now - g.in.startNow, BarStart: barStart}) {
		g.send(c, core.Resident)
	}
	g.ctx.ResidentHand, g.ctx.ResidentHandOn = g.in.res.Hand()
	g.ctx.Energy = g.in.res.Energy()
	g.ctx.Phase = uint8(g.in.res.Phase())
	ph, remain, _ := g.in.res.Pomodoro()
	g.ctx.PomodoroRest = ph == resident.Rest
	g.ctx.PomodoroRemainSec = remain
	g.ctx.ManualLocked = false
	for i := 0; i < int(engine.NumParams); i++ {
		if g.in.res.Locked(engine.ParamID(i)) {
			g.ctx.ManualLocked = true
			break
		}
	}
	// 벽시계 드롭: 매시 정각(3초 창) 1회. 서버 없이 접속 중인 모든 레지던트가 함께 떨어진다.
	if h, m, s := g.ctx.Bridge.WallClock(); m == 0 && s < 3 && g.in.wallDrop != h {
		g.in.wallDrop = h
		g.send(engine.Cmd{Kind: engine.Drop}, core.System)
	}
}

func newGame() (*game, error) {
	g := &game{start: time.Now()}
	g.last = g.start
	g.ctx.Bridge = core.NewBridge()
	g.ctx.Font = core.NewFontSet(assets.FontAtlasPNG, assets.FontAtlasJSON)
	g.in = newInteg(g.ctx.Bridge.SeedWord())
	if l := sharedLog(); l != nil {
		g.in.shared = l.Entries
	}
	var err error
	if g.room, err = room.New(&g.ctx); err != nil {
		return nil, err
	}
	if g.device, err = device.New(&g.ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *game) Layout(w, h int) (int, int) { return core.LogicalW, core.LogicalH }

// collectPointers — 마우스 + 터치를 논리 좌표 Pointer로 모은다(슬라이스 재사용, 프레임당 할당 0 목표).
func (g *game) collectPointers() {
	g.pointers = g.pointers[:0]
	mx, my := ebiten.CursorPosition()
	pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	just := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft)
	rel := inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft)
	if pressed || just || rel {
		g.pointers = append(g.pointers, core.Pointer{ID: -1, X: float64(mx), Y: float64(my), JustPressed: just, Pressed: pressed, JustReleased: rel})
	}
	g.touchIDs = inpututil.AppendJustPressedTouchIDs(g.touchIDs[:0])
	nJust := len(g.touchIDs)
	g.touchIDs = ebiten.AppendTouchIDs(g.touchIDs)
	for _, id := range g.touchIDs[nJust:] {
		x, y := ebiten.TouchPosition(id)
		just := false
		for _, j := range g.touchIDs[:nJust] {
			if j == id {
				just = true
				break
			}
		}
		g.pointers = append(g.pointers, core.Pointer{ID: int(id), X: float64(x), Y: float64(y), JustPressed: just, Pressed: true})
	}
	g.touchIDs = inpututil.AppendJustReleasedTouchIDs(g.touchIDs[:0])
	for _, id := range g.touchIDs {
		x, y := inpututil.TouchPositionInPreviousTick(id)
		g.pointers = append(g.pointers, core.Pointer{ID: int(id), X: float64(x), Y: float64(y), JustReleased: true})
	}
	g.ctx.Pointers = g.pointers
}

func (g *game) Update() error {
	g.frameStart = time.Now()
	g.ctx.DT = g.frameStart.Sub(g.last).Seconds()
	g.last = g.frameStart
	g.ctx.Now = g.frameStart.Sub(g.start).Seconds()
	g.collectPointers()

	// 첫 제스처에 오디오 시작(호스트가 300ms 안에 소리를 낸다 — 게이트는 host.js 텔레메트리).
	if !g.audioOn {
		for _, p := range g.pointers {
			if p.JustPressed {
				g.ctx.Bridge.Start()
				g.audioOn = true
				break
			}
		}
	}
	g.ctx.Tick = g.ctx.Bridge.Tick()
	g.ctx.CleanScreen = g.ctx.Bridge.CleanScreen()
	g.updateIntegration()

	switch g.mode {
	case modeRoom:
		g.room.Update(&g.ctx)
		if g.room.DeviceTapped() {
			g.mode = modeDevice
		}
	case modeDevice:
		g.device.Update(&g.ctx)
		if g.device.BackTapped() {
			g.mode = modeRoom
		}
	}

	// 힙 할당 계측: 60프레임마다.
	if g.frame%60 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if g.memValid {
			g.ctx.Bridge.AllocPerFrame(float64(m.TotalAlloc-g.memBase) / 60)
		}
		g.memBase, g.memValid = m.TotalAlloc, true
	}
	g.frame++
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	// 백그라운드 탭: 렌더 정지(오디오는 워클릿이 이어 간다). Ebitengine은 hidden에서 rAF가 멎지만 안전망으로 한 번 더.
	if g.ctx.Bridge.Hidden() {
		return
	}
	switch g.mode {
	case modeRoom:
		g.room.Draw(screen, &g.ctx)
	case modeDevice:
		g.device.Draw(screen, &g.ctx)
	}
	if !g.firstDrawn {
		g.firstDrawn = true
		g.ctx.Bridge.FirstFrame()
	}
	g.lastMs = float64(time.Since(g.frameStart).Microseconds()) / 1000.0
	g.ctx.Bridge.Frame(g.lastMs)
}

func main() {
	ebiten.SetWindowSize(core.LogicalW/2, core.LogicalH/2)
	ebiten.SetWindowTitle("장단 / Jangdan")
	// TPS를 FPS에 동기 — 120Hz 화면에서 기본 TPS 60이면 Update가 프레임을 건너뛴다(스파이크 실측).
	ebiten.SetTPS(ebiten.SyncWithFPS)
	// 큰 PNG는 wasm 밖에서 온다(assets 패키지 주석) — 호스트 prefetch 완료까지 대기(데스크톱은 즉시).
	assets.WaitReady()
	g, err := newGame()
	if err != nil {
		panic(err)
	}
	installShare(g)
	if err := ebiten.RunGame(g); err != nil {
		panic(err)
	}
}
