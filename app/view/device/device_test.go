// device_test.go — 계약↔단언. 헤드리스 렌더가 불가하므로 New(이미지) 없이
// newView(레이아웃만) + 기록형 fakeBridge로 상태기계를 검증한다.
// 좌표는 하드코딩하지 않고 레이아웃에서 찾은 컨트롤의 값을 쓴다(JSON이 단일 소유자).
//
// — P2 계약↔단언 표(스펙 P2-device, 항목당 단언 ≥ 2) —
//
//	계약                                    | 단언
//	----------------------------------------|----------------------------------------------
//	play 탭 → Transport A=1/0 토글           | TestTransport: 재생 중 A=0·정지 중 A=1 각 정확히 1개,
//	                                        |   라벨 "PLAY"·버튼 매핑(newView 실패 시 에러)
//	rec 탭 → Drop + DropTapped 1프레임       | TestTransport: Drop 정확히 1개 + 플래그 1프레임 수명,
//	                                        |   라벨 "DROP"
//	ResumeTapped 폐지(항상 false)            | TestTransport: 탭 프레임·다음 프레임 둘 다 false
//	코드 띠 8셀 균등·간격 3                   | TestChordBandLayout: 셀 수·동폭, 끝 정렬(마지막 셀 = 띠 끝)
//	셀 탭 → 선택기(같은 띠) 열림·무송신       | TestChordSelector: 열림 프레임 Cmd 0개, bar = 셀
//	도수 탭 → SetChord 1개 후 닫힘           | TestChordSelector: {A:bar,B:deg,C:flags} 정확히 1개·닫힘
//	셀 7 = 7th 토글(열림 유지)               | TestChordSeventhToggle: C 플래그 XOR 송신 2회·도수 보존·열림
//	밖 탭 / 6초 무조작 → 닫힘(무송신)         | TestChordSelectorClose: 빈 판 (360,615) 탭·5s 열림→6.5s 닫힘
//	라벨 = 도수 로마자(+7)·입력 방어 deg%7    | TestChordLabels: "iv"/"iv7"/deg 10→"iv", 값 불변 시 재구성 0
//	B 표시창 탭 → 모드 순환 5단계             | TestBassModeCycle: (1,0)→(1,1)→(1,2)→(2,0)→(0,0) 전체 순서,
//	                                        |   nextBassMode(7,9) 범위 밖 정규화
//	B 표시창: 노브 값 2초 → 모드 문자열       | TestBassModeDisplayWindow: "CUT <기본값×99>"→2초 후 "BASS" 복귀
//	하단 = "Am 120 B3 BUILD" 포맷            | TestBottomDisplay: 키·BPM·마디·페이즈 전부 + MANUAL 치환
//	정상 상태 Update 힙 할당 0                | TestUpdateSteadyNoAlloc: AllocsPerRun 200프레임 == 0,
//	                                        |   TestChordLabels/TestDisplayCache의 재구성 카운터
//	화성 API 구 브리지 호환(패닉 래치)        | TestHarmonyGuard: Chord 패닉 브리지에서 무크래시·래치·기본값,
//	                                        |   이후 Chord 실호출 없음(카운터 1 고정)
//	라인 VU 매핑 −36..0dB→0..1(방어 포함)     | TestVuOf: 0·0.0158·음수·NaN→0, 0.1→0.444, 0.5→0.833, 1·2·100→1
//	밸리스틱 어택 즉시·릴리스 4/s             | TestVuBallistics: dt 0.1에서 1→0.6·하한 0 스냅, Update 배선
//	                                        |   (Levels→disp·Peak→master), 구 호스트 전환 1초 뒤 잔상 0
//	패드 lit α = max(탭, 0.12+0.5vu) 상한 0.62 | TestPadLitAlpha: (0.18,0.5)→0.37, 레벨 0이면 탭 유지, 상한 0.62
//	세그먼트 수 round(vu×N)·구 호스트 완전 소거 | TestMeterSegments/TestMeterDarkWhenLevelsZero: 풀스케일 결정
//	                                        |   50건(12+12+20+6) 양성 대조, Levels 전부 0이면 결정 0, 탭 lit만으로 0
//
// — P4-scroll 계약↔단언(스크롤 랙 §13.3, 단언은 scroll_test.go) —
//
//	계약                                    | 단언
//	----------------------------------------|----------------------------------------------
//	빈 판 드래그 → scrollY(클램프 0..max)    | TestScrollDragClamp: −200px→200·과다→max·맨위→0,
//	  max = 레이아웃 높이−1280(v4 = 720)     |   손 뗀 뒤 무관성(v 0 유지)
//	컨트롤 우선 — 노브 시작 드래그는 스크롤   | TestKnobDragNoScroll: CUTOFF −200px → SetParam만,
//	                                        |   불변 scrollY 0
//	관성 ×0.9/프레임·|v|<2px 정지·경계 정지  | TestScrollInertia: ≤60프레임 내 정지·이후 불변,
//	                                        |   경계 밖 속도 → 520에서 즉시 정지
//	포인터 y+scrollY 변환(단일 소유자)       | TestScrolledKnobHit: scrollY 400에서 화면 y=cy−400
//	                                        |   탭 → REV_SIZE SetParam(레이아웃 좌표 판정 증명)
//	첫 스크롤 포인터만(두 번째는 무동작)      | TestScrollSecondPointerIgnored: 동시 2포인터, 첫 것만 이동
//	장식 버튼 = Cmd 0·스크롤로도 안 넘어감    | TestDecoButtons: bkDeco 4종 탭 Cmd 0개·라벨 REV/CHO/PRE/ST
//	구 레이아웃(높이≤1280) 스크롤 비활성     | TestScrollDisabledShortLayout: scrollMax 0·빈 판 잡아도 pkNone
//	휠 NaN·거대 값 방어·인디케이터 기하        | TestScrollClamp: NaN/±Inf/±거대 → 경계, scrollIndGeom
//	                                        |   길이 1280²/1800·y 비례
//	스크롤 상태 Update 무할당                 | TestScrollNoAlloc: scrollY 200·관성 감쇠 중·잡은 채 셋 다 0
//
// — P5-poly 계약↔단언(폴리 리드 모듈 바인딩 — §14.1 DeviceParam·§12.3 폴리 문단) —
//
//	계약                                    | 단언
//	----------------------------------------|----------------------------------------------
//	poly 노브 8 = KnobDevParam(슬롯 7, k)    | TestLayoutCounts: 노브 55·이름판 8·dev 노브 8개
//	                                        |   전부 (슬롯 7, k 0..7), 전역 노브 47 매핑 불변
//	dev 노브 드래그 → DeviceParam{A:7,B:k}  | TestPolyKnobDevParam: 스크롤 상태(cy 1886−scrollY)
//	  (SetParam 0개)                        |   드래그 → DeviceParam 정확히 1개·SetParam 0개,
//	                                        |   미러 갱신·JustGrabbed 미보고(전역 id 오염 봉쇄)
//	표시값 = DevParam(음수·NaN→기본값,       | TestPolyKnobDevParam: 미러 0.25 추적·−1/NaN →
//	  1 초과 클램프)                        |   DevParamDefault(7,0)=0.55·2.0 → 1
//	릴리스 후 브리지 값 복귀(로컬 잔존 無)   | TestPolyKnobDevParam: 드래그 릴리스 뒤 dev 노브
//	                                        |   표시값이 미러를 다시 따름
//	탭 스윕도 DeviceParam 경로               | TestPolyKnobSweep: 스윕 전 송신이 DeviceParam뿐,
//	                                        |   SetParam 0개, 최종값 = 시작값 ±1/4095
//	라벨 표 CUT/RESO/ENV/ATK/DEC/REL/DET/LVL| TestPolyLabels: 8종 표 + 띠색 (60,130,170)
//	FlagPoly 프레임 점등·150ms 감쇠          | TestPolyTrigDot: 미점화 0·점화 α=1·9프레임(150ms)
//	                                        |   뒤 < 0.1·무할당(스테디 Update에 FlagPoly 상시)
//	poly 없는 구 레이아웃 → New 성공·판 없음 | TestPolyAbsentOldLayout: poly 노브·판 제거 뷰 OK,
//	                                        |   dev 노브 0·hasSection[4] false·scrollMax 불변
//	매핑 없는 poly 노브 이름 → New 에러      | TestPolyUnknownKnobName: TEMPO 이름 → 매핑 없음
//
// FAIL-first(구현 전 소스에서 실측, 2026-09-06):
//
//	go test ./app/view/device/ -run 'TestTransport|TestDisplayCache' -count=1
//	→ --- FAIL: TestTransport — play 탭(재생 중)이 Transport가 아니라 Drop(Kind 6) 송신
//	→ --- FAIL: TestDisplayCache — 하단 표시창이 구식 "BPM 130 - BAR 0 - INTRO" 포맷
//	(신규 테스트 6종은 구현과 동시에 도입되어 구소스에서의 적색은 위 두 간접 증거로 대신한다.)
//	구 브리지 패닉(가드 도입 동기): 캡처 MANIFEST 로그 — jsBridge.Chord가 "property chord
//	is not a function" 패닉으로 Go program exited → 가드 적용 후 캡처 정상 완주.
//
// P3-meters(2026-09-06): 라인 미터 단언 5종(TestVuOf·TestVuBallistics·TestPadLitAlpha·
// TestMeterSegments·TestMeterDarkWhenLevelsZero)은 신규 API(vuOf·vuStep·padLitAlpha·
// vuSegsOn·meterDraws)와 동시 도입이라 구소스에서는 정의 없음 컴파일 실패가 FAIL-first다.
// 헤드리스 경로의 소거 단언은 그리기 결정 카운터(meterDraws — room 뷰 draws 관례)로 한다.
//
// P4-scroll(2026-09-06) FAIL-first(구현 전 소스 — v3 레이아웃 반영 전):
//
//	go test ./app/view/device/ -count=1
//	→ 전 테스트가 newView에서 "device: 노브 "mixer"/"REV_A"의 파라미터 매핑 없음"으로 사망
//	  (layout.json은 이미 v3인데 KnobParam이 mixer/fx2를 모름). 매핑 추가 뒤에는
//	  TestLayoutCounts "노브 47개(29 예상)"가 유일 적색 — 실측 갱신으로 닫았다.
//
// P5-poly(2026-09-06) FAIL-first(구현 전 소스 — v4 레이아웃 반영 전, 같은 형태):
//
//	go test ./app/view/device/ -count=1
//	→ 전 테스트가 newView에서 "device: 노브 "poly"/"CUTOFF"의 파라미터 매핑 없음"으로 사망
//	  (layout.json은 이미 v4 poly 8노브를 데리고 있는데 KnobParam이 poly를 모른다).
//	  KnobDevParam 폴백 추가 뒤 TestLayoutCounts "노브 47개(55 예상)"·"이름판 7개(8 예상)"
//	  이 다음 적색 — 실측 갱신으로 닫았다.
package device

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/midagedev/jangdan/app/assets"
	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// fakeBridge — 기록형 브리지: 보낸 Cmd(시각 포함)와 파라미터·스텝·뮤트·화성 미러.
type fakeBridge struct {
	cmds      []recCmd
	params    [engine.NumParams]float32
	devParams [engine.DevParams]float32 // 폴리 슬롯 로컬 파라미터 미러(DevParam)
	bass      [2][engine.Steps][2]uint8 // [note, flags]
	drum      [6][engine.Steps]uint8
	muted     [engine.NumParts]bool
	slot      [2]uint8
	key       int
	chord     [engine.ChordBars][2]uint8 // [deg, flags]
	mode      [2][2]uint8                // [mode, dir] — 파트 A/B
	now       float64
	tick      core.Tick

	// 랙 위상 미러(§14.3) — 진짜 엔진 한 벌이다. 호스트의 섀도 엔진과 같은 방식으로
	// AddDevice/RemoveDevice/Connect/Disconnect를 그대로 적용하므로, 뒷면 뷰가 보낸
	// 명령이 케이블 표에 어떻게 반영되는지를 테스트가 실제 규칙(순환 거부·표 가득)으로 잰다.
	rackEng *engine.Engine

	// cablesCalls — Cables 호출 수(§14.3 읽기 계약 게이트: 뒷면 뷰는 위상 리비전이
	// 변할 때만 표를 다시 읽는다 — P5-back-view).
	cablesCalls int
}

// rack — 지연 생성 미러 엔진(기본 랙). New(1)의 시드는 위상과 무관하다.
func (f *fakeBridge) rack() *engine.Engine {
	if f.rackEng == nil {
		f.rackEng = engine.New(1)
	}
	return f.rackEng
}

func (f *fakeBridge) RackRev() uint32 { return f.rack().RackRev() }

func (f *fakeBridge) RackKind(slot int) engine.DeviceKind { return f.rack().Kind(slot) }

func (f *fakeBridge) Cables(dst []core.RackCable) int {
	f.cablesCalls++
	e := f.rack()
	n := e.NumCables()
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		src, sp, d, dp, g, bind, ok := e.Cable(i)
		if !ok {
			return i
		}
		dst[i] = core.RackCable{Src: src, SP: sp, Dst: d, DP: dp, Bind: uint8(bind), Gain: g}
	}
	return n
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

func (f *fakeBridge) DevParam(slot, k int) float32 {
	if slot == engine.SlotPoly && k >= 0 && k < engine.DevParams {
		return f.devParams[k]
	}
	return -1
}

func (f *fakeBridge) BassStep(p engine.Part, step int) (uint8, uint8) {
	return f.bass[p][step][0], f.bass[p][step][1]
}

func (f *fakeBridge) DrumStep(p engine.Part, step int) uint8 { return f.drum[p-2][step] }
func (f *fakeBridge) Muted(p engine.Part) bool               { return f.muted[p] }
func (f *fakeBridge) Slot(p engine.Part) uint8               { return f.slot[p] }
func (f *fakeBridge) KeyRoot() int                           { return f.key }
func (f *fakeBridge) Chord(bar int) (uint8, uint8) {
	c := f.chord[bar&(engine.ChordBars-1)]
	return c[0], c[1]
}
func (f *fakeBridge) Mode(p engine.Part) (uint8, uint8) {
	m := f.mode[p&1]
	return m[0], m[1]
}
func (f *fakeBridge) Hint(int) {}

func (f *fakeBridge) Cmd(c engine.Cmd, a core.Author) {
	f.cmds = append(f.cmds, recCmd{c, f.now, a})
	switch c.Kind {
	case engine.AddDevice, engine.RemoveDevice, engine.Connect, engine.Disconnect:
		f.rack().Apply(c) // 위상 명령은 미러 엔진이 규칙대로 처리한다(뷰가 되읽는 표가 진짜다)
	case engine.SetParam:
		if c.A < uint8(engine.NumParams) {
			f.params[c.A] = c.V
		}
	case engine.DeviceParam: // P5-poly — 폴리 슬롯 로컬 파라미터 미러(송신→표시값 추적 단언용)
		if c.A == uint8(engine.SlotPoly) && c.B < uint8(engine.DevParams) {
			f.devParams[c.B] = c.V
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
	case engine.SetKey:
		f.key = int(c.A % engine.NumKeys)
	case engine.SetChord:
		f.chord[c.A&uint8(engine.ChordBars-1)] = [2]uint8{c.B, c.C}
	case engine.BassMode:
		f.mode[c.A&1] = [2]uint8{c.B, c.C}
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
	h.ctx.Tick = h.fb.tick // 제품 경로와 동일: main.go가 매 프레임 Bridge.Tick()으로 채운다
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
	// P4-scroll(v3 레이아웃): 믹서 노브 12·fx2 6, fx2 장식 버튼 4, 믹서 활동 LED 8·fx2 장식 LED 4 추가.
	// P5-poly(v4): 폴리 노브 8·이름판 1 추가 — 노브 47+8 = 55, 이름판 7+1 = 8.
	if len(h.v.knobs) != 55 {
		t.Fatalf("노브 %d개(55 예상)", len(h.v.knobs))
	}
	if len(h.v.buttons) != 42 {
		t.Fatalf("버튼 %d개(42 예상)", len(h.v.buttons))
	}
	if len(h.v.pads) != 6 {
		t.Fatalf("패드 %d개(6 예상)", len(h.v.pads))
	}
	if len(h.v.leds) != 48 {
		t.Fatalf("LED %d개(48 예상)", len(h.v.leds))
	}
	if len(h.v.layout.Plates) != 8 {
		t.Fatalf("이름판 %d개(8 예상)", len(h.v.layout.Plates))
	}
	// 55 노브 전부가 전역 KnobParam 또는 장치 로컬 KnobDevParam(슬롯 7)으로 매핑 —
	// dev 노브는 정확히 8개(레이아웃 poly 섹션과 1:1). 매핑 안 된 노브는 newView가 이미 에러.
	dev := 0
	for _, k := range h.v.knobs {
		if _, ok := core.KnobParam(sectionName(k.sec), k.name); ok {
			if k.dev {
				t.Fatalf("노브 %s 전역 매핑인데 dev 플래그", k.name)
			}
			continue
		}
		if !k.dev || k.slot != engine.SlotPoly || k.k < 0 || k.k >= engine.PolyParams {
			t.Fatalf("노브 %s(%s) 매핑 오류(dev %v, 슬롯 %d, k %d)", k.name, sectionName(k.sec), k.dev, k.slot, k.k)
		}
		dev++
	}
	if dev != 8 {
		t.Fatalf("장치 로컬 노브 %d개(8 예상)", dev)
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
	case secFx:
		return "fx"
	case secMixer:
		return "mixer"
	case secFx2:
		return "fx2"
	}
	return "poly"
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
	k := knobAt(h.v, secBassB, "CUTOFF") // 피크 = clamp01(기본값 + 0.5) — 기본값은 엔진 표에서 읽는다(2026-09-06 리드 기본값 변경 후 하드코딩 폐기)
	wantPeak := clamp01(engine.DefaultParams()[engine.CutoffB] + 0.5)
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
	if math.Abs(float64(sp[maxI].c.V-wantPeak)) > 0.02 {
		t.Fatalf("피크 %v(%v 근사 예상)", sp[maxI].c.V, wantPeak)
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
	base := engine.DefaultParams()[engine.CutoffB]
	if math.Abs(float64(sp[len(sp)-1].c.V-base)) > 1.0/4095 {
		t.Fatalf("최종 송신값 %v(시작값 %v ±1/4095 예상)", sp[len(sp)-1].c.V, base)
	}
	if math.Abs(float64(h.fb.params[k.id]-base)) > 1.0/4095 {
		t.Fatalf("미러 최종값 %v(%v ±1/4095 예상)", h.fb.params[k.id], base)
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
	// oct 3단(basslineB, 기본 0.85 = +1옥타브 리드 — 2026-09-06): oct- → 0.5 → 0.15 → 0.15 유지.
	pressButton(h, btnAt(h.v, secBassB, "oct-"))
	if c, _ := lastCmd(h); c.V != 0.5 {
		t.Fatalf("oct- → %+v(0.5 예상)", c)
	}
	pressButton(h, btnAt(h.v, secBassB, "oct-"))
	if c, _ := lastCmd(h); c.V != 0.15 {
		t.Fatalf("oct- 2회 → %+v(0.15 예상)", c)
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

// — 계약↔단언: 트랜스포트(PLAY/STOP·DROP — §12.3) —

func TestTransport(t *testing.T) {
	h := newHarness(t)
	// play = PLAY/STOP 토글: 재생 중 탭 → Transport A=0(정지) 정확히 1개.
	h.fb.tick.Playing = true
	pressButton(h, btnAt(h.v, secFx, "play"))
	if len(h.fb.cmds) != 1 || h.fb.cmds[0].c.Kind != engine.Transport || h.fb.cmds[0].c.A != 0 {
		t.Fatalf("play 탭(재생 중) → %+v(Transport A=0 1개 예상)", h.fb.cmds)
	}
	// 정지 중 탭 → A=1(재생).
	h.fb.tick.Playing = false
	h.fb.cmds = nil
	pressButton(h, btnAt(h.v, secFx, "play"))
	if len(h.fb.cmds) != 1 || h.fb.cmds[0].c.Kind != engine.Transport || h.fb.cmds[0].c.A != 1 {
		t.Fatalf("play 탭(정지 중) → %+v(Transport A=1 1개 예상)", h.fb.cmds)
	}
	// rec = DROP: Cmd 1개 + DropTapped 1프레임.
	h.fb.cmds = nil
	pressButton(h, btnAt(h.v, secFx, "rec"))
	if len(h.fb.cmds) != 1 || h.fb.cmds[0].c.Kind != engine.Drop {
		t.Fatalf("rec 탭 → %+v(Drop 1개 예상)", h.fb.cmds)
	}
	if !h.v.DropTapped() {
		t.Fatal("rec 탭 프레임에 DropTapped false")
	}
	h.frame()
	if h.v.DropTapped() {
		t.Fatal("다음 프레임에 DropTapped 잔존")
	}
	// RESUME 폐지: 어떤 탭 이후에도 ResumeTapped false.
	if h.v.ResumeTapped() {
		t.Fatal("rec 탭 프레임에 ResumeTapped true(폐지 예상)")
	}
	h.frame()
	if h.v.ResumeTapped() {
		t.Fatal("다음 프레임에 ResumeTapped 잔존")
	}
	// 라벨: play "PLAY", rec "DROP".
	if b := btnAt(h.v, secFx, "play"); b.label != "PLAY" {
		t.Fatalf("play 라벨 %q(PLAY 예상)", b.label)
	}
	if b := btnAt(h.v, secFx, "rec"); b.label != "DROP" {
		t.Fatalf("rec 라벨 %q(DROP 예상)", b.label)
	}
}

// — 계약↔단언: 한 프레임 플래그 —

// 이름판 탭 = 놓을 때 방(2026-09-06 P5-back-view 개정 — 누르는 즉시였다). 이유: 같은
// 이름판 누름이 길게 누르기(0.5초)로 뒷면 전환의 시작점이 되면서, "누르는 즉시 방"이면
// 뒷면 진입 제스처가 불가능해진다. 스펙 §14.3: 탭(짧게) → 방, 길게 누르기 → 뒷면.
func TestFrameFlags(t *testing.T) {
	h := newHarness(t)
	cx, cy := h.v.titlePlate.Center()
	// 누르는 프레임만으로는 방이 아니다(길게 누르기 후보 상태).
	h.frame(ptrPress(-1, cx, cy))
	if h.v.BackTapped() {
		t.Fatal("누르는 프레임에 BackTapped true(놓을 때 판정 예상)")
	}
	if h.v.rear {
		t.Fatal("누르는 프레임에 뒷면 전환")
	}
	// 짧게 놓으면 탭 — 이 프레임에 방.
	h.frame(ptrRel(-1, cx, cy))
	if !h.v.BackTapped() {
		t.Fatal("이름판 탭 놓기 프레임에 BackTapped false")
	}
	h.frame()
	if h.v.BackTapped() {
		t.Fatal("다음 프레임에 BackTapped 잔존")
	}
}

// — 계약↔단언: 코드 트랙 띠(§12.3) —

// tapCell — 코드 띠 셀 i의 중심 탭(1프레임).
func tapCell(h *harness, i int) {
	cx, cy := h.v.chordCells[i].Center()
	h.frame(ptrPress(-1, cx, cy))
}

// countChordCmds — SetChord 송신 수.
func countChordCmds(h *harness) int {
	n := 0
	for _, r := range h.fb.cmds {
		if r.c.Kind == engine.SetChord {
			n++
		}
	}
	return n
}

func TestChordBandLayout(t *testing.T) {
	h := newHarness(t)
	if n := len(h.v.chordCells); n != engine.ChordBars {
		t.Fatalf("셀 %d개(8 예상)", n)
	}
	w := h.v.chordCells[0][2]
	for i, c := range h.v.chordCells {
		if c[2] != w {
			t.Fatalf("셀 %d 폭 %v(동폭 %v 예상)", i, c[2], w)
		}
		if c[3] != h.v.chordRect[3] || c[1] != h.v.chordRect[1] {
			t.Fatalf("셀 %d 높이/세로 위치가 띠와 다름", i)
		}
	}
	// 간격 = chordGap(3): 피치(셀 시작 간 거리) − 셀폭.
	pitch := h.v.chordCells[1][0] - h.v.chordCells[0][0]
	if d := math.Abs(pitch - w - chordGap); d > 1e-9 {
		t.Fatalf("셀 간격 %v(3 예상)", pitch-w)
	}
	// 끝 정렬: 첫 셀이 띠 왼쪽에, 마지막 셀 오른쪽이 띠 오른쪽에 정확히 닿는다.
	last := h.v.chordCells[engine.ChordBars-1]
	if h.v.chordCells[0][0] != h.v.chordRect[0] ||
		math.Abs(last[0]+last[2]-(h.v.chordRect[0]+h.v.chordRect[2])) > 1e-9 {
		t.Fatal("띠 양 끝 정렬 안 됨")
	}
	// 히트: 셀 중심은 자기 인덱스, 띠 밖은 -1.
	for i, c := range h.v.chordCells {
		cx, cy := c.Center()
		if got := h.v.chordCellAt(cx, cy); got != i {
			t.Fatalf("chordCellAt(셀 %d 중심) = %d", i, got)
		}
	}
	if got := h.v.chordCellAt(h.v.chordRect[0]-1, h.v.chordRect[1]+1); got != -1 {
		t.Fatalf("띠 밖 히트 %d(-1 예상)", got)
	}
}

func TestChordSelector(t *testing.T) {
	h := newHarness(t)
	h.fb.chord[5] = [2]uint8{3, 0}
	// 셀 탭 → 선택기 열림(무송신), 대상 마디 = 셀.
	tapCell(h, 5)
	if !h.v.chord.open || h.v.chord.bar != 5 {
		t.Fatalf("셀 5 탭 후 선택기 (%v, bar %d)", h.v.chord.open, h.v.chord.bar)
	}
	if len(h.fb.cmds) != 0 {
		t.Fatalf("열림 프레임 송신 %d개(0 예상)", len(h.fb.cmds))
	}
	// 도수 셀 탭 → SetChord{A:bar, B:deg, C:flags} 정확히 1개 + 닫힘.
	tapCell(h, 2)
	if n := countChordCmds(h); n != 1 {
		t.Fatalf("도수 탭 SetChord %d개(1 예상)", n)
	}
	c := h.fb.cmds[len(h.fb.cmds)-1].c
	if c.Kind != engine.SetChord || c.A != 5 || c.B != 2 || c.C != 0 {
		t.Fatalf("SetChord = %+v({A:5 B:2 C:0} 예상)", c)
	}
	if h.v.chord.open {
		t.Fatal("도수 확정 후 선택기 열려 있음")
	}
	if h.fb.chord[5] != [2]uint8{2, 0} {
		t.Fatalf("미러 갱신 안 됨: %+v", h.fb.chord[5])
	}
}

func TestChordSeventhToggle(t *testing.T) {
	h := newHarness(t)
	h.fb.chord[5] = [2]uint8{3, 0}
	tapCell(h, 5) // 열림
	tapCell(h, 7) // 7th on → flags 0^1 = 1
	if n := countChordCmds(h); n != 1 {
		t.Fatalf("7th 탭 SetChord %d개(1 예상)", n)
	}
	c := h.fb.cmds[len(h.fb.cmds)-1].c
	if c.Kind != engine.SetChord || c.A != 5 || c.B != 3 || c.C != engine.ChordSeventh {
		t.Fatalf("7th on = %+v({A:5 B:3 C:1} 예상)", c)
	}
	if !h.v.chord.open {
		t.Fatal("7th 탭 후 선택기 닫힘(열림 유지 예상)")
	}
	tapCell(h, 7) // 7th off → flags 1^1 = 0
	if n := countChordCmds(h); n != 2 {
		t.Fatalf("7th 재탭 누적 SetChord %d개(2 예상)", n)
	}
	c = h.fb.cmds[len(h.fb.cmds)-1].c
	if c.B != 3 || c.C != 0 {
		t.Fatalf("7th off = %+v(도수 3·플래그 0 예상)", c)
	}
	if !h.v.chord.open {
		t.Fatal("7th 재탭 후 선택기 닫힘")
	}
}

func TestChordSelectorClose(t *testing.T) {
	h := newHarness(t)
	tapCell(h, 3) // 열림
	if !h.v.chord.open {
		t.Fatal("선택기 안 열림")
	}
	// 띠 밖(빈 판 — 이름판 638·버튼 행 584 사이) 탭 → 닫힘, 무송신, 눌림은 원래 동작 계속.
	h.frame(ptrPress(-1, 360, 615))
	if h.v.chord.open {
		t.Fatal("밖 탭 후 선택기 열려 있음")
	}
	if len(h.fb.cmds) != 0 {
		t.Fatalf("밖 탭 송신 %d개(0 예상)", len(h.fb.cmds))
	}
	// 6초 무조작: 5초까지 열림, 6.5초에 닫힘(송신 없음).
	tapCell(h, 3)
	h.run(300) // 5.0s
	if !h.v.chord.open {
		t.Fatal("5초 무조작에 닫힘(6초 예상)")
	}
	h.run(90) // 누적 6.5s
	if h.v.chord.open {
		t.Fatal("6.5초 무조작에도 열려 있음")
	}
	if n := countChordCmds(h); n != 0 {
		t.Fatalf("무조작 닫힘 송신 %d개(0 예상)", n)
	}
}

func TestChordLabels(t *testing.T) {
	h := newHarness(t)
	h.fb.chord[2] = [2]uint8{3, 0} // 도수 3 → "iv"
	h.frame()
	if got := h.v.chord.cells[2].text; got != "iv" {
		t.Fatalf("도수 3 라벨 %q(iv 예상)", got)
	}
	h.fb.chord[2] = [2]uint8{3, engine.ChordSeventh}
	h.frame()
	if got := h.v.chord.cells[2].text; got != "iv7" {
		t.Fatalf("도수 3 + 7th 라벨 %q(iv7 예상)", got)
	}
	// 입력 방어: 범위 밖 도수 10은 10%7 = 3으로 그린다("?" 아님).
	h.fb.chord[4] = [2]uint8{10, 0}
	h.frame()
	if got := h.v.chord.cells[4].text; got != "iv" {
		t.Fatalf("도수 10 라벨 %q(deg%%7 = iv 예상)", got)
	}
	// 캐시 계약: 값 불변 프레임 재구성 0, 변화 시 정확히 1.
	base := h.v.rebuilds
	h.run(60)
	if h.v.rebuilds != base {
		t.Fatalf("값 불변 60프레임 재구성 %d회(0 예상)", h.v.rebuilds-base)
	}
	h.fb.chord[6] = [2]uint8{2, 0}
	h.frame()
	if h.v.rebuilds != base+1 {
		t.Fatalf("셀 1개 변화 재구성 %d회(1 예상)", h.v.rebuilds-base)
	}
	if got := h.v.chord.cells[6].text; got != "III" {
		t.Fatalf("도수 2 라벨 %q(III 예상)", got)
	}
}

// panicBridge — 화성 API(Chord/Mode/KeyRoot)가 패닉나는 브리지 — 구 호스트(P2 이전
// host.js, jsBridge.Chord의 "property chord is not a function") 재현. 나머지는 fakeBridge 위임.
type panicBridge struct {
	*fakeBridge
	chordCalls int
}

func (p *panicBridge) Chord(int) (uint8, uint8)        { p.chordCalls++; panic("chord is not a function") }
func (p *panicBridge) Mode(engine.Part) (uint8, uint8) { panic("mode is not a function") }
func (p *panicBridge) KeyRoot() int                    { panic("keyRoot is not a function") }

// TestHarmonyGuard — 구 브리지에서 기기 뷰가 죽지 않는다: 첫 패닉을 래치하고 기본값
// (도수 0 "i"·모드 BASS·키 C)으로 동작, 이후 화성 읽기는 브리지를 다시 부르지 않는다.
func TestHarmonyGuard(t *testing.T) {
	h := newHarness(t)
	pb := &panicBridge{fakeBridge: h.fb}
	h.ctx.Bridge = pb
	h.frame() // 패닉이 뷰 밖으로 새면 테스트 프로세스 자체가 죽는다(그것이 게이트)
	if h.v.harmonyOK {
		t.Fatal("패닉 후 harmonyOK 잔존")
	}
	if got := h.v.chord.cells[0].text; got != "i" {
		t.Fatalf("기본 도수 라벨 %q(i 예상)", got)
	}
	if got := h.v.disp[1].text; got != "BASS" {
		t.Fatalf("기본 모드 문자열 %q(BASS 예상)", got)
	}
	if got := h.v.bottom.text; got != "Cm 130 B0 INTRO" {
		t.Fatalf("기본 하단 표시창 %q(Cm 130 B0 INTRO 예상)", got)
	}
	// 래치 뒤 상호작용: 선택기·모드 순환도 무패닉, Chord 실호출은 최초 1회뿐.
	tapCell(h, 5)
	tapCell(h, 2) // SetChord 송신(미러는 fakeBridge가 흡수)
	if n := countChordCmds(h); n != 1 {
		t.Fatalf("구 브리지에서도 SetChord %d개(1 예상)", n)
	}
	if pb.chordCalls != 1 {
		t.Fatalf("Chord 실호출 %d회(래치 뒤 재호출 없이 1 고정 예상)", pb.chordCalls)
	}
}

// — 계약↔단언: B 모드 순환·표시창(§12.3) —

func TestBassModeCycle(t *testing.T) {
	h := newHarness(t)
	cx, cy := h.v.dispRects[1].Center()
	// BASS에서 5탭: (1,0) → (1,1) → (1,2) → (2,0) → (0,0). A는 항상 파트 B(1).
	want := [5][2]uint8{{engine.ModeArp, engine.DirUp}, {engine.ModeArp, engine.DirDown},
		{engine.ModeArp, engine.DirUpDown}, {engine.ModeChord, engine.DirUp}, {engine.ModeBass, engine.DirUp}}
	h.frame(ptrPress(-1, cx, cy))
	if got := h.v.disp[1].text; got != "ARP UP" {
		t.Fatalf("1탭 후 B 표시창 %q(ARP UP 예상)", got)
	}
	for i := 1; i < 5; i++ {
		h.frame(ptrPress(-1, cx, cy))
	}
	var got [][2]uint8
	for _, r := range h.fb.cmds {
		if r.c.Kind == engine.BassMode {
			if r.c.A != uint8(engine.BassB) {
				t.Fatalf("BassMode A = %d(파트 B 예상)", r.c.A)
			}
			got = append(got, [2]uint8{r.c.B, r.c.C})
		}
	}
	if len(got) != 5 {
		t.Fatalf("BassMode 송신 %d개(5 예상)", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d번째 탭 (%d,%d), (%d,%d) 예상", i+1, got[i][0], got[i][1], want[i][0], want[i][1])
		}
	}
	if got := h.v.disp[1].text; got != "BASS" {
		t.Fatalf("5탭 후 B 표시창 %q(BASS 예상)", got)
	}
	// 범위 밖 mode·dir은 나머지로 정규화: (7,9) ≡ (Arp,Up) → 다음은 (Arp,Down).
	if m, d := nextBassMode(7, 9); m != engine.ModeArp || d != engine.DirDown {
		t.Fatalf("nextBassMode(7,9) = (%d,%d)((%d,%d) 예상)", m, d, engine.ModeArp, engine.DirDown)
	}
	// 모드 문자열 5종 전부.
	for _, c := range []struct {
		m, d uint8
		want string
	}{{0, 0, "BASS"}, {1, 0, "ARP UP"}, {1, 1, "ARP DN"}, {1, 2, "ARP UD"}, {2, 0, "CHORD"}} {
		if got := bassModeName(c.m, c.d); got != c.want {
			t.Fatalf("bassModeName(%d,%d) = %q(%q 예상)", c.m, c.d, got, c.want)
		}
	}
}

func TestBassModeDisplayWindow(t *testing.T) {
	h := newHarness(t)
	h.frame()
	if got := h.v.disp[1].text; got != "BASS" {
		t.Fatalf("초기 B 표시창 %q(BASS 예상)", got)
	}
	// B 섹션 노브 접촉 → 값 표시(CUTOFF 기본값 × 99 반올림 — 기본값은 엔진 표에서).
	k := knobAt(h.v, secBassB, "CUTOFF")
	wantCut := fmt.Sprintf("CUT %d", int32(engine.DefaultParams()[engine.CutoffB]*99+0.5))
	h.frame(ptrPress(-1, k.cx, k.cy))
	if got := h.v.disp[1].text; got != wantCut {
		t.Fatalf("노브 접촉 직후 B 표시창 %q(%s 예상)", got, wantCut)
	}
	// 1초 유지(값 불변) — 여전히 값.
	h.hold(-1, k.cx, k.cy, 60)
	if got := h.v.disp[1].text; got != wantCut {
		t.Fatalf("1초 후 B 표시창 %q(%s 유지 예상)", got, wantCut)
	}
	// 2초 경과 — 모드 문자열 복귀(값이 안 움직이면 knobT가 이어붙지 않는다).
	h.hold(-1, k.cx, k.cy, 150)
	if got := h.v.disp[1].text; got != "BASS" {
		t.Fatalf("3.5초 후 B 표시창 %q(BASS 복귀 예상)", got)
	}
}

// — 계약↔단언: 하단 표시창("Am 120 B3 BUILD" 포맷 — §12.3) —

func TestBottomDisplay(t *testing.T) {
	h := newHarness(t)
	h.fb.key = 9                                   // A
	h.fb.params[engine.Tempo] = float32(1.0 / 3.0) // BPMOf = 100+60/3 = 120
	h.fb.tick.Bar = 3
	h.ctx.Phase = 1 // Build
	h.frame()
	if got := h.v.bottom.text; got != "Am 120 B3 BUILD" {
		t.Fatalf("하단 표시창 %q(Am 120 B3 BUILD 예상)", got)
	}
	// MANUAL 잠금 중 페이즈 이름 치환.
	h.ctx.ManualLocked = true
	h.frame()
	if got := h.v.bottom.text; got != "Am 120 B3 MANUAL" {
		t.Fatalf("잠금 중 하단 표시창 %q(Am 120 B3 MANUAL 예상)", got)
	}
	// 키 변화: 11 = B.
	h.ctx.ManualLocked = false
	h.fb.key = 11
	h.frame()
	if got := h.v.bottom.text; got != "Bm 120 B3 BUILD" {
		t.Fatalf("키 B 하단 표시창 %q(Bm 120 B3 BUILD 예상)", got)
	}
	// 키 정규화: appendKey는 12로 나머지(13 → 1 = C#).
	if b := appendKey(nil, 13); string(b) != "C#m" {
		t.Fatalf("appendKey(13) = %q(C#m 예상)", b)
	}
}

// — 계약↔단언: 정상 상태 할당 0 —

func TestUpdateSteadyNoAlloc(t *testing.T) {
	h := newHarness(t)
	h.fb.key = 9
	h.fb.tick.Bar = 3
	h.ctx.Phase = 1
	h.run(120) // 워밍업 — 문자열 캐시 전부 1회 구성
	allocs := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) })
	if allocs != 0 {
		t.Fatalf("정상 상태 Update 힙 할당 %.0f회/프레임(0 예상)", allocs)
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
	if h.v.bottom.text != "Cm 130 B0 INTRO" {
		t.Fatalf("하단 표시창 %q(Cm 130 B0 INTRO 예상)", h.v.bottom.text)
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

// — 계약↔단언: 노브 라벨 표시명(비전 처방 4 — 드럼 내부명 노출 금지) —

func TestKnobDisplayNames(t *testing.T) {
	h := newHarness(t)
	// 드럼: 내부 파라미터명(BD_LEVEL)이 아니라 표시명(LEVEL) — 보이스명은 패드가 담당.
	for _, name := range []string{"BD_LEVEL", "SD_LEVEL", "CH_LEVEL", "OH_LEVEL", "CP_LEVEL", "CY_LEVEL"} {
		k := knobAt(h.v, secDrums, name)
		if k == nil {
			t.Fatalf("드럼 노브 %s 없음", name)
		}
		if k.label != "LEVEL" {
			t.Fatalf("%s 라벨 %q(LEVEL 예상)", name, k.label)
		}
	}
	for _, name := range []string{"BD_TUNE", "CY_TUNE"} {
		if k := knobAt(h.v, secDrums, name); k.label != "TUNE" {
			t.Fatalf("%s 라벨 %q(TUNE 예상)", name, k.label)
		}
	}
	// 베이스라인·fx는 레이아웃 이름 그대로.
	if k := knobAt(h.v, secBassA, "CUTOFF"); k.label != "CUTOFF" {
		t.Fatalf("CUTOFF 라벨 %q", k.label)
	}
	if k := knobAt(h.v, secFx, "MASTER"); k.label != "MASTER" {
		t.Fatalf("MASTER 라벨 %q", k.label)
	}
}

// — 계약↔단언: 버튼 라벨 축소 규칙(비전 처방 6 — 예산 = rect 폭 − 4, 하한 0.3) —

func TestShrinkScale(t *testing.T) {
	cases := []struct {
		base, w, maxW, want float64
	}{
		{0.45, 42.4, 43, 0.45},             // 예산 안 — 불변(RESUME 실측 42.4 vs 47−4)
		{0.45, 30.9, 30, 0.45 * 30 / 30.9}, // SLIDE 실측 30.9 vs 34−4 — 비례 축소
		{0.45, 94.1, 30, 0.3},              // 축소해도 하한 밑 — 0.3
		{0.45, 0, 30, 0.45},                // 빈 라벨 — 나눗셈 없음
	}
	for _, c := range cases {
		if got := shrinkScale(c.base, c.w, c.maxW); math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("shrinkScale(%v, %v, %v) = %v(%v 예상)", c.base, c.w, c.maxW, got, c.want)
		}
	}
	// 규칙의 산출 스케일에서 다시 재면 예산 안이어야 한다(하한이 걸린 경우는 제외).
	if s := shrinkScale(0.45, 30.9, 30); s*30.9/0.45 > 30+1e-9 {
		t.Fatalf("축소 후 폭 %v가 예산 초과", s*30.9/0.45)
	}
}

// — 계약↔단언: 스코프 자동 이득(비전 처방 7) —

func TestScopeGain(t *testing.T) {
	cases := []struct {
		peak, want float32
	}{
		{0, 8},     // 무신호/빈 창도 ×8(중앙선)
		{0.04, 8},  // 노이즈 바닥(< 0.05) — ×8
		{0.05, 8},  // 경계 — 1/0.05 = 20이 상한에 걸림
		{0.125, 8}, // 1/0.125 = 8 = 상한
		{0.2, 5},   // 1/peak — 창의 peak가 진폭 예산을 정확히 채움
		{0.4, 2.5},
		{0.5, 2},
		{1, 1}, // 클램프 상단 — 이득 없음
	}
	for _, c := range cases {
		if got := scopeGain(c.peak); math.Abs(float64(got-c.want)) > 1e-5 {
			t.Fatalf("scopeGain(%v) = %v(%v 예상)", c.peak, got, c.want)
		}
	}
}

// — 계약↔단언: 스코프 DC 제거(2차 비전 처방 — 하이패스·트리거) —

func TestScopeHighpass(t *testing.T) {
	// DC 0.5 상수 입력 → 출력 평균 |y| < 0.02. 하이패스 상태는 0에서 출발(앱 부팅과 동일)하므로
	// 첫 창들은 과도기다 — 20Hz 계수의 감쇠 시정수(≈385샘플)만큼 돌린 뒤 마지막 창으로 판정한다.
	// 실기기에서는 상태가 프레임 간 유지되므로 부팅 수십 ms 후 항상 정상 상태다.
	var x1, y1 float32
	const windows = 8
	for i := 0; i < windows*scopeSamples; i++ {
		y := hpStep(0.5, x1, y1)
		x1, y1 = 0.5, y
	}
	sum := 0.0
	for i := 0; i < scopeSamples; i++ {
		sum += math.Abs(float64(y1))
		y1 = hpStep(0.5, x1, y1)
		x1 = 0.5
	}
	if mean := sum / scopeSamples; mean >= 0.02 {
		t.Fatalf("DC 0.5 정상 상태 평균 |y| = %v(< 0.02 예상)", mean)
	}
	// 교대 입력(+0.5/−0.5)은 통과시킨다 — 하이패스가 교류 성분을 죽이지 않는다는 최소 확인.
	x1, y1 = 0, 0
	peak := float32(0)
	for i := 0; i < 4*scopeSamples; i++ {
		x := float32(0.5)
		if i%2 == 1 {
			x = -0.5
		}
		y1 = hpStep(x, x1, y1)
		x1 = x
		if y1 > peak {
			peak = y1
		} else if -y1 > peak {
			peak = -y1
		}
	}
	if peak < 0.5 {
		t.Fatalf("교대 입력 통과 peak = %v(≥ 0.5 예상 — 교류 성분 보존)", peak)
	}
}

func TestScopeTrigger(t *testing.T) {
	if i := scopeTrigger(make([]float32, 8)); i != 0 {
		t.Fatalf("무신호(전부 0) 트리거 %d(0 예상)", i)
	}
	if i := scopeTrigger([]float32{0.5, 0.2, -0.1, -0.3}); i != 0 {
		t.Fatalf("양수만 있는 창 트리거 %d(0 예상)", i)
	}
	if i := scopeTrigger([]float32{-0.3, -0.1, 0.2, 0.5, 0.1, -0.2, -0.4}); i != 2 {
		t.Fatalf("상승 제로크로싱 트리거 %d(2 예상)", i)
	}
	// 하강 전이(양→음)는 트리거가 아니다 — 그다음 상승(인덱스 4)을 잡는다.
	if i := scopeTrigger([]float32{0.5, 0.2, -0.1, -0.3, 0.1}); i != 4 {
		t.Fatalf("하강 후 재상승 트리거 %d(4 예상)", i)
	}
}

func TestScopeRemoveMean(t *testing.T) {
	// 상수(DC) 창 → 전부 0: 자동 이득이 오프셋을 증폭할 수 없다.
	b := make([]float32, 64)
	for i := range b {
		b[i] = 0.5
	}
	removeMean(b)
	for i, s := range b {
		if s != 0 {
			t.Fatalf("DC 창 제거 후 b[%d] = %v(0 예상)", i, s)
		}
	}
	// 비대칭 창(한쪽으로 치우친 혹) → 평균 0: 중앙선 계약의 축.
	b = []float32{1, 1, 1, -3}
	removeMean(b)
	sum := float32(0)
	for _, s := range b {
		sum += s
	}
	if sum != 0 {
		t.Fatalf("제거 후 합 %v(0 예상)", sum)
	}
	// 교류 성분은 크기를 보존한다(진폭 축소 없음).
	b = []float32{0.5, -0.5, 0.5, -0.5}
	removeMean(b)
	if b[0] != 0.5 || b[1] != -0.5 {
		t.Fatalf("교류 창 변형 %v([0.5 -0.5 ...] 예상)", b)
	}
}

// — 계약↔단언: 스텝 버튼 면 색(게이트 1의 색 축 — 주황 면 존재 R−B ≥ 40) —

func TestStepFaceColor(t *testing.T) {
	if d := int(colStepFace.R) - int(colStepFace.B); d < 40 {
		t.Fatalf("스텝 면 R−B = %d(≥ 40 예상)", d)
	}
	if g := int(colStepFace.G); g < 60 || g > 130 {
		t.Fatalf("스텝 면 G = %d(패널 중앙값 (146,94,59) 대역 예상)", g)
	}
}

// — 계약↔단언: 폴리 리드 모듈 바인딩(P5-poly — §14.1 DeviceParam) —

// devHostileBridge — DevParam이 고정값 하나만 돌려주는 적대 브리지(3클래스 입력 방어:
// 음수(미러 부재 신호)·NaN·1 초과). 나머지는 fakeBridge에 위임한다.
type devHostileBridge struct {
	*fakeBridge
	v float32
}

func (b *devHostileBridge) DevParam(slot, k int) float32 { return b.v }

// TestPolyKnobDevParam — 장치 로컬 노브의 값 소스·송신·잔존 없음. dev 분기는
// knobValue·sendParam에만 있으므로 히트·드래그 상태기계는 전역 노브과 같은 경로를 쓴다.
func TestPolyKnobDevParam(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secPoly, "CUTOFF")
	if k == nil {
		t.Fatal("poly CUTOFF 노브 없음")
	}
	if !k.dev || k.slot != engine.SlotPoly || k.k != engine.PolyCutoff {
		t.Fatalf("CUTOFF 매핑 (dev %v, 슬롯 %d, k %d)((true, %d, %d) 예상)", k.dev, k.slot, k.k, engine.SlotPoly, engine.PolyCutoff)
	}
	// 값 소스: 전역 파라미터가 아니라 미러(devParams[0]). 기본값 0이 아닌 0.25를 심어 구분.
	h.fb.devParams[0] = 0.25
	h.frame()
	if got := h.v.knobValue(h.ctx, k); math.Abs(float64(got)-0.25) > 1e-6 {
		t.Fatalf("미러 표시값 %v(0.25 예상)", got)
	}
	// 스크롤 상태(cy 1886 → 화면 y = cy − scrollY)에서 드래그 → DeviceParam 정확히 1개,
	// SetParam 0개(전역 파라미터 오염 봉쇄). 200px 상승 = 0.25+1.0 → 클램프 1.
	h.v.scrollY = h.v.scrollMax - 20
	sy := k.cy - h.v.scrollY
	if sy < 0 || sy >= core.LogicalH {
		t.Fatalf("화면 y %v가 화면 밖(scrollMax %v)", sy, h.v.scrollMax)
	}
	h.frame(ptrPress(-1, k.cx, sy))
	if _, ok := h.v.JustGrabbed(); ok {
		t.Fatal("dev 노브 잡음이 JustGrabbed에 전역 ParamID로 보고됨(레지던트 거짓 잠금 봉쇄 위반)")
	}
	h.fb.cmds = nil
	h.frame(ptrMove(-1, k.cx, sy-200))
	dp, sp := 0, 0
	var last engine.Cmd
	for _, r := range h.fb.cmds {
		switch r.c.Kind {
		case engine.DeviceParam:
			dp++
			last = r.c
		case engine.SetParam:
			sp++
		}
	}
	if dp != 1 || sp != 0 {
		t.Fatalf("드래그 프레임 DeviceParam %d·SetParam %d(1·0 예상)", dp, sp)
	}
	if last.A != uint8(engine.SlotPoly) || last.B != uint8(engine.PolyCutoff) || last.V != 1 {
		t.Fatalf("DeviceParam = %+v({A:7 B:0 V:1} 예상)", last)
	}
	if h.fb.devParams[0] != 1 {
		t.Fatalf("미러 갱신 안 됨 %v(1 예상)", h.fb.devParams[0])
	}
	// 무변화 프레임 → 송신 0(전역 노브 계약과 같은 절약).
	h.fb.cmds = nil
	h.frame(ptrMove(-1, k.cx, sy-200))
	if len(h.fb.cmds) != 0 {
		t.Fatalf("무변화 프레임 송신 %d개(0 예상)", len(h.fb.cmds))
	}
	// 이동 200px ≥ 탭 → 릴리스에 스윕 없음. useLocal 해제 → 표시값은 다시 미러를 따른다
	// (뷰 전환 후에도 로컬 값이 얼어붙지 않는다는 잔존 클래스의 직접 단언).
	h.fb.devParams[0] = 0.4
	h.frame(ptrRel(-1, k.cx, sy-200))
	if k.swActive {
		t.Fatal("이동 릴리스에 스윕 시작(탭 아닌데)")
	}
	if got := h.v.knobValue(h.ctx, k); math.Abs(float64(got)-0.4) > 1e-6 {
		t.Fatalf("릴리스 후 표시값 %v(미러 0.4 예상 — 로컬 잔존)", got)
	}
	// 3클래스 입력 방어: 미러 부재(음수)·NaN → 기본값 폴백, 1 초과 → 클램프.
	wantDef := core.DevParamDefault(engine.SlotPoly, engine.PolyCutoff)
	for _, c := range []struct {
		v    float32
		want float32
	}{{-1, wantDef}, {math.Float32frombits(0x7F800001), wantDef}, {2, 1}, {0.7, 0.7}} {
		h.ctx.Bridge = &devHostileBridge{fakeBridge: h.fb, v: c.v}
		if got := h.v.knobValue(h.ctx, k); got != c.want {
			t.Fatalf("적대 미러 %v → 표시값 %v(%v 예상)", c.v, got, c.want)
		}
	}
	h.ctx.Bridge = h.fb
}

// TestPolyKnobSweep — 탭 스윕도 sendParam을 경유하므로 DeviceParam뿐이다: 스윕 전체에서
// SetParam 0개, 최종 송신값 = 시작값(미러) ±1/4095.
func TestPolyKnobSweep(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secPoly, "LEVEL") // k = 7 — B 비트까지 정확히 보는 쪽
	if k == nil || k.k != engine.PolyLevel {
		t.Fatal("poly LEVEL 노브 없음(k 7 예상)")
	}
	h.fb.devParams[engine.PolyLevel] = 0.3
	h.frame(ptrPress(-1, k.cx, k.cy-h.v.scrollY))
	h.frame(ptrRel(-1, k.cx, k.cy-h.v.scrollY)) // 이동 0 → 탭 → 2바 스윕
	if !k.swActive {
		t.Fatal("탭 릴리스에 스윕 미시작")
	}
	h.fb.cmds = nil
	h.run(300) // 130BPM 2바 ≈ 3.69s + 여유
	dp, sp := 0, 0
	var lastV float32
	for _, r := range h.fb.cmds {
		if r.c.Kind == engine.DeviceParam {
			if r.c.A != uint8(engine.SlotPoly) || r.c.B != uint8(engine.PolyLevel) {
				t.Fatalf("DeviceParam = %+v(A 7·B 7 예상)", r.c)
			}
			dp++
			lastV = r.c.V
		}
		if r.c.Kind == engine.SetParam {
			sp++
		}
	}
	if dp < 10 || sp != 0 {
		t.Fatalf("스윕 송신 DeviceParam %d·SetParam %d(≥10·0 예상)", dp, sp)
	}
	if math.Abs(float64(lastV-0.3)) > 1.0/4095 {
		t.Fatalf("최종 송신값 %v(시작값 0.3 ±1/4095 예상)", lastV)
	}
	if math.Abs(float64(h.fb.devParams[engine.PolyLevel]-0.3)) > 1.0/4095 {
		t.Fatalf("미러 최종값 %v(0.3 ±1/4095 예상)", h.fb.devParams[engine.PolyLevel])
	}
	if k.swActive || k.useLocal {
		t.Fatal("스윕 완료 후 활성/로컬 잔존")
	}
}

// TestPolyLabels — 라벨 축약 표(r25 라벨판 폭)·섹션 띠 임시색(와이어프레임 틴트).
func TestPolyLabels(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"CUTOFF", "CUT"}, {"RESO", "RESO"}, {"ENV", "ENV"}, {"ATTACK", "ATK"},
		{"DECAY", "DEC"}, {"RELEASE", "REL"}, {"DETUNE", "DET"}, {"LEVEL", "LVL"},
	} {
		if got := knobLabel(secPoly, c.name); got != c.want {
			t.Fatalf("knobLabel(poly, %s) = %q(%q 예상)", c.name, got, c.want)
		}
	}
	if got := knobLabel(secPoly, "TEMPO"); got != "TEMPO" {
		t.Fatalf("표 밖 이름 %q(그대로 예상)", got)
	}
	if colPlateBand[6] != (color.NRGBA{R: 42, G: 133, B: 178, A: 255}) {
		t.Fatalf("poly 띠색 %v((42,133,178) 예상 — 채택 패널 seed 7 띠 중앙값 실측, 2026-09-06 리드 재조정)", colPlateBand[6])
	}
	if len(colPlateBand) != 7 {
		t.Fatalf("띠색 표 %d항(7 예상)", len(colPlateBand))
	}
}

// TestPolyTrigDot — 트리거 점 알파: 미점화 0, 점화 프레임 1, 150ms(9프레임) 뒤 < 0.1.
// 시계 감쇠 이전 프레임(경계 이동 등)도 안전하게 1로 클램프.
func TestPolyTrigDot(t *testing.T) {
	h := newHarness(t)
	if a := h.v.polyTrigA(h.ctx.Now); a != 0 {
		t.Fatalf("미점화 α = %v(0 예상)", a)
	}
	h.fb.tick.Flags = engine.FlagPoly
	h.frame()
	h.fb.tick.Flags = 0
	if a := h.v.polyTrigA(h.ctx.Now); a != 1 {
		t.Fatalf("점화 프레임 α = %v(1 예상)", a)
	}
	if a := h.v.polyTrigA(h.ctx.Now - 0.05); a != 1 {
		t.Fatalf("감쇠 이전 경계 α = %v(1 클램프 예상)", a)
	}
	h.run(9) // 9 × 1/60 = 0.15s — e^−3 ≈ 0.0498
	if a := h.v.polyTrigA(h.ctx.Now); a >= 0.1 {
		t.Fatalf("150ms 뒤 α = %v(< 0.1 예상)", a)
	}
	h.run(60)
	if a := h.v.polyTrigA(h.ctx.Now); a > 1.0/255 {
		t.Fatalf("2.5s 뒤 α = %v(사실상 0 예상)", a)
	}
}

// TestPolyAbsentOldLayout — 방어 1: poly 노브·이름판이 없는 구(v3) 레이아웃에서도
// newView는 성공해야 한다(폴백 경로는 KnobDevParam 실패 = 그냥 그 노브가 없을 뿐).
// dev 노브 0개·poly 판 부재·스크롤 상한은 레이아웃 높이에서 유도되므로 그대로.
func TestPolyAbsentOldLayout(t *testing.T) {
	l, err := core.LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	maxWithPoly := l.Size[1] - core.LogicalH
	knobs := make([]core.Knob, 0, len(l.Knobs))
	for _, k := range l.Knobs {
		if k.Section != "poly" {
			knobs = append(knobs, k)
		}
	}
	l.Knobs = knobs
	plates := make([]core.For, 0, len(l.Plates))
	for _, p := range l.Plates {
		if p.For != "poly" {
			plates = append(plates, p)
		}
	}
	l.Plates = plates
	v, err := newView(l)
	if err != nil {
		t.Fatalf("poly 없는 레이아웃 newView: %v", err)
	}
	for _, k := range v.knobs {
		if k.dev {
			t.Fatalf("dev 노브 잔존 %s", k.name)
		}
	}
	if v.hasSection[secPoly-2] {
		t.Fatal("poly 이름판이 없는데 hasSection[4] true")
	}
	if math.Abs(v.scrollMax-maxWithPoly) > 1e-9 {
		t.Fatalf("scrollMax %v(%v 예상 — 높이 유도)", v.scrollMax, maxWithPoly)
	}
}

// TestPolyUnknownKnobName — poly 섹션의 매핑 없는 이름(전역 이름 차용 포함)은 레이아웃
// 오류(기존 계약 유지) — 조용한 무시가 아니라 New 실패.
func TestPolyUnknownKnobName(t *testing.T) {
	l, err := core.LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	for i := range l.Knobs {
		if l.Knobs[i].Section == "poly" {
			l.Knobs[i].Name = "TEMPO" // 전역 fx 이름 — poly에선 매핑 없음
			break
		}
	}
	_, err = newView(l)
	if err == nil {
		t.Fatal("매핑 없는 poly 노브 이름이 newView를 통과")
	}
	if !strings.Contains(err.Error(), "매핑 없음") {
		t.Fatalf("에러 문구 %q(기존 계약 \"매핑 없음\" 예상)", err.Error())
	}
}
