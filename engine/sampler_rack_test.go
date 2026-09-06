// sampler_rack_test.go — 샘플러 장치의 **랙 결합** 게이트(리드 소유). 장치 내부 계약은
// sampler_test.go, 팩 계약은 samplerpack_test.go가 잰다. 여기서 재는 것은 배선뿐이다:
//
//	| 계약                                   | 테스트                                |
//	|----------------------------------------|---------------------------------------|
//	| 기본 랙에 샘플러가 없다(이 라운드 계약) | TestSamplerNotInDefaultRack           |
//	| Add→Connect→DeviceStep이 들린다         | TestSamplerRackAudible                |
//	| RemoveDevice가 즉시 조용해진다          | TestSamplerRackRemoveSilences         |
//	| Transport 정지가 릴리즈로 보낸다        | TestSamplerRackTransportStop          |
//	| 로컬 파라미터·패턴이 상태 왕복에 산다   | TestSamplerRackStateRoundTrip         |
//	| 샘플러가 꽂힌 랙도 렌더 무할당          | TestSamplerRackNoAllocs               |
//
// 기본값 해시(fx2_test.go TestFx2DefaultHash)가 이 라운드에 불변인 근거가 첫 테스트다 —
// 기본 랙에 들어가지 않으므로 기본 스트림의 연산이 하나도 바뀌지 않는다.
package engine

import (
	"testing"
	"time"
)

const smpTestSlot = 8 // 기본 랙이 쓰는 0..7 밖의 빈 슬롯

// addSamplerRack — 슬롯 8에 샘플러를 놓고 Fx 직결 입력(포트 1 — 드럼과 같은 자리)에 1.0으로 잇는다.
func addSamplerRack(t *testing.T, e *Engine) {
	t.Helper()
	e.Apply(Cmd{Kind: AddDevice, A: smpTestSlot, B: uint8(KindSampler)})
	if e.rack.kind[smpTestSlot] != KindSampler {
		t.Fatalf("AddDevice(%d, KindSampler) 실패 — kind=%d", smpTestSlot, e.rack.kind[smpTestSlot])
	}
	e.Apply(Cmd{Kind: Connect, A: smpTestSlot, B: SlotFx, C: 0 | 1<<4, D: uint8(Unbound), V: 1})
}

// renderPeak — n블록 렌더의 최대 |샘플|.
func renderPeak(e *Engine, blocks int) float32 {
	var out [256]float32
	var peak float32
	for b := 0; b < blocks; b++ {
		e.Render(out[:])
		for _, v := range out {
			if a := abs32(v); a > peak {
				peak = a
			}
		}
	}
	return peak
}

// 1. 기본 랙에는 샘플러가 없다 — 이 라운드가 기본값 해시를 건드리지 않는 이유.
func TestSamplerNotInDefaultRack(t *testing.T) {
	e := New(1)
	for s := 0; s < RackSlots; s++ {
		if e.rack.kind[s] == KindSampler {
			t.Fatalf("기본 랙 슬롯 %d에 샘플러가 있다 — 이 라운드 계약은 '기본 랙 불변'이다", s)
		}
	}
	if kindCap[KindSampler] != 1 || kindPorts[KindSampler] != [2]uint8{0, 1} {
		t.Fatalf("포트·인스턴스 표: cap=%d ports=%v (기대 1, [0 1])", kindCap[KindSampler], kindPorts[KindSampler])
	}
}

// 2. Add → Connect → DeviceStep 이 실제로 들린다. 비교 기준은 같은 시드의 샘플러 없는 엔진이다.
func TestSamplerRackAudible(t *testing.T) {
	base := New(1)
	basePeak := renderPeak(base, 200)

	e := New(1)
	addSamplerRack(t, e)
	// 스텝 0·4·8·12에 게이트(4분음). note 0 = 그 마디 코드 루트.
	for st := 0; st < Steps; st += 4 {
		e.Apply(Cmd{Kind: DeviceStep, A: smpTestSlot, B: uint8(st), C: 0, D: StepGate})
	}
	e.Apply(Cmd{Kind: DeviceParam, A: smpTestSlot, B: SmpLevel, V: 1})
	peak := renderPeak(e, 200)
	if peak <= basePeak+0.02 {
		t.Fatalf("샘플러를 꽂고 스텝을 켰는데 피크가 안 늘었다: base=%.4f with=%.4f", basePeak, peak)
	}
	if !e.smp[0].active() {
		t.Fatalf("200블록 뒤에도 샘플러 보이스가 하나도 안 울렸다")
	}
}

// 3. RemoveDevice — 빠진 장치는 즉시 조용하고, 닿은 케이블도 사라진다.
func TestSamplerRackRemoveSilences(t *testing.T) {
	e := New(1)
	addSamplerRack(t, e)
	nWith := e.NumCables()
	e.Apply(Cmd{Kind: DeviceStep, A: smpTestSlot, B: 0, C: 0, D: StepGate})
	renderPeak(e, 40)
	if !e.smp[0].active() {
		t.Fatalf("전제 실패 — 제거 전에 울리고 있어야 한다")
	}
	e.Apply(Cmd{Kind: RemoveDevice, A: smpTestSlot})
	if e.rack.kind[smpTestSlot] != KindNone {
		t.Fatalf("RemoveDevice 후에도 슬롯 %d에 장치가 있다", smpTestSlot)
	}
	if e.NumCables() != nWith-1 {
		t.Fatalf("케이블이 안 지워졌다: %d → %d (기대 %d)", nWith, e.NumCables(), nWith-1)
	}
	// allOff가 갔으므로 릴리즈 안에 꺼진다(기본 SmpRelease 0.3 ≈ 80ms · 12τ < 1초).
	for b := 0; b < 400 && e.smp[0].active(); b++ {
		var out [256]float32
		e.Render(out[:])
	}
	if e.smp[0].active() {
		t.Fatalf("RemoveDevice 1초 뒤에도 샘플러가 울린다 — allOff 경로가 없다")
	}
}

// 4. Transport 정지 → 릴리즈. 폴리와 같은 규칙이다.
func TestSamplerRackTransportStop(t *testing.T) {
	e := New(1)
	addSamplerRack(t, e)
	e.Apply(Cmd{Kind: DeviceParam, A: smpTestSlot, B: SmpLoop, V: 1})         // 루프 모드
	e.Apply(Cmd{Kind: DeviceParam, A: smpTestSlot, B: SmpSelect, V: 5.0 / 7}) // 슬롯 5 TAPE(loop < n)
	e.Apply(Cmd{Kind: DeviceStep, A: smpTestSlot, B: 0, C: 0, D: StepGate})
	renderPeak(e, 40)
	if !e.smp[0].active() {
		t.Fatalf("전제 실패 — 정지 전에 울리고 있어야 한다")
	}
	e.Apply(Cmd{Kind: Transport, A: 0})
	for b := 0; b < 400 && e.smp[0].active(); b++ {
		var out [256]float32
		e.Render(out[:])
	}
	if e.smp[0].active() {
		t.Fatalf("Transport 정지 1초 뒤에도 샘플러가 울린다")
	}
}

// 5. 상태 왕복 — 슬롯 로컬 파라미터와 스텝 패턴은 v5 포맷이 이미 담는다(장치 종류 무관).
// 읽은 뒤 계수까지 유도되는지(applyDevParam 경로)를 함께 잰다.
func TestSamplerRackStateRoundTrip(t *testing.T) {
	e := New(7)
	addSamplerRack(t, e)
	e.Apply(Cmd{Kind: DeviceParam, A: smpTestSlot, B: SmpSelect, V: 3.0 / 7})
	e.Apply(Cmd{Kind: DeviceParam, A: smpTestSlot, B: SmpTone, V: 0.25})
	e.Apply(Cmd{Kind: DeviceStep, A: smpTestSlot, B: 5, C: 9, D: StepGate | StepAccent})

	buf := make([]byte, StateSize)
	if n := e.WriteState(buf); n != StateSize {
		t.Fatalf("WriteState=%d", n)
	}
	d := New(999)
	if !d.ReadState(buf) {
		t.Fatalf("ReadState 거부")
	}
	if d.rack.kind[smpTestSlot] != KindSampler {
		t.Fatalf("복원된 랙에 샘플러가 없다")
	}
	if d.rack.devParQ[smpTestSlot][SmpSelect] != e.rack.devParQ[smpTestSlot][SmpSelect] ||
		d.rack.devParQ[smpTestSlot][SmpTone] != e.rack.devParQ[smpTestSlot][SmpTone] {
		t.Fatalf("로컬 파라미터 불일치: %v vs %v", d.rack.devParQ[smpTestSlot], e.rack.devParQ[smpTestSlot])
	}
	if d.rack.devPat[smpTestSlot][5] != e.rack.devPat[smpTestSlot][5] {
		t.Fatalf("스텝 패턴 불일치")
	}
	if d.smp[0].sel != e.smp[0].sel || d.smp[0].lpCoef != e.smp[0].lpCoef {
		t.Fatalf("계수 유도가 안 됐다(applyDevParam 경로): sel %d/%d lp %g/%g",
			d.smp[0].sel, e.smp[0].sel, d.smp[0].lpCoef, e.smp[0].lpCoef)
	}
}

// 6. 샘플러가 꽂힌 랙의 렌더도 무할당(핫 루프 계약).
func TestSamplerRackNoAllocs(t *testing.T) {
	e := New(1)
	addSamplerRack(t, e)
	for st := 0; st < Steps; st += 2 {
		e.Apply(Cmd{Kind: DeviceStep, A: smpTestSlot, B: uint8(st), C: 0, D: StepGate})
	}
	out := make([]float32, 256)
	e.Render(out) // 워밍업
	if n := testing.AllocsPerRun(50, func() { e.Render(out) }); n != 0 {
		t.Fatalf("샘플러 랙 Render 할당 %g회/블록 — 무할당 계약 위반", n)
	}
}

// 7. 굽기 비용의 연기경보(벤치마크가 아니다). 팩은 New/Reset 최초 1회에 구워지고 그 시간은
// firstSound 예산(≤300ms) 안에 들어가야 한다 — 워클릿에서 wasm은 네이티브의 2~3배다.
// 상한 60ms는 실측 10.7ms(리드 2026-09-07, 파형표 도입 후)의 5.6배 여유라 머신이 바쁠 때
// 흔들리지 않는다. 이 게이트가 잡는 것은 "굽기가 조용히 무거워졌다"는 계급이다: VOX가
// 샘플마다 144개 사인을 더해 43ms를 먹던 결함을 이 축이 없어 벤치를 따로 짜서야 찾았다.
func TestPackBakeStartupCost(t *testing.T) {
	buf := new([packFrames]float32)
	bakePack(buf) // 캐시·페이지 워밍업
	start := time.Now()
	bakePack(buf)
	el := time.Since(start)
	if el > 60*time.Millisecond {
		t.Fatalf("팩 굽기 %v > 60ms — 시작 경로(New) 비용이 커졌다. 슬롯별로 재서 원인을 좁혀라", el)
	}
	t.Logf("팩 굽기 %v (상한 60ms, 실측 기준 ~11ms)", el)
}
