// room_test.go — 방 뷰 로직 단위 테스트. 계약↔단언 대응은 스펙 "테스트" 절.
//
// 프레임 로직(state.step)은 이미지 없이 돌므로 전부 순수 Go 단위 테스트다.
// 좌표는 fixtureLayout이 준다(레이아웃 값에 의존하지 않는 상대 단언만 쓴다).
// 플래시 게이트 테스트는 FAIL-first로 게이트 배선을 검증한다(보고서에 원본 출력 첨부).
//
// 첫 접촉 힌트(P2-host §C) 계약↔단언:
//
//	계약                                      | 단언
//	------------------------------------------|------------------------------------------
//	시작 전 플레이트 ×0.85, 시작 후 해제        | TestHintPreStart / TestHintAfterStartImmediately
//	시작 전 기기 rect+12px 라운드 링(굵기 3)    | TestHintPreStart + TestHintRingGeometry(원점·크기)
//	시작 직후(시작 탭 프레임 포함) 기기 외곽 힌트 | TestHintAfterStartImmediately — 구 규칙(15~60s
//	                                          |  창 + everTouched)에서 빨강(FAIL-first)
//	첫 기기 탭에서 즉시 종료                     | TestHintExpiryAndDeviceTap · TestHintDeviceTapRecordedInView
//	20초 경과 종료                              | TestHintExpiryAndDeviceTap
package room

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

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
func (f *fakeBridge) KeyRoot() int                                 { return 0 }
func (f *fakeBridge) Chord(bar int) (uint8, uint8)                 { return 0, 0 }
func (f *fakeBridge) Mode(p engine.Part) (uint8, uint8)            { return 0, 0 }
func (f *fakeBridge) Hint(int)                                     {}
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

// ----------------------------------------------------------------------------
// 회귀 — 2026-09-06 방 뷰 결함 3종(원점 복제·순백 사각·비/스탠드/먼지 미표시)

// TestActorGeoMPlacement — 결함 A(원점 복제): 서브이미지·포즈가 rect 제자리에 놓이고
// 앵커 기준 변형이 되는지. 옛 코드는 GeoM에 rect 배치가 없어 플레이트 서브이미지를
// (0,0)에 복제했다. 소스 좌표(서브이미지/포즈 안) → 화면 좌표 매핑만 단언한다.
func TestActorGeoMPlacement(t *testing.T) {
	r := core.Rect{100, 200, 50, 80}
	anchor := [2]float64{120, 240} // rect 안의 앵커

	// 항등: 소스 (0,0)→rect 좌상단, (w,h)→rect 우하단, 앵커 상당점→앵커.
	var g ebiten.GeoM
	actorGeoM(&g, [2]int{}, r, anchor, 0, 0, 0, 1)
	for _, tc := range []struct{ sx, sy, wx, wy float64 }{
		{0, 0, r[0], r[1]},
		{r[2], r[3], r[0] + r[2], r[1] + r[3]},
		{anchor[0] - r[0], anchor[1] - r[1], anchor[0], anchor[1]},
	} {
		gx, gy := g.Apply(tc.sx, tc.sy)
		if math.Abs(gx-tc.wx) > 1e-9 || math.Abs(gy-tc.wy) > 1e-9 {
			t.Fatalf("항등 변환 (%f,%f) → (%f,%f), want (%f,%f)", tc.sx, tc.sy, gx, gy, tc.wx, tc.wy)
		}
	}

	// 변위: 소스 앵커 상당점 → 앵커+변위.
	g.Reset()
	actorGeoM(&g, [2]int{}, r, anchor, 6, -3, 0, 1)
	gx, gy := g.Apply(anchor[0]-r[0], anchor[1]-r[1])
	if math.Abs(gx-anchor[0]-6) > 1e-9 || math.Abs(gy-anchor[1]+3) > 1e-9 {
		t.Fatalf("앵커+변위 = (%f,%f), want (%f,%f)", gx, gy, anchor[0]+6, anchor[1]-3)
	}

	// 회전: 앵커 상당점은 회전해도 앵커에 머문다(앵커 기준 변형 계약).
	g.Reset()
	actorGeoM(&g, [2]int{}, r, anchor, 0, 0, math.Pi/2, 1)
	gx, gy = g.Apply(anchor[0]-r[0], anchor[1]-r[1])
	if math.Abs(gx-anchor[0]) > 1e-9 || math.Abs(gy-anchor[1]) > 1e-9 {
		t.Fatalf("회전 후 앵커 = (%f,%f), want (%f,%f)", gx, gy, anchor[0], anchor[1])
	}

	// 포즈: 스프라이트 (0,0)/(pw,ph) → rect 좌상단/우하단(rect에 맞춰 스케일).
	g.Reset()
	actorGeoM(&g, [2]int{25, 40}, r, anchor, 0, 0, 0, 1)
	gx, gy = g.Apply(0, 0)
	if math.Abs(gx-r[0]) > 1e-9 || math.Abs(gy-r[1]) > 1e-9 {
		t.Fatalf("포즈 (0,0) → (%f,%f), want rect 좌상단 (%f,%f)", gx, gy, r[0], r[1])
	}
	gx, gy = g.Apply(25, 40)
	if math.Abs(gx-(r[0]+r[2])) > 1e-9 || math.Abs(gy-(r[1]+r[3])) > 1e-9 {
		t.Fatalf("포즈 (pw,ph) → (%f,%f), want rect 우하단 (%f,%f)", gx, gy, r[0]+r[2], r[1]+r[3])
	}
}

// TestShaderOptionsBindUniforms — 결함 C 근본 원인 회귀: 드로우 옵션의 Uniforms가
// 생성 시 바인딩돼 있는지. 옛 코드는 유니폼 맵을 갱신만 하고 옵션에 담지 않아
// v2.9가 모든 유니폼을 0으로 채웠다(Density 0 → 비 전멸, Radius 0 → 램프 소멸).
// 바인딩이 같은 맵 인스턴스여야 제자리 갱신이 드로우에 반영된다.
func TestShaderOptionsBindUniforms(t *testing.T) {
	sh, err := newShaders()
	if err != nil {
		t.Fatalf("newShaders: %v", err)
	}
	if sh.rainShop.Uniforms == nil || sh.lampShop.Uniforms == nil || sh.dustShop.Uniforms == nil {
		t.Fatalf("옵션 Uniforms 미바인딩: rain=%v lamp=%v dust=%v",
			sh.rainShop.Uniforms, sh.lampShop.Uniforms, sh.dustShop.Uniforms)
	}
	// 같은 맵 인스턴스 — 갱신이 옵션을 거쳐 셰이더에 그대로 가는지.
	sh.rainU["Time"] = float32(7)
	if got, ok := sh.rainShop.Uniforms["Time"].(float32); !ok || got != 7 {
		t.Fatalf("rainShop.Uniforms가 rainU와 같은 맵이 아니다(Time=%v ok=%v)", got, ok)
	}
	sh.lampU["Radius"] = float32(150)
	if got, ok := sh.lampShop.Uniforms["Radius"].(float32); !ok || got != 150 {
		t.Fatalf("lampShop.Uniforms가 lampU와 같은 맵이 아니다(Radius=%v ok=%v)", got, ok)
	}
	sh.dustU["Time"] = float32(3)
	if got, ok := sh.dustShop.Uniforms["Time"].(float32); !ok || got != 3 {
		t.Fatalf("dustShop.Uniforms가 dustU와 같은 맵이 아니다(Time=%v ok=%v)", got, ok)
	}
}

// ----------------------------------------------------------------------------
// 첫 접촉 힌트(P2-host §C) — 상태 결정은 state.step(이미지 없음)

// TestHintPreStart — 시작 전: 링 켜짐·외곽 힌트 꺼짐·플레이트 감쇠.
func TestHintPreStart(t *testing.T) {
	s := newState(fixtureLayout())
	sig := baseSig()
	sig.Started = false
	s.step(sig, 1.0/120)
	if !s.hintRing {
		t.Fatal("시작 전 링 없음")
	}
	if s.hintDevice {
		t.Fatal("시작 전 기기 외곽 힌트가 켜짐")
	}
	if math.Abs(float64(s.plateDim-plateDimPreStart)) > 1e-6 {
		t.Fatalf("시작 전 감쇠 = %f, want %f", s.plateDim, plateDimPreStart)
	}
	// LED 트래킹 전제: 시작 전에도 스텝 신호가 들어온다(가짜 시계).
	if s.lastStep != sig.Step {
		t.Fatalf("lastStep = %d, want 신호 스텝 %d(시작 전 LED 트래킹)", s.lastStep, sig.Step)
	}
}

// TestHintAfterStartImmediately — 시작 직후(2초)에 기기 외곽 힌트가 이미 켜져 있어야 한다.
// FAIL-first: 구 규칙(hintIdle 15~60s 창 + everTouched)에서 이 단언은 빨강이었다 —
// 시작 탭 프레임의 접촉이 everTouched를 켜고 15초 창 이전이어서 어느 쪽으로도 죽는다.
func TestHintAfterStartImmediately(t *testing.T) {
	s := newState(fixtureLayout())
	sig := baseSig()
	sig.Now = 0
	s.step(sig, 1.0/120) // 시작 프레임(시작 탭)
	sig.Now = 2
	s.step(sig, 1.0/120)
	if s.hintRing {
		t.Fatal("시작 후에도 시작 전 링")
	}
	if !s.hintDevice {
		t.Fatal("시작 2초에 기기 외곽 힌트 없음 — 구 규칙(15~60s+everTouched) 잔여")
	}
	if s.plateDim != 1 {
		t.Fatalf("시작 후 감쇠 = %f, want 1", s.plateDim)
	}
}

// TestHintExpiryAndDeviceTap — 20초 경과 종료 + 첫 기기 탭 즉시 종료(나이 무관).
func TestHintExpiryAndDeviceTap(t *testing.T) {
	s := newState(fixtureLayout())
	sig := baseSig()
	sig.Now = 0
	s.step(sig, 0)
	sig.Now = hintAfterStartSec + 0.1
	s.step(sig, 0)
	if s.hintDevice {
		t.Fatal("20초 뒤에도 기기 외곽 힌트")
	}
	sig.Now = 5 // 되돌려도(테스트 편의) 탭 전엔 켜져 있어야 한다
	s.step(sig, 0)
	if !s.hintDevice {
		t.Fatal("5초에 힌트 없음(전제)")
	}
	s.deviceTapped = true
	sig.Now = 5.1
	s.step(sig, 0)
	if s.hintDevice {
		t.Fatal("첫 기기 탭 뒤에도 힌트")
	}
}

// TestHintDeviceTapRecordedInView — View 레벨: 기기 rect 안 탭이 deviceTapped로 기록되고
// 다음 스텝에서 외곽 힌트가 꺼진다(감쇠 해제·링 종료도 함께).
func TestHintDeviceTapRecordedInView(t *testing.T) {
	v := newTestView(t)
	dcx, dcy := v.layout.Device.Center()
	p := core.Pointer{ID: 1, X: dcx, Y: dcy, JustPressed: true, Pressed: true}
	stepPointer(t, v, p)
	p2 := core.Pointer{ID: 1, X: dcx, Y: dcy, JustReleased: true}
	if !stepPointer(t, v, p2) {
		t.Fatal("기기 탭이 DeviceTapped로 안 잡힘(전제)")
	}
	if !v.st.deviceTapped {
		t.Fatal("첫 기기 탭이 state에 기록 안 됨")
	}
	sig := Signals{DT: 1.0 / 120, Now: 1, Started: true, Cutoff: 0.5, Tempo: 0.5, Phase: 1}
	v.st.step(sig, 1.0/120)
	if v.st.hintDevice {
		t.Fatal("기기 탭 뒤 시작되어도 외곽 힌트")
	}
	if v.st.hintRing {
		t.Fatal("Started 뒤에도 시작 전 링")
	}
}

// TestHintRingGeometry — 링은 기기 rect를 12px 부풀린 라운드 사각(굵기 3 → 원점 offset 15).
// 스트로크 **위치**까지 잰다(2026-09-06 적발: 경로가 rect 자체를 그려 부풀린 밴드가 비어
// 있었다 — pixcheck 휘도 상승 1.4로 발견. 원점·크기만으로는 이미지 패딩이 같아 못 잡는다).
func TestHintRingGeometry(t *testing.T) {
	v := newTestView(t)
	dr := v.layout.Device
	pad := hintRingInset + hintRingStroke
	if v.ringImg == nil {
		t.Fatal("링 이미지 없음")
	}
	if math.Abs(v.ringX-(dr[0]-pad)) > 1e-9 || math.Abs(v.ringY-(dr[1]-pad)) > 1e-9 {
		t.Fatalf("링 원점 = (%f,%f), want rect−(%g) = (%f,%f)", v.ringX, v.ringY, pad, dr[0]-pad, dr[1]-pad)
	}
	w, h := v.ringImg.Size()
	if wantW, wantH := int(dr[2]+2*pad)+1, int(dr[3]+2*pad)+1; w != wantW || h != wantH {
		t.Fatalf("링 크기 = (%d,%d), want (%d,%d)", w, h, wantW, wantH)
	}
	// 위 가장자리 중심: 부풀린 밴드(y = pad−inset = 3)엔 잉크가, rect 가장자리(y = pad)엔 없어야.
	// 픽셀 판독(At)은 게임 루프 밖에서 패닉(ReadPixels)이라 경로 rect 자체를 게이트한다.
	want := [4]float64{dr[0] - hintRingInset, dr[1] - hintRingInset,
		dr[2] + 2*hintRingInset, dr[3] + 2*hintRingInset}
	if v.ringRect != want {
		t.Fatalf("링 경로 rect = %v, want 부풀린 rect %v", v.ringRect, want)
	}
}
