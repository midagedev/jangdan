// sampler_test.go — 샘플러 재생 장치 수치 게이트(P5-sampler-dev 소유 파일). samplerDev를
// 직접 만들어 setParam·noteOn/noteOff/allOff·process를 구동한다. 테스트 파일은 math 자유
// (엔진 규칙 — poly_test.go와 같은 관례). 팩 파형의 성격(밝기·감쇠 모양)에 의존하는 단언은
// 없다 — 계약은 슬롯 길이·기준음·루프 지점·피크 0.9뿐이며, 현재 bakePack 자리표 파형에서도
// 파형 교체 후에도 통과해야 한다(필요한 슬롯 성질은 packTab 리터럴에서 직접 읽는다).
//
// 계약 ↔ 단언 표(태스크 스펙 12항 ↔ 이 파일):
//
// | #  | 계약              | 단언                                                                                                                            | FAIL-first |
// |----|-------------------|---------------------------------------------------------------------------------------------------------------------------------|------------|
// | 1  | 침묵              | TestSamplerSilence: init 직후 512샘플 전부 Float32bits == 0                                                                     | 출력 단 부호 반전(-out)을 넣으면 −0 비트(0x80000000)로 실패 확인 |
// | 2  | 원샷 자동 종료    | TestSamplerOneShot: 슬롯 0(48000프레임) 원음 noteOn → noteOff 무시(게이트·보이스 유지)·48000+64샘플 내 active()==false             | noteOff가 원샷도 게이트 오프하면 "게이트 유지" 단언에서 실패 확인 |
// | 3  | 피치              | TestSamplerPitch: 원음 inc == 1.0(비트), root±12 → 2.0/0.5 ±0.002, noteOn(200) == noteOn(MaxSemis) 클램프                        | MaxSemis 클램프를 지우면 note 200 비교에서 실패 확인 |
// | 4  | 튠                | TestSamplerTune: SmpTune 1.0 → inc 2.0 ±0.002, 0.0 → 0.5 ±0.002                                                                | 튠 매핑 배수를 24→12로 틀리면 ±0.002 밖으로 실패 확인 |
// | 5  | 라운드로빈·스틸   | TestSamplerSteal: 서로 다른 note 5회 noteOn → 보이스 4개 on, 다섯째는 가장 오래된 보이스(0번) 자리, 보이스 1..3 inc 무결                | 스틸이 최신 보이스(seq 비교 >)를 고르면 보이스 3 무결 단언에서 실패 확인 |
// | 6  | 보이스 슬롯 래치  | TestSamplerSlotLatch: 슬롯 0 발음 중 SmpSelect 1.0(슬롯 7) → 그 보이스 sel 0·on 유지, 다음 noteOn의 sel은 7                        | noteOn이 v.sel을 물리지 않으면 "다음 noteOn sel 7"에서 실패 확인 |
// | 7  | 루프              | TestSamplerLoop: 슬롯 5(TAPE, loop 0 < n) 2.5배 렌더에 active()·RMS > 0·pos < n(랩 확인), noteOff 후 12τ 내 종료                  | 랩 차감(pos −= n−loop)을 지우면 pos ≥ n 단언에서 실패 확인 |
// | 8  | allOff            | TestSamplerAllOff: 원샷 재생 중 allOff → 릴리즈 시정수(기본 0.3 ≈ 80ms)의 12배 안에 active()==false                               | allOff를 no-op로 두면 12τ에도 active()==true로 실패 확인 |
// | 9  | 출력 상한         | TestSamplerBound: Level 1·같은 note·전부 액센트 4보이스 × 전 슬롯 전 구간 max |out| ≤ 0.99027                                     | outClipScale(×0.82)을 빼면 max ≈ 1.0015 > 0.99027로 실패 확인 |
// | 10 | SmpStart          | TestSamplerStart: q 0.5 → pos 23999(= uint32(0.5·(48000−1))), q 0 → 0, q 1 → n−1(끝 클램프 경로 무패닉)                            | pos를 항상 0으로 두면 23999 단언에서 실패 확인 |
// | 11 | 파라미터 정규화   | TestSamplerNormalize: k −1/8/SmpParams 무동작(무패닉), q NaN → 0과 비트 동일, 5 → 1과 비트 동일                                   | NaN 클램프 분기를 지우면 NaN tune이 0 tune과 다른 비트로 실패 확인 |
// | 12 | 무할당            | TestSamplerNoAllocs: ① process 128샘플 루프 ② setParam 8개 ③ noteOn+noteOff — AllocsPerRun == 0                                   | process에 힙 탈출 make(전역 싱크 대입)를 넣으면 실패 확인(engine_test #3와 같은 원리 — 지역 make는 스택 배정으로 안 잡힘) |
//
// 스펙 노트: 7번의 "noteOff 후 릴리즈 시정수 안에"는 문언 그대로(1τ)면 성립할 수 없다 — 종료
// 역치가 aenv < 1e-5이므로 최소 ln(10^5) = 11.51τ가 걸리고, 같은 스펙의 8번이 "12배"를 규정한다.
// 8번과 같은 12τ(poly.go polySilence 주석과 같은 관례)로 해석해 단언한다.
package engine

import (
	"math"
	"testing"
)

// smpRenderN — n샘플 렌더(모노). poly_test.go polyRenderN과 같은 꼴(장치 타입만 다름).
func smpRenderN(s *samplerDev, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = s.process()
	}
	return out
}

// smpRms — 전 구간 RMS.
func smpRms(x []float32) float64 {
	var acc float64
	for _, v := range x {
		acc += float64(v) * float64(v)
	}
	return math.Sqrt(acc / float64(len(x)))
}

// 1. 침묵 — init 직후 첫 noteOn 전 512샘플은 정확히 +0(비트. −0도 실패).
func TestSamplerSilence(t *testing.T) {
	var s samplerDev
	s.init(1)
	if s.active() {
		t.Fatalf("init 직후 active()==true — 상태 0 계약 위반")
	}
	for i := 0; i < 512; i++ {
		if v := s.process(); math.Float32bits(v) != 0 {
			t.Fatalf("첫 noteOn 전 샘플 %d = %v(비트 %#x) — +0이 아님", i, v, math.Float32bits(v))
		}
	}
}

// 2. 원샷 자동 종료 — noteOff는 무시(끝까지 운다), 슬롯 길이를 다 읽으면 꺼진다.
func TestSamplerOneShot(t *testing.T) {
	var s samplerDev
	s.init(3)
	if s.sel != 0 {
		t.Fatalf("기본 선택 슬롯 = %d — 0이어야 한다", s.sel)
	}
	s.noteOn(packTab[0].root, true) // 슬롯 0(48000프레임) 원음 — inc 1.0
	offAt := -1
	for i := 1; i <= 48064; i++ {
		if i == 1000 {
			s.noteOff()
			if !s.voices[0].gate || !s.voices[0].on {
				t.Fatalf("원샷이 noteOff로 게이트 오프됨 — 원샷은 끝까지 우는 계약")
			}
		}
		s.process()
		if i == 24000 && !s.active() {
			t.Fatalf("noteOff 24000샘플 뒤 무음 — 원샷이 끊겼다")
		}
		if offAt < 0 && !s.active() {
			offAt = i
		}
	}
	if offAt < 0 {
		t.Fatalf("48000+64 샘플 안에 원샷이 끝나지 않음")
	}
	if offAt < 47000 || offAt > 48064 {
		t.Fatalf("원샷 종료 프레임 이상: %d(슬롯 0 = 48000프레임, inc 1.0)", offAt)
	}
	t.Logf("원샷 종료 프레임(noteOn 후): %d", offAt)
}

// 3. 피치 — 원음 inc 정확히 1.0, ±12반음 2.0/0.5, note 클램프.
func TestSamplerPitch(t *testing.T) {
	var s samplerDev
	s.init(3) // SmpTune 기본 0.5 → tuneSemis 0
	root := packTab[0].root
	s.noteOn(root, false)
	if inc := s.voices[0].inc; inc != 1 {
		t.Fatalf("원음 inc = %v — 정확히 1.0이어야 한다", inc)
	}
	s.noteOn(root+12, false)
	if inc := s.voices[1].inc; math.Abs(float64(inc)-2) > 0.002 {
		t.Fatalf("root+12 inc = %v — 2.0 ±0.002", inc)
	}
	s.noteOn(root-12, false)
	if inc := s.voices[2].inc; math.Abs(float64(inc)-0.5) > 0.002 {
		t.Fatalf("root−12 inc = %v — 0.5 ±0.002", inc)
	}
	s.noteOn(200, false) // 클램프 도메인 밖
	clamped := s.voices[3].inc
	s.noteOn(MaxSemis, true) // 전부 켜져 있어 스틸 — 가장 오래된 보이스 0
	if s.voices[0].inc != clamped {
		t.Fatalf("noteOn(200) inc %v ≠ noteOn(MaxSemis) inc %v — 클램프 계약", clamped, s.voices[0].inc)
	}
}

// 4. 튠 — SmpTune 양끝에서 ±12반음.
func TestSamplerTune(t *testing.T) {
	root := packTab[0].root
	var up samplerDev
	up.init(3)
	up.setParam(SmpTune, 1.0)
	up.noteOn(root, false)
	if inc := up.voices[0].inc; math.Abs(float64(inc)-2) > 0.002 {
		t.Fatalf("SmpTune 1.0 원음 inc = %v — 2.0 ±0.002", inc)
	}
	var down samplerDev
	down.init(3)
	down.setParam(SmpTune, 0.0)
	down.noteOn(root, false)
	if inc := down.voices[0].inc; math.Abs(float64(inc)-0.5) > 0.002 {
		t.Fatalf("SmpTune 0.0 원음 inc = %v — 0.5 ±0.002", inc)
	}
}

// 5. 라운드로빈·보이스 스틸 — 꺼진 보이스 중 최소 번호, 전부 켜져 있으면 가장 오래된 것.
func TestSamplerSteal(t *testing.T) {
	notes := [5]uint8{24, 27, 31, 34, 36}
	var s samplerDev
	s.init(3)
	for _, n := range notes[:4] {
		s.noteOn(n, true)
	}
	before := [4]float32{}
	for i := range before {
		before[i] = s.voices[i].inc
	}
	s.noteOn(notes[4], true)
	onCount := 0
	for i := range s.voices {
		if s.voices[i].on {
			onCount++
		}
	}
	if onCount != 4 {
		t.Fatalf("켜진 보이스 %d — 4여야 한다(스틸은 자리를 바꾼다)", onCount)
	}
	if s.voices[0].inc == before[0] {
		t.Fatalf("보이스 0이 그대로 — 다섯 번째 noteOn의 스틸이 일어나지 않았다")
	}
	for i := 1; i < smpVoices; i++ {
		if s.voices[i].inc != before[i] {
			t.Fatalf("보이스 %d inc가 바뀜 — 스틸은 가장 오래된 보이스(0번)만", i)
		}
	}
	var ref samplerDev
	ref.init(3)
	ref.noteOn(notes[4], true)
	if s.voices[0].inc != ref.voices[0].inc {
		t.Fatalf("스틸 자리의 inc %v ≠ 다섯 번째 노트 inc %v", s.voices[0].inc, ref.voices[0].inc)
	}
}

// 6. 보이스가 슬롯을 물고 간다 — 울리는 중의 SmpSelect 변경은 다음 noteOn부터.
func TestSamplerSlotLatch(t *testing.T) {
	var s samplerDev
	s.init(3)
	s.noteOn(24, true) // 슬롯 0(기본)
	if s.voices[0].sel != 0 {
		t.Fatalf("첫 noteOn sel = %d — 0이어야 한다", s.voices[0].sel)
	}
	s.setParam(SmpSelect, 1.0) // int(mul32(1,7)+0.5) = 7
	if s.sel != 7 {
		t.Fatalf("SmpSelect 1.0 → 슬롯 %d — 7이어야 한다", s.sel)
	}
	for i := 0; i < 1000; i++ {
		s.process()
	}
	if !s.voices[0].on || s.voices[0].sel != 0 {
		t.Fatalf("울리는 보이스가 슬롯을 바꿈/꺼짐 — 시작 슬롯을 끝까지 읽는 계약(on=%v sel=%d)", s.voices[0].on, s.voices[0].sel)
	}
	if s.voices[0].pos <= 0 || s.voices[0].pos >= float32(packTab[0].n) {
		t.Fatalf("보이스 0 pos %v — 슬롯 0 구간 안에서 진행 중이어야 한다", s.voices[0].pos)
	}
	s.noteOn(24, true) // 새 보이스(1번, 꺼져 있던 최소 번호)는 슬롯 7
	if s.voices[1].sel != 7 {
		t.Fatalf("다음 noteOn sel = %d — 7이어야 한다", s.voices[1].sel)
	}
}

// 7. 루프 — 게이트가 살아 있는 동안(그리고 릴리즈 중에도) [loop, n)을 돈다.
func TestSamplerLoop(t *testing.T) {
	if packTab[5].loop >= packTab[5].n {
		t.Fatalf("전제 위반: 슬롯 5가 루프 슬롯이 아님(packTab 리터럴 확인)")
	}
	var s samplerDev
	s.init(3)
	s.setParam(SmpSelect, 5.0/7.0) // 슬롯 5 TAPE(n 38400, loop 0)
	if s.sel != 5 {
		t.Fatalf("SmpSelect 5/7 → 슬롯 %d — 5이어야 한다", s.sel)
	}
	s.setParam(SmpLoop, 1)
	s.noteOn(packTab[5].root, true)
	if !s.voices[0].loop {
		t.Fatalf("SmpLoop 1 + 루프 슬롯 — 보이스가 루프 모드가 아님")
	}
	n := int(packTab[5].n)
	out := make([]float32, int(2.5*float64(n)))
	for i := range out {
		if i%4800 == 0 && !s.active() {
			t.Fatalf("루프 구간 %d에서 무음 — 게이트가 살아 있는 동안 계속 돌아야 한다", i)
		}
		out[i] = s.process()
	}
	if !s.active() {
		t.Fatalf("2.5×n 렌더 뒤 무음 — 루프 계약")
	}
	if rms := smpRms(out); rms <= 0 {
		t.Fatalf("루프 구간 RMS = %v — 0이면 안 된다", rms)
	} else {
		t.Logf("슬롯 5 루프 2.5×n(96000샘플) RMS = %.6f", rms)
	}
	if pos := s.voices[0].pos; pos < 0 || pos >= float32(n) {
		t.Fatalf("랩 후 pos = %v — [0, n) 안이어야 한다(랩 계약)", pos)
	}
	s.noteOff()
	tau := 0.02 * math.Pow(100, 0.3) // SmpRelease 기본 0.3 → τ ≈ 79.6ms
	lim := int(12*tau*SampleRate) + 64
	offAt := -1
	for i := 1; i <= lim; i++ {
		s.process()
		if !s.active() {
			offAt = i
			break
		}
	}
	if offAt < 0 {
		t.Fatalf("noteOff 후 12τ(%d샘플) 안에 종료 안 됨", lim)
	}
	t.Logf("noteOff 후 종료 프레임: %d(12τ 한계 %d)", offAt, lim)
}

// 8. allOff — 원샷 재생 중에도 릴리즈로 들어간다(위치 유지).
func TestSamplerAllOff(t *testing.T) {
	var s samplerDev
	s.init(3)
	s.setParam(SmpTune, 0.0) // −12반음 — inc 0.5, 슬롯 0이 96000샘플 울림(버퍼 끝이 아니라 엔벨로프가 끈다)
	s.noteOn(packTab[0].root, true)
	smpRenderN(&s, 2400) // 어택 완료(기본 0.05 → 약 0.68ms ≈ 33샘플) 뒤
	s.allOff()
	if !s.active() {
		t.Fatalf("allOff 직후 active()==false — 릴리즈가 시작도 안 됨")
	}
	if s.voices[0].gate {
		t.Fatalf("allOff 후 게이트가 살아 있음")
	}
	tau := 0.02 * math.Pow(100, 0.3) // SmpRelease 기본 0.3 ≈ 80ms
	lim := int(12*tau*SampleRate) + 64
	offAt := -1
	for i := 1; i <= lim; i++ {
		s.process()
		if !s.active() {
			offAt = i
			break
		}
	}
	if offAt < 0 {
		t.Fatalf("allOff 후 12τ(%d샘플) 안에 active()==false 아님", lim)
	}
	t.Logf("allOff 후 종료 프레임: %d(12τ 한계 %d)", offAt, lim)
}

// 9. 출력 상한 — 최악 조합(같은 note·전부 액센트·Level 1) 4보이스 × 전 슬롯 전 구간.
func TestSamplerBound(t *testing.T) {
	bound := 1.20763 * 0.82 // = 0.990256… 게이트는 반올림 여유 0.99027
	maxAll := float64(0)
	for slot := 0; slot < packSlots; slot++ {
		var s samplerDev
		s.init(3)
		s.setParam(SmpSelect, float32(slot)/7)
		s.setParam(SmpLevel, 1)
		root := packTab[slot].root
		for k := 0; k < smpVoices; k++ {
			s.noteOn(root, true)
		}
		n := int(packTab[slot].n)
		slotMax := float64(0)
		for i := 0; i < n+2000; i++ {
			v := s.process()
			if math.IsNaN(float64(v)) {
				t.Fatalf("슬롯 %d 샘플 %d NaN", slot, i)
			}
			if a := math.Abs(float64(v)); a > slotMax {
				slotMax = a
			}
		}
		if slotMax > maxAll {
			maxAll = slotMax
		}
		t.Logf("슬롯 %d(%s) 최대 |out| = %.6f", slot, PackNames[slot], slotMax)
	}
	if maxAll > bound+0.00001 {
		t.Fatalf("출력 상한 초과: %.6f > %.6f", maxAll, bound)
	}
	t.Logf("전 슬롯 최대 |out| = %.6f(상한 %.6f)", maxAll, bound)
}

// 10. SmpStart — 시작 오프셋 = uint32(mul32(q, n−1)).
func TestSamplerStart(t *testing.T) {
	var s samplerDev
	s.init(3)
	s.setParam(SmpStart, 0.5)
	s.noteOn(packTab[0].root, false)
	if pos := s.voices[0].pos; pos != 23999 { // uint32(mul32(0.5, 48000−1)) = uint32(23999.5)
		t.Fatalf("SmpStart 0.5 → pos = %v — 23999여야 한다", pos)
	}
	var z samplerDev
	z.init(3)
	z.setParam(SmpStart, 0)
	z.noteOn(packTab[0].root, false)
	if pos := z.voices[0].pos; pos != 0 {
		t.Fatalf("SmpStart 0 → pos = %v — 0이어야 한다", pos)
	}
	var e samplerDev
	e.init(3)
	e.setParam(SmpStart, 1)
	e.noteOn(packTab[0].root, false) // pos = n−1 — i0+1 끝 클램프 경로
	if pos := e.voices[0].pos; pos != float32(packTab[0].n-1) {
		t.Fatalf("SmpStart 1 → pos = %v — n−1이어야 한다", pos)
	}
	for i := 0; i < 8; i++ {
		if v := e.process(); math.IsNaN(float64(v)) {
			t.Fatalf("끝 클램프 경로 NaN: %v", v)
		}
	}
}

// 11. 파라미터 정규화 — 범위 밖 k 무동작, NaN → 0, >1 → 1.
func TestSamplerNormalize(t *testing.T) {
	var s samplerDev
	s.init(3)
	for _, k := range []int{-1, SmpParams, 8, 99} {
		s.setParam(k, 0.9) // 무동작 — 무패닉이면 된다
	}
	var fresh samplerDev
	fresh.init(3)
	if s.sel != fresh.sel || s.tune != fresh.tune || s.startQ != fresh.startQ || s.loopQ != fresh.loopQ ||
		s.atkInc != fresh.atkInc || s.relCoef != fresh.relCoef || s.lpCoef != fresh.lpCoef || s.level != fresh.level {
		t.Fatalf("범위 밖 k가 상태를 바꿈")
	}
	s.setParam(SmpTune, float32(math.NaN()))
	var z samplerDev
	z.init(3)
	z.setParam(SmpTune, 0)
	if math.Float32bits(s.tune) != math.Float32bits(z.tune) {
		t.Fatalf("NaN → 0 클램프 위반: %#x ≠ %#x", math.Float32bits(s.tune), math.Float32bits(z.tune))
	}
	s.setParam(SmpTune, 5)
	var one samplerDev
	one.init(3)
	one.setParam(SmpTune, 1)
	if math.Float32bits(s.tune) != math.Float32bits(one.tune) {
		t.Fatalf("5 → 1 클램프 위반")
	}
	s.setParam(SmpLevel, float32(math.NaN()))
	if s.level != 0 {
		t.Fatalf("Level NaN → %v — 0이어야 한다", s.level)
	}
	s.setParam(SmpLevel, 5)
	if s.level != 1 {
		t.Fatalf("Level 5 → %v — 1이어야 한다", s.level)
	}
}

// 12. 무할당 — process·setParam·noteOn/noteOff 힙 할당 0.
func TestSamplerNoAllocs(t *testing.T) {
	var s samplerDev
	s.init(7)
	s.noteOn(24, true)
	if n := testing.AllocsPerRun(100, func() {
		for i := 0; i < 128; i++ {
			s.process()
		}
	}); n != 0 {
		t.Fatalf("process 128샘플 할당: %v", n)
	}
	if n := testing.AllocsPerRun(100, func() {
		for k := 0; k < SmpParams; k++ {
			s.setParam(k, 0.5)
		}
	}); n != 0 {
		t.Fatalf("setParam 8개 할당: %v", n)
	}
	if n := testing.AllocsPerRun(100, func() {
		s.noteOn(24, true)
		s.noteOff()
	}); n != 0 {
		t.Fatalf("noteOn+noteOff 할당: %v", n)
	}
}
