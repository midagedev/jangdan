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

const (
	manualIdleSec        = 30.0 // MANUAL 잡은 뒤 무접촉이 이만큼 지나면 잠금 해제 1회
	captionAfterStartSec = 20.0 // 시작 후 이 시간까지 캡션 2(기기 뷰 진입 전)
	captionDeviceSec     = 30.0 // 기기 뷰 첫 진입 후 캡션 3 상한
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

	lastHint        int     // 직전 캡션 상태(-1 = 아직 없음), 변화 시에만 Hint 호출
	deviceEntries   int     // 기기 뷰 진입 횟수 — 두 번째 진입부터 캡션 3이 없다
	deviceEnteredAt float64 // 마지막 기기 뷰 진입 시각(ctx.Now)
	deviceTouched   bool    // 기기 뷰에서 사람 Cmd·노브 잡기 관측(캡션 3 종료)
	lastTouch       float64 // 마지막 잡기·드래그 시각(ctx.Now), -1 = 없음
	idleResumed     bool    // 이 무접촉 구간에 이미 Resume을 돌렸다
	fakeLastStep    int64   // 가짜 시계가 마지막으로 본 스텝 인덱스(-1 = 없음)

	// 공유 URL 재생(?s=): 디코드된 엔트리를 블록 순서로 엔진에 보낸다. 재생이 남아 있는 동안
	// 로컬 레지던트는 쉰다(로그 안에 만든 사람의 레지던트 궤적이 이미 들어 있다). 끝나면 레지던트가 이어 튼다.
	shared     []session.Entry
	sharedIdx  int
	sharedWait bool // ?s=<id> 세션 GET이 호스트에서 진행 중 — settle할 때까지 매 프레임 재확인(§12.6)
}

func newInteg(word string) integ {
	seed := session.SeedFromWord(word)
	return integ{
		res:       resident.New(seed, resident.DeepFocus, resident.Config{FocusMin: 25, RestMin: 5, DemoFocusMin: 5}),
		log:       &session.Log{Seed: seed, Word: word},
		wallDrop:  -1,
		lastHint:  -1,
		lastTouch: -1,
	}
}

// cmdBridge — 엔진에 닿는 모든 Cmd 경로의 단일 소유자. 실제 브리지 전송에 더해 이 파일에서만
// 일어나야 할 일을 여기서 한다: (a) Go 미러 로그 append(Replay 제외 — 재공유 중복 방지),
// (b) 사람 화성 명령(SetKey·SetChord·BassMode) → 레지던트 LockHarmony, (c) 첫 Human
// SetParam 텔레메트리, (d) 기기 뷰 첫 조작 표시(캡션 3 종료). 기기 뷰가 브리지를 직접
// 호출하는 경로도 이 래퍼를 지나므로, 공유 URL이 사람 조작을 놓치는 버그 클래스가 닫힌다.
type cmdBridge struct {
	core.Bridge
	g *game
}

func (b *cmdBridge) Cmd(c engine.Cmd, a core.Author) {
	b.Bridge.Cmd(c, a)
	if a != core.Replay {
		b.g.in.log.Append(uint32(b.g.ctx.Tick.Block)+2, session.Author(a), c)
	}
	if a != core.Human {
		return
	}
	if b.g.mode == modeDevice {
		b.g.in.deviceTouched = true
	}
	switch c.Kind {
	case engine.SetKey, engine.SetChord, engine.BassMode:
		b.g.in.res.LockHarmony() // 사람이 화성을 만졌다 — 세션 내 영구(§12.2)
	}
	if c.Kind == engine.SetParam && !b.g.in.firstKnob {
		b.g.in.firstKnob = true
		b.Bridge.Telemetry("first_knob_ms", (b.g.ctx.Now-b.g.in.startNow)*1000)
	}
}

// updateIntegration — Tick 갱신 직후. 사용자 손→잠금, 레지던트 틱→Cmd, 방 뷰 연출 입력, 벽시계 드롭.
func (g *game) updateIntegration() {
	t := g.ctx.Tick
	if g.in.sharedWait {
		// ?s=<id> 세션 GET이 앱보다 늦게 도착하는 경우 — 호스트가 settle할 때까지 매 프레임
		// 재확인하고 도착하면 재생을 시작한다(그동안 레지던트는 정상 진행, 도착 후 재생 우선).
		if st := sharedLogState(); st != 0 {
			g.in.sharedWait = false
			if l := sharedLog(); l != nil {
				g.in.shared = l.Entries
				// 늦은 도착 보정: 엔트리 블록은 원 세션 시작 기준 절대값이라 현재 블록보다
				// 과거면 전체가 한 프레임에 몰아 발송된다(호스트 cmd는 항상 now+2로 예약).
				// 첫 엔트리를 현재 블록+2에 평행이동해 상대 간격만 유지한다.
				if n := len(g.in.shared); n > 0 {
					if base := uint32(t.Block) + 2; base > g.in.shared[0].Block {
						off := base - g.in.shared[0].Block
						for i := 0; i < n; i++ {
							g.in.shared[i].Block += off
						}
					}
				}
			} else if st == 1 {
				g.ctx.Bridge.Telemetry("open_failed", 1)
			}
		}
	}
	// 사용자 손·기기 뷰 반응은 오디오 시작 전에도 잡는다(시작 제스처와 진입이 같은 탭에
	// 겹치는 창이 있다 — 잠금·텔레메트리가 한 프레임 늦는 것보다 놓치는 게 나쁘다).
	if id, ok := g.device.JustGrabbed(); ok {
		g.in.res.Lock(id)
		g.in.deviceTouched = true
		g.in.lastTouch = g.ctx.Now
		g.in.idleResumed = false
	}
	if g.device.DropTapped() {
		g.ctx.Bridge.Telemetry("drop", 1)
	}
	if !t.Started {
		return // 레지던트·공유 재생은 진짜 tick만 본다(가짜 시계 이중 틱 방어)
	}
	if !g.in.started {
		g.in.started = true
		g.in.startNow = g.ctx.Now
		g.in.lastBar = t.Bar
	}
	// MANUAL 무접촉 30초 → 잠금 해제 1회. 잡거나 드래그 중이면 재무장.
	if g.mode == modeDevice {
		for i := range g.ctx.Pointers {
			if g.ctx.Pointers[i].Pressed {
				g.in.lastTouch = g.ctx.Now
				g.in.idleResumed = false
				break
			}
		}
	}
	if g.in.lastTouch >= 0 && !g.in.idleResumed && g.ctx.Now-g.in.lastTouch >= manualIdleSec {
		g.in.idleResumed = true
		g.in.res.Resume()
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
		g.ctx.Bridge.Cmd(c, core.Resident)
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
		g.ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Drop}, core.System)
	}
}

// fakeTick — 첫 제스처 전(워클릿 tick이 아직 없다) 방 뷰를 살아 있게 하는 가짜 시계.
// 템포는 기본 파라미터(130BPM)의 스텝 길이로, Step/Bar는 앱 시작 후 경과에서 유도한다.
// 스텝 0에 새로 들어간 프레임에만 FlagBar를 붙인다(연속 프레임 중복 방지). 진짜 tick이
// 도착하면 호출부가 그대로 교체한다(점프 허용 — 페이드 인이 아니라 즉시). Peak 0,
// Block 0 — 소리가 없는 시간이므로. 레지던트는 Started 게이트로 이 틱을 보지 않는다.
func (g *game) fakeTick(t core.Tick) core.Tick {
	stepDur := 60.0 / engine.BPMOf(engine.DefaultParams()[engine.Tempo]) / 4.0 // 16분음표 스텝
	total := int64(g.ctx.Now / stepDur)
	t.Playing = true
	t.Step = int(total % int64(engine.Steps))
	t.Bar = uint32(total / int64(engine.Steps))
	t.Flags = 0
	t.Peak = 0
	if total != g.in.fakeLastStep {
		if total%int64(engine.Steps) == 0 {
			t.Flags |= engine.FlagBar
		}
		g.in.fakeLastStep = total
	}
	return t
}

// updateCaption — 첫 접촉 캡션 상태기계(문구는 호스트 DOM, 여기선 상태만):
// 1 탭 전 · 2 시작 후 20초(기기 뷰 진입 전) · 3 기기 뷰 첫 진입 후 첫 조작 전(30초 상한).
// 두 번째 진입부터 3이 없다. 상태가 바뀔 때만 Hint를 건넌다(호스트 DOM 갱신 최소화).
func (g *game) updateCaption() {
	st := 0
	switch {
	case !g.ctx.Tick.Started:
		st = 1
	case g.in.deviceEntries == 0 && g.ctx.Now-g.in.startNow <= captionAfterStartSec:
		st = 2
	case g.in.deviceEntries == 1 && !g.in.deviceTouched && g.mode == modeDevice &&
		g.ctx.Now-g.in.deviceEnteredAt <= captionDeviceSec:
		st = 3
	}
	if st != g.in.lastHint {
		g.in.lastHint = st
		g.ctx.Bridge.Hint(st)
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
	} else {
		switch sharedLogState() {
		case 0:
			g.in.sharedWait = true // 호스트 GET 진행 중 — updateIntegration에서 프레임마다 재확인
		case 1:
			// 페이로드는 도착했는데 디코드가 안 된다(손상) — 일반 세션 + 실패 신호만 남긴다.
			g.ctx.Bridge.Telemetry("open_failed", 1)
		}
	}
	// 모든 Cmd는 cmdBridge를 지난다(로그·화성 잠금·캡션 종료의 단일 소유자).
	g.ctx.Bridge = &cmdBridge{Bridge: g.ctx.Bridge, g: g}
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
	if !g.ctx.Tick.Started {
		// 첫 제스처 전 가짜 시계 — 방 뷰(LED·창)가 시작 전에도 산다. 진짜 tick이 오면 교체.
		g.ctx.Tick = g.fakeTick(g.ctx.Tick)
	}
	g.ctx.CleanScreen = g.ctx.Bridge.CleanScreen()
	g.updateIntegration()

	switch g.mode {
	case modeRoom:
		g.room.Update(&g.ctx)
		if g.room.DeviceTapped() {
			g.mode = modeDevice
			g.in.deviceEntries++
			g.in.deviceEnteredAt = g.ctx.Now
		}
	case modeDevice:
		g.device.Update(&g.ctx)
		if g.device.BackTapped() {
			g.mode = modeRoom
		}
	}
	g.updateCaption()

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
