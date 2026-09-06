// scroll_test.go — P4-scroll 스크롤 랙 계약 단언(§13.3). device_test.go 헤더의
// P4-scroll 계약↔단언 표가 원본이다. 휠 자체는 헤드리스에서 ebiten.Wheel()이 항상 0이라
// 구동 불가 — clamp·stepScroll 단위 단언과 함께 제품 측정(measure.mjs)이 근거다.
//
// 좌표 관례: 빈 판 지점 (700,950)은 fx 패널 안이지만 어떤 컨트롤과도 겹치지 않는다
// (TEMPO 노브 (619,1010) r42+6까지 거리 ≈ 73px — 레이아웃 실측).
package device

import (
	"math"
	"testing"

	"github.com/midagedev/jangdan/app/assets"
	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// emptyX/Y — 스크롤 제스처용 빈 판 지점(컨트롤 없음, scrollY 0에서).
const (
	emptyX = 700.0
	emptyY = 950.0
)

// TestScrollDragClamp — 빈 판 잡기 드래그: dy만큼 scrollY 이동, 0..520 클램프.
// 잡는 순간 이전 관성은 끊긴다(새 잡기 v=0). 놓기만 하고 이동 0이면 관성도 없다.
func TestScrollDragClamp(t *testing.T) {
	h := newHarness(t)
	if m := h.v.scrollMax; math.Abs(m-520) > 1e-9 {
		t.Fatalf("scrollMax = %v(1800−1280 = 520 예상)", m)
	}
	// 이동 없이 눌렀다 놓기 → 관성 없음(잡는 순간 v=0이고 이동 프레임이 없다).
	h.frame(ptrPress(-1, emptyX, emptyY))
	h.frame(ptrRel(-1, emptyX, emptyY))
	if h.v.scrollV != 0 {
		t.Fatalf("무이동 놓기 직후 scrollV = %v(0 예상)", h.v.scrollV)
	}
	h.run(10)
	if h.v.scrollY != 0 {
		t.Fatalf("무이동 놓기 10프레임 후 scrollY = %v(0 예상)", h.v.scrollY)
	}
	// 위로 200px 드래그 → scrollY 200.
	h.frame(ptrPress(-1, emptyX, emptyY))
	h.frame(ptrMove(-1, emptyX, emptyY-200))
	if math.Abs(h.v.scrollY-200) > 1e-9 {
		t.Fatalf("드래그 −200px 후 scrollY = %v(200 예상)", h.v.scrollY)
	}
	// 과다 드래그 → 상한 520 클램프(잡은 채).
	h.frame(ptrMove(-1, emptyX, emptyY-2000))
	if h.v.scrollY != h.v.scrollMax {
		t.Fatalf("과다 드래그 후 scrollY = %v(%v 클램프 예상)", h.v.scrollY, h.v.scrollMax)
	}
	// 아래로 과다 → 하한 0 클램프.
	h.frame(ptrMove(-1, emptyX, emptyY+2000))
	if h.v.scrollY != 0 {
		t.Fatalf("하방 과다 드래그 후 scrollY = %v(0 클램프 예상)", h.v.scrollY)
	}
	// 하한에서 큰 속도로 놓아도 경계에서 즉시 정지(반발 없음 — TestScrollInertia 경계 변주와 짝).
	h.frame(ptrRel(-1, emptyX, emptyY+2000))
	h.run(10)
	if h.v.scrollY != 0 || h.v.scrollV != 0 {
		t.Fatalf("경계 놓기 10프레임 후 scrollY %v·v %v(0·0 예상)", h.v.scrollY, h.v.scrollV)
	}
}

// TestKnobDragNoScroll — 컨트롤 우선(§13.3): 노브에서 시작한 드래그는 끝까지 노브.
// 노브 값은 변하고 scrollY는 변하지 않는다.
func TestKnobDragNoScroll(t *testing.T) {
	h := newHarness(t)
	k := knobAt(h.v, secBassA, "CUTOFF") // 기본값 0.45
	h.frame(ptrPress(-1, k.cx, k.cy))
	if id, ok := h.v.JustGrabbed(); !ok || id != k.id {
		t.Fatalf("JustGrabbed = (%d,%v)(노브 %d 예상)", id, ok, k.id)
	}
	h.frame(ptrMove(-1, k.cx, k.cy-200))
	if got := h.fb.params[k.id]; math.Abs(float64(got)-1) > 1e-6 {
		t.Fatalf("노브 드래그 후 값 %v(1.0 예상)", got)
	}
	if h.v.scrollY != 0 {
		t.Fatalf("노브 드래그에 scrollY = %v(0 불변 예상)", h.v.scrollY)
	}
	h.frame(ptrRel(-1, k.cx, k.cy-200)) // 이동 200px → 탭 아님
	h.run(30)
	if h.v.scrollY != 0 {
		t.Fatalf("노브 릴리스 후에도 scrollY = %v(0 불변 예상)", h.v.scrollY)
	}
}

// TestScrollInertia — 놓으면 직전 프레임 dy로 관성: ×0.9/프레임 감쇠, |v|<2px 정지.
// 경계에 닿아 더 못 가면 즉시 정지(반발 없음).
func TestScrollInertia(t *testing.T) {
	h := newHarness(t)
	// v = −30(px/프레임)로 놓기: 총 이동 ≈ 30·Σ0.9^k ≈ 300px — 520에 못 미친다.
	h.frame(ptrPress(-1, emptyX, emptyY))
	h.frame(ptrMove(-1, emptyX, emptyY-30))
	h.frame(ptrRel(-1, emptyX, emptyY-30))
	final := 0.0
	stopped := -1
	for i := 0; i < 60; i++ {
		h.frame()
		if h.v.scrollV == 0 {
			stopped = i
			final = h.v.scrollY
			break
		}
		if h.v.scrollY < 0 || h.v.scrollY > h.v.scrollMax {
			t.Fatalf("관성 중 클램프 이탈 scrollY = %v@%d", h.v.scrollY, i)
		}
	}
	if stopped < 0 {
		t.Fatalf("60프레임 내 관성 미정지(v = %v)", h.v.scrollV)
	}
	if final < 280 || final > 340 {
		t.Fatalf("관성 종착 scrollY = %v(≈311 예상 범위 밖)", final)
	}
	h.run(30)
	if h.v.scrollY != final || h.v.scrollV != 0 {
		t.Fatalf("정지 후 변동 scrollY %v·v %v(%v·0 불변 예상)", h.v.scrollY, h.v.scrollV, final)
	}
	// 경계 정지: 상한에 담긴 채 큰 속도로 놓으면 다음 프레임에 즉시 정지.
	h.v.scrollY, h.v.scrollV = h.v.scrollMax, -800
	h.frame()
	if h.v.scrollY != h.v.scrollMax || h.v.scrollV != 0 {
		t.Fatalf("경계 관성 후 scrollY %v·v %v(%v·0 즉시 정지 예상)", h.v.scrollY, h.v.scrollV, h.v.scrollMax)
	}
}

// TestScrolledKnobHit — 좌표 변환의 단일 소유자 증명: 화면 y + scrollY = 레이아웃 y.
// scrollY 400에서 fx2 REV_SIZE(레이아웃 cy 1638)는 화면 y 1238에 보인다 — 그 지점을
// 탭하면 REV_SIZE 파라미터를 잡고, 드래그는 노브로 간다(스크롤 불변).
// (스펙 예시의 scrollY 300은 화면 y 1338 = 1280px 화면 밖이라 물리적으로 불가능한
// 지점이다 — 같은 증명을 화면 안 숫자로 잡았다. 보고서 참조.)
func TestScrolledKnobHit(t *testing.T) {
	h := newHarness(t)
	h.v.scrollY = 400
	k := knobAt(h.v, secFx2, "REV_SIZE")
	if k.cy != 1638 {
		t.Fatalf("REV_SIZE cy = %v(레이아웃 v3 = 1638 예상 — 레이아웃이 바뀌면 이 테스트 값도 함께)", k.cy)
	}
	sy := k.cy - h.v.scrollY
	if sy >= core.LogicalH {
		t.Fatalf("화면 y %v가 화면 높이 %d 밖", sy, core.LogicalH)
	}
	h.frame(ptrPress(-1, k.cx, sy))
	if id, ok := h.v.JustGrabbed(); !ok || id != engine.RevSize {
		t.Fatalf("JustGrabbed = (%d,%v)(RevSize %d 예상)", id, ok, engine.RevSize)
	}
	h.frame(ptrMove(-1, k.cx, sy-200))
	sp := h.setParamsFor(engine.RevSize)
	if len(sp) == 0 {
		t.Fatal("스크롤 상태 노브 드래그에 SetParam 없음")
	}
	if got := h.fb.params[engine.RevSize]; math.Abs(float64(got)-1) > 1e-6 {
		t.Fatalf("REV_SIZE 값 %v(기본 0.5 + 1.0 클램프 예상)", got)
	}
	if h.v.scrollY != 400 {
		t.Fatalf("노브 드래그 중 scrollY = %v(400 불변 예상)", h.v.scrollY)
	}
}

// TestScrollSecondPointerIgnored — 동시 두 빈 판 포인터: 첫 것만 스크롤을 잡는다.
func TestScrollSecondPointerIgnored(t *testing.T) {
	h := newHarness(t)
	h.frame(ptrPress(-1, emptyX, emptyY))
	h.frame(ptrMove(-1, emptyX, emptyY-50), ptrPress(-2, 300, 950)) // 두 번째도 빈 판
	if math.Abs(h.v.scrollY-50) > 1e-9 {
		t.Fatalf("첫 이동 후 scrollY = %v(50 예상)", h.v.scrollY)
	}
	// 두 번째가 잡혔다면 여기서 같이 움직인다 — 첫 것의 dy만 반영돼야 한다.
	h.frame(ptrMove(-1, emptyX, emptyY-150), ptrMove(-2, 300, 700))
	if math.Abs(h.v.scrollY-150) > 1e-9 {
		t.Fatalf("두 번째 포인터 반영 scrollY = %v(150 = 첫 것 dy만 예상)", h.v.scrollY)
	}
}

// TestDecoButtons — fx2 장식 버튼(bkDeco — §13.3): 라벨 4종, 탭 Cmd 0개,
// 눌림이 스크롤 제스처로 넘어가지 않는다(버튼 히트가 빈 판보다 우선).
func TestDecoButtons(t *testing.T) {
	h := newHarness(t)
	h.v.scrollY = h.v.scrollMax // 520 — 버튼 행(레이아웃 y 1724)이 화면 1204에 온다
	want := map[string]string{"rev_on": "REV", "cho_on": "CHO", "rev_pre": "PRE", "cho_st": "ST"}
	for name, lbl := range want {
		b := btnAt(h.v, secFx2, name)
		if b == nil {
			t.Fatalf("장식 버튼 %s 없음", name)
		}
		if b.kind != bkDeco {
			t.Fatalf("%s kind = %d(bkDeco %d 예상)", name, b.kind, bkDeco)
		}
		if b.label != lbl {
			t.Fatalf("%s 라벨 %q(%q 예상)", name, b.label, lbl)
		}
		cx, cy := b.rect.Center()
		h.frame(ptrPress(-1, cx, cy-h.v.scrollY))
		if n := len(h.fb.cmds); n != 0 {
			t.Fatalf("%s 탭 Cmd %d개(0 예상)", name, n)
		}
		h.frame(ptrRel(-1, cx, cy-h.v.scrollY))
	}
	// 장식 버튼에서 시작한 드래그도 스크롤이 아니다.
	b := btnAt(h.v, secFx2, "rev_on")
	cx, cy := b.rect.Center()
	h.frame(ptrPress(-1, cx, cy-h.v.scrollY))
	h.frame(ptrMove(-1, cx, cy-h.v.scrollY-100))
	if h.v.scrollY != h.v.scrollMax {
		t.Fatalf("장식 버튼 드래그에 scrollY = %v(%v 불변 예상)", h.v.scrollY, h.v.scrollMax)
	}
	h.frame(ptrRel(-1, cx, cy-h.v.scrollY-100))
}

// TestScrollDisabledShortLayout — 방어 1: 높이 ≤ 화면(구 레이아웃 720×1280)이면
// scrollMax 0 — 빈 판을 잡아도 스크롤이 아니고(pkNone), 인디케이터 기하도 퇴화한다.
func TestScrollDisabledShortLayout(t *testing.T) {
	l, err := core.LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	l.Size[1] = core.LogicalH // 구 레이아웃으로 접는다 — 컨트롤 좌표는 그대로(범위 밖 히트 없음 가정)
	v, err := newView(l)
	if err != nil {
		t.Fatalf("뷰 구축: %v", err)
	}
	if v.scrollMax != 0 {
		t.Fatalf("구 레이아웃 scrollMax = %v(0 예상)", v.scrollMax)
	}
	h := &harness{v: v, fb: &fakeBridge{params: engine.DefaultParams()}, ctx: &core.Ctx{Bridge: nil}, dt: 1.0 / 60.0}
	h.fb.tick = core.Tick{}
	h.ctx.Bridge = h.fb
	h.frame(ptrPress(-1, emptyX, emptyY))
	if v.nptrs == 0 || v.ptrs[v.nptrs-1].kind != pkNone {
		t.Fatal("구 레이아웃 빈 판 눌림이 스크롤을 잡음(pkNone 예상)")
	}
	h.frame(ptrMove(-1, emptyX, emptyY-200))
	h.frame(ptrRel(-1, emptyX, emptyY-200))
	if v.scrollY != 0 || v.scrollV != 0 {
		t.Fatalf("구 레이아웃에서 scrollY %v·v %v(0·0 예상)", v.scrollY, v.scrollV)
	}
}

// TestScrollClamp — 경계·입력 방어(방어 2): NaN·음수 → 0, 상한 초과·거대 값 → max.
// 인디케이터 기하(§13.3): 길이 = 1280²/1800, y는 scrollY에 비례, 끝에서 정확히 맞닿는다.
func TestScrollClamp(t *testing.T) {
	h := newHarness(t)
	if got := h.v.clampScroll(math.NaN()); got != 0 {
		t.Fatalf("clampScroll(NaN) = %v(0 예상)", got)
	}
	if got := h.v.clampScroll(-3); got != 0 {
		t.Fatalf("clampScroll(−3) = %v(0 예상)", got)
	}
	if got := h.v.clampScroll(1e9); got != h.v.scrollMax {
		t.Fatalf("clampScroll(1e9) = %v(%v 예상)", got, h.v.scrollMax)
	}
	if got := h.v.clampScroll(math.Inf(1)); got != h.v.scrollMax {
		t.Fatalf("clampScroll(+Inf) = %v(%v 예상)", got, h.v.scrollMax)
	}
	hh := float64(core.LogicalH) * float64(core.LogicalH) / 1800
	if y, gh := scrollIndGeom(0, 520, 1800); y != 0 || math.Abs(gh-hh) > 1e-9 {
		t.Fatalf("scrollIndGeom(0) = (%v,%v)(0,%v 예상)", y, gh, hh)
	}
	y, _ := scrollIndGeom(520, 520, 1800)
	if math.Abs(y-(float64(core.LogicalH)-hh)) > 1e-9 {
		t.Fatalf("scrollIndGeom(맨 아래) y = %v(%v 예상 — 끝 정렬)", y, float64(core.LogicalH)-hh)
	}
	y2, _ := scrollIndGeom(260, 520, 1800)
	if math.Abs(y2-y/2) > 1e-9 {
		t.Fatalf("scrollIndGeom(중간) y = %v(%v = 절반 예상 — 비례)", y2, y/2)
	}
}

// TestScrollNoAlloc — 스크롤 상태에서도 Update 무할당: (a) 스크롤된 정지 상태
// (b) 관성 감쇠 중 (c) 스크롤 포인터를 잡은 채(사본 경로 포함). 포인터 슬라이스는
// 클로저 밖에서 1회 만든다(프레임당 할당 측정 오염 방지).
func TestScrollNoAlloc(t *testing.T) {
	h := newHarness(t)
	h.run(120) // 워밍업 — 표시창 캐시 전부 1회 구성
	h.v.scrollY, h.v.scrollV = 200, 0
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(a) 스크롤 정지 상태 할당 %.0f회/프레임(0 예상)", a)
	}
	h.v.scrollV = 30 // 관성 감쇠 중 — 매 실행 상태가 변해도 할당은 0이어야 한다
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(b) 관성 감쇠 중 할당 %.0f회/프레임(0 예상)", a)
	}
	held := []core.Pointer{ptrPress(-1, emptyX, emptyY)}
	h.ctx.DT = h.dt
	h.ctx.Tick = h.fb.tick
	h.ctx.Pointers = held
	h.v.Update(h.ctx) // 잡기 프레임(JustPressed 1회) — 이후 정상 상태만 잰다
	held[0].JustPressed = false
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(c) 스크롤 잡은 채 할당 %.0f회/프레임(0 예상)", a)
	}
}

// TestChordSelectorScrollCoexist — 자기검증(스펙 3결함 클래스): 코드 선택기 열림 중
// 빈 판을 잡으면 선택기는 닫히고(띠 밖 눌림 — §12.3 "눌림의 원래 동작은 계속된다")
// 스크롤은 그 눌림의 원래 동작으로 진행된다. 6초 타임아웃(chordIdleClose)은
// 스크롤과 무관한 시계라 간섭 없음 — 여기선 닫힘 경로만 단언한다.
func TestChordSelectorScrollCoexist(t *testing.T) {
	h := newHarness(t)
	tapCell(h, 3)
	if !h.v.chord.open {
		t.Fatal("선택기 안 열림")
	}
	h.frame(ptrPress(-1, emptyX, emptyY))
	h.frame(ptrMove(-1, emptyX, emptyY-50))
	if h.v.chord.open {
		t.Fatal("빈 판 잡기(스크롤) 후 선택기 열려 있음(띠 밖 눌림 닫힘 예상)")
	}
	if math.Abs(h.v.scrollY-50) > 1e-9 {
		t.Fatalf("선택기 닫힘과 함께 스크롤 안 됨 scrollY = %v(50 예상)", h.v.scrollY)
	}
	if n := countChordCmds(h); n != 0 {
		t.Fatalf("스크롤 잡기 SetChord %d개(0 예상)", n)
	}
	h.frame(ptrRel(-1, emptyX, emptyY-50))
	h.run(120) // 관성 감쇠 — 랙이 내려가 있다(≈500)
	// 랙이 내려간 상태에서 띠의 '스크롤 전 화면 자리'를 눌러도 선택기가 열리지 않는다 —
	// 히트 판정이 레이아웃 좌표임의 반대 증명(TestScrolledKnobHit의 짝).
	if h.v.scrollY <= 0 {
		t.Fatalf("관성 후 scrollY = %v(내려가 있어야 반대 증명이 성립)", h.v.scrollY)
	}
	cx, cy := h.v.chordCells[3].Center()
	h.frame(ptrPress(-1, cx, cy))
	h.frame(ptrRel(-1, cx, cy))
	if h.v.chord.open {
		t.Fatal("스크롤된 상태에서 띠 자리 탭이 선택기를 엶(레이아웃 좌표 판정 위반)")
	}
	h.v.scrollY, h.v.scrollV = 0, 0 // 맨 위로 돌려 보낸 뒤(제스처 아닌 상태 이동 — 단정 목적)
	tapCell(h, 3)
	if !h.v.chord.open {
		t.Fatal("스크롤 이력 후 선택기 재개방 실패")
	}
	tapCell(h, 2)
	if n := countChordCmds(h); n != 1 {
		t.Fatalf("스크롤 이력 후 SetChord %d개(1 예상)", n)
	}
}
