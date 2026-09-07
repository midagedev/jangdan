// back_test.go — 뒷면 케이블 뷰 게이트(§14.3, P5-back-view). back.go 단독 소유.
//
// FAIL-first 기록(2026-09-06): 구현 전 소스에서 이 파일 추가 → `v.rear undefined`,
// `v.pressRear undefined` 등 정의 없음 컴파일 실패(P3-meters·P5-poly-dsp 관례 — 정의
// 없음 컴파일 실패가 FAIL-first다). 동작 변경 단언(TestFrameFlags 이름판 탭 = 놓을 때)
// 는 device_test.go 쪽 적색(h.v.rear undefined)이 같은 커밋 시점의 증거다.
//
// P5-gain 계약↔테스트 대응(2026-09-07 라운드가 추가한 단언 — back.go 파일 주석의 계약 순서):
//
//	같은 잭 탭 = 랙 무동작(결속·게인·수 불변) → 7b TestRearTapKeepsBinding
//	팝업 열림 조건(입력 탭·출력 탭·빈 입력·slop·길게 누르기) → 7c TestRearTapOpensGainPop
//	비결속 드래그 = 같은 끝점 Connect만, 수·결속 불변 → 7d TestRearGainPopUnboundDrag
//	결속 드래그 = SetParam만, 결속 불변·게인은 파라미터 추종 → 7e TestRearGainPopBoundDrag
//	닫힘 3종(판 밖 탭·앞면 복귀·케이블 소실) → 7f TestRearGainPopClose
//	화면 내 클램프(맨 아래 잭 위로·맨 위 잭 아래로) → 7g TestRearGainPopOnScreen
//	무할당(팝업 정지·드래그 유지 프레임) → 13 TestRearUpdateNoAlloc (c)(d)
//
// 좌표 계약: 잭 좌표는 rear.json(v.rearL)에서 읽는다 — 테스트도 하드코딩하지 않고
// 레이아웃에서 유도한다(픽셀 상수의 단일 소유자). 화면 좌표 = 레이아웃 y − scrollY.
package device

import (
	"bytes"
	"image"
	"sort"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/jangdan/app/assets"
	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// rearJackAt — 레이아웃에서 (슬롯, 포트, 방향)의 잭. 테스트 좌표의 단일 원본.
func rearJackAt(t *testing.T, v *View, slot, port int, in bool) core.Jack {
	t.Helper()
	d := v.rearL.RearDeviceAt(slot)
	if d == nil {
		t.Fatalf("rear.json에 슬롯 %d 장치 없음", slot)
	}
	js := d.In
	if !in {
		js = d.Out
	}
	for i := range js {
		if js[i].Port == port {
			return js[i]
		}
	}
	t.Fatalf("슬롯 %d %s 포트 %d 잭 없음", slot, map[bool]string{true: "입력", false: "출력"}[in], port)
	return core.Jack{}
}

// enterRear — 이름판 길게 누르기(0.6s > padHoldMute 0.5s)로 뒷면 진입.
func enterRear(t *testing.T, h *harness) {
	t.Helper()
	cx, cy := h.v.titlePlate.Center()
	h.frame(ptrPress(-1, cx, cy))
	h.hold(-1, cx, cy, 36)
	h.frame(ptrRel(-1, cx, cy))
	if !h.v.rear {
		t.Fatal("이름판 길게 누르기로 뒷면 진입 안 됨")
	}
}

// tap — 짧은 누름·놓기(4프레임 ≈ 0.067s < tapDurMax 0.25s, 이동 0).
func tap(h *harness, x, y float64) {
	h.frame(ptrPress(-1, x, y))
	h.hold(-1, x, y, 3)
	h.frame(ptrRel(-1, x, y))
}

// rackCmds — 특정 CmdKind 송신 기록만.
func rackCmds(h *harness, k engine.CmdKind) []engine.Cmd {
	var out []engine.Cmd
	for _, r := range h.fb.cmds {
		if r.c.Kind == k {
			out = append(out, r.c)
		}
	}
	return out
}

// 1. 이름판 길게 누르기(0.6초) → 뒷면. 탭(방)은 아니어야 한다.
func TestBackTitleLongPress(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	if h.v.BackTapped() {
		t.Fatal("길게 누르기에 BackTapped true(탭 아님)")
	}
	if h.v.jackDrag.on {
		t.Fatal("진입 시 잭 드래그 잔존")
	}
}

// 2. 이름판 짧은 탭(0.1초) → 방. 뒷면이 아니어야 한다.
func TestBackTitleTap(t *testing.T) {
	h := newHarness(t)
	cx, cy := h.v.titlePlate.Center()
	tap(h, cx, cy)
	if !h.v.BackTapped() {
		t.Fatal("이름판 탭에 BackTapped false")
	}
	if h.v.rear {
		t.Fatal("짧은 탭에 뒷면 전환")
	}
}

// 3. 뒷면에서 장치 이름판 탭 → 앞면 복귀.
func TestRearPlateTapReturn(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	d := h.v.rearL.RearDeviceAt(engine.SlotBassA)
	if d == nil {
		t.Fatal("rear.json에 bassA 없음")
	}
	cx, cy := d.Plate.Center()
	tap(h, cx, cy)
	if h.v.rear {
		t.Fatal("장치 이름판 탭에도 뒷면 유지")
	}
	if h.v.BackTapped() {
		t.Fatal("이름판 탭이 방 전환으로 오독")
	}
}

// 4. 케이블 표 읽기 계약(§14.3): 앞면에서는 읽지 않고, 뒷면에서는 위상 리비전이
// 변할 때만 읽는다 — 진입 1회 뒤 10프레임 동안 Cables 호출은 그 1회뿐.
func TestRearCableReadContract(t *testing.T) {
	h := newHarness(t)
	h.run(10)
	if n := h.fb.cablesCalls; n != 0 {
		t.Fatalf("(a) 앞면 10프레임에 Cables %d회(0 예상)", n)
	}
	enterRear(t, h)
	// 진입 프레임(뒤집힌 직후 Update 꼬리)에 1회.
	if n := h.fb.cablesCalls; n != 1 {
		t.Fatalf("(b) 뒷면 진입 직후 Cables %d회(1 예상)", n)
	}
	h.run(10)
	if n := h.fb.cablesCalls; n != 1 {
		t.Fatalf("(c) 리비전 불변 10프레임에 Cables %d회(1 예상)", n)
	}
}

// 5. 폴리 OUT → fx IN 0(DUCK) 드래그 = Connect 1건(비결속 D=NumParams, V=1). 미러 엔진
// 표에도 반영(케이블 수 +1). 스펙 예시 쌍 "폴리 OUT → 리버브 IN"은 기본 랙에 이미 있는
// 케이블이다(buildDefault 폴리 센드 qRev — connect가 그 자리를 갱신만 하고 수를 늘리지
// 않는다). 수 증가를 재려면 기본 랙에 없는 쌍이어야 한다(스펙 전제 수정, P5-back-view
// 실측 — 리버브·코러스·fx DLY(포트 3)는 전부 폴리 센드 대상이라 안 됨).
func TestRearConnectDrag(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	before := h.fb.rack().NumCables()
	h.v.scrollY = h.v.scrollMax // 맨 아래(폴리 잭 y 1186 · fx 잭 y 328 — 둘 다 화면 안)
	oj := rearJackAt(t, h.v, engine.SlotPoly, 0, false)
	ij := rearJackAt(t, h.v, engine.SlotFx, 0, true)
	h.frame(ptrPress(-1, oj.CX, oj.CY-h.v.scrollY))
	if !h.v.jackDrag.on || h.v.jackDrag.fromIn || h.v.jackDrag.srcSlot != engine.SlotPoly {
		t.Fatalf("출력 잭 잡기 실패: %+v", h.v.jackDrag)
	}
	h.frame(ptrMove(-1, 360, 1000))
	h.frame(ptrRel(-1, ij.CX, ij.CY-h.v.scrollY))
	cs := rackCmds(h, engine.Connect)
	if len(cs) != 1 {
		t.Fatalf("Connect %d건(1 예상)", len(cs))
	}
	c := cs[0]
	if c.A != engine.SlotPoly || c.B != engine.SlotFx || c.C != 0 ||
		c.D != uint8(engine.Unbound) || c.V != 1 {
		t.Fatalf("Connect 명령 %+v(A=폴리 B=fx C=0 D=NumParams V=1 예상)", c)
	}
	if ds := rackCmds(h, engine.Disconnect); len(ds) != 0 {
		t.Fatalf("이동 없는 드래그에 Disconnect %d건(0 예상)", len(ds))
	}
	if after := h.fb.rack().NumCables(); after != before+1 {
		t.Fatalf("미러 케이블 수 %d(%d 예상)", after, before+1)
	}
	if h.v.jackDrag.on {
		t.Fatal("놓은 뒤 잭 드래그 잔존")
	}
	if h.v.rejT >= 0 {
		t.Fatal("성공 연결에 거부 피드백 점화")
	}
}

// 6. 메인 IN L 케이블 잡아 빈 판에 놓기 = 뽑기(Disconnect 1건, 케이블 수 −1).
// 기본 랙에서 메인 IN L에 오는 케이블은 Fx·Reverb·Chorus 3개 — 표의 마지막(가장 최근
// 삽입)은 Chorus L(§14.1 합산 순서 = 삽입 순)이므로 그것이 뽑힌다.
func TestRearUnplugDrag(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	before := h.fb.rack().NumCables()
	h.v.scrollY = 200
	ij := rearJackAt(t, h.v, engine.SlotMain, 0, true)
	h.frame(ptrPress(-1, ij.CX, ij.CY-h.v.scrollY))
	if !h.v.jackDrag.on || !h.v.jackDrag.fromIn {
		t.Fatalf("입력 잭 케이블 잡기 실패: %+v", h.v.jackDrag)
	}
	if h.v.jackDrag.srcSlot != engine.SlotChorus {
		t.Fatalf("잡은 케이블 src %d(코러스 %d 예상 — 표의 마지막 삽입)", h.v.jackDrag.srcSlot, engine.SlotChorus)
	}
	// 빈 판(드럼 행 중앙 — 잭도 이름판도 아님)에 놓기.
	h.frame(ptrMove(-1, 360, 500))
	h.frame(ptrRel(-1, 360, 500))
	ds := rackCmds(h, engine.Disconnect)
	if len(ds) != 1 {
		t.Fatalf("Disconnect %d건(1 예상)", len(ds))
	}
	if d := ds[0]; d.A != engine.SlotChorus || d.B != engine.SlotMain || d.C != 0 {
		t.Fatalf("Disconnect 명령 %+v(코러스→메인 포트0 예상)", d)
	}
	if cs := rackCmds(h, engine.Connect); len(cs) != 0 {
		t.Fatalf("빈 판 놓기에 Connect %d건(0 예상)", len(cs))
	}
	if after := h.fb.rack().NumCables(); after != before-1 {
		t.Fatalf("미러 케이블 수 %d(%d 예상)", after, before-1)
	}
}

// 7. 메인 IN L 케이블 → 메인 IN R 자리 옮기기 = Disconnect(옛 자리) 뒤 Connect(새 자리)
// 순서로 1건씩. 케이블 수는 불변.
func TestRearMoveDrag(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	before := h.fb.rack().NumCables()
	h.v.scrollY = 200
	src := rearJackAt(t, h.v, engine.SlotMain, 0, true)
	dst := rearJackAt(t, h.v, engine.SlotMain, 1, true)
	h.frame(ptrPress(-1, src.CX, src.CY-h.v.scrollY))
	h.frame(ptrMove(-1, src.CX, src.CY-h.v.scrollY-40))
	h.frame(ptrRel(-1, dst.CX, dst.CY-h.v.scrollY))
	cs, ds := rackCmds(h, engine.Connect), rackCmds(h, engine.Disconnect)
	if len(cs) != 1 || len(ds) != 1 {
		t.Fatalf("자리 옮기기 Connect %d·Disconnect %d건(1·1 예상)", len(cs), len(ds))
	}
	if d := ds[0]; d.A != engine.SlotChorus || d.B != engine.SlotMain || d.C != 0 {
		t.Fatalf("Disconnect %+v(코러스→메인 L 예상)", d)
	}
	if c := cs[0]; c.A != engine.SlotChorus || c.B != engine.SlotMain || c.C != 0|1<<4 {
		t.Fatalf("Connect %+v(코러스→메인 R C=0|1<<4 예상)", c)
	}
	if after := h.fb.rack().NumCables(); after != before {
		t.Fatalf("미러 케이블 수 %d(불변 %d 예상)", after, before)
	}
}

// 7b. 같은 잭 탭 = 랙 무동작(P5-gain 게이트 1): 결속 케이블이 꽂힌 입력 잭을 잡아 그 자리에
// 놓으면(짧은 탭) 그 케이블의 Bind·Gain·케이블 수가 정확히 그대로여야 한다. 수정 전 결함:
// 같은 자리 놓기도 Disconnect+Connect(Unbound, 1.0) "재연결"로 결속을 끊고 게인을 튀게
// 했다(리드 실측 "BassB→Reverb 탭 전 gain=0.400 bind=36 → 탭 후 gain=1.000 bind=59").
// 이 테스트의 대상 케이블은 메인 IN L의 마지막 = 코러스 L 리턴(결속 ChoMix=50, 게인
// 0.8×기본값) — 리드 예시 쌍 BassB→Reverb는 이제 그 포트의 마지막 케이블이 아니므로(샘플러
// 센드·드럼 센드 8개가 뒤에 붙는다) 탭으로 잡히는 결속 케이블의 대표로 이것을 잰다.
func TestRearTapKeepsBinding(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	j := rearJackAt(t, h.v, engine.SlotMain, 0, true)
	h.v.scrollY = h.v.clampScroll(j.CY - 640)
	before := h.fb.rack().NumCables()
	bGain, bBind := cableAt(h, engine.SlotChorus, 0, engine.SlotMain, 0)
	if bBind != uint8(engine.ChoMix) {
		t.Fatalf("전제: 메인 IN L 마지막 케이블 결속 %d(ChoMix %d 예상)", bBind, engine.ChoMix)
	}
	tap(h, j.CX, j.CY-h.v.scrollY)
	for _, r := range h.fb.cmds {
		switch r.c.Kind {
		case engine.Connect, engine.Disconnect, engine.SetParam:
			t.Fatalf("같은 잭 탭에 송신 %+v(랙 무동작 예상)", r.c)
		}
	}
	if after := h.fb.rack().NumCables(); after != before {
		t.Fatalf("케이블 수 %d→%d(불변 예상)", before, after)
	}
	aGain, aBind := cableAt(h, engine.SlotChorus, 0, engine.SlotMain, 0)
	if aBind != bBind || aGain != bGain {
		t.Fatalf("탭 뒤 Bind %d→%d · Gain %v→%v(불변 예상)", bBind, aBind, bGain, aGain)
	}
}

// cableAt — 뷰가 다시 읽은 표에서 (src,sp,dst,dp) 케이블의 (게인, 결속). 없으면 Bind에
// 0xFF를 돌려준다(테스트 전제 확인용).
func cableAt(h *harness, src, sp, dst, dp int) (float32, uint8) {
	for i := 0; i < h.v.nCables; i++ {
		c := &h.v.cables[i]
		if int(c.Src) == src && int(c.SP) == sp && int(c.Dst) == dst && int(c.DP) == dp {
			return c.Gain, c.Bind
		}
	}
	return 0, 0xFF
}

// popTapJack — (slot, port) 입력 잭을 화면 세로 중앙에 오도록 스크롤해 탭한다. 팝업
// 개방 여부는 호출자가 판단한다(열림·비열림 양쪽 제스처에 같은 동작으로 쓴다).
func popTapJack(t *testing.T, h *harness, slot, port int) {
	t.Helper()
	j := rearJackAt(t, h.v, slot, port, true)
	h.v.scrollY = h.v.clampScroll(j.CY - 640)
	tap(h, j.CX, j.CY-h.v.scrollY)
}

// popScreenRect — 팝업 rect의 화면 좌표판(레이아웃 y − scrollY). 클램프 단언의 축.
func popScreenRect(h *harness) core.Rect {
	r := h.v.gainPopRect(h.fb)
	return core.Rect{r[0], r[1] - h.v.scrollY, r[2], r[3]}
}

// assertNoRackCmds — 지금까지 송신에 랙·파라미터 명령이 없어야 한다(무동작 단언 공용).
func assertNoRackCmds(t *testing.T, h *harness) {
	t.Helper()
	for _, r := range h.fb.cmds {
		switch r.c.Kind {
		case engine.Connect, engine.Disconnect, engine.SetParam:
			t.Fatalf("무동작 예상 자리에 송신 %+v", r.c)
		}
	}
}

// 7c. 팝업 열림 조건(P5-gain 게이트 3): 입력 잭 탭 = 개방(대상 = 그 포트의 마지막 케이블,
// 출처 라벨은 rear.json 이름·라벨의 조립), 출력 잭 탭 = 무동작, 빈 입력 잭 탭 = 무동작,
// tapSlop 초과 이동 = 드래그(무동작), tapDur 초과 길게 누르기 = 무동작.
func TestRearTapOpensGainPop(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	// (a) 입력 잭 탭 — 메인 IN L 마지막 케이블 = 코러스 L(테스트 7b와 같은 대상).
	popTapJack(t, h, engine.SlotMain, 0)
	po := &h.v.jackDrag.pop
	if !po.on {
		t.Fatal("(a) 입력 잭 탭에 팝업 미개방")
	}
	if po.src != engine.SlotChorus || po.sp != 0 || po.dst != engine.SlotMain || po.dp != 0 {
		t.Fatalf("(a) 팝업 대상 (%d,%d)->(%d,%d)(코러스 L->메인 L 예상)", po.src, po.sp, po.dst, po.dp)
	}
	if po.label != "CHORUS L -> MAIN L" {
		t.Fatalf("(a) 출처 라벨 %q(\"CHORUS L -> MAIN L\" 예상 — rear.json 이름·라벨 조립)", po.label)
	}
	// 판 밖 탭으로 정리(닫힘 자체는 7f가 잰다) — 이하 단계의 전제.
	tap(h, 360, 100)
	if po.on {
		t.Fatal("판 밖 탭에 팝업 잔존(이하 단계 전제 깨짐)")
	}
	// (b) 출력 잭 탭 — 케이블 늘리기로 잡혔다 같은 잭 놓기. 팝업도 송신도 없어야 한다.
	h.v.scrollY = 0
	h.fb.cmds = nil
	o := rearJackAt(t, h.v, engine.SlotBassA, 0, false)
	tap(h, o.CX, o.CY)
	if h.v.jackDrag.pop.on {
		t.Fatal("(b) 출력 잭 탭에 팝업 개방")
	}
	assertNoRackCmds(t, h)
	// (c) 빈 입력 잭 탭 — 미리 fx SC의 유일한 케이블(드럼 SC)을 뽑아 둔다.
	h.fb.Cmd(engine.Cmd{Kind: engine.Disconnect, A: engine.SlotDrums, B: engine.SlotFx,
		C: 1 | 2<<4}, core.Human)
	h.frame()
	h.fb.cmds = nil
	popTapJack(t, h, engine.SlotFx, 2)
	if h.v.jackDrag.pop.on {
		t.Fatal("(c) 빈 입력 잭 탭에 팝업 개방")
	}
	assertNoRackCmds(t, h)
	// (d) tapSlop 초과 이동(10px > 6px, 잭 히트 반지름 16 안이라 같은 잭) — 탭이 아니다.
	h.fb.cmds = nil
	m := rearJackAt(t, h.v, engine.SlotMain, 0, true)
	h.v.scrollY = h.v.clampScroll(m.CY - 640)
	sy := m.CY - h.v.scrollY
	h.frame(ptrPress(-1, m.CX, sy))
	h.frame(ptrMove(-1, m.CX+10, sy))
	h.frame(ptrRel(-1, m.CX+10, sy))
	if h.v.jackDrag.pop.on {
		t.Fatal("(d) tapSlop 초과 이동에 팝업 개방")
	}
	assertNoRackCmds(t, h)
	// (e) tapDur 초과 길게 누르기(25프레임 ≈ 0.417s > 0.35s) — 제자리여도 탭이 아니다.
	h.fb.cmds = nil
	h.frame(ptrPress(-1, m.CX, sy))
	h.hold(-1, m.CX, sy, 24)
	h.frame(ptrRel(-1, m.CX, sy))
	if h.v.jackDrag.pop.on {
		t.Fatal("(e) 길게 눌림에 팝업 개방")
	}
	assertNoRackCmds(t, h)
}

// 7d. 비결속 팝업 게인 드래그(P5-gain 게이트 4): 위로 끌면 게인이 오르되 송신은 전부 같은
// 끝점의 Connect(D=Unbound — connect가 그 자리를 갱신)뿐이고 케이블 수·결속 상태는 불변.
// 기본 랙의 비결속 마지막 케이블은 게인이 전부 1.0이라(스펙 전제 수정 — 실측) 증감을 재려면
// 미리 값을 깔아야 한다: 샘플러→fx DIR(직렬 입력)을 0.3으로.
func TestRearGainPopUnboundDrag(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	h.fb.Cmd(engine.Cmd{Kind: engine.Connect, A: engine.SlotSampler, B: engine.SlotFx,
		C: 0 | 1<<4, D: uint8(engine.Unbound), V: 0.3}, core.Human)
	h.frame()
	h.fb.cmds = nil
	before := h.fb.rack().NumCables()
	popTapJack(t, h, engine.SlotFx, 1)
	if !h.v.jackDrag.pop.on {
		t.Fatal("전제: fx DIR 탭에 팝업 미개방")
	}
	g0, b0 := cableAt(h, engine.SlotSampler, 0, engine.SlotFx, 1)
	if b0 != uint8(engine.Unbound) || g0 < 0.29 || g0 > 0.31 {
		t.Fatalf("전제: DIR 케이블 게인 %v 결속 %d(≈0.3·비결속 예상)", g0, b0)
	}
	// 팝업판 안에서 위로 40px(4보 이동 + 놓기) — 감도 popDragRange 136px = Δ1.0.
	r := popScreenRect(h)
	cx, cy := r[0]+r[2]/2, r[1]+r[3]/2
	h.frame(ptrPress(-1, cx, cy))
	for i := 1; i <= 4; i++ {
		h.frame(ptrMove(-1, cx, cy-10*float64(i)))
	}
	h.frame(ptrRel(-1, cx, cy-40))
	cs := rackCmds(h, engine.Connect)
	if len(cs) == 0 {
		t.Fatal("드래그에 Connect 송신 없음")
	}
	for _, c := range cs {
		if c.A != engine.SlotSampler || c.B != engine.SlotFx || c.C != 0|1<<4 ||
			c.D != uint8(engine.Unbound) {
			t.Fatalf("Connect %+v(샘플러→fx DIR·D=Unbound 예상 — 같은 끝점 갱신)", c)
		}
	}
	if last := cs[len(cs)-1].V; last < 0.55 || last > 0.65 { // 0.3 + 40/136 ≈ 0.594
		t.Fatalf("마지막 송신 게인 %v(≈0.594 예상)", last)
	}
	for _, k := range [...]engine.CmdKind{engine.SetParam, engine.Disconnect} {
		if n := len(rackCmds(h, k)); n != 0 {
			t.Fatalf("비결속 드래그에 %d번 종류 송신 %d건(0 예상)", k, n)
		}
	}
	h.frame() // 미러 재독(놓은 프레임 뒤 확정)
	g1, b1 := cableAt(h, engine.SlotSampler, 0, engine.SlotFx, 1)
	if g1 < 0.55 || g1 > 0.65 {
		t.Fatalf("드래그 뒤 게인 %v(≈0.594 예상)", g1)
	}
	if b1 != uint8(engine.Unbound) {
		t.Fatalf("비결속 케이블 결속이 %d로 변함(Unbound 예상)", b1)
	}
	if after := h.fb.rack().NumCables(); after != before {
		t.Fatalf("케이블 수 %d→%d(불변 예상)", before, after)
	}
	if !h.v.jackDrag.pop.on {
		t.Fatal("드래그 놓음에 팝업 닫힘(판은 유지 예상)")
	}
}

// 7e. 결속 팝업 게인 드래그(P5-gain 게이트 5): 결속 케이블(메인 IN L = 코러스 L 리턴,
// 결속 ChoMix)의 드래그는 SetParam만 보낸다(Connect의 D가 결속을 다시 써 결속을 끊는다).
// 케이블 결속은 불변이고 게인은 파라미터에서 유도된다(ChoMix → ×0.8).
func TestRearGainPopBoundDrag(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	popTapJack(t, h, engine.SlotMain, 0)
	if !h.v.jackDrag.pop.on {
		t.Fatal("전제: 메인 IN L 탭에 팝업 미개방")
	}
	h.fb.cmds = nil
	r := popScreenRect(h)
	cx, cy := r[0]+r[2]/2, r[1]+r[3]/2
	h.frame(ptrPress(-1, cx, cy))
	for i := 1; i <= 4; i++ {
		h.frame(ptrMove(-1, cx, cy-10*float64(i)))
	}
	h.frame(ptrRel(-1, cx, cy-40))
	sp := rackCmds(h, engine.SetParam)
	if len(sp) == 0 {
		t.Fatal("결속 드래그에 SetParam 송신 없음")
	}
	for _, c := range sp {
		if c.A != uint8(engine.ChoMix) {
			t.Fatalf("SetParam %+v(A=ChoMix %d 예상)", c, engine.ChoMix)
		}
	}
	if last := sp[len(sp)-1].V; last < 0.75 || last > 0.85 { // 0.5 + 40/136 ≈ 0.794
		t.Fatalf("마지막 송신 값 %v(≈0.794 예상)", last)
	}
	for _, k := range [...]engine.CmdKind{engine.Connect, engine.Disconnect} {
		if n := len(rackCmds(h, k)); n != 0 {
			t.Fatalf("결속 드래그에 %d번 종류 송신 %d건(0 예상)", k, n)
		}
	}
	if !h.v.jackDrag.pop.on {
		t.Fatal("드래그 놓음에 팝업 닫힘(판은 유지 예상)")
	}
	// 게인의 정본은 파라미터: 마지막 SetParam을 미러 엔진에 적용하면(applyParam → setBound)
	// 재독 표에서 케이블 게인이 0.8×파라미터로 따라온다. fakeBridge의 Cmd는 params만
	// 미러하므로 랙 반영은 테스트가 직접 적용한다(장치 테스트 관례).
	h.fb.rack().Apply(sp[len(sp)-1])
	h.frame()
	g, b := cableAt(h, engine.SlotChorus, 0, engine.SlotMain, 0)
	if b != uint8(engine.ChoMix) {
		t.Fatalf("결속 %d→%d(불변 예상)", uint8(engine.ChoMix), b)
	}
	if g < 0.6 || g > 0.68 { // 0.8 × 0.794 ≈ 0.635
		t.Fatalf("결속 게인 %v(≈0.635 = 0.8×파라미터 예상)", g)
	}
}

// 7f. 팝업 닫힘(P5-gain 게이트 6): 판 밖 탭 · 앞면 복귀(장치 이름판 탭) · 대상 케이블이
// 다른 경로로 뽑힘(재독 표에서 사라짐). 앞면 복귀는 device.go의 잭 상태 리셋
// (jackDrag = jackDrag{})이 팝업을 싣고 있어 구조적으로 닫는다.
func TestRearGainPopClose(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	// (a) 판 밖(빈 판) 탭 — 팝업만 닫히고 뒷면은 유지.
	popTapJack(t, h, engine.SlotMain, 0)
	if !h.v.jackDrag.pop.on {
		t.Fatal("(a) 전제: 팝업 미개방")
	}
	h.fb.cmds = nil
	tap(h, 360, 100)
	if h.v.jackDrag.pop.on {
		t.Fatal("(a) 판 밖 탭에 팝업 잔존")
	}
	if !h.v.rear {
		t.Fatal("(a) 판 밖 탭이 앞면 전환으로 새어 나감(닫기만 해야 한다)")
	}
	assertNoRackCmds(t, h)
	// (b) 다시 열고 장치 이름판 탭 — 앞면 복귀와 함께 닫힌다.
	popTapJack(t, h, engine.SlotMain, 0)
	if !h.v.jackDrag.pop.on {
		t.Fatal("(b) 전제: 재개방 실패")
	}
	tapRearPlate(t, h, engine.SlotFx)
	if h.v.jackDrag.pop.on {
		t.Fatal("(b) 앞면 복귀에 팝업 잔존")
	}
	// (c) 다시 뒷면에서 열고 대상 케이블을 다른 경로로 뽑기 — 재독에서 사라지면 닫힌다.
	// 이름판(앞면 맨 위)이 화면 안에 들어오게 스크롤을 돌려놓고 진입한다(테스트 12 관례).
	h.v.scrollY, h.v.scrollV = 0, 0
	enterRear(t, h)
	popTapJack(t, h, engine.SlotMain, 0)
	if !h.v.jackDrag.pop.on {
		t.Fatal("(c) 전제: 재개방 실패")
	}
	h.fb.Cmd(engine.Cmd{Kind: engine.Disconnect, A: engine.SlotChorus, B: engine.SlotMain,
		C: 0}, core.Human)
	h.frame()
	if h.v.jackDrag.pop.on {
		t.Fatal("(c) 대상 케이블 소실에 팝업 잔존")
	}
	if !h.v.rear {
		t.Fatal("(c) 케이블 소실 닫기가 앞면 전환으로 새어 나감")
	}
}

// 7g. 팝업판 화면 내 클램프(P5-gain 게이트 7): 맨 아래 입력 잭(코러스 IN — 슬롯 8 샘플러
// 행에는 입력 잭이 없다, 스펙 전제 수정)은 위로 열리고 좌측 클램프, 맨 위 입력 잭(fx DUCK)
// 은 아래로 연다 — 어느 쪽이든 화면 720×1280에 전부 들어온다.
func TestRearGainPopOnScreen(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	// (a) 맨 아래: 코러스 IN(y 1730)을 scrollY 500 창에 — 아래 공간이 68판에 못 미쳐 위로.
	h.v.scrollY = 500
	bj := rearJackAt(t, h.v, engine.SlotChorus, 0, true)
	tap(h, bj.CX, bj.CY-h.v.scrollY)
	if !h.v.jackDrag.pop.on {
		t.Fatal("(a) 전제: 코러스 IN 탭 팝업 미개방")
	}
	r := h.v.gainPopRect(h.fb)
	if r[1]+r[3] > bj.CY {
		t.Fatalf("(a) 맨 아래 잭 팝업이 아래로 열림(y %v..%v, 잭 %v — 위로 열어야)", r[1], r[1]+r[3], bj.CY)
	}
	if r[0] != popMargin {
		t.Fatalf("(a) 좌측 클램프 x %v(popMargin %v 예상 — 잭 x 76 < 판 반폭 92)", r[0], popMargin)
	}
	sr := popScreenRect(h)
	if sr[0] < 0 || sr[1] < 0 || sr[0]+sr[2] > core.LogicalW || sr[1]+sr[3] > core.LogicalH {
		t.Fatalf("(a) 화면 밖 팝업 %v(720×1280 안 예상)", sr)
	}
	// (b) 맨 위: fx DUCK(y 1048)을 scrollMax(940) 창에 — 아래 여유가 넉넉해 아래로.
	h2 := newHarness(t)
	enterRear(t, h2)
	h2.v.scrollY = h2.v.scrollMax
	tj := rearJackAt(t, h2.v, engine.SlotFx, 0, true)
	tap(h2, tj.CX, tj.CY-h2.v.scrollY)
	if !h2.v.jackDrag.pop.on {
		t.Fatal("(b) 전제: fx DUCK 탭 팝업 미개방")
	}
	r2 := h2.v.gainPopRect(h2.fb)
	if r2[1] < tj.CY {
		t.Fatalf("(b) 맨 위 잭 팝업이 위로 열림(y %v, 잭 %v — 아래 여유가 충분하다)", r2[1], tj.CY)
	}
	sr2 := popScreenRect(h2)
	if sr2[0] < 0 || sr2[1] < 0 || sr2[0]+sr2[2] > core.LogicalW || sr2[1]+sr2[3] > core.LogicalH {
		t.Fatalf("(b) 화면 밖 팝업 %v(720×1280 안 예상)", sr2)
	}
}

// 8. 순환 거부 피드백. 스펙 전제 수정(P5-back-view 실측): 기본 랙에는 Fx→리버브가
// 없어 "리버브 OUT → Fx IN"은 순환이 아니다(buildDefault 전수 확인). 미러 엔진에 먼저
// Fx→리버브를 보내 전제를 만든 뒤 드래그하면 진짜 순환이다 — Connect는 보내되 표에
// 반영되지 않고(거부), 뷰는 이 프레임에 rejT를 점화한다(뷰는 판정을 흉내 내지 않는다).
func TestRearCycleReject(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	h.fb.Cmd(engine.Cmd{Kind: engine.Connect, A: engine.SlotFx, B: engine.SlotReverb,
		C: 0, D: uint8(engine.Unbound), V: 1}, core.Human)
	h.frame()       // 리비전 반영(표 재독)
	h.fb.cmds = nil // 전제 세팅 송신 기록 제외 — 이하는 드래그의 송신만 잰다
	before := h.fb.rack().NumCables()
	h.v.scrollY = 400
	oj := rearJackAt(t, h.v, engine.SlotReverb, 0, false)
	ij := rearJackAt(t, h.v, engine.SlotFx, 0, true)
	h.frame(ptrPress(-1, oj.CX, oj.CY-h.v.scrollY))
	h.frame(ptrMove(-1, 360, 900))
	h.frame(ptrRel(-1, ij.CX, ij.CY-h.v.scrollY))
	if cs := rackCmds(h, engine.Connect); len(cs) != 1 {
		t.Fatalf("순환 Connect 시도 %d건(1 예상 — 뷰는 보낸다)", len(cs))
	}
	if after := h.fb.rack().NumCables(); after != before {
		t.Fatalf("거부됐는데 케이블 수 %d→%d", before, after)
	}
	if h.v.rejT != h.ctx.Now {
		t.Fatalf("거부 피드백 미점화(rejT %v, 이 프레임 %v 예상)", h.v.rejT, h.ctx.Now)
	}
	if h.v.rejSlot != engine.SlotFx || h.v.rejPort != 0 || !h.v.rejIn {
		t.Fatalf("거부 대상 잭 (%d,%d,in=%v)(Fx 입력 0 예상)", h.v.rejSlot, h.v.rejPort, h.v.rejIn)
	}
	if h.v.pendConn.on {
		t.Fatal("판정 뒤 pendConn 잔존")
	}
}

// 9. 잭 히트 반지름(§14.3 ≥28px): 중심에서 14px는 잡히고 20px는 안 잡힌다
// (r12 + hitJackPad 4 = 16 → 지름 32).
func TestRearJackHitRadius(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	j := rearJackAt(t, h.v, engine.SlotBassA, 0, false) // (643,264) 근처 — scrollY 0으로 보임
	h.v.scrollY = 0
	h.frame(ptrPress(-1, j.CX+14, j.CY))
	if !h.v.jackDrag.on || h.v.jackDrag.srcSlot != engine.SlotBassA {
		t.Fatalf("14px 오프셋 잭 미히트: %+v", h.v.jackDrag)
	}
	h.frame(ptrRel(-1, j.CX+14, j.CY))
	h.frame(ptrPress(-1, j.CX+20, j.CY))
	if h.v.jackDrag.on {
		t.Fatal("20px 오프셋이 잭으로 히트(16 반지름 예상)")
	}
	if cs := rackCmds(h, engine.Connect); len(cs) != 0 {
		t.Fatalf("잭 아닌 눌림에 Connect %d건", len(cs))
	}
}

// 10. 스크롤 공유(자기검증 ①): scrollY 200에서 잭 히트는 화면 좌표 200px 위에서
// 맞는다(레이아웃 y + scrollY 변환의 단일 소유자 = press). 드래그 중 좌표는 화면계.
func TestRearScrollJackHit(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	h.v.scrollY = 200
	j := rearJackAt(t, h.v, engine.SlotBassA, 0, false)
	h.frame(ptrPress(-1, j.CX, j.CY-200))
	if !h.v.jackDrag.on || h.v.jackDrag.srcSlot != engine.SlotBassA {
		t.Fatalf("스크롤 보정 잭 히트 실패: %+v", h.v.jackDrag)
	}
	h.frame(ptrMove(-1, j.CX, j.CY-100))
	if h.v.jackDrag.x != j.CX || h.v.jackDrag.y != j.CY-100 {
		t.Fatalf("드래그 좌표가 화면계가 아님: (%v,%v)", h.v.jackDrag.x, h.v.jackDrag.y)
	}
	h.frame(ptrRel(-1, j.CX, j.CY-100)) // 잭 아닌 곳 + fromIn 아님 → 무동작
	if cs := rackCmds(h, engine.Connect); len(cs) != 0 {
		t.Fatalf("출력 잭→빈 곳 놓기에 Connect %d건(0 예상)", len(cs))
	}
}

// 11. 케이블 표 ↔ 그려진 베지어 수 일치(§14.3 게이트). 기본 랙 케이블 35개의 근거:
// buildDefault — dry 6 + 폴리 센드 3 + 샘플러 센드 2 + 딜레이 센드 8 + 리버브 센드 8 +
// 코러스 2 + 리턴 6. 35개 전부 양 끝 잭이 rear.json에 있으므로(기본 랙 9장치 = 뒷면 9행
// 전부) 전부 그려진다. 헤드리스 Draw는 픽셀 읽기 없이 카운터로 잰다(rearDraws).
func TestRearDrawMatchesTable(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	h.frame() // 표 동기화
	if n := h.v.nCables; n != 35 {
		t.Fatalf("기본 랙 케이블 %d개(35 예상 — dry 6+폴리 3+샘플러 2+딜레이 8+리버브 8+코러스 2+리턴 6)", n)
	}
	screen := ebiten.NewImage(720, 1280)
	h.v.Draw(screen, h.ctx)
	if h.v.rearDraws != h.v.nCables {
		t.Fatalf("그려진 케이블 %d개(표 %d개 예상)", h.v.rearDraws, h.v.nCables)
	}
}

// 11b. 샘플러 뒷면 행(P5-sampler): 슬롯 8 출력 잭 히트가 잭 드래그를 잡고, 케이블 색은
// 슬롯 8 표값(wire.py 띠색 (170,90,120) — rear.png 밴드 실측 중앙값 (165,88,117)과 차 5·2·3).
// 기본 랙이 슬롯 8 발신 케이블을 실제로 가지고 있어 이 색이 그려진다(색 = 기능 계약).
func TestRearSamplerJack(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	h.v.scrollY = h.v.scrollMax // 슬롯 8 행(v5 맨 아래)을 화면 안으로
	j := rearJackAt(t, h.v, engine.SlotSampler, 0, false)
	h.frame(ptrPress(-1, j.CX, j.CY-h.v.scrollY))
	if !h.v.jackDrag.on || h.v.jackDrag.srcSlot != engine.SlotSampler || h.v.jackDrag.srcPort != 0 {
		t.Fatalf("샘플러 OUT 잭 히트 실패: %+v", h.v.jackDrag)
	}
	h.frame(ptrRel(-1, j.CX, j.CY-h.v.scrollY)) // 빈 곳 놓기 — 정리만
	// 슬롯 8 케이블색의 **소유자는 TestCableColorsMatchPanel**이다(그림 띠 실측과 대조).
	// 여기에 색 리터럴을 또 두면 재핀할 때마다 두 자리를 고쳐야 하고, 실제로 한쪽만 고쳐
	// 빨강이 났다(2026-09-07). 이 테스트는 잭 히트와 케이블 수만 잰다.
	n := 0
	for i := 0; i < h.v.nCables; i++ {
		if h.v.cables[i].Src == uint8(engine.SlotSampler) {
			n++
		}
	}
	if n == 0 {
		t.Fatal("기본 랙에 슬롯 8 발신 케이블 없음(색이 그려지지 않는다)")
	}
}

// 12. 앞·뒷면 입력 분리(자기검증 ③): 뒷면에서 앞면 노브 위치를 만져도 파라미터
// 송신이 없고, 앞면에서 뒷면 잭 위치를 만져도 랙 명령이 없다. 앞면 회귀의 본증거는
// 기존 TestDrag·TestKnobDragNoScroll·TestScrolledKnobHit(전부 rear=false 경로)이고
// 여기는 뒷면이 그들을 침범하지 않는지만 잰다.
func TestRearFrontIsolation(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	kn := knobAt(h.v, secBassA, "TUNE")
	if kn == nil {
		t.Fatal("bassA TUNE 노브 없음")
	}
	h.frame(ptrPress(-1, kn.cx, kn.cy))
	h.frame(ptrMove(-1, kn.cx, kn.cy-120))
	h.frame(ptrRel(-1, kn.cx, kn.cy-120))
	for _, r := range h.fb.cmds {
		if r.c.Kind == engine.SetParam || r.c.Kind == engine.DeviceParam {
			t.Fatalf("뒷면에서 앞면 노브 송신 %+v", r.c)
		}
	}
	// 노브 제스처가 스크롤을 건드렸다(빈 판 = pkScroll) — 손을 뗀 뒤에도 관성이 남아
	// 이후 탭 프레임의 좌표 환산을 흔든다(scrollY 120→240+). 이 테스트의 축은 입력 분리지
	// 스크롤 물리가 아니므로 상태를 정지시키고 잰다.
	h.v.scrollY, h.v.scrollV = 0, 0
	tapRearPlate(t, h, engine.SlotFx) // 앞면 복귀
	j := rearJackAt(t, h.v, engine.SlotBassA, 0, false)
	sy := j.CY - h.v.scrollY // 잭 좌표도 화면계로(스크롤은 앞·뒷면 공유 상태)
	h.frame(ptrPress(-1, j.CX, sy))
	h.frame(ptrMove(-1, j.CX, sy-120))
	h.frame(ptrRel(-1, j.CX, sy-120))
	for _, r := range h.fb.cmds {
		if r.c.Kind == engine.Connect || r.c.Kind == engine.Disconnect {
			t.Fatalf("앞면에서 랙 명령 송신 %+v", r.c)
		}
	}
}

// tapRearPlate — 뒷면 장치 이름판 탭으로 앞면 복귀(테스트 12 보조). 이름판 좌표는
// 레이아웃 계라 화면 y로 환산해서 보낸다(좌표 계약 — 이 테스트는 노브 제스처가 스크롤을
// 움직인 뒤라 scrollY가 0이 아니다).
func tapRearPlate(t *testing.T, h *harness, slot int) {
	t.Helper()
	d := h.v.rearL.RearDeviceAt(slot)
	if d == nil {
		t.Fatalf("rear.json에 슬롯 %d 장치 없음", slot)
	}
	cx, cy := d.Plate.Center()
	tap(h, cx, cy-h.v.scrollY)
	if h.v.rear {
		t.Fatal("이름판 탭 복귀 실패")
	}
}

// 13. 뒷면 Update 무할당(프레임 루프 계약): 정지·드래그 유지·거부 감쇠 중 어느 상태도
// 힙 할당이 없어야 한다(스크롤 TestScrollNoAlloc 관례). (c)(d) P5-gain: 게인 팝업도
// 정지·게인 드래그 유지 프레임에 할당을 내지 않는다(라벨은 개방 1회, 숫자는 값 변화시만).
func TestRearUpdateNoAlloc(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(a) 뒷면 정지 상태 할당 %.0f회/프레임(0 예상)", a)
	}
	j := rearJackAt(t, h.v, engine.SlotBassA, 0, false)
	held := []core.Pointer{ptrPress(-1, j.CX, j.CY)}
	h.ctx.DT = h.dt
	h.ctx.Tick = h.fb.tick
	h.ctx.Pointers = held
	h.v.Update(h.ctx)
	held[0].JustPressed = false
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(b) 잭 잡은 채 할당 %.0f회/프레임(0 예상)", a)
	}
	h.ctx.Pointers = nil
	// (c) 팝업 열린 채 정지 — 표 재독(rev 불변)도 송신도 일어나지 않는 프레임.
	popTapJack(t, h, engine.SlotMain, 0)
	if !h.v.jackDrag.pop.on {
		t.Fatal("(c) 전제: 팝업 미개방")
	}
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(c) 팝업 정지 상태 할당 %.0f회/프레임(0 예상)", a)
	}
	// (d) 팝업 게인 드래그 유지(이동 없음) — 잡기만 하고 값이 안 변하는 프레임.
	r := popScreenRect(h)
	held = []core.Pointer{ptrPress(-1, r[0]+r[2]/2, r[1]+r[3]/2)}
	h.ctx.Pointers = held
	h.v.Update(h.ctx)
	held[0].JustPressed = false
	if a := testing.AllocsPerRun(200, func() { h.v.Update(h.ctx) }); a != 0 {
		t.Fatalf("(d) 팝업 드래그 유지 할당 %.0f회/프레임(0 예상)", a)
	}
	h.ctx.Pointers = nil
}

// TestCableGroupCount — 케이블 색 그룹 수가 상한 안인가. 상한이 모자라면 초과분이 남의
// 색으로 그려지는데 화면에는 그냥 '색이 이상한 케이블'로만 보인다(원인 추적이 어렵다).
// 실측 2026-09-06: 기본 랙 32케이블이 16그룹 — 옛 상한 16을 정확히 채웠다. 상한을
// RackCables로 올린 근거가 이 수치다.
func TestCableGroupCount(t *testing.T) {
	var f fakeBridge
	var cs [64]core.RackCable
	n := f.Cables(cs[:])
	seen := map[[2]uint8]bool{}
	for i := 0; i < n; i++ {
		a := cableA0 + cableAK*float64(cs[i].Gain)
		if a < cableA0 {
			a = cableA0
		} else if a > cableAMax {
			a = cableAMax
		}
		st := uint8(a * cableASteps)
		if st >= cableASteps {
			st = cableASteps - 1
		}
		seen[[2]uint8{cs[i].Src, st}] = true
	}
	t.Logf("기본 랙 케이블 %d개 → 색 그룹 %d개(상한 %d)", n, len(seen), maxCableGroups)
	if len(seen) > maxCableGroups {
		t.Fatalf("그룹 %d > 상한 %d — 초과분이 마지막 그룹 색으로 그려진다", len(seen), maxCableGroups)
	}
}

// TestCableColorsMatchPanel — 케이블 색(colCable)이 뒷면 패널의 섹션 색 띠 실측과 같은가.
// 색은 "이 줄이 어디서 나오는가"의 유일한 단서라 그림과 어긋나면 배선이 거짓말을 한다.
// 그림 라운드가 패널을 바꾸면 이 게이트가 먼저 걸린다(2026-09-06 채택본 시드 1234 기준).
// 허용 오차 6은 팔레트 양자화(256색 저장)와 중앙값 계산의 여유다.
func TestCableColorsMatchPanel(t *testing.T) {
	l, err := core.LoadRearLayout(assets.DeviceRearJSON)
	if err != nil {
		t.Fatalf("rear.json: %v", err)
	}
	data, err := assets.Read("device/rear.png")
	if err != nil {
		t.Skipf("rear.png 없음(데스크톱 embed 경로 밖): %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("rear.png 디코드: %v", err)
	}
	for i := range l.Devices {
		d := &l.Devices[i]
		x0, y0 := int(d.Rect[0])+4, int(d.Rect[1])+10
		x1, y1 := int(d.Rect[0])+16, int(d.Rect[1]+d.Rect[3])-10
		var rs, gs, bs []int
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				rs = append(rs, int(r>>8))
				gs = append(gs, int(g>>8))
				bs = append(bs, int(b>>8))
			}
		}
		sort.Ints(rs)
		sort.Ints(gs)
		sort.Ints(bs)
		mid := len(rs) / 2
		want := colCable[d.Slot]
		got := [3]int{rs[mid], gs[mid], bs[mid]}
		for k, v := range [3]int{int(want.R), int(want.G), int(want.B)} {
			if diff := got[k] - v; diff > 6 || diff < -6 {
				t.Errorf("슬롯 %d(%s) 띠 실측 %v ≠ colCable %v", d.Slot, d.Name, got, want)
				break
			}
		}
	}
}
