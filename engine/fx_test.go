// 계약 ↔ 단언 표 (T1c 스펙 "수치 게이트" ↔ 이 파일의 단언).
//
// 테스트 파일은 math 패키지 자유(스펙 공통 계약 — DFT·기준값 계산용). FMA mul32
// 계약의 게이트 대상은 비테스트 코드다(tools/check-fma.sh는 go build ./engine만 본다).
//
// 구동 방식: `var f fxChain`은 384KB라 전부 `f := new(fxChain)`로 힙에 둔다.
// 파라미터 인덱스 0=Delay 1=Drive 2=Comp 3=Master, setTempo(5538) → delaySamp=33228.
//
// | 계약                          | 단언                                        | FAIL-first |
// |-------------------------------|---------------------------------------------|------------|
// | 바이패스(Drive0 Comp0 Delay0 Master1) | TestFxBypass: bass=0.3 상수 → 0.82·rat(rat(0.3)) ±1e-3, L==R | 최종 0.82 스케일을 지우자 "바이패스 출력 0.28504312, want 0.233735"로 실패 |
// | 덕킹: 1ms 안 −12dB 이상        | TestFxDucking: 임펄스 뒤 48샘플 내 최대 출력비 ≤ 0.25 | 어택(f.duck=1) 분기 제거 시 "덕킹 직후 비 1.0000 > 0.25 (i=0)"로 실패 |
// | 덕킹: 릴리즈 120ms 지수         | TestFxDucking: 120ms 복귀 ≥ 0.63, 400ms ≥ 0.95 | duckRel을 1로 두면(감쇠 없음) "120ms 복귀 0.0000 < 0.63"로 실패 |
// | 덕킹은 베이스만                | TestFxDucking: drums 경로는 bdSide 유무와 무관하게 바이트 동일 | 게인을 (bass+drums) 합에 걸면 "드럼 경로가 덕킹됨: 샘플 0"으로 실패 |
// | 드라이브 고조파                | TestFxDriveHarmonics: 3차/기본 비가 Drive1에서 ≥10dB 큼 | 프리게인을 상수 1로 두면 "3차 고조파 비 차 -2.5dB < 10dB"로 실패 |
// | 딜레이 시간 = 6스텝            | TestFxDelayTime: 첫 에코 L, D±1 / 둘째 에코 R, 2D±1 | 읽기 오프셋을 delaySamp/2로 바꾸면 "첫 에코 위치 16614, want 33228±1"로 실패 |
// | 핑퐁(L→R→L)                   | TestFxDelayTime: L[2D] ≤ R 에코의 10%        | 입력을 R 버퍼에도 넣으면 "L[2D]=0.0196 > R 에코의 10%"로 실패 |
// | 피드백 감쇠 ≤ 0.7             | TestFxDelayFeedbackLP: 연속 에코 비 ≤ 0.7    | 피드백을 mul32(q,1.4)로 바꾸면 "연속 에코 비 0.817 > 0.7"로 실패 |
// | 피드백 LP(4kHz)로 어두워짐     | TestFxDelayFeedbackLP: 8kHz 비 < 1kHz 비의 70% | LP 없이 원신호 피드백 시 "8kHz 비 0.4210 ≥ 1kHz 비 0.4213의 70%"로 실패 |
// | Delay 0 완전 바이패스          | TestFxDelayBypass: 버퍼 채운 뒤 q=0, 임펄스 후 전 구간 |y| < 1e-6 | 믹스를 mul32(q,0.5)+0.01로 두면 "에코 누출: 샘플 1"로 실패(이 변이 검증 중 init의 driveCor 누락 버그를 발견·수정) |
// | 마스터 선형                   | TestFxMasterLinear: Master 0.5/1 비 0.5±1%   | 마스터 곱을 지우면 "Master 0.5 비 1.0000, want 0.5±0.005"로 실패 |
// | 상한 |out| ≤ 1.0·유한         | TestFxBounds: 극단 2초 + 81조합 × 임펄스·상수 | 스케일 0.82→1.3 시 "상한: |1.1133| > 1.0", master−1 나눗셈 삽입 시 "유한 아님 — 샘플 0 = +Inf"로 실패 |
// | setTempo 클램프               | TestFxSetTempoClamp: 1e9→47999, 0/NaN/음수→1, 5538→33228 | 상한 클램프 제거 시 "setTempo(1e9) → 6000000000, want 47999"로 실패 |
// | 무할당                        | TestFxNoAllocDeterminism: process/setParam/setTempo AllocsPerRun==0 | process에 패키지 슬라이스 make를 넣으면 "process 할당: 1"로 실패 |
// | 결정론                        | TestFxNoAllocDeterminism: 같은 이력 두 번 바이트 동일 | process가 패키지 카운터를 출력에 더하면 "결정론 위반: 샘플 0"으로 실패 |
package engine

import (
	"math"
	"testing"
)

// --- 테스트 픽스처(384KB 구조체라 전부 new로 힙) ---

// fxRun — 파라미터 4개와 샘플별 입력 함수로 n샘플 구동.
func fxRun(delay, drive, comp, master float32, bass, drums, bdSide, dropDrive func(int) float32, n int) ([]float32, []float32) {
	f := new(fxChain)
	f.init()
	f.setTempo(5538)
	f.setParam(0, delay)
	f.setParam(1, drive)
	f.setParam(2, comp)
	f.setParam(3, master)
	L := make([]float32, n)
	R := make([]float32, n)
	for i := 0; i < n; i++ {
		l, r := f.process(bass(i), drums(i), bdSide(i), dropDrive(i))
		L[i] = l
		R[i] = r
	}
	return L, R
}

func constf(v float32) func(int) float32 { return func(int) float32 { return v } }
func zerof(int) float32                  { return 0 }

// impulsef — 지정 위치(들)에 v 1샘플.
func impulsef(v float32, at ...int) func(int) float32 {
	return func(i int) float32 {
		for _, a := range at {
			if i == a {
				return v
			}
		}
		return 0
	}
}

// impulseTrain — every 간격으로 v 1샘플.
func impulseTrain(v float32, every, n int) func(int) float32 {
	return func(i int) float32 {
		if every > 0 && i%every == 0 {
			return v
		}
		return 0
	}
}

// ratF — 유리식 소프트클립 x(27+x²)/(27+9x²)의 float64 기준값.
func ratF(x float64) float64 { return x * (27 + x*x) / (27 + 9*x*x) }

// dB — 정수 주기 DFT 크기(dB). 테스트 전용 math 자유.
func dB(y []float32, hz float64) float64 {
	var re, im float64
	w := 2 * math.Pi * hz / float64(len(y))
	for i, v := range y {
		re += float64(v) * math.Cos(w*float64(i))
		im -= float64(v) * math.Sin(w*float64(i))
	}
	return 20 * math.Log10(math.Hypot(re, im))
}

// --- 게이트 ---

// 1. 바이패스 — 전 노브 0(Master 1)이면 체인은 두 번의 유리식+스케일뿐이다.
//    FAIL-first: 최종 outClip의 mul32(y, 0.82)를 y로 두면 실패함을 확인했다.
func TestFxBypass(t *testing.T) {
	L, R := fxRun(0, 0, 0, 1, constf(0.3), zerof, zerof, zerof, 4800)
	want := float64(outClipScale) * ratF(ratF(0.3))
	for i := range L {
		if d := math.Abs(float64(L[i]) - want); d > 1e-3 {
			t.Fatalf("바이패스 출력 %v, want %.6f ±1e-3 (샘플 %d)", L[i], want, i)
		}
		if L[i] != R[i] {
			t.Fatalf("바이패스에서 L≠R: 샘플 %d %v vs %v", i, L[i], R[i])
		}
	}
}

// 2. 사이드체인 덕킹 — 어택 1ms 이내 −12dB, 릴리즈 120ms(63%)/400ms(95%) 복귀,
//    드럼 경로는 영향받지 않는다. 복귀율 = (y−y_duck)/(y0−y_duck).
//    FAIL-first: 어택 분기 제거 / duckRel=1 / 게인을 합에 적용 — 세 경우 모두 실패 확인.
func TestFxDucking(t *testing.T) {
	const n = 24000
	base, _ := fxRun(0, 0, 1, 1, constf(0.3), zerof, zerof, zerof, n)
	L, _ := fxRun(0, 0, 1, 1, constf(0.3), zerof, impulsef(1.0, 0), zerof, n)
	y0 := base[100]
	for i := 0; i <= 48; i++ { // 1ms
		if ratio := float64(L[i]) / float64(y0); ratio > 0.25 {
			t.Fatalf("덕킹 직후 비 %.4f > 0.25 (i=%d)", ratio, i)
		}
	}
	yDuck := L[48]
	rec := func(i int) float64 {
		return (float64(L[i]) - float64(yDuck)) / (float64(y0) - float64(yDuck))
	}
	if r := rec(5760); r < 0.63 { // 120ms
		t.Fatalf("120ms 복귀 %.4f < 0.63", r)
	}
	if r := rec(19200); r < 0.95 { // 400ms
		t.Fatalf("400ms 복귀 %.4f < 0.95", r)
	}
	// 드럼은 덕킹되지 않는다 — bdSide 유무와 무관하게 바이트 동일
	d0, _ := fxRun(0, 0, 1, 1, zerof, constf(0.3), zerof, zerof, n)
	d1, _ := fxRun(0, 0, 1, 1, zerof, constf(0.3), impulsef(1.0, 0), zerof, n)
	for i := range d0 {
		if d0[i] != d1[i] {
			t.Fatalf("드럼 경로가 덕킹됨: 샘플 %d %v vs %v", i, d0[i], d1[i])
		}
	}
}

// 3. 드라이브 고조파 — 100Hz 사인 0.5의 3차/기본파 DFT 비가 Drive 1에서 ≥10dB 크다.
//    FAIL-first: 프리게인 pg를 상수 1로 두면 차가 0dB가 되어 실패함을 확인했다.
func TestFxDriveHarmonics(t *testing.T) {
	sine := func(i int) float32 {
		return 0.5 * float32(math.Sin(2*math.Pi*100*float64(i)/48000))
	}
	h := func(drive float32) float64 {
		L, _ := fxRun(0, drive, 0, 1, sine, zerof, zerof, zerof, 48000)
		return dB(L, 300) - dB(L, 100)
	}
	d0, d1 := h(0), h(1)
	if d1-d0 < 10 {
		t.Fatalf("3차 고조파 비 차 %.1fdB < 10dB (Drive0 %.1fdB, Drive1 %.1fdB)", d1-d0, d0, d1)
	}
}

// 4. 딜레이 시간·핑퐁 — 첫 에코는 L의 D=33228±1, 둘째는 R의 2D±1, L[2D]는 R 에코의 10% 이하.
//    FAIL-first: 읽기 오프셋 / 입력 R 버퍼 추가 — 두 경우 모두 실패 확인.
func TestFxDelayTime(t *testing.T) {
	const D = 33228
	n := 3*D + 100
	L, R := fxRun(0.6, 0, 0, 1, impulsef(0.5, 0), zerof, zerof, zerof, n)
	best, bi := float32(0), -1
	for i := 1; i < n; i++ {
		if a := abs32(L[i]); a > best {
			best, bi = a, i
		}
	}
	if bi < D-1 || bi > D+1 {
		t.Fatalf("첫 에코 위치 %d, want %d±1 (peak %v)", bi, D, best)
	}
	best2, bi2 := float32(0), -1
	for i := 2*D - 48; i <= 2*D+48; i++ {
		if a := abs32(R[i]); a > best2 {
			best2, bi2 = a, i
		}
	}
	if bi2 < 2*D-1 || bi2 > 2*D+1 {
		t.Fatalf("두 번째 에코 위치 %d, want %d±1 (peak %v)", bi2, 2*D, best2)
	}
	if a := abs32(L[2*D]); a > 0.1*best2 {
		t.Fatalf("핑퐁 위반: L[2D]=%v > R 에코의 10%%(%v)", a, 0.1*best2)
	}
}

// 5. 피드백 감쇠·어두워짐 — 연속 에코 비 ≤ 0.7이고, 피드백 LP 때문에 8kHz 버스트의
//    둘째/첫 에코 비가 1kHz보다 작다.
//    FAIL-first: 피드백 1.4배 / LP 우회 — 두 경우 모두 실패 확인.
func TestFxDelayFeedbackLP(t *testing.T) {
	const D = 33228
	ratio := func(hz float64) float64 {
		burst := func(i int) float32 {
			if i < 480 {
				return 0.4 * float32(math.Sin(2*math.Pi*hz*float64(i)/48000))
			}
			return 0
		}
		L, R := fxRun(0.6, 0, 0, 1, burst, zerof, zerof, zerof, 2*D+300)
		a1, a2 := 0.0, 0.0
		for i := D - 240; i <= D+240; i++ {
			if a := float64(abs32(L[i])); a > a1 {
				a1 = a
			}
		}
		for i := 2*D - 240; i <= 2*D+240; i++ {
			if a := float64(abs32(R[i])); a > a2 {
				a2 = a
			}
		}
		return a2 / a1
	}
	r1k, r8k := ratio(1000), ratio(8000)
	if r1k > 0.7 {
		t.Fatalf("연속 에코 진폭 비 %.3f > 0.7", r1k)
	}
	// 여유 마진 요구: LP 이론 감쇠 |H(8k)|/|H(1k)| ≈ 0.48이라 0.7로 잡는다.
	// (순수 크기 비교 "<"만으로는 우회 변이가 소수점 노이즈로 통과했다 — m09 실측.)
	if r8k >= 0.7*r1k {
		t.Fatalf("피드백 LP 미작동: 8kHz 비 %.4f ≥ 1kHz 비 %.4f의 70%%(%.4f)", r8k, r1k, 0.7*r1k)
	}
}

// 6. Delay 0 완전 바이패스 — 버퍼에 잔여 에코가 있어도 q=0이면 출력에 에코 없음.
//    FAIL-first: 믹스에 상수 누출(+0.01)을 넣으면 실패함을 확인했다.
func TestFxDelayBypass(t *testing.T) {
	f := new(fxChain)
	f.init()
	f.setTempo(5538)
	f.setParam(0, 0.6)
	for i := 0; i < 60000; i++ { // 버퍼를 먼저 채운다
		f.process(0.3, 0, 0, 0)
	}
	f.setParam(0, 0)
	n := 2*f.delaySamp + 100
	L, R := make([]float32, n), make([]float32, n)
	for i := 0; i < n; i++ {
		var bass float32
		if i == 0 {
			bass = 0.5
		}
		L[i], R[i] = f.process(bass, 0, 0, 0)
	}
	for i := 1; i < n; i++ {
		if abs32(L[i]) > 1e-6 || abs32(R[i]) > 1e-6 {
			t.Fatalf("바이패스에서 에코 누출: 샘플 %d L=%v R=%v", i, L[i], R[i])
		}
	}
}

// 7. 마스터 선형 — Master 0.5 출력이 Master 1의 0.5배 ±1%(미포화 입력 0.1).
//    FAIL-first: 마스터 곱을 지우면 비가 1.0이 되어 실패함을 확인했다.
func TestFxMasterLinear(t *testing.T) {
	y1, _ := fxRun(0, 0, 0, 1, constf(0.1), zerof, zerof, zerof, 100)
	yh, _ := fxRun(0, 0, 0, 0.5, constf(0.1), zerof, zerof, zerof, 100)
	ratio := float64(yh[50]) / float64(y1[50])
	if math.Abs(ratio-0.5) > 0.005 {
		t.Fatalf("Master 0.5 비 %.4f, want 0.5±0.005", ratio)
	}
}

// 8. 상한·유한 — 극단 상수 2초 + 파라미터 81조합 × 임펄스·상수 입력으로 |out| ≤ 1.0, NaN/Inf 0.
//    FAIL-first: 최종 스케일 0.82→1.3 / 출력에 1/(master−1) 나눗셈(Inf 유도) — 두 경우 모두 실패 확인.
func TestFxBounds(t *testing.T) {
	check := func(tag string, L, R []float32) {
		t.Helper()
		for i := range L {
			for _, v := range [2]float32{L[i], R[i]} {
				if v != v || v > 1e30 || v < -1e30 {
					t.Fatalf("%s: 유한 아님 — 샘플 %d = %v", tag, i, v)
				}
				if v > 1.0 || v < -1.0 {
					t.Fatalf("%s: |%v| > 1.0 — 샘플 %d", tag, v, i)
				}
			}
		}
	}
	// 상한: 최대 입력 + 전 이펙트 최대, 2초
	L, R := fxRun(1, 1, 1, 1, constf(8), constf(8), impulseTrain(1.0, 4800, 96000), constf(1), 96000)
	check("상한", L, R)
	// 81조합 × 임펄스·상수
	for _, dl := range [...]float32{0, 0.5, 1} {
		for _, dr := range [...]float32{0, 0.5, 1} {
			for _, cp := range [...]float32{0, 0.5, 1} {
				for _, ma := range [...]float32{0, 0.5, 1} {
					imp := impulsef(0.8, 0, 20000)
					L, R := fxRun(dl, dr, cp, ma, imp, impulsef(-0.8, 0, 20000), imp, constf(1), 24000)
					check("81조합 임펄스", L, R)
					L, R = fxRun(dl, dr, cp, ma, constf(0.5), constf(0.3), impulseTrain(1.0, 12000, 24000), constf(1), 24000)
					check("81조합 상수", L, R)
				}
			}
		}
	}
}

// 9. setTempo 클램프 — 1e9→47999, 0/NaN/음수→1, 5538→33228.
//    FAIL-first: 상한 클램프 제거 시 6000000000이 나와 실패함을 확인했다.
func TestFxSetTempoClamp(t *testing.T) {
	f := new(fxChain)
	f.init()
	f.setTempo(1e9)
	if f.delaySamp != delayBufLen-1 {
		t.Fatalf("setTempo(1e9) → %d, want %d", f.delaySamp, delayBufLen-1)
	}
	for _, v := range [...]float64{0, math.NaN(), -5} {
		f.setTempo(v)
		if f.delaySamp != 1 {
			t.Fatalf("setTempo(%v) → %d, want 1", v, f.delaySamp)
		}
	}
	f.setTempo(5538)
	if f.delaySamp != 33228 {
		t.Fatalf("setTempo(5538) → %d, want 33228", f.delaySamp)
	}
}

// 10. 무할당·결정론 — process(딜레이 경로 포함)/setParam/setTempo AllocsPerRun==0,
//     같은 이력의 두 인스턴스는 바이트 동일(wpos 랩 포함 70000샘플).
//     FAIL-first: process에 슬라이스 append / 패키지 카운터 누적 — 두 경우 모두 실패 확인.
func TestFxNoAllocDeterminism(t *testing.T) {
	f := new(fxChain)
	f.init()
	f.setTempo(5538)
	f.setParam(0, 0.5)
	f.setParam(1, 0.3)
	f.process(0.2, 0.1, 0, 0)
	if n := testing.AllocsPerRun(1000, func() { f.process(0.2, 0.1, 0.4, 0.5) }); n != 0 {
		t.Fatalf("process 할당: %v (무할당 계약 위반)", n)
	}
	if n := testing.AllocsPerRun(1000, func() { f.setParam(1, 0.4) }); n != 0 {
		t.Fatalf("setParam 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { f.setTempo(6000) }); n != 0 {
		t.Fatalf("setTempo 할당: %v", n)
	}
	run := func() ([]float32, []float32) {
		g := new(fxChain)
		g.init()
		g.setTempo(5538)
		g.setParam(0, 0.6)
		g.setParam(1, 0.2)
		g.setParam(2, 0.8)
		g.setParam(3, 0.9)
		L, R := make([]float32, 70000), make([]float32, 70000)
		for i := range L {
			var bd, dd float32
			if i == 1000 {
				bd = 1
			}
			if i > 40000 {
				dd = 1
			}
			var b float32
			if i < 480 {
				b = 0.5 * float32(math.Sin(2*math.Pi*float64(i)*220/48000))
			}
			L[i], R[i] = g.process(b, 0.2, bd, dd)
		}
		return L, R
	}
	a1, a2 := run()
	b1, b2 := run()
	for i := range a1 {
		if a1[i] != b1[i] || a2[i] != b2[i] {
			t.Fatalf("결정론 위반: 샘플 %d %v/%v vs %v/%v", i, a1[i], a2[i], b1[i], b2[i])
		}
	}
}
