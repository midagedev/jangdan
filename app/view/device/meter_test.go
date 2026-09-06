// meter_test.go — 라인 미터(P3-meters) 계약↔단언. 계약 표는 device_test.go 헤더 참조.
//
// 헤드리스 경로(newView — 이미지 없음)에서 drawOverlays·drawMeters는 white1·padLEDImg가
// nil이라 실제 blit 없이 그리기 결정만 카운트한다(meterDraws) — 그 카운터로 소거 계약을
// 단언한다(room 뷰 draws 관례). 레벨은 fakeBridge tick에 넣어 검증한다(이 트리에서
// Tick.Levels는 다른 라운드가 배선 중 — 항상 0).
package device

import (
	"math"
	"testing"

	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// — 계약↔단언: vu 매핑(−36..0dB → 0..1, 입력 방어) —

func TestVuOf(t *testing.T) {
	cases := []struct {
		in   float32
		want float32
	}{
		{0, 0},                   // 무신호
		{0.0158, 0},              // −36dB 밑(20·log10(0.0158) ≈ −36.03)
		{0.1, 16.0 / 36.0},       // −20dB → 0.444…
		{0.5, 0.8328},            // −6.02dB → 0.833(독립 리터럴 — 구현식 재사용 아님)
		{1, 1},                   // 0dB 풀스케일
		{2, 1},                   // 클리핑 전 프리 FX — 상한 1
		{100, 1},                 // 상한(극단)
		{-0.3, 0},                // 음수 → 0
		{float32(math.NaN()), 0}, // NaN → 0(NaN 비교 거짓으로 가드)
	}
	for _, c := range cases {
		if got := vuOf(c.in); math.Abs(float64(got-c.want)) > 1e-3 {
			t.Fatalf("vuOf(%v) = %v(%v 예상)", c.in, got, c.want)
		}
	}
}

// — 계약↔단언: 밸리스틱(어택 즉시·릴리스 4/s)·Update 배선 —

func TestVuBallistics(t *testing.T) {
	// 릴리스: dt 0.1s에서 풀스케일 1 → 0.6(4/s 감쇠).
	if got := vuStep(0, 1, 0.1); math.Abs(float64(got-0.6)) > 1e-6 {
		t.Fatalf("vuStep(0,1,0.1) = %v(0.6 예상)", got)
	}
	// 하한 스냅: 감쇠량이 잔량보다 크면 음수 잔존이 아니라 정확히 0.
	if got := vuStep(0, 0.3, 0.1); got != 0 {
		t.Fatalf("vuStep(0,0.3,0.1) = %v(0 스냅 예상)", got)
	}
	// 어택 즉시: 새 vu가 크면 dt와 무관하게 그 프레임에 바로.
	if got := vuStep(0.5, 0.1, 1.0/60.0); got != 0.5 {
		t.Fatalf("vuStep(0.5,0.1,·) = %v(0.5 즉시 예상)", got)
	}
	// Update 배선: fakeBridge tick.Levels → meters.disp(파트 인덱스 = Levels 순서)·master = Peak.
	h := newHarness(t)
	h.fb.tick.Levels[engine.BD] = 0.5
	h.fb.tick.Peak = 1
	h.frame()
	want := vuOf(0.5)
	if got := h.v.meters.disp[engine.BD]; math.Abs(float64(got-want)) > 1e-6 {
		t.Fatalf("프레임 후 BD 표시값 %v(%v 예상)", got, want)
	}
	if h.v.meters.master != 1 {
		t.Fatalf("마스터 표시값 %v(1 예상)", h.v.meters.master)
	}
	// 구 호스트로 전환(Levels 전부 0): 1초 뒤 밸리스틱 잔상 없음.
	h.fb.tick = core.Tick{}
	h.run(60)
	for i, d := range h.v.meters.disp {
		if d != 0 {
			t.Fatalf("레벨 0(구 호스트)인데 파트 %d 표시값 %v 잔존", i, d)
		}
	}
	if h.v.meters.master != 0 {
		t.Fatalf("마스터 잔존 %v(0 예상)", h.v.meters.master)
	}
}

// — 계약↔단언: 패드 lit α 합성 —

func TestPadLitAlpha(t *testing.T) {
	// 상한 0.20(비전 FIX 2026-09-06 — 구 0.62는 점등 패드 라벨 대비 1.93:1; 이 표의 구 값은 새 상한에서 FAIL했다).
	cases := []struct{ tap, vu, want float32 }{
		{0.18, 0.1, 0.18},   // 레벨 0.1 → 0.12+0.05 = 0.17 < 탭 0.18 → 탭 유지
		{0.18, 0.15, 0.195}, // 레벨 0.15 → 0.195 > 탭
		{0.18, 0, 0.18},     // 레벨 0 — 탭 lit 유지(소거 계약)
		{0, 0.14, 0.19},     // 레벨 단독 0.12+0.07
		{0, 1, 0.20},        // 상한
		{0.18, 1, 0.20},     // 상한(탭+레벨 겹침)
		{0, 0, 0},           // 둘 다 0 — 그리지 않는다
	}
	for _, c := range cases {
		if got := padLitAlpha(c.tap, c.vu); math.Abs(float64(got-c.want)) > 1e-6 {
			t.Fatalf("padLitAlpha(%v,%v) = %v(%v 예상)", c.tap, c.vu, got, c.want)
		}
	}
}

// — 계약↔단언: 세그먼트 수·그리기 결정 카운터 —

func TestMeterSegments(t *testing.T) {
	// 켜진 세그먼트 수 = round(vu×segs), 상한 segs, vu ≤ 0 → 0.
	cases := []struct {
		vu   float32
		segs int
		want int
	}{
		{0, 12, 0},
		{0.444, 12, 5}, // 0.444×12 = 5.33
		{0.5, 12, 6},   // 6.0
		{1, 12, 12},
		{1.2, 12, 12},   // 상한(초과 vu도 segs를 넘지 않는다)
		{0.04, 12, 0},   // round(0.48) = 0
		{1, 20, 20},     // 하단(마스터)
		{0.833, 20, 17}, // 16.67 → 17
	}
	for _, c := range cases {
		if got := vuSegsOn(c.vu, c.segs); got != c.want {
			t.Fatalf("vuSegsOn(%v,%d) = %d(%d 예상)", c.vu, c.segs, got, c.want)
		}
	}
	// 양성 대조 — 카운터가 상시 0이 아님을 증명: 풀스케일에서 베이스 띠 2×12 +
	// 마스터 띠 20 + 패드 LED 점 6 = 50건. 헤드리스 경로(white1·padLEDImg nil)는
	// 결정만 카운트한다.
	h := newHarness(t)
	for i := range h.fb.tick.Levels {
		h.fb.tick.Levels[i] = 1
	}
	h.fb.tick.Peak = 1
	h.run(3)
	h.v.drawOverlays(nil, h.ctx)
	h.v.drawMeters(nil, h.ctx)
	if n := h.v.meterDraws; n != 50 {
		t.Fatalf("풀스케일 미터 그리기 결정 %d건(12+12+20+6 = 50 예상)", n)
	}
	// 탭 lit만으로는 미터 결정이 늘지 않는다(탭은 라인 미터가 아니다 — litUntil은
	// 릴리스 탭 판정에서 설정되므로 press+release 두 프레임).
	h2 := newHarness(t)
	bd := padAt(h2.v, "BD")
	cx, cy := bd.rect.Center()
	h2.frame(ptrPress(-1, cx, cy))
	h2.frame(ptrRel(-1, cx, cy))
	if bd.litUntil <= h2.ctx.Now {
		t.Fatal("탭 후 lit 없음(테스트 전제)")
	}
	h2.v.drawOverlays(nil, h2.ctx)
	h2.v.drawMeters(nil, h2.ctx)
	if n := h2.v.meterDraws; n != 0 {
		t.Fatalf("탭 lit 프레임 미터 결정 %d건(0 예상 — 레벨 0)", n)
	}
}

// — 계약↔단언: 구 호스트(Levels 전부 0) 미터 완전 소거 —

func TestMeterDarkWhenLevelsZero(t *testing.T) {
	h := newHarness(t)
	h.run(120) // 구 호스트: fakeBridge zero value — Levels·Peak 전부 0
	h.v.drawOverlays(nil, h.ctx)
	h.v.drawMeters(nil, h.ctx)
	if n := h.v.meterDraws; n != 0 {
		t.Fatalf("레벨 전부 0인데 미터 그리기 결정 %d건(0 예상)", n)
	}
}

// — 계약↔단언: 비전 FIX 2026-09-06 — 점등 패드 워시 상한·베이스 VU 트랙·선택기 분리 —

func TestPadLitCap(t *testing.T) {
	// 워시 아래 합성 경로: drawPadLit이 litA를 남기고, 레벨 풀스케일이면 상한 0.20(라벨 대비 계약).
	h := newHarness(t)
	for i := range h.fb.tick.Levels {
		h.fb.tick.Levels[i] = 1
	}
	h.run(3)
	h.v.drawPadLit(nil, h.ctx)
	if a := padAt(h.v, "BD").litA; a != padLitMaxA || padLitMaxA > 0.25 {
		t.Fatalf("풀스케일 BD litA %v(%v 예상, 상한 ≤0.25)", a, padLitMaxA)
	}
}

func TestChordPickerGeometry(t *testing.T) {
	// 토글 셀은 셀 7의 오른쪽 끝을 유지하고 왼쪽 chordTogGap을 비운다; 외곽선 rect는 띠 사방 inset.
	h := newHarness(t)
	c7, tg := h.v.chordCells[7], h.v.chordTogRect
	if tg[0]+tg[2] != c7[0]+c7[2] || tg[0]-c7[0] != chordTogGap {
		t.Fatalf("토글 셀 %v vs 셀7 %v", tg, c7)
	}
	rr, cr := h.v.chordRingRect, h.v.chordRect
	if cr[0]-rr[0] != chordOpenInset || rr[2]-cr[2] != 2*chordOpenInset || rr[3]-cr[3] != 2*chordOpenInset {
		t.Fatalf("외곽선 rect %v vs 띠 %v", rr, cr)
	}
	// 꺼진 트랙은 레벨과 무관하게 그려지되 meterDraws(켜진 세그먼트)는 늘리지 않는다.
	h.v.drawVuBand(nil, h.v.dispRects[0], 0, vuSegsBass, vuBandH)
	if h.v.meterDraws != 0 {
		t.Fatalf("레벨 0 트랙 그리기가 meterDraws를 %d로 올림", h.v.meterDraws)
	}
	// 12세그먼트 × 5px + 11 간격 = 71 = 창 폭 75 − 2×여백(비전 처방 x 611..682와 일치).
	r := h.v.dispRects[0]
	segW := (r[2] - 2*vuSegPad - vuSegGap*float64(vuSegsBass-1)) / float64(vuSegsBass)
	if r[2] == 75 && segW != 5 {
		t.Fatalf("세그먼트 폭 %v(5 예상)", segW)
	}
}
