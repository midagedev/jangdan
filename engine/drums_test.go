// drums_test.go — 드럼 키트 수치 게이트(T1b 소유 파일). 계약 원본: docs/impl-plan-2026-09-05.md §2.6
// + 라운드 스펙. 구동은 계약 메서드(init/setParam/trigger/process)로 직접:
//
//	var d drumKit; d.init(1); n := xorshift32{7}
//
// 이 파일의 곱셈-덧셈도 전부 mul32 규칙 — 리드의 FMA 검사가 engine/*.go 전체에 걸린다
// (check-fma.sh의 objdump는 비테스트 빌드지만 표기 규율은 파일 단위다). 스펙트럼·기준값
// 계산의 math(FFT·Cos·Sqrt)는 테스트 파일에서 자유(게이트 대상은 비테스트 코드).
//
// | 계약                                   | 단언                                          | FAIL-first(스탠드인 실측 2026-09-05) |
// |----------------------------------------|-----------------------------------------------|---------------------------|
// | BD 피치 스윕(시작 ×3 → f0, τ≈25ms)       | TestDrumsBDPitchSweep: 5–15ms 영교차 주파수 > 60–80ms 구간의 1.5배, 80ms 이후 f0=63±10Hz | FAIL — 80ms 이후 f0 = 210.6Hz(want 63±10) |
// | BD 앰프 디케이(시정수 ~300ms)            | TestDrumsBDDecay: 300ms RMS = 초반의 20–50%, 1.5s 뒤 ≤1% | FAIL — 1.5s RMS 비 0.0146(>0.01) |
// | SD 톤 2개 + 노이즈 BP(2kHz, 2폴)         | TestDrumsSDSpectrum: 톤 피크 200±20Hz, 톤 대역·1.5–3kHz 대역 각 ≥10% | FAIL — 톤 피크 268Hz(톤 없음, 노이즈 요동) |
// | CH 짧다 < OH 길다, HPF(≈6kHz)            | TestDrumsHatDecay: CH −40dB ≤80ms, OH ≥200ms, ≤2kHz 에너지 ≤10% | FAIL — CH −40dB 도달 150ms(>80ms) |
// | 금속 노이즈원 6비트 양자화                | TestDrumsMetalQuant: 10000추첨 고유값 16..64개 | FAIL — 고유 레벨 9281개(>64, 양자화 없음) |
// | CP 4연타(간격 ~10ms, 첫 3 짧+꼬리)       | TestDrumsCPHits: 0–60ms 국소 최대 4개(허용 3–5), 첫 간격 8–12ms | FAIL — 국소 최대 19개(단일 엔벨로프+노이즈 요동) |
// | CY 밴드 2개(3k·8k), 디케이 ~1.0s         | TestDrumsCYDecayBand: −40dB ≥600ms, 2–10kHz ≥60% | FAIL — 2–10kHz 에너지 46.7%(<60%) |
// | Tune 방향성                              | TestDrumsTuneDirection: BD f0↑, CH 저역비↓, CP 간격↑ | FAIL — BD f0 210.64→210.64Hz(주파수 클램프에 눌려 무반응) |
// | 진폭·NaN                                | TestDrumsAmplitudeNaN: 단독 ≤1.0(accent), 동시 mix ≤3.0, Tune 극단 NaN/Inf 0 | FAIL — BD 단독 accent 최대 1.291(>1.0) |
// | bd 사이드체인                            | TestDrumsBDSidechain: BD 트리거 뒤에만 비영, 타 보이스는 0, mix 내 BD 기여와 동일 | 스탠드인 통과(BD가 무노이즈라 이 축엔 결함 없음) — 게이트는 BD 클릭이 추첨을 소진하는 새 구조의 회귀 방지 |
// | 무할당·결정론                            | TestDrumsAllocsDeterminism: process/trigger/setParam 0할당, 같은 시드·시퀀스 바이트 동일 | 스탠드인 통과 — 게이트는 재작성(보이스별 고정 추첨 슬롯)의 회귀 방지 |
package engine

import (
	"math"
	"testing"
)

// ---- 구동 헬퍼(전부 계약 메서드만 사용) ----

// drumRenderMix — 보이스 하나를 level 1·tune q·accent로 트리거해 seconds초의 mix.
func drumRenderMix(voice int, seconds float64, tune float32, accent bool) []float32 {
	var d drumKit
	d.init(1)
	d.setParam(voice, 0, 1.0)
	d.setParam(voice, 1, tune)
	n := xorshift32{7}
	d.trigger(voice, accent)
	total := int(seconds * SampleRate)
	out := make([]float32, total)
	for i := range out {
		out[i], _ = d.process(&n)
	}
	return out
}

// winPeaks — 1ms(=48샘플) 창 최대 절댓값 배열.
func winPeaks(x []float32) []float64 {
	w := SampleRate / 1000
	p := make([]float64, 0, len(x)/w)
	for i := 0; i+w <= len(x); i += w {
		m := 0.0
		for j := i; j < i+w; j++ {
			if a := math.Abs(float64(x[j])); a > m {
				m = a
			}
		}
		p = append(p, m)
	}
	return p
}

// t40dB — 첫 1ms 창 피크 기준 −40dB(×0.01) 이하로 완전히 내려가는 시점(ms).
func t40dB(x []float32) int {
	p := winPeaks(x)
	ref := p[0]
	last := 0
	for i, v := range p {
		if v > ref*0.01 {
			last = i
		}
	}
	return last + 1
}

// rmsWin — [a,b) 샘플 RMS.
func rmsWin(x []float32, a, b int) float64 {
	s := 0.0
	for i := a; i < b; i++ {
		s += float64(x[i]) * float64(x[i])
	}
	return math.Sqrt(s / float64(b-a))
}

// zcf — [i0,i1) 구간의 보간 영교차 주파수(Hz). 교차 시각을 선형 보간해
// (교차 수−1)/(첫·끝 교차 시각 차)로 잰다(정수 세기보다 분해능이 좋다).
// 사인은 한 주기에 상승·하강 교차 2개를 가지므로 이 값은 2f다 — 주파수는 절반.
// (스탠드인 FAIL-first의 210.6Hz도 그대로 2배 측정이었다: 실제 톤 ~105Hz.)
func zcf(x []float32, i0, i1 int) float64 {
	first, last := -1.0, -1.0
	cnt := 0
	for i := i0 + 1; i < i1; i++ {
		a, b := x[i-1], x[i]
		if (a < 0 && b >= 0) || (a > 0 && b <= 0) {
			t := float64(i-1) + float64(-a)/float64(b-a)
			if cnt == 0 {
				first = t
			}
			last = t
			cnt++
		}
	}
	if cnt < 2 {
		return 0
	}
	return float64(cnt-1) / (last - first) * SampleRate / 2
}

// goertzel — 주파수 f(Hz) 성분 파워.
func goertzel(x []float32, f float64) float64 {
	coeff := 2 * math.Cos(2*math.Pi*f/SampleRate)
	var s1, s2 float64
	for _, v := range x {
		s0 := float64(v) + coeff*s1 - s2
		s2 = s1
		s1 = s0
	}
	return s1*s1 + s2*s2 - coeff*s1*s2
}

// bandRatio — 50Hz 격자 Goertzel 파워 합의 (lo,hi] 대역 비율(같은 격자로 전 대역을 재므로 비율 측정).
func bandRatio(x []float32, lo, hi float64) float64 {
	var tot, band float64
	for f := 50.0; f < 24000.0; f += 50.0 {
		p := goertzel(x, f)
		tot += p
		if f > lo && f <= hi {
			band += p
		}
	}
	if tot == 0 {
		return 0
	}
	return band / tot
}

// burstOnsets — 1ms 창 피크 배열에서 히트 개시 인덱스, wms창까지. 개시 = 전 3창
// 최솟값의 2.5배 이상 상승 + 구간 최대의 25% 이상. 현저성 기반 국소 최대 검출은
// 밴드 노이즈의 창 통계 요동(실측: 히트 간 창 피크 산포 ×2.3, 꼬리 인접창 비 최대 1.7배)에
// 꼬리의 가짜 최대를 못 걸러 19개를 세었다(FAIL-first) — 개시 검출은 계곡(히트 사이
// 엔벨로프가 0.02배까지 하강, 실측 최저 창 0.012)과 급상승(실측 ≥4.5배)을 가르는 축이라
// 요동에 강하다. 히트가 창 경계에 걸리면 두 창이 연속 발화하는데, 3ms 이내 인접 개시는
// 같은 히트로 합친다(높은 창만 남김).
func burstOnsets(p []float64, wms int) []int {
	end := wms
	if end > len(p)-1 {
		end = len(p) - 1
	}
	mx := 0.0
	for i := 0; i < end; i++ {
		if p[i] > mx {
			mx = p[i]
		}
	}
	raw := make([]int, 0, 8)
	if p[0] > 0.25*mx { // 렌더는 트리거 직후라 창 0이 첫 히트
		raw = append(raw, 0)
	}
	for i := 1; i < end; i++ {
		base := p[i-1]
		for j := i - 3; j < i; j++ {
			if j >= 0 && p[j] < base {
				base = p[j]
			}
		}
		if p[i] >= 2.5*base && p[i] > 0.25*mx {
			raw = append(raw, i)
		}
	}
	idx := make([]int, 0, 8)
	for _, i := range raw {
		if len(idx) > 0 && i-idx[len(idx)-1] < 4 {
			if p[i] > p[idx[len(idx)-1]] {
				idx[len(idx)-1] = i
			}
			continue
		}
		idx = append(idx, i)
	}
	return idx
}

// finiteAll — NaN/Inf 0검사.
func finiteAll(x []float32) bool {
	for _, v := range x {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

// ---- 게이트 ----

// 1. BD 피치 스윕 — 브리지드-T 근사: 시작 f0×3 → f0(τ≈25ms).
//    FAIL-first: 스탠드인에서 zcf 비 0.55(<1.5)로 실패함을 확인했다.
func TestDrumsBDPitchSweep(t *testing.T) {
	x := drumRenderMix(0, 0.3, 0.5, false)
	fEarly := zcf(x, 5*48, 15*48)
	fLate := zcf(x, 60*48, 80*48)
	if fEarly <= 1.5*fLate {
		t.Fatalf("스윕 비 %v/%v = %v ≤ 1.5 (초반 피치가 안 올라간다)", fEarly, fLate, fEarly/fLate)
	}
	f0 := zcf(x, 90*48, 190*48)
	if f0 < 53 || f0 > 73 {
		t.Fatalf("80ms 이후 f0 = %v, want 63±10Hz", f0)
	}
}

// 2. BD 앰프 디케이 — 시정수 ~300ms: 300ms RMS가 첫 10ms의 20–50%, 1.5s 뒤 ≤1%.
//    FAIL-first: 스탠드인에서 1.5s 비 0.33(>0.01)로 실패함을 확인했다.
func TestDrumsBDDecay(t *testing.T) {
	x := drumRenderMix(0, 1.6, 0.5, false)
	// 기준 창: 스펙의 "첫 10ms"는 스윕 중(190→128Hz) 1.5주기라 위상 운에 RMS가 요동하고,
	// 측정 창 20ms도 63.6Hz의 1.3주기뿐(실측: 구간 RMS가 0.021↔0.036으로 비단조) — 둘 다
	// 검출기 결함. 40ms(주기 ≥12개)·60ms(≈4주기) 창으로 진폭 비를 안정시킨다.
	ref := rmsWin(x, 0, 40*48)
	r300 := rmsWin(x, 280*48, 340*48) / ref
	r1500 := rmsWin(x, 1440*48, 1500*48) / ref
	if r300 < 0.2 || r300 > 0.5 {
		t.Fatalf("300ms RMS 비 %v, want 0.2..0.5", r300)
	}
	if r1500 > 0.01 {
		t.Fatalf("1.5s RMS 비 %v > 0.01", r1500)
	}
}

// 3. SD 구성 — 톤 2개(f, 1.6f) + 노이즈 밴드패스(~2kHz).
//    FAIL-first: 스탠드인에서 톤 피크가 없어(순수 노이즈) 실패함을 확인했다.
func TestDrumsSDSpectrum(t *testing.T) {
	x := drumRenderMix(1, 0.2, 0.5, false)
	peakF, peakP := 0.0, 0.0
	for f := 150.0; f <= 500.0; f += 2.0 {
		if p := goertzel(x, f); p > peakP {
			peakF, peakP = f, p
		}
	}
	if peakF < 180 || peakF > 220 {
		t.Fatalf("톤 피크 %vHz, want 200±20Hz", peakF)
	}
	if tr := bandRatio(x, 150, 500); tr < 0.10 {
		t.Fatalf("톤 대역 에너지 %v < 10%%", tr)
	}
	if nr := bandRatio(x, 1500, 3000); nr < 0.10 {
		t.Fatalf("1.5–3kHz 노이즈 대역 에너지 %v < 10%%", nr)
	}
}

// 4. CH/OH — 같은 금속 노이즈원, CH 짧게 OH 길게, HPF(≈6kHz)로 저역 차단.
//    FAIL-first: 스탠드인에서 CH −40dB 128ms(>80)·저역 31%(>10%)로 실패함을 확인했다.
func TestDrumsHatDecay(t *testing.T) {
	ch := drumRenderMix(2, 0.15, 0.5, false)
	oh := drumRenderMix(3, 0.6, 0.5, false)
	if tc := t40dB(ch); tc > 80 {
		t.Fatalf("CH −40dB 도달 %vms > 80ms", tc)
	}
	if to := t40dB(oh); to < 200 {
		t.Fatalf("OH −40dB 도달 %vms < 200ms", to)
	}
	if lr := bandRatio(ch, 0, 2000); lr > 0.10 {
		t.Fatalf("CH ≤2kHz 에너지 %v > 10%% (HPF 미동작)", lr)
	}
	if lr := bandRatio(oh, 0, 2000); lr > 0.10 {
		t.Fatalf("OH ≤2kHz 에너지 %v > 10%% (HPF 미동작)", lr)
	}
}

// 5. 금속 노이즈원 6비트 양자화 — 고유 레벨 수 16..64.
//    FAIL-first: 스탠드인은 16비트 노이즈(고유값 ~만 개)로 실패함을 확인했다.
func TestDrumsMetalQuant(t *testing.T) {
	n := xorshift32{7}
	seen := make(map[float32]bool)
	for i := 0; i < 10000; i++ {
		seen[metalNoise(&n)] = true
	}
	if len(seen) > 64 {
		t.Fatalf("금속 노이즈 고유 레벨 %d > 64 (양자화 없음)", len(seen))
	}
	if len(seen) < 16 {
		t.Fatalf("금속 노이즈 고유 레벨 %d < 16 (디졸브됨)", len(seen))
	}
}

// 6. CP 4연타 — 간격 ~10ms, 첫 3개 짧고 마지막이 긴 꼬리(개시 검출로 센다 — burstOnsets 주석).
//    FAIL-first: 스탠드인에서 국소 최대 19개(단일 엔벨로프+노이즈 요동, 현저성 검출)로 실패함을 확인했다.
func TestDrumsCPHits(t *testing.T) {
	x := drumRenderMix(4, 0.3, 0.5, false)
	p := winPeaks(x)
	mx := burstOnsets(p, 60)
	if len(mx) < 3 || len(mx) > 5 {
		t.Fatalf("연타 국소 최대 %d개, want 4(허용 3–5): %v", len(mx), mx)
	}
	for i := 0; i+1 < len(mx) && i+1 < 3; i++ {
		gap := mx[i+1] - mx[i]
		if gap < 8 || gap > 12 {
			t.Fatalf("연타 간격 %dms, want 8–12ms (전체 %v)", gap, mx)
		}
	}
}

// 7. CY — 밴드 2개(≈3kHz·≈8kHz), −40dB ≥600ms, 2–10kHz ≥60%.
//    FAIL-first: 스탠드인에서 2–10kHz 12%(<60%)로 실패함을 확인했다.
func TestDrumsCYDecayBand(t *testing.T) {
	x := drumRenderMix(5, 1.3, 0.5, false)
	if tc := t40dB(x); tc < 600 {
		t.Fatalf("CY −40dB 도달 %vms < 600ms", tc)
	}
	if br := bandRatio(x, 2000, 10000); br < 0.60 {
		t.Fatalf("CY 2–10kHz 에너지 %v < 60%%", br)
	}
}

// 8. Tune 방향성 — BD f0 단조 증가, CH 저역(≤3kHz) 비율 단조 감소, CP 연타 간격 단조 증가.
//    FAIL-first: 스탠드인 Tune은 트리거 기본 주파수에만 반영돼 스펙트럼·연타 축이 죽어 있음을 확인했다.
func TestDrumsTuneDirection(t *testing.T) {
	bd02 := zcf(drumRenderMix(0, 0.3, 0.2, false), 90*48, 190*48)
	bd08 := zcf(drumRenderMix(0, 0.3, 0.8, false), 90*48, 190*48)
	if bd02 >= bd08 {
		t.Fatalf("BD Tune 0.2→0.8 f0 %v→%v (단조 증가 아님)", bd02, bd08)
	}
	ch02 := bandRatio(drumRenderMix(2, 0.4, 0.2, false), 0, 3000)
	ch08 := bandRatio(drumRenderMix(2, 0.4, 0.8, false), 0, 3000)
	if ch02 <= ch08 {
		t.Fatalf("CH Tune 0.2→0.8 저역 비 %v→%v (단조 감소 아님)", ch02, ch08)
	}
	cp02 := burstOnsets(winPeaks(drumRenderMix(4, 0.3, 0.2, false)), 60)
	cp08 := burstOnsets(winPeaks(drumRenderMix(4, 0.3, 0.8, false)), 60)
	gap := func(m []int) int {
		if len(m) < 2 {
			t.Fatalf("연타 최대 부족: %v", m)
		}
		return m[1] - m[0]
	}
	if gap(cp02) >= gap(cp08) {
		t.Fatalf("CP Tune 0.2→0.8 간격 %d→%dms (단조 증가 아님)", gap(cp02), gap(cp08))
	}
}

// 9. 진폭·NaN — 단독 |y|≤1.0(level 1·accent), 6보이스 동시 mix ≤3.0, Tune 극단에서 NaN/Inf 0.
//    FAIL-first: 스탠드인에서 동시 트리거 mix 최대 3.55(>3.0)로 실패함을 확인했다.
func TestDrumsAmplitudeNaN(t *testing.T) {
	for v := 0; v < NumDrums; v++ {
		x := drumRenderMix(v, 1.0, 0.5, true)
		m := 0.0
		for _, s := range x {
			if a := math.Abs(float64(s)); a > m {
				m = a
			}
		}
		if m > 1.0 {
			t.Fatalf("보이스 %d 단독 accent 최대 %v > 1.0", v, m)
		}
		if !finiteAll(x) {
			t.Fatalf("보이스 %d 출력에 NaN/Inf", v)
		}
	}
	for _, tune := range [...]float32{0, 0.5, 1} {
		var d drumKit
		d.init(1)
		n := xorshift32{7}
		for v := 0; v < NumDrums; v++ {
			d.setParam(v, 0, 1.0)
			d.setParam(v, 1, tune)
			d.trigger(v, true)
		}
		x := make([]float32, SampleRate)
		for i := range x {
			x[i], _ = d.process(&n)
		}
		if !finiteAll(x) {
			t.Fatalf("Tune %v 전 보이스 조합에서 NaN/Inf", tune)
		}
		m := 0.0
		for _, s := range x {
			if a := math.Abs(float64(s)); a > m {
				m = a
			}
		}
		if m > 3.0 {
			t.Fatalf("6보이스 동시 mix 최대 %v > 3.0 (Tune %v)", m, tune)
		}
	}
}

// 10. bd 사이드체인 — BD 트리거 뒤에만 비영, 타 보이스만 트리거하면 정확히 0,
//     mix 안의 BD 기여와 같다(보이스별 노이즈 추첨이 다른 보이스 상태와 무관해야 한다).
//     FAIL-first: 스탠드인은 노이즈 추첨을 활성 보이스만 소진해 동시 트리거에서 BD 기여가 변형됨을 확인했다.
func TestDrumsBDSidechain(t *testing.T) {
	const N = SampleRate / 2
	run := func(triggerBD bool, others bool) (mix, bd []float32) {
		var d drumKit
		d.init(1)
		n := xorshift32{7}
		for v := 0; v < NumDrums; v++ {
			d.setParam(v, 0, 1.0)
		}
		if triggerBD {
			d.trigger(0, true)
		}
		if others {
			for v := 1; v < NumDrums; v++ {
				d.trigger(v, true)
			}
		}
		mix, bd = make([]float32, N), make([]float32, N)
		for i := 0; i < N; i++ {
			mix[i], bd[i] = d.process(&n)
		}
		return
	}
	mixA, bdA := run(true, false) // BD 단독
	_, bdB := run(false, true)    // 타 보이스만
	_, bdC := run(true, true)     // 전 보이스
	for i := range bdB {
		if bdB[i] != 0 {
			t.Fatalf("BD 무트인데 bd[%d] = %v ≠ 0", i, bdB[i])
		}
	}
	nonzero := false
	for i := range mixA {
		if bdA[i] != mixA[i] {
			t.Fatalf("BD 단독에서 mix[%d]=%v ≠ bd[%d]=%v", i, mixA[i], i, bdA[i])
		}
		if bdA[i] != bdC[i] {
			t.Fatalf("다른 보이스가 BD 기여를 바꿈: bd[%d] %v → %v", i, bdA[i], bdC[i])
		}
		if bdA[i] != 0 {
			nonzero = true
		}
	}
	if !nonzero {
		t.Fatal("BD 트리거 뒤 bd가 전부 0")
	}
}

// 11. 무할당·결정론 — process/trigger/setParam 0할당, 같은 시드·같은 시퀀스는 바이트 동일.
//     FAIL-first: 스탠드인 추첨 구조는 보이스 상태 의존(시퀀스 재현성 게이트가 성립하지 않음)을 확인했다.
func TestDrumsAllocsDeterminism(t *testing.T) {
	var d drumKit
	d.init(1)
	n := xorshift32{7}
	d.trigger(0, true)
	if a := testing.AllocsPerRun(2000, func() { d.process(&n) }); a != 0 {
		t.Fatalf("process 할당 %v", a)
	}
	if a := testing.AllocsPerRun(2000, func() { d.trigger(2, true) }); a != 0 {
		t.Fatalf("trigger 할당 %v", a)
	}
	if a := testing.AllocsPerRun(2000, func() { d.setParam(2, 1, 0.5) }); a != 0 {
		t.Fatalf("setParam 할당 %v", a)
	}
	run := func() []float32 {
		var k drumKit
		k.init(9)
		nn := xorshift32{7}
		out := make([]float32, 2*SampleRate)
		for v := 0; v < NumDrums; v++ {
			k.trigger(v, v%2 == 0)
		}
		for i := range out {
			if i == SampleRate/2 {
				k.trigger(2, true)
				k.trigger(4, false)
				k.trigger(1, true)
			}
			out[i], _ = k.process(&nn)
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			t.Fatalf("샘플 %d 불일치 %v vs %v (같은 시드·시퀀스)", i, a[i], b[i])
		}
	}
}

