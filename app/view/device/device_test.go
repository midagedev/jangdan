// device_test.go — 계약↔단언. 헤드리스 렌더가 불가하므로 New(이미지) 없이
// newView(레이아웃만) + 기록형 fakeBridge로 상태기계를 검증한다.
// 좌표는 하드코딩하지 않고 레이아웃에서 찾은 컨트롤의 값을 쓴다(JSON이 단일 소유자).
package device

import (
	"math"
	"testing"

	"github.com/midagedev/revirth/app/assets"
	"github.com/midagedev/revirth/app/core"
	"github.com/midagedev/revirth/engine"
)

// fakeBridge — 기록형 브리지: 보낸 Cmd(시각 포함)와 파라미터·스텝·뮤트 미러.
type fakeBridge struct {
	cmds   []recCmd
	params [engine.NumParams]float32
	bass   [2][engine.Steps][2]uint8 // [note, flags]
	drum   [6][engine.Steps]uint8
	muted  [engine.NumParts]bool
	slot   [2]uint8
	now    float64
	tick   core.Tick
}

type recCmd struct {
	c engine.Cmd
	t float64
	a core.Author
}

var _ core.Bridge = (*fakeBridge)(nil)

func (f *fakeBridge) Start()                     {}
func (f *fakeBridge) Telemetry(string, float64)  {}
func (f *fakeBridge) Replay(float64)             {}
func (f *fakeBridge) SeedWord() string           { return "" }
func (f *fakeBridge) ReducedMotion() bool        { return false }
func (f *fakeBridge) Hidden() bool               { return false }
func (f *fakeBridge) CleanScreen() bool          { return false }
func (f *fakeBridge) WallClock() (int, int, int) { return 0, 0, 0 }
func (f *fakeBridge) Frame(float64)              {}
func (f *fakeBridge) FirstFrame()                {}
func (f *fakeBridge) AllocPerFrame(float64)      {}
func (f *fakeBridge) Tick() core.Tick            { return f.tick }
func (f *fakeBridge) Scope([]byte) bool          { return false }
func (f *fakeBridge) Param(id engine.ParamID) float32 {
	return f.params[id]
}

func (f *fakeBridge) BassStep(p engine.Part, step int) (uint8, uint8) {
	return f.bass[p][step][0], f.bass[p][step][1]
}

func (f *fakeBridge) DrumStep(p engine.Part, step int) uint8 { return f.drum[p-2][step] }
func (f *fakeBridge) Muted(p engine.Part) bool               { return f.muted[p] }
func (f *fakeBridge) Slot(p engine.Part) uint8               { return f.slot[p] }

func (f *fakeBridge) Cmd(c engine.Cmd, a core.Author) {
	f.cmds = append(f.cmds, recCmd{c, f.now, a})
	switch c.Kind {
	case engine.SetParam:
		if c.A < uint8(engine.NumParams) {
			f.params[c.A] = c.V
		}
	case engine.BassStep:
		if c.A <= 1 {
			f.bass[c.A][c.B&15] = [2]uint8{c.C, c.D}
		}
	case engine.DrumStep:
		if c.A >= 2 && c.A < uint8(engine.NumParts) {
			f.drum[c.A-2][c.B&15] = c.D
		}
	case engine.SelectPattern:
		if c.A <= 1 {
			f.slot[c.A] = c.B
		}
	case engine.Mute:
		if c.A < uint8(engine.NumParts) {
			f.muted[c.A] = c.B != 0
		}
	}
}

// harness — 프레임 단위 구동. 시각은 dt씩 전진(스윕·길게 누르기 판정의 원본).
type harness struct {
	v   *View
	fb  *fakeBridge
	ctx *core.Ctx
	dt  float64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	l, err := core.LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	v, err := newView(l)
	if err != nil {
		t.Fatalf("뷰 구축: %v", err)
	}
	fb := &fakeBridge{params: engine.DefaultParams()}
	return &harness{v: v, fb: fb, ctx: &core.Ctx{Bridge: fb}, dt: 1.0 / 60.0}
}

func (h *harness) frame(ptrs ...core.Pointer) {
	h.ctx.DT = h.dt
	h.ctx.Now += h.dt
	h.ctx.Pointers = ptrs
	h.fb.now = h.ctx.Now
	h.v.Update(h.ctx)
	h.ctx.Pointers = nil
}

func (h *harness) run(n int) {
	for i := 0; i < n; i++ {
		h.frame()
	}
}

// hold — 포인터를 누른 채 n프레임(길게 누르기 판정용).
func (h *harness) hold(id int, x, y float64, n int) {
	for i := 0; i < n; i++ {
		h.frame(ptrMove(id, x, y))
	}
}

func ptrPress(id int, x, y float64) core.Pointer {
	return core.Pointer{ID: id, X: x, Y: y, JustPressed: true, Pressed: true}
}

func ptrMove(id int, x, y float64) core.Pointer {
	return core.Pointer{ID: id, X: x, Y: y, Pressed: true}
}

func ptrRel(id int, x, y float64) core.Pointer {
	return core.Pointer{ID: id, X: x, Y: y, JustReleased: true}
}

func knobAt(v *View, sec uint8, name string) *knob {
	for i := range v.knobs {
		if v.knobs[i].sec == sec && v.knobs[i].name == name {
			return &v.knobs[i]
		}
	}
	return nil
}

func btnAt(v *View, sec uint8, name string) *button {
	for i := range v.buttons {
		if v.buttons[i].sec == sec && v.buttons[i].name == name {
			return &v.buttons[i]
		}
	}
	return nil
}

func padAt(v *View, name string) *padCtl {
	for i := range v.pads {
		if v.pads[i].name == name {
			return &v.pads[i]
		}
	}
	return nil
}

// setParamsFor — 특정 파라미터의 SetParam 기록만.
func (h *harness) setParamsFor(id engine.ParamID) []recCmd {
	var out []recCmd
	for _, r := range h.fb.cmds {
		if r.c.Kind == engine.SetParam && r.c.A == uint8(id) {
			out = append(out, r)
		}
	}
	return out
}

func lastCmd(h *harness) (engine.Cmd, bool) {
	if len(h.fb.cmds) == 0 {
		return engine.Cmd{}, false
	}
	return h.fb.cmds[len(h.fb.cmds)-1].c, true
}

func pressButton(h *harness, b *button) {
	cx, cy := b.rect.Center()
	h.frame(ptrPress(-1, cx, cy))
}

// — 계약↔단언: 레이아웃 파싱 —

func TestLayoutCounts(t *testing.T) {
	h := newHarness(t)
	if len(h.v.knobs) != 29 {
		t.Fatalf("노브 %d개(29 예상)", len(h.v.knobs))
	}
	if len(h.v.buttons) != 38 {
		t.Fatalf("버튼 %d개(38 예상)", len(h.v.buttons))
	}
	if len(h.v.pads) != 6 {
		t.Fatalf("패드 %d개(6 예상)", len(h.v.pads))
	}
	if len(h.v.leds) != 36 {
		t.Fatalf("LED %d개(36 예상)", len(h.v.leds))
	}
	if len(h.v.layout.Plates) != 5 {
		t.Fatalf("이름판 %d개(5 예상)", len(h.v.layout.Plates))
	}
	// 29/29 노브가 KnobParam으로 매핑됨(newView는 실패 시 에러 — 여기까지 온 것이 증거).
	for _, k := range h.v.knobs {
		if _, ok := core.KnobParam(sectionName(k.sec), k.name); !ok {
			t.Fatalf("노브 %s 매핑 실패", k.name)
		}
	}
	// 6/6 패드가 PadPart(2..7)로 매핑됨.
	for _, p := range h.v.pads {
		if _, ok := core.PadPart(p.name); !ok || p.part < engine.BD || p.part > engine.CY {
			t.Fatalf("패드 %s 파트 매핑 오류(%d)", p.name, p.part)
		}
	}
	// LED↔버튼 짝짓기 전부 성사.
	for s := 0; s < 2; s++ {
		for j, id := range h.v.secLEDs[s] {
			if id < 0 {
				t.Fatalf("섹션 %d 버튼 %d의 LED 없음", s, j)
			}
		}
	}
	for i, id := range h.v.fxLEDs {
		if id < 0 {
			t.Fatalf("스텝 %d의 LED 없음", i)
		}
	}
	if h.v.fxPlay < 0 || h.v.fxRec < 0 {
		t.Fatalf("play/rec 버튼 미발견(%d/%d)", h.v.fxPlay, h.v.fxRec)
	}
}

func sectionName(sec uint8) string {
	switch sec {
	case secBassA:
		return "basslineA"
	case secBassB:
		return "basslineB"
	case secDrums:
		return "drums"
	}
	return "fx"
}

// — 계약↔단언: 히트 —

func TestHitKnob(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secBassA, "CUTOFF")
	if h.v.hitKnob(k.cx, k.cy) < 0 {
		t.Fatal("노브 중심 미스")
	}
	if h.v.hitKnob(k.cx+k.r+hitKnobPad-0.1, k.cy) < 0 {
		t.Fatal("r+6 경계 안쪽 미스")
	}
	if h.v.hitKnob(k.cx+k.r+hitKnobPad+0.5, k.cy) >= 0 {
		t.Fatal("r+6 밖에서 히트")
	}
	tun := knobAt(h.v, secBassA, "TUNE")
	if h.v.hitKnob((tun.cx+k.cx)/2, k.cy) >= 0 {
		t.Fatal("노브 사이 빈 공간에서 히트")
	}
}

func TestHitButton(t *testing.T) {
	h := newHarness(t)
	b := btnAt(h.v, secBassA, "saw")
	if h.v.hitButton(b.rect[0]+3, b.rect[1]+3) < 0 {
		t.Fatal("rect 내부 미스")
	}
	if h.v.hitButton(b.rect[0]-hitRectPad+0.1, b.rect[1]+b.rect[3]/2) < 0 {
		t.Fatal("rect+4 경계 안쪽 미스")
	}
	if h.v.hitButton(b.rect[0]-hitRectPad-0.1, b.rect[1]+b.rect[3]/2) >= 0 {
		t.Fatal("rect+4 밖에서 히트")
	}
	if h.v.hitButton(b.rect[0]+b.rect[2]+hitRectPad+0.1, b.rect[1]+b.rect[3]+hitRectPad+0.1) >= 0 {
		t.Fatal("모서리 +4 밖에서 히트")
	}
}

// — 계약↔단언: 드래그 —

func TestDrag(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secBassA, "CUTOFF") // 기본값 0.45
	h.frame(ptrPress(-1, k.cx, k.cy))
	id, ok := h.v.JustGrabbed()
	if !ok || id != k.id {
		t.Fatalf("JustGrabbed = (%d,%v), 노브 %d 예상", id, ok, k.id)
	}
	// 위로 200px → +1.0 클램프, 프레임당 SetParam 1개.
	h.fb.cmds = nil
	h.frame(ptrMove(-1, k.cx, k.cy-200))
	sp := h.setParamsFor(k.id)
	if len(sp) != 1 {
		t.Fatalf("이동 프레임 SetParam %d개(1개 예상)", len(sp))
	}
	if math.Abs(float64(sp[0].c.V)-1) > 1e-6 {
		t.Fatalf("값 %v(1.0 클램프 예상)", sp[0].c.V)
	}
	// 값 불변 프레임 → 0개.
	h.fb.cmds = nil
	h.frame(ptrMove(-1, k.cx, k.cy-200))
	if n := len(h.setParamsFor(k.id)); n != 0 {
		t.Fatalf("무변화 프레임 SetParam %d개(0개 예상)", n)
	}
	// JustGrabbed는 잡은 프레임에만.
	if _, ok = h.v.JustGrabbed(); ok {
		t.Fatal("JustGrabbed가 두 번째 프레임에도 true")
	}
	// 아래로 200px → 0.45-1.0 → 0 클램프.
	h.frame(ptrMove(-1, k.cx, k.cy+200))
	h.frame(ptrRel(-1, k.cx, k.cy+200)) // 이동 200px ≥ 6 → 탭 아님
	if h.fb.params[k.id] != 0 {
		t.Fatalf("아래 드래그 후 %v(0 클램프 예상)", h.fb.params[k.id])
	}
	h.run(30)
	if h.fb.params[k.id] != 0 || k.swActive {
		t.Fatal("드래그 릴리스에 스윕이 시작됨(탭 아닌데)")
	}
}

// — 계약↔단언: 탭 → 2바 자동 스윕 —

func TestTapSweep(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secBassB, "CUTOFF") // 기본값 0.35 → 피크 0.85
	h.frame(ptrPress(-1, k.cx, k.cy))
	h.frame(ptrRel(-1, k.cx, k.cy)) // 이동 0, 눌림 1프레임 → 탭
	h.fb.cmds = nil
	h.run(300) // 2바(130BPM ≈ 3.69s) + 여유
	sp := h.setParamsFor(k.id)
	if len(sp) < 10 {
		t.Fatalf("스윕 송신 %d개(너무 적음)", len(sp))
	}
	for i := 1; i < len(sp); i++ {
		if d := sp[i].t - sp[i-1].t; d < 0.05-1e-6 {
			t.Fatalf("송신 간격 %v(< 50ms, %d번째)", d, i)
		}
	}
	maxI := 0
	for i, r := range sp {
		if r.c.V > sp[maxI].c.V {
			maxI = i
		}
	}
	if math.Abs(float64(sp[maxI].c.V)-0.85) > 0.02 {
		t.Fatalf("피크 %v(0.85 근사 예상)", sp[maxI].c.V)
	}
	for i := 1; i < maxI; i++ {
		if sp[i].c.V < sp[i-1].c.V-1e-6 {
			t.Fatalf("상승 구간 비단조 @%d", i)
		}
	}
	for i := maxI + 1; i < len(sp); i++ {
		if sp[i].c.V > sp[i-1].c.V+1e-6 {
			t.Fatalf("복귀 구간 비단조 @%d", i)
		}
	}
	if math.Abs(float64(sp[len(sp)-1].c.V)-0.35) > 1.0/4095 {
		t.Fatalf("최종 송신값 %v(시작값 0.35 ±1/4095 예상)", sp[len(sp)-1].c.V)
	}
	if math.Abs(float64(h.fb.params[k.id])-0.35) > 1.0/4095 {
		t.Fatalf("미러 최종값 %v(0.35 ±1/4095 예상)", h.fb.params[k.id])
	}
	if k.swActive {
		t.Fatal("스윕이 아직 활성")
	}
}

// — 계약↔단언: 버튼 매핑 —

func TestButtonMapping(t *testing.T) {
	// saw/sqr → BWave 0/1.
	h := newHarness(t)
	pressButton(h, btnAt(h.v, secBassA, "saw"))
	if c, ok := lastCmd(h); !ok || c.Kind != engine.SetParam || c.A != uint8(engine.BassAParams+engine.BWave) || c.V != 0 {
		t.Fatalf("saw → %+v", c)
	}
	pressButton(h, btnAt(h.v, secBassA, "sqr"))
	if c, _ := lastCmd(h); c.V != 1 {
		t.Fatalf("sqr → %+v", c)
	}
	// oct 3단(basslineB, 기본 0.5).
	pressButton(h, btnAt(h.v, secBassB, "oct-"))
	if c, _ := lastCmd(h); c.V != 0.15 {
		t.Fatalf("oct- → %+v(0.15 예상)", c)
	}
	pressButton(h, btnAt(h.v, secBassB, "oct-")) // 하단 유지
	if c, _ := lastCmd(h); c.V != 0.15 {
		t.Fatalf("oct- 재누름 → %+v(0.15 유지 예상)", c)
	}
	pressButton(h, btnAt(h.v, secBassB, "oct+"))
	if c, _ := lastCmd(h); c.V != 0.5 {
		t.Fatalf("oct+ → %+v(0.5 예상)", c)
	}
	pressButton(h, btnAt(h.v, secBassB, "oct+"))
	if c, _ := lastCmd(h); c.V != 0.85 {
		t.Fatalf("oct+ 재누름 → %+v(0.85 예상)", c)
	}
	// patB → SelectPattern 슬롯 1.
	pressButton(h, btnAt(h.v, secBassA, "patB"))
	if c, _ := lastCmd(h); c.Kind != engine.SelectPattern || c.A != 0 || c.B != 1 {
		t.Fatalf("patB → %+v", c)
	}
}

func TestStepToggle(t *testing.T) {
	h := newHarness(t)
	// 기본 모드 = 게이트 토글, note·다른 플래그 보존.
	h.fb.bass[0][5] = [2]uint8{24, engine.StepGate | engine.StepSlide}
	pressButton(h, btnAt(h.v, secFx, "step6"))
	if c, _ := lastCmd(h); c.Kind != engine.BassStep || c.A != 0 || c.B != 5 || c.C != 24 || c.D != engine.StepSlide {
		t.Fatalf("게이트 토글 → %+v(note 보존 예상)", c)
	}
	// slide 모드 = StepSlide 토글(게이트 보존).
	pressButton(h, btnAt(h.v, secBassA, "slide"))
	h.fb.bass[0][4] = [2]uint8{30, engine.StepGate}
	pressButton(h, btnAt(h.v, secFx, "step5"))
	if c, _ := lastCmd(h); c.Kind != engine.BassStep || c.C != 30 || c.D != engine.StepGate|engine.StepSlide {
		t.Fatalf("slide 모드 → %+v", c)
	}
	pressButton(h, btnAt(h.v, secBassA, "slide")) // 모드 끔
	if h.v.mode != emNone {
		t.Fatalf("slide 토글 해제 후 모드 %d", h.v.mode)
	}
	// acc 모드 + 베이스라인 B 이름판 탭으로 선택 파트 변경.
	pressButton(h, btnAt(h.v, secBassB, "acc"))
	cx, cy := h.v.bassPlates[1].Center()
	h.frame(ptrPress(-1, cx, cy))
	if h.v.selPart != engine.BassB {
		t.Fatalf("이름판 탭 후 선택 파트 %d", h.v.selPart)
	}
	h.fb.bass[1][7] = [2]uint8{36, 0}
	pressButton(h, btnAt(h.v, secFx, "step8"))
	if c, _ := lastCmd(h); c.Kind != engine.BassStep || c.A != 1 || c.B != 7 || c.C != 36 || c.D != engine.StepAccent {
		t.Fatalf("acc 모드(B) → %+v", c)
	}
	// 드럼 파트: 패드 탭으로 선택 → 게이트 토글, acc 모드면 StepAccent.
	pressButton(h, btnAt(h.v, secBassB, "acc")) // 모드 끔
	dcx, dcy := padAt(h.v, "CY").rect.Center()
	h.frame(ptrPress(3, dcx, dcy))
	h.frame(ptrRel(3, dcx, dcy))
	if h.v.selPart != engine.CY {
		t.Fatalf("패드 탭 후 선택 파트 %d", h.v.selPart)
	}
	h.fb.drum[5][3] = engine.StepGate
	pressButton(h, btnAt(h.v, secFx, "step4"))
	if c, _ := lastCmd(h); c.Kind != engine.DrumStep || c.A != uint8(engine.CY) || c.B != 3 || c.D != 0 {
		t.Fatalf("드럼 게이트 토글 → %+v", c)
	}
	pressButton(h, btnAt(h.v, secBassA, "acc")) // acc 모드
	h.fb.drum[5][3] = engine.StepGate
	pressButton(h, btnAt(h.v, secFx, "step4"))
	if c, _ := lastCmd(h); c.D != engine.StepGate|engine.StepAccent {
		t.Fatalf("드럼 acc 모드 → %+v", c)
	}
}

// — 계약↔단언: 패드 —

func TestPads(t *testing.T) {
	h := newHarness(t)
	p := padAt(h.v, "CH") // 파트 4
	cx, cy := p.rect.Center()
	h.frame(ptrPress(7, cx, cy))
	h.frame(ptrRel(7, cx, cy)) // 짧은 탭
	if c, _ := lastCmd(h); c.Kind != engine.Trigger || c.A != uint8(engine.CH) {
		t.Fatalf("패드 탭 → %+v", c)
	}
	if h.v.selPart != engine.CH {
		t.Fatalf("탭 후 선택 파트 %d", h.v.selPart)
	}
	if p.litUntil <= h.ctx.Now {
		t.Fatal("탭 후 lit 없음")
	}
	// 길게(≥500ms) → 뮤트 토글 B=1, 릴리스에 Trigger 없음.
	trigBefore := len(h.fb.cmds)
	h.frame(ptrPress(7, cx, cy))
	h.hold(7, cx, cy, 35) // ≈0.583s
	if c, _ := lastCmd(h); c.Kind != engine.Mute || c.A != uint8(engine.CH) || c.B != 1 {
		t.Fatalf("길게 누르기 → %+v(Mute B=1 예상)", c)
	}
	h.frame(ptrRel(7, cx, cy))
	for _, r := range h.fb.cmds[trigBefore:] {
		if r.c.Kind == engine.Trigger {
			t.Fatal("길게 누르기 릴리스에 Trigger 발생")
		}
	}
	// 다시 길게 → B=0.
	h.frame(ptrPress(7, cx, cy))
	h.hold(7, cx, cy, 35)
	if c, _ := lastCmd(h); c.Kind != engine.Mute || c.B != 0 {
		t.Fatalf("뮤트 해제 → %+v(B=0 예상)", c)
	}
	h.frame(ptrRel(7, cx, cy))
	// 이동한 채 놓기 → 탭 아님.
	h.frame(ptrPress(7, cx, cy))
	h.frame(ptrMove(7, cx+100, cy))
	n := len(h.fb.cmds)
	h.frame(ptrRel(7, cx+100, cy))
	if len(h.fb.cmds) != n {
		t.Fatal("이동한 릴리스에 Cmd 발생")
	}
}

// — 계약↔단언: 한 프레임 플래그 —

func TestFrameFlags(t *testing.T) {
	h := newHarness(t)
	pressButton(h, btnAt(h.v, secFx, "play"))
	if !h.v.DropTapped() {
		t.Fatal("play 탭 프레임에 DropTapped false")
	}
	if c, _ := lastCmd(h); c.Kind != engine.Drop {
		t.Fatalf("play → %+v(Drop 예상)", c)
	}
	h.frame()
	if h.v.DropTapped() {
		t.Fatal("다음 프레임에 DropTapped 잔존")
	}
	n := len(h.fb.cmds)
	pressButton(h, btnAt(h.v, secFx, "rec"))
	if !h.v.ResumeTapped() {
		t.Fatal("rec 탭 프레임에 ResumeTapped false")
	}
	if len(h.fb.cmds) != n {
		t.Fatal("rec 탭이 Cmd를 보냄(보고만 해야)")
	}
	h.frame()
	if h.v.ResumeTapped() {
		t.Fatal("다음 프레임에 ResumeTapped 잔존")
	}
	cx, cy := h.v.titlePlate.Center()
	h.frame(ptrPress(-1, cx, cy))
	if !h.v.BackTapped() {
		t.Fatal("이름판 탭 프레임에 BackTapped false")
	}
	h.frame()
	if h.v.BackTapped() {
		t.Fatal("다음 프레임에 BackTapped 잔존")
	}
}

// — 계약↔단언: 표시 문자열 캐시 —

func TestDisplayCache(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secBassA, "TUNE") // 기본값 0.5 → "TUN 50"
	h.frame(ptrPress(-1, k.cx, k.cy))
	if h.v.disp[0].text != "TUN 50" {
		t.Fatalf("표시창 %q(TUN 50 예상)", h.v.disp[0].text)
	}
	if h.v.bottom.text != "BPM 130"+asciiSep+"BAR 0"+asciiSep+"INTRO" {
		t.Fatalf("하단 표시창 %q", h.v.bottom.text)
	}
	base := h.v.rebuilds
	for i := 0; i < 100; i++ {
		h.frame(ptrMove(-1, k.cx, k.cy)) // 잡은 채 유지(값 불변)
	}
	if h.v.rebuilds != base {
		t.Fatalf("같은 값 100프레임에 재구성 %d회(0회 예상)", h.v.rebuilds-base)
	}
	// 값이 바뀌면 정확히 1회 재구성.
	h.frame(ptrMove(-1, k.cx, k.cy-200)) // 0.5 → 1.0 → "TUN 99"
	if h.v.rebuilds != base+1 {
		t.Fatalf("값 변화 후 재구성 %d회(1회 예상)", h.v.rebuilds-base)
	}
	if h.v.disp[0].text != "TUN 99" {
		t.Fatalf("표시창 %q(TUN 99 예상)", h.v.disp[0].text)
	}
}
