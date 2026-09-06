// back_test.go — 뒷면 케이블 뷰 게이트(§14.3, P5-back-view). back.go 단독 소유.
//
// FAIL-first 기록(2026-09-06): 구현 전 소스에서 이 파일 추가 → `v.rear undefined`,
// `v.pressRear undefined` 등 정의 없음 컴파일 실패(P3-meters·P5-poly-dsp 관례 — 정의
// 없음 컴파일 실패가 FAIL-first다). 동작 변경 단언(TestFrameFlags 이름판 탭 = 놓을 때)
// 는 device_test.go 쪽 적색(h.v.rear undefined)이 같은 커밋 시점의 증거다.
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

// 11. 케이블 표 ↔ 그려진 베지어 수 일치(§14.3 게이트). 기본 랙 케이블 32개의 근거:
// buildDefault — dry 5 + 폴리 센드 3 + 딜레이 센드 8 + 리버브 센드 8 + 코러스 2 + 리턴 6.
// 32개 전부 양 끝 잭이 rear.json에 있으므로(기본 랙 8장치 = 뒷면 8행 전부) 전부 그려진다.
// 헤드리스 Draw는 픽셀 읽기 없이 카운터로 잰다(rearDraws).
func TestRearDrawMatchesTable(t *testing.T) {
	h := newHarness(t)
	enterRear(t, h)
	h.frame() // 표 동기화
	if n := h.v.nCables; n != 32 {
		t.Fatalf("기본 랙 케이블 %d개(32 예상 — dry 5+폴리 3+딜레이 8+리버브 8+코러스 2+리턴 6)", n)
	}
	screen := ebiten.NewImage(720, 1280)
	h.v.Draw(screen, h.ctx)
	if h.v.rearDraws != h.v.nCables {
		t.Fatalf("그려진 케이블 %d개(표 %d개 예상)", h.v.rearDraws, h.v.nCables)
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
// 힙 할당이 없어야 한다(스크롤 TestScrollNoAlloc 관례).
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
