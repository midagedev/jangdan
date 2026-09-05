// room_test.go — 방 뷰 로직 단위 테스트. 계약↔단언 대응은 스펙 "테스트" 절.
//
// 프레임 로직(state.step)은 이미지 없이 돌므로 전부 순수 Go 단위 테스트다.
// 좌표는 fixtureLayout이 준다(레이아웃 값에 의존하지 않는 상대 단언만 쓴다).
// 플래시 게이트 테스트는 FAIL-first로 게이트 배선을 검증한다(보고서에 원본 출력 첨부).
package room

import (
	"math"
	"testing"

	"github.com/midagedev/revirth/app/core"
	"github.com/midagedev/revirth/engine"
)

// fakeBridge — Bridge 20메서드 무동작 구현(테스트 패키지 소유).
type fakeBridge struct{}

func (f *fakeBridge) Start()                                       {}
func (f *fakeBridge) Cmd(c engine.Cmd, a core.Author)              {}
func (f *fakeBridge) Tick() core.Tick                              { return core.Tick{} }
func (f *fakeBridge) Scope(dst []byte) bool                        { return false }
func (f *fakeBridge) Param(id engine.ParamID) float32              { return 0.5 }
func (f *fakeBridge) BassStep(p engine.Part, s int) (uint8, uint8) { return 0, 0 }
func (f *fakeBridge) DrumStep(p engine.Part, s int) uint8          { return 0 }
func (f *fakeBridge) Muted(p engine.Part) bool                     { return false }
func (f *fakeBridge) Slot(p engine.Part) uint8                     { return 0 }
func (f *fakeBridge) Telemetry(ev string, v float64)               {}
func (f *fakeBridge) Replay(sec float64)                           {}
func (f *fakeBridge) SeedWord() string                             { return "" }
func (f *fakeBridge) ReducedMotion() bool                          { return false }
func (f *fakeBridge) Hidden() bool                                 { return false }
func (f *fakeBridge) CleanScreen() bool                            { return false }
func (f *fakeBridge) WallClock() (int, int, int)                   { return 0, 0, 0 }
func (f *fakeBridge) Frame(ms float64)                             {}
func (f *fakeBridge) FirstFrame()                                  {}
func (f *fakeBridge) AllocPerFrame(b float64)                      {}

// fixtureLayout — 테스트용 레이아웃. 좌표는 실제와 무관하게 잡되, 창 면적 상한 테스트를
// 위해 "작은 창 12개 + 상한을 혼자 초과하는 거대 창 1개"를 넣는다.
func fixtureLayout() *core.RoomLayout {
	l := &core.RoomLayout{
		Size:      [2]float64{720, 1280},
		Plates:    core.RoomPlates{Night: "plate-night.png", Evening: "plate-night.png", Afternoon: "plate-night.png"},
		Lamp:      core.RoomLamp{Bulb: [2]float64{200, 700}, Radius: 220, Cone: core.Rect{80, 720, 320, 200}},
		Skylight:  core.Rect{120, 60, 480, 340},
		Mug:       core.RoomMug{Rect: core.Rect{420, 820, 40, 40}, Steam: [2]float64{440, 815}},
		Cat:       core.RoomActor{Rect: core.Rect{480, 1040, 160, 120}, Anchor: [2]float64{560, 1160}},
		Character: core.RoomActor{Rect: core.Rect{260, 600, 220, 340}, Anchor: [2]float64{370, 940}},
		Device:    core.Rect{250, 860, 260, 90},
		Scope:     core.Rect{340, 870, 80, 30},
		Radiator:  core.Rect{40, 900, 120, 200},
		Records:   []core.Rect{{300, 200, 60, 60}, {380, 200, 60, 60}},
		SeedText:  core.Rect{40, 1200, 640, 50},
		Palette:   core.RoomPalette{LampWarm: "#f2b866", Ink: "#1c1a24", Rain: "#9fb3c8", WindowLit: "#e8c27a"},
	}
	for i := 0; i < 12; i++ {
		l.Windows = append(l.Windows, core.Rect{40 + float64(i%6)*110, 120 + float64(i/6)*80, 120, 60})
	}
	l.Windows = append(l.Windows, core.Rect{500, 900, 400, 300}) // 120000px² — 상한(46080) 단독 초과
	return l
}

// baseSig — 조용한 기본 프레임(Started, 130BPM, 컷오프 중간).
func baseSig() Signals {
	return Signals{DT: 1.0 / 120, Started: true, Cutoff: 0.5, Tempo: 0.5, Phase: 1}
}

// ----------------------------------------------------------------------------
// §1 스탠드

func TestLampBreathing(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()
	sig.Cutoff = 0
	dt := 1.0 / 120

	// 정지 상태 밝기 = 컷오프 q=0 → 0.82
	s.step(sig, dt)
	if got := s.lampBrightness(); math.Abs(float64(got-0.82)) > 1e-6 {
		t.Fatalf("q=0 밝기 = %f, want 0.82", got)
	}
	// q=1 → 1.0
	sig.Cutoff = 1
	s.step(sig, dt)
	if got := s.lampBase; math.Abs(float64(got-1.0)) > 1e-6 {
		t.Fatalf("q=1 밝기 = %f, want 1.0", got)
	}

	// BD 트리거 → 정확히 +6% 상한(감쇠 먼저, 트리거 나중이라 피크 프레임이 상한 그대로)
	sig.Cutoff = 0
	s.step(sig, dt) // pulse 소진
	base := s.lampBrightness()
	sig.Flags = 1 << engine.BD
	s.step(sig, dt)
	peak := s.lampBrightness()
	if peak > lampDimBright+lampBreathAmp+1e-6 || peak < lampDimBright+lampBreathAmp-1e-6 {
		t.Fatalf("피크 = %f, want %f(상한 +6%%)", peak, lampDimBright+lampBreathAmp)
	}

	// 150ms 무트리거 → base + 0.06·e⁻¹ ± 0.005
	sig.Flags = 0
	for i := 0; i < 18; i++ { // 18프레임 = 0.15s @120fps
		s.step(sig, dt)
	}
	got := float64(s.lampBrightness() - base)
	want := lampBreathAmp * math.Exp(-1)
	if math.Abs(got-want) > 0.005 {
		t.Fatalf("150ms 후 잔류 = %f, want %f±0.005", got, want)
	}
}

// ----------------------------------------------------------------------------
// §3 창 — 동시 변화 면적 ≤ 화면 5%

func TestWindowAreaCap(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()
	dt := 1.0 / 120
	huge := len(l.Windows) - 1
	capArea := 720.0 * 1280.0 * 0.05 // 46080

	prevLit := 0.0
	barDur := 4.0 * (60.0 / engine.BPMOf(0.5)) // 130BPM 바
	for f := 0; f < int(60/dt); f++ {
		tt := float64(f) * dt
		sig.Flags = 0
		if math.Mod(tt, barDur) < dt { // 매 바: 토글 + 드롭
			sig.Flags = engine.FlagBar | engine.FlagDrop
		}
		s.step(sig, dt)
		lit := 0.0
		for i, r := range l.Windows {
			if s.winLit[i] {
				lit += r[2] * r[3]
			}
		}
		if s.winLit[huge] {
			t.Fatalf("상한 초과 거대 창이 켜졌다(면적 %f > %f)", l.Windows[huge][2]*l.Windows[huge][3], capArea)
		}
		if d := lit - prevLit; d > capArea+1e-9 || d < -capArea-1e-9 {
			t.Fatalf("t=%f 한 프레임 변화 면적 %f, 상한 %f", tt, math.Abs(d), capArea)
		}
		prevLit = lit
	}
}

// ----------------------------------------------------------------------------
// §9 플래시 게이트 — FAIL-first(배선 무력화 빌드에서 빨강 확인 → 복원 후 초록).

// drive160 — 60초 160BPM: BD 매 박, FlagBar+FlagDrop 매 바. 초당 쌍 수 최댓값 반환.
func drive160(s *state, seconds float64) (maxPairs int) {
	dt := 1.0 / 120
	beat := 60.0 / 160.0
	sig := Signals{DT: dt, Started: true, Cutoff: 0.3, Tempo: 1.0, Phase: 2} // q=1 → 160BPM
	n := int(seconds / dt)
	nextBeat := 0.0
	beats := 0
	for f := 0; f < n; f++ {
		sig.Flags = 0
		if float64(f)*dt >= nextBeat {
			sig.Flags |= 1 << engine.BD
			beats++
			if beats%4 == 1 { // 바 시작(스텝 0)
				sig.Flags |= engine.FlagBar | engine.FlagDrop
				sig.Bar++
			}
			nextBeat += beat
		}
		s.step(sig, dt)
		if p := s.flash.pairs(s.t); p > maxPairs {
			maxPairs = p
		}
	}
	return maxPairs
}

func TestFlashGateQuietAtTempo(t *testing.T) {
	s := newState(fixtureLayout())
	maxPairs := drive160(&s, 60)
	if s.FlashClamped != 0 {
		t.Fatalf("160BPM 정상 연주에서 클램프 %d회, want 0", s.FlashClamped)
	}
	if maxPairs > flashMaxPairs {
		t.Fatalf("1초 창 상승·하강 쌍 최대 %d, want ≤ %d", maxPairs, flashMaxPairs)
	}
}

func TestFlashGateClamps(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	s.lampAmp = 0.3 // 비정상 진폭 픽스처(호흡이 10% 임계를 넘게 왜곡)
	drive160(&s, 5)
	if s.FlashClamped == 0 {
		t.Fatalf("진폭 0.3 픽스처에서 클램프 0회 — 게이트가 못 잡음")
	}
}

// ----------------------------------------------------------------------------
// §5 고양이·§7 캐릭터 상태기계

func TestCatStateMachine(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()

	// 기본 tail — 박 위상 0.25에서 +1.5°(dt=0이면 위상 유지)
	s.beatPhase = 0.25
	s.step(sig, 0)
	if s.cat.action != catTail {
		t.Fatalf("기본 동작 = %d, want tail", s.cat.action)
	}
	if want := 1.5 * deg; math.Abs(s.cat.ang-want) > 1e-9 {
		t.Fatalf("꼬리 각 = %f, want %f(±1.5°)", s.cat.ang, want)
	}

	// 드롭 뒤 2바 귀 리프트
	sig.Flags = engine.FlagDrop
	s.step(sig, 0)
	sig.Flags = 0
	sig.Bar = 1 // headup.. earsUntilBar = 0+2 = 2
	s.step(sig, 0)
	if s.cat.action != catEars || s.cat.dy != -catEarsLiftPx {
		t.Fatalf("드롭 뒤 동작 = %d dy=%f, want ears dy=%f", s.cat.action, s.cat.dy, -catEarsLiftPx)
	}
	sig.Bar = 2 // 2바 지나면 복귀
	s.step(sig, 0)
	if s.cat.action != catTail {
		t.Fatalf("2바 뒤 동작 = %d, want tail 복귀", s.cat.action)
	}

	// 브레이크다운(Phase 3) → 잠, 스케일 0.97 ± 1%
	sig.Phase = 3
	s.step(sig, 1)
	if s.cat.action != catSleep {
		t.Fatalf("Phase 3 동작 = %d, want sleep", s.cat.action)
	}
	if s.cat.scale < catSleepScale*(1-catBreathAmp)-1e-9 || s.cat.scale > catSleepScale*(1+catBreathAmp)+1e-9 {
		t.Fatalf("잠 스케일 = %f, want 0.97±1%%", s.cat.scale)
	}
	sig.Phase = 1
}

func TestCharStateMachine(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()

	// nod — 항상 합성(다른 동작 없을 때 ±1.5°)
	s.beatPhase = 0.25
	s.step(sig, 0)
	if s.char.action != charNone {
		t.Fatalf("기본 동작 = %d, want none", s.char.action)
	}
	if want := 1.5 * deg; math.Abs(s.char.ang-want) > 1e-9 {
		t.Fatalf("끄덕임 각 = %f, want %f", s.char.ang, want)
	}

	// 드롭 뒤 1바 고개 들기(−3°, nod 0에서 정확히)
	sig.Flags = engine.FlagDrop
	s.step(sig, 0)
	sig.Flags = 0
	sig.Bar = 0
	s.beatPhase = 0
	s.step(sig, 0)
	if s.char.action != charHeadup {
		t.Fatalf("드롭 뒤 동작 = %d, want headup", s.char.action)
	}
	if want := -3 * deg; math.Abs(s.char.ang-want) > 1e-9 {
		t.Fatalf("고개 들기 각 = %f, want %f(−3°)", s.char.ang, want)
	}
	sig.Bar = 1 // 1바 뒤 해제
	s.step(sig, 0)
	if s.char.action == charHeadup {
		t.Fatal("1바 뒤에도 headup")
	}

	// reach — 레지던트 손 0.5s 이징으로 기기 방향 6px
	sig.ResidentHandOn = true
	for i := 0; i < 4; i++ {
		s.step(sig, 0.125)
	}
	if s.char.action != charReach || s.char.reachT < 1-1e-9 {
		t.Fatalf("reach 진행 = %f 동작 %d, want 완료(1.0)", s.char.reachT, s.char.action)
	}
	if math.Abs(s.char.dx-s.reachDX) > 1e-6 || math.Abs(s.char.dy-s.reachDY) > 1e-6 {
		t.Fatalf("reach 변위 = (%f,%f), want (%f,%f)", s.char.dx, s.char.dy, s.reachDX, s.reachDY)
	}
	if d := math.Hypot(s.char.dx, s.char.dy); math.Abs(d-charReachPx) > 1e-6 {
		t.Fatalf("reach 거리 = %f, want %f", d, charReachPx)
	}

	// MANUAL 잠금 — 우선순위로 manual, 0.3s에 거둠
	sig.ManualLocked = true
	s.step(sig, 0)
	if s.char.action != charManual {
		t.Fatalf("잠금 동작 = %d, want manual", s.char.action)
	}
	for i := 0; i < 3; i++ {
		s.step(sig, 0.1)
	}
	if s.char.reachT > 1e-9 {
		t.Fatalf("잠금 후 reach 진행 = %f, want 0(0.3s)", s.char.reachT)
	}
}

func TestReducedMotionFreezes(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()
	sig.ReducedMotion = true
	sig.Flags = 1<<engine.BD | engine.FlagAccent | engine.FlagBar | engine.FlagDrop
	sig.Phase = 1
	s.beatPhase = 0.25
	s.step(sig, 0.05)

	if s.cat.ang != 0 || s.cat.dx != 0 || s.cat.dy != 0 {
		t.Fatalf("RM 고양이 변환 = (%f,%f,%f), want 0", s.cat.dx, s.cat.dy, s.cat.ang)
	}
	if s.char.ang != 0 || s.char.dx != 0 || s.char.dy != 0 {
		t.Fatalf("RM 캐릭터 변환 = (%f,%f,%f), want 0", s.char.dx, s.char.dy, s.char.ang)
	}
	if s.lampShakeOff != [2]float64{} {
		t.Fatalf("RM 스탠드 떨림 = %v, want 0", s.lampShakeOff)
	}
	fades := append([]float32(nil), s.winFade...)
	s.step(sig, 0.05)
	for i := range fades {
		if s.winFade[i] != fades[i] {
			t.Fatalf("RM 창 페이드 진행(%d: %f→%f)", i, fades[i], s.winFade[i])
		}
	}
	// Phase 3 + RM — 잠자는 정지 자세(정적 0.97)는 유지
	sig.Phase = 3
	s.step(sig, 0.05)
	if s.cat.action != catSleep || s.cat.scale != catSleepScale {
		t.Fatalf("RM 잠 = %d 스케일 %f, want sleep 0.97", s.cat.action, s.cat.scale)
	}
}

// ----------------------------------------------------------------------------
// §8 시간대

func TestTimeOfDayCrossfade(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()
	sig.PomodoroRest = true
	for i := 0; i < 15; i++ {
		s.step(sig, 1)
	}
	if s.tod != todEvening {
		t.Fatalf("휴식 시간대 = %d, want evening", s.tod)
	}
	if math.Abs(s.todBlend-0.5) > 1e-9 {
		t.Fatalf("15초 블렌드 = %f, want 0.5", s.todBlend)
	}
	if _, g, _ := s.tintNow(); math.Abs(float64(g-0.98)) > 1e-6 {
		t.Fatalf("t=15 초록 채널 = %f, want 0.98", g)
	}
	// 세션 45분 초과 → afternoon(휴식 아닐 때)
	sig.PomodoroRest = false
	sig.Now = 45*60 + 1
	s.step(sig, 0)
	if s.tod != todAfternoon {
		t.Fatalf("45분 세션 시간대 = %d, want afternoon", s.tod)
	}
}

// ----------------------------------------------------------------------------
// §14 DeviceTapped — 눌림·떼림 모두 기기 rect 안일 때만

func newTestView(t *testing.T) *View {
	t.Helper()
	v, err := New(&core.Ctx{Bridge: &fakeBridge{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func stepPointer(t *testing.T, v *View, p core.Pointer) bool {
	t.Helper()
	ctx := &core.Ctx{Bridge: &fakeBridge{}, Pointers: []core.Pointer{p}}
	v.Update(ctx)
	return v.DeviceTapped()
}

func TestDeviceTapped(t *testing.T) {
	v := newTestView(t)
	// 좌표는 실제 layout.json의 device rect에서 유도한다(2026-09-05: 비전 라운드가 플레이스홀더 좌표를
	// 실측값으로 바꾸자 하드코딩 380,905가 기기 밖이 되어 실패했다 — 좌표의 소유자는 JSON이다).
	dcx, dcy := v.layout.Device.Center()
	in := func(p core.Pointer) core.Pointer { p.X, p.Y = dcx, dcy; return p }
	out := func(p core.Pointer) core.Pointer { p.X, p.Y = v.layout.Device[0]-40, dcy; return p } // rect 왼쪽 밖
	// 케이스 1: 안에서 눌리고 안에서 떼어짐 → true, 다음 프레임 false
	if stepPointer(t, v, in(core.Pointer{ID: 1, JustPressed: true, Pressed: true})) {
		t.Fatal("누르는 프레임에 true")
	}
	if !stepPointer(t, v, in(core.Pointer{ID: 1, JustReleased: true})) {
		t.Fatal("안에서 떼었는데 false")
	}
	if stepPointer(t, v, in(core.Pointer{ID: 1})) {
		t.Fatal("다음 프레임에도 true — 1프레임 펄스 아님")
	}

	// 케이스 2: 안에서 눌리고 밖에서 떼어짐 → false
	if stepPointer(t, v, in(core.Pointer{ID: 2, JustPressed: true, Pressed: true})) {
		t.Fatal("누르는 프레임에 true(2)")
	}
	if stepPointer(t, v, out(core.Pointer{ID: 2, JustReleased: true})) {
		t.Fatal("밖에서 떼었는데 true")
	}

	// 케이스 3: 밖에서 눌리고 안에서 떼어짐 → false(무장 안 됨)
	stepPointer(t, v, out(core.Pointer{ID: 3, JustPressed: true, Pressed: true}))
	if stepPointer(t, v, in(core.Pointer{ID: 3, JustReleased: true})) {
		t.Fatal("밖에서 눌러 안에서 떼었는데 true")
	}
}

// ----------------------------------------------------------------------------
// §2 비 — 밀도는 CH 게이트 수만 본다(뮤트 상태는 공식에 없음 — Signals에 뮤트 없음)

func TestRainDensity(t *testing.T) {
	l := fixtureLayout()
	s := newState(l)
	sig := baseSig()

	sig.CHGates = 0
	s.step(sig, 0)
	if math.Abs(float64(s.rainDensity-0.15)) > 1e-6 {
		t.Fatalf("게이트 0 밀도 = %f, want 0.15", s.rainDensity)
	}
	sig.CHGates = engine.Steps
	s.step(sig, 0)
	if math.Abs(float64(s.rainDensity-1.0)) > 1e-6 {
		t.Fatalf("게이트 16 밀도 = %f, want 1.0", s.rainDensity)
	}
	// 굵기 = OH 게이트 수 → 1..2.5px
	sig.OHGates = 0
	s.step(sig, 0)
	if math.Abs(float64(s.rainThick-1.0)) > 1e-6 {
		t.Fatalf("OH 0 굵기 = %f, want 1.0", s.rainThick)
	}
	sig.OHGates = engine.Steps
	s.step(sig, 0)
	if math.Abs(float64(s.rainThick-2.5)) > 1e-6 {
		t.Fatalf("OH 16 굵기 = %f, want 2.5", s.rainThick)
	}
}

// ----------------------------------------------------------------------------
// §15~§17 셰이더 3종 컴파일

func TestShadersCompile(t *testing.T) {
	sh, err := newShaders()
	if err != nil {
		t.Fatalf("newShaders: %v", err)
	}
	if sh.rain == nil || sh.lamp == nil || sh.dust == nil {
		t.Fatalf("셰이더 누락: rain=%v lamp=%v dust=%v", sh.rain, sh.lamp, sh.dust)
	}
}
