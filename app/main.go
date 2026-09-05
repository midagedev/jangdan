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
}

func newGame() (*game, error) {
	g := &game{start: time.Now()}
	g.last = g.start
	g.ctx.Bridge = core.NewBridge()
	g.ctx.Font = core.NewFontSet(assets.FontAtlasPNG, assets.FontAtlasJSON)
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
	g, err := newGame()
	if err != nil {
		panic(err)
	}
	if err := ebiten.RunGame(g); err != nil {
		panic(err)
	}
}
