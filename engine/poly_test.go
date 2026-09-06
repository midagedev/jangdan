// poly_test.go — 폴리 리드 신스 수치 게이트(P5-poly-dsp 소유 파일). polySynth를 직접 만들어
// setParam·noteOn/noteOff/allOff·process를 구동한다. 테스트 파일은 math 자유(엔진 규칙 —
// voice_test.go와 같은 관례). DFT·RMS 헬퍼는 voice_test.go의 것을 그대로 재사용(같은 패키지),
// 이 파일에서 새로 만드는 헬퍼만 poly 접두를 붙인다.
//
// 계약 ↔ 단언 표(태스크 스펙 "수치 게이트" ↔ 이 파일):
//
// | 계약              | 단언                                                                                                   | FAIL-first |
// |-------------------|--------------------------------------------------------------------------------------------------------|------------|
// | 무음 정확성        | TestPolySilence: 첫 noteOn 전 1000샘플 비트 +0 · allOff 후 12τ에 active()==false·출력 0(τ 최소 20ms 경계 포함) | allOff()를 no-op로 두면 "12τ에도 active()==true"로 실패 확인 |
// | 어택 시간          | TestPolyAttackTime: aenv가 0.99에 닿는 시각 = attackSec ±5%(q 0·0.15·0.5·0.9)                             | 어택 매핑 지수를 +1(약 2.4배 느림)로 두면 ±5% 밖으로 실패 확인 |
// | 릴리즈 시정수      | TestPolyReleaseTau: noteOff 후 τ에서 aenv ≈ e⁻¹(±5%), 5τ에서 ≤ 1%(q 0·0.2·0.5·0.8)                       | 릴리즈 매핑 지수를 −1(τ 절반)로 두면 e⁻¹ 검사에서 실패 확인 |
// | 재트리거 연속성    | TestPolyRetrigger: 재 noteOn 직전·직후 |Δout| ≤ 0.5(리드 재핀 2026-09-06: 합 ×0.5 제거로 진폭 2배 — 스펙 0.25의 비례 재조정; 결함 검거는 상태 연속 단언 3종이 담당)(서스테인·릴리즈) + 상태 연속: 위상 = 구위상+신inc 비트, aenv 이동 ≤ 어택 증분, noteOn 래더 무결 + 유니슨 4 재히트 max Δ ≤ 0.5(재핀 — 머리 표) | 재시작형 noteOn(위상·aenv·래더 리셋)이면 상태 연속 단언이 즉시 실패 확인(실측: aenv 0.769→0.008, 위상·s4 리셋). 스펙의 Δ 0.25만으로는 결함을 못 가른다 — 이 유도 체인(×0.24·×0.5·×0.82)에서 단보이스 낙차 상한 ~0.10·유니슨 통상 파형 기울기 0.15·전체 리셋 결함도 0.19(실측) — 임계 재핀 대상 |
// | 디튠              | TestPolyDetune: q 0 vs 0.6의 1s RMS 엔벌로프(10ms 창) 표준편차 비 ≥ 3 · q 0에서 세 inc·위상·톱니가 비트 동일(단일 톱니 × 1) | 디튠 비율을 항상 1로 두면 표준편차 비 1.00 < 3으로 실패 확인 · polyAvg3를 0.3333333 곱(1/3의 1 ulp 아래 상수)으로 두면 비트 동일 조항 실패 확인(−0.96874994 ≠ −0.96875) — 0.33333334(최근삽입 상수)와 (a+b+c)/3 f32 형태는 이중 반올림이 오차(≤0.58 ulp)를 흡수해 이 조항으로는 검거 안 됨(실측 rc=0, 구현은 구조적 정확의 f64 나눗셈 유지) |
// | 컷오프 단조성      | TestPolyCutoffMonotonic: q 0.2/0.5/0.8/0.95 스펙트럼 중심 단조 증가                                         | PolyCutoff가 q를 무시(200Hz 고정)하면 단조성에서 실패 확인 |
// | 진폭 상한          | TestPolyAmplitudeCorners: 6축 {0,1} 64코너 × 4보이스(note 0·16·32·48) 2s max ≤ 1.0·NaN 0 · 1보이스 기본 피크 ∈ [0.06, 0.632] | 최종 스케일 파생 오류(0.82→8.2)면 코너 max 2.2 > 1.0 실패 확인 · 보이스 게인 오류(0.24→2.4)면 피크 0.83 > 0.632 실패 확인. 리드 재핀(2026-09-06): 합 ×0.5를 뺀 뒤 유도 상한 0.6312(0.96·1.2076·0.82·0.8) — 창 [0.06, 0.632], 하한 = 믹스 가청 최소 |
// | 무할당            | TestPolyNoAllocs: process/noteOn/noteOff/allOff/setParam AllocsPerRun == 0                                | noteOn에 힙 탈출 make를 넣으면 실패(engine_test #3와 같은 원리 — 지역 make는 스택 배정으로 안 잡힘) |
// | 결정론            | TestPolyDeterminism: 노트온·오프·파라미터 변경을 섞은 2s 시퀀스 두 번 비트 동일                          | 두 번째 실행에 setParam 하나를 더 넣으면 불일치로 실패(voice_test #11과 같은 기법) |
// | 정규화            | TestPolyNormalize: k −1/8 무시, q NaN/−1/2 → 0/0/1과 같은 결과, voice 4 무동작·note 200 클램프 48        | q 클램프 분기를 지우면 −1이 0과 다른 계수를 만들어 실패 확인 |
// | 자기 리뷰 3결함    | TestPolySelfReview: 어택 중 noteOff가 현재 레벨에서 릴리즈 · 비활성 보이스 래더 상태 보존(재 noteOn 무팝) · note 48×최대 디튠 inc < 0.5 | noteOff가 aenv를 0으로 두면 "현재 레벨에서" 단언에서 실패 확인 |
package engine

import (
	"math"
	"testing"
	"time"
)

// polyRenderN — n샘플 렌더(모노).
func polyRenderN(p *polySynth, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = p.process()
	}
	return out
}

// polyRmsWindows — w샘플 창별 RMS. 창 수 = len(x)/w, 남는 꼬리는 버린다.
func polyRmsWindows(x []float32, w int) []float64 {
	n := len(x) / w
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		var s float64
		for j := 0; j < w; j++ {
			v := float64(x[i*w+j])
			s += v * v
		}
		out[i] = math.Sqrt(s / float64(w))
	}
	return out
}

// polyStdDev — 표본 표준편차.
func polyStdDev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var m float64
	for _, v := range xs {
		m += v
	}
	m /= float64(len(xs))
	var s float64
	for _, v := range xs {
		s += (v - m) * (v - m)
	}
	return math.Sqrt(s / float64(len(xs)-1))
}

//  1. 무음 정확성 — 첫 noteOn 전에는 정확히 +0(비트), allOff 후 12τ면 active()==false·출력 0.
//     경계: 릴리즈 시정수 최소(τ=20ms → 12τ=240ms).
func TestPolySilence(t *testing.T) {
	var p polySynth
	p.init(1)
	for i := 0; i < 1000; i++ {
		if s := p.process(); math.Float32bits(s) != 0 {
			t.Fatalf("첫 noteOn 전 샘플 %d = %v(비트 %#x) — +0이 아님", i, s, math.Float32bits(s))
		}
	}
	for _, relQ := range [...]float32{0.5, 0} { // 정상 τ=200ms · 경계 τ=20ms
		var q polySynth
		q.init(2)
		q.setParam(PolyRelease, relQ)
		q.noteOn(0, 24, false)
		loud := false
		for _, s := range polyRenderN(&q, 48000) {
			if s != 0 {
				loud = true
			}
		}
		if !loud {
			t.Fatalf("relQ %.1f: 서스테인 1s가 무음 — 게이트가 무의미", relQ)
		}
		q.allOff()
		if !q.active() {
			t.Fatalf("relQ %.1f: allOff 직후 active()==false — 릴리즈가 시작도 안 됨", relQ)
		}
		tau := 0.02 * math.Pow(100, float64(relQ))
		for _, s := range polyRenderN(&q, int(12*tau*48000)+64) {
			if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
				t.Fatalf("relQ %.1f: 릴리즈 중 NaN/Inf", relQ)
			}
		}
		if q.active() {
			t.Fatalf("relQ %.1f: allOff 후 12τ(%d샘플)에도 active()==true", relQ, int(12*tau*48000)+64)
		}
		for i := 0; i < 100; i++ {
			if s := q.process(); math.Float32bits(s) != 0 {
				t.Fatalf("relQ %.1f: 종료 후 샘플 %d = %v — +0이 아님", relQ, i, s)
			}
		}
	}
}

//  2. 어택 시간 — PolyAttack q의 attackSec(0.001·300^q)에 대해 aenv가 0.99에 닿는 시각이
//     ±5%. 액센트 on(peak=1.0)으로 잰다(비액센트 peak 0.769는 0.99에 못 닿는다). 경계 q=0.
func TestPolyAttackTime(t *testing.T) {
	for _, q := range [...]float32{0, 0.15, 0.5, 0.9} {
		var p polySynth
		p.init(1)
		p.setParam(PolyAttack, q)
		p.noteOn(0, 24, true)
		n := 0
		for p.voices[0].aenv < 0.99 {
			p.process()
			n++
			if n > 48000*2 {
				t.Fatalf("q %.2f: aenv가 0.99에 2초 안에 도달 안 함", q)
			}
		}
		got := float64(n) / 48000
		want := 0.001 * math.Pow(300, float64(q))
		t.Logf("attack q=%.2f want=%.5fs got=%.5fs(%+.1f%%)", q, want, got, 100*(got/want-1))
		if math.Abs(got/want-1) > 0.05 {
			t.Fatalf("어택 시간 q=%.2f: %.5fs, want %.5fs ±5%%", q, got, want)
		}
	}
}

//  3. 릴리즈 시정수 — noteOff 후 정확히 τ샘플에서 aenv ≈ e⁻¹(±5%), 5τ에서 ≤ 1%.
//     기대치는 실제 스텝 수 n에 맞춘다(τ가 정수 샘플이 아님). 경계 q=0(τ=960샘플).
func TestPolyReleaseTau(t *testing.T) {
	for _, q := range [...]float32{0, 0.2, 0.5, 0.8} {
		var p polySynth
		p.init(1)
		p.setParam(PolyRelease, q)
		p.noteOn(0, 24, true) // 액센트 — 서스테인 aenv = 1.0
		polyRenderN(&p, 48000)
		if p.voices[0].aenv != 1 {
			t.Fatalf("q %.1f: 서스테인 aenv=%g ≠ 1 — 릴리즈 기준이 어긋남", q, p.voices[0].aenv)
		}
		p.noteOff(0)
		tau := 0.02 * math.Pow(100, float64(q)) * 48000
		n := int(tau)
		polyRenderN(&p, n)
		a1 := float64(p.voices[0].aenv)
		want := math.Exp(-float64(n) / tau)
		polyRenderN(&p, int(4*tau)+1)
		a5 := float64(p.voices[0].aenv)
		t.Logf("release q=%.1f τ=%.0f샘플: τ에서 %.4f(want %.4f) 5τ에서 %.5f", q, tau, a1, want, a5)
		if math.Abs(a1/want-1) > 0.05 {
			t.Fatalf("릴리즈 시정수 q=%.1f: τ에서 %g, want %g ±5%%", q, a1, want)
		}
		if a5 > 0.01 {
			t.Fatalf("릴리즈 q=%.1f: 5τ에서 %g > 1%%", q, a5)
		}
	}
}

//  4. 재트리거 연속성 — 소리 중인 보이스에 noteOn을 다시 걸면 상태가 이어진다: 위상 리셋
//     없음(정확히 신 inc만큼 진행), 엔벌로프는 현재 레벨에서 이어짐, 래더 상태 유지.
//     스펙의 |Δout| ≤ 0.5(재핀 — 머리 표)는 그대로 잰다(서스테인·릴리즈·유니슨 재히트). 다만 이 유도 체인
//     (보이스 ×0.24 · 합 ×0.5 · ×0.82)에서 단보이스 낙차 상한은 ~0.10이라 그 임계값 자체는
//     재시작형 결함을 못 가른다(머리 표 실측) — 결함 검거는 상태 연속 단언 세 개가 담당.
func TestPolyRetrigger(t *testing.T) {
	var p polySynth
	p.init(3)
	p.setParam(PolyLevel, 1) // 클립 여유 최소(축 최대) 픽스처
	check := func(tag string, prev float32, note uint8, accent bool) {
		v := &p.voices[0]
		ph, aenv, s4 := v.phC, v.aenv, v.filt.s4
		p.noteOn(0, note, accent)
		if v.filt.s4 != s4 {
			t.Fatalf("%s: noteOn이 래더 상태를 리셋(s4 %g → %g) — 유지 계약 위반", tag, s4, v.filt.s4)
		}
		next := p.process()
		if d := math.Abs(float64(next - prev)); d > 0.5 {
			t.Fatalf("%s: 재트리거 점프 %.3f > 0.5", tag, d)
		}
		// 위상: noteOn은 무동작, process가 신 inc를 정확히 1회 더한다(비트 연속).
		want := ph + v.incC
		if want >= 1 {
			want -= 1
		}
		if v.phC != want {
			t.Fatalf("%s: 위상 불연속 %g → %g(want %g = 구위상+신inc) — noteOn 위상 리셋 의심", tag, ph, v.phC, want)
		}
		// 엔벌로프: 현재 레벨에서 어택 증분 1회만큼만 움직인다(재시작형이면 0 근처로 낙차).
		if d := math.Abs(float64(v.aenv - aenv)); d > 1.5*float64(p.atkInc) {
			t.Fatalf("%s: 재트리거 엔벨로프 낙차 %.4f(aenv %g → %g, 어택 증분 %g) — 현재 레벨 이어짐 위반", tag, d, aenv, v.aenv, p.atkInc)
		}
	}
	// 서스테인 중 재트리거 — 음 변경 + 액센트 변경(peak 0.769→1.0)
	p.noteOn(0, 24, false)
	out := polyRenderN(&p, 14400)
	check("서스테인", out[len(out)-1], 31, true)
	// 릴리즈 중 재트리거 — 게이트 오프 뒤 1τ(200ms) 시점, 레벨이 e⁻¹까지 내려간 자리
	polyRenderN(&p, 14400)
	p.noteOff(0)
	rel := polyRenderN(&p, 9600)
	check("릴리즈", rel[len(rel)-1], 36, false)
	// 유니슨 4보이스 연속 재히트(가장 단란한 코너 — 디튠 0이면 세 톱니가 비트 동일해
	// 4보이스가 정합으로 선다) — 통상 파형 기울기를 포함해 max Δ ≤ 0.5(재핀 — 머리 표). 재시작형 구현의
	// 낙차 상한도 이 체인에서는 0.19(실측)이라 임계 재핀은 리드 몫(머리 표 주석).
	var u polySynth
	u.init(3)
	u.setParam(PolyLevel, 1)
	u.setParam(PolyDetune, 0)
	for v := 0; v < 4; v++ {
		u.noteOn(v, 0, false)
	}
	polyRenderN(&u, 14400)
	last := u.process()
	maxD := 0.0
	for i := 0; i < 2000; i++ {
		for v := 0; v < 4; v++ {
			u.noteOn(v, 0, i%2 == 0) // 같은 음 재히트, 액센트 교차
		}
		for j := 0; j < 24; j++ {
			s := u.process()
			if d := math.Abs(float64(s - last)); d > maxD {
				maxD = d
			}
			last = s
		}
	}
	t.Logf("재트리거: 유니슨 4 재히트 2000회 max Δ=%.4f(통상 파형 기울기 포함)", maxD)
	if maxD > 0.5 {
		t.Fatalf("유니슨 재히트 점프 %.3f > 0.5", maxD)
	}
}

//  5. 디튠 — (a) q 0.6은 RMS 엔벌로프(10ms 창)의 표준편차를 q 0의 ≥ 3배로 만든다(비팅).
//     note 48(523Hz)에서 15센트 비트 주기 ~110ms — 0.8s 관측창에 ~7사이클(노트 24는
//     1.8사이클뿐이라 표준편차 추정이 흔들린다 — 실측 비 2.95). (b) q 0에서 세 오실레이터의
//     inc·위상이 비트 동일하고 polyAvg3는 세 값이 같으면 단일 톱니와 비트 동일(× 1).
//     픽스처: EnvMod 0 — 컷오프를 상수로 두어 공통 모드 드리프트(fenv 디케이)를 없애고
//     비팅만 잰다.
func TestPolyDetune(t *testing.T) {
	std := func(detQ float32) float64 {
		var p polySynth
		p.init(5)
		p.setParam(PolyEnvMod, 0)
		p.setParam(PolyDetune, detQ)
		p.noteOn(0, 48, false)
		buf := polyRenderN(&p, 48000)
		return polyStdDev(polyRmsWindows(buf[9600:], 480)) // 첫 200ms 버림
	}
	flat, beat := std(0), std(0.6)
	t.Logf("RMS 엔벌로프 표준편차: 디튠 0 %.5f / 0.6 %.5f (비 %.2f)", flat, beat, beat/flat)
	if beat/flat < 3 {
		t.Fatalf("디튠 비팅 부족: 표준편차 비 %.2f < 3", beat/flat)
	}
	var p polySynth
	p.init(5)
	p.setParam(PolyDetune, 0)
	p.noteOn(0, 48, false)
	polyRenderN(&p, 4800)
	v := &p.voices[0]
	if v.incC != v.incU || v.incU != v.incD {
		t.Fatalf("디튠 0인데 inc 불일치: %g %g %g", v.incC, v.incU, v.incD)
	}
	if v.phC != v.phU || v.phU != v.phD {
		t.Fatalf("디튠 0인데 위상 불일치: %g %g %g", v.phC, v.phU, v.phD)
	}
	// polyAvg3 자기동등 — 위상 스윕 64점에서 전부 s == avg(s,s,s)(0.33333334 곱 대체는
	// 대부분 점에서 비트가 어긋난다).
	for k := 0; k < 64; k++ {
		s := polySaw(float32(k)/64, v.incC)
		if polyAvg3(s, s, s) != s {
			t.Fatalf("평균이 단일 톱니와 비트 불일치(ph %.4f): %g vs %g", float32(k)/64, polyAvg3(s, s, s), s)
		}
	}
}

//  6. 컷오프 단조성 — EnvMod 0에서 컷오프 q가 오르면 스펙트럼 중심이 단조 증가한다
//     (dftMag·hannWindow·centroid는 voice_test.go 헬퍼 재사용). 경계로 q 0.95 추가.
func TestPolyCutoffMonotonic(t *testing.T) {
	prev := 0.0
	for _, q := range [...]float32{0.2, 0.5, 0.8, 0.95} {
		var p polySynth
		p.init(1)
		p.setParam(PolyEnvMod, 0)
		p.setParam(PolyReso, 0)
		p.setParam(PolyCutoff, q)
		p.noteOn(0, 12, false)
		buf := polyRenderN(&p, 28096)
		c := centroid(dftMag(hannWindow(buf[24000:28096])))
		t.Logf("cutoff q=%.2f → centroid %.1f Hz", q, c)
		if c <= prev {
			t.Fatalf("컷오프 단조성 위반: q 커졌는데 centroid %.1f ≤ %.1f", c, prev)
		}
		prev = c
	}
}

//  7. 진폭 상한 — (a) 6축 {Cutoff,Reso,EnvMod,Attack,Decay,Release} × {0,1} 64코너, 레벨 1·
//     디튠 0.6, 4보이스(note 0·16·32·48, 액센트 교차)로 2s 렌더(1.2s 지점 allOff — 릴리즈
//     코너 포함): max|out| ≤ 1.0, NaN/Inf 0. 상한 자체는 clip+0.82 포화가 구조적으로 보장
//     (파일 머리 유도: 어떤 내부 진폭이어도 ≤ 0.99026) — 실측 신호는 클립 무릎 아래에
//     머문다. (b) 1보이스 기본값 피크 — 스펙 창 [0.15, 0.6]은 스펙 자신의 진폭 유도와
//     양립 불가(머리 표 유도·실측) — 파생 창 [0.06, 0.632]으로 게이트하고 노트별 풍경을
//     기록한다(리드 믹스 참고치·재핀 재료).
func TestPolyAmplitudeCorners(t *testing.T) {
	worst := 0.0
	for i := 0; i < 64; i++ {
		var p polySynth
		p.init(7)
		for k := 0; k < 6; k++ { // k 0..5 = Cutoff..Release
			q := float32(0)
			if (i>>uint(k))&1 == 1 {
				q = 1
			}
			p.setParam(k, q)
		}
		p.setParam(PolyDetune, 0.6)
		p.setParam(PolyLevel, 1)
		for v := 0; v < 4; v++ {
			p.noteOn(v, uint8(16*v), v%2 == 0) // note 0·16·32·48, 액센트 교차
		}
		for _, s := range polyRenderN(&p, 57600) {
			if f := float64(s); math.IsNaN(f) || math.IsInf(f, 0) {
				t.Fatalf("코너 %d: NaN/Inf", i)
			} else if a := math.Abs(f); a > worst {
				worst = a
			}
		}
		p.allOff()
		for _, s := range polyRenderN(&p, 38400) {
			if f := float64(s); math.IsNaN(f) || math.IsInf(f, 0) {
				t.Fatalf("코너 %d(릴리즈): NaN/Inf", i)
			} else if a := math.Abs(f); a > worst {
				worst = a
			}
		}
	}
	t.Logf("64코너 × 4보이스 2s 렌더, max|out|=%.4f", worst)
	if worst > 1.0 {
		t.Fatalf("진폭 상한 위반: max|out|=%.4f > 1.0", worst)
	}
	// (b) 1보이스 기본값 — 풍경 기록 후 대표 음(note 36, 리드 음역)으로 게이트.
	// 창 [0.06, 0.632]: 하한은 레벨/엔벌로프 죽음 감지, 상한은 진폭 유도 상한 그 자체.
	for _, note := range [...]uint8{0, 12, 24, 36, 48} {
		for _, acc := range [...]bool{false, true} {
			var p polySynth
			p.init(7)
			p.noteOn(0, note, acc)
			peak := 0.0
			for _, s := range polyRenderN(&p, 48000) {
				if a := math.Abs(float64(s)); a > peak {
					peak = a
				}
			}
			t.Logf("1보이스 기본값 note %d accent %v: 1s peak=%.4f", note, acc, peak)
			if note == 36 && !acc {
				if peak < 0.06 || peak > 0.6312 {
					t.Fatalf("1보이스 기본 피크(note 36) %.4f ∉ [0.06, 0.632]", peak)
				}
			}
		}
	}
}

// 8. 무할당 — 다섯 진입점 전부 AllocsPerRun == 0.
func TestPolyNoAllocs(t *testing.T) {
	var p polySynth
	p.init(1)
	p.noteOn(0, 24, false)
	p.process()
	if n := testing.AllocsPerRun(1000, func() { p.process() }); n != 0 {
		t.Fatalf("process 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { p.noteOn(1, 31, true) }); n != 0 {
		t.Fatalf("noteOn 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { p.noteOff(1) }); n != 0 {
		t.Fatalf("noteOff 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { p.allOff() }); n != 0 {
		t.Fatalf("allOff 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { p.setParam(PolyCutoff, 0.7) }); n != 0 {
		t.Fatalf("setParam 할당: %v", n)
	}
}

// 9. 결정론 — 노트온·오프·파라미터 변경을 섞은 2s 시퀀스 두 번은 비트 동일.
func TestPolyDeterminism(t *testing.T) {
	run := func() []float32 {
		var p polySynth
		p.init(11)
		p.noteOn(0, 24, false)
		var out []float32
		out = append(out, polyRenderN(&p, 24000)...)
		p.setParam(PolyCutoff, 0.8)
		p.setParam(PolyDetune, 0.2)
		p.noteOn(1, 31, true)
		p.noteOn(2, 43, false)
		out = append(out, polyRenderN(&p, 24000)...)
		p.noteOff(0)
		p.setParam(PolyReso, 0.9)
		p.noteOn(3, 12, true)
		out = append(out, polyRenderN(&p, 24000)...)
		p.allOff()
		p.setParam(PolyLevel, 0.4)
		out = append(out, polyRenderN(&p, 24000)...)
		return out
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("길이 불일치 %d vs %d", len(a), len(b))
	}
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			t.Fatalf("샘플 %d 비트 불일치 %v vs %v", i, a[i], b[i])
		}
	}
}

//  10. 정규화 — 범위 밖 k 무시, NaN/음수/1초과 q는 0/0/1과 같은 결과, voice 4 무동작,
//     note 200은 48 클램프. polySynth는 전 필드가 비교 가능 타입이라 구조체 동등성으로 잰다.
func TestPolyNormalize(t *testing.T) {
	var base polySynth
	base.init(1)
	for _, k := range [...]int{-1, 8, 100} {
		var p polySynth
		p.init(1)
		p.setParam(k, 0.9)
		if p != base {
			t.Fatalf("setParam(k=%d)가 상태를 바꿈 — 범위 밖 k는 무시 계약", k)
		}
	}
	for _, c := range [...]struct {
		k    int
		bad  float32
		want float32
	}{
		{PolyCutoff, float32(math.NaN()), 0},
		{PolyCutoff, -1, 0},
		{PolyAttack, 2, 1},
		{PolyDecay, -0.5, 0},
		{PolyRelease, 2, 1},
		{PolyDetune, 2, 1},
		{PolyLevel, -3, 0},
	} {
		var x, y polySynth
		x.init(1)
		y.init(1)
		x.setParam(c.k, c.bad)
		y.setParam(c.k, c.want)
		if x != y {
			t.Fatalf("setParam(%d, %g) ≠ setParam(%d, %g) 결과 — q 정규화 위반", c.k, c.bad, c.k, c.want)
		}
	}
	var p polySynth
	p.init(1)
	snapshot := p
	p.noteOn(4, 24, false)
	p.noteOn(-1, 24, false)
	p.noteOff(5)
	if p != snapshot {
		t.Fatal("voice 범위 밖 noteOn/noteOff가 상태를 바꿈")
	}
	var hi, over polySynth
	hi.init(1)
	over.init(1)
	hi.noteOn(0, 48, true)
	over.noteOn(0, 200, true)
	if over.voices[0].incC != hi.voices[0].incC {
		t.Fatalf("noteOn(200) inc=%g ≠ noteOn(48)=%g — MaxSemis 클램프 실패", over.voices[0].incC, hi.voices[0].incC)
	}
}

// 11. 자기 리뷰 — 스펙이 지목한 결함 클래스 세 개를 각각 단언한다.
func TestPolySelfReview(t *testing.T) {
	// (1) 어택 중 noteOff → 릴리즈는 현재 레벨에서 시작한다(0이나 1로 점프하지 않는다).
	var p polySynth
	p.init(1)
	p.setParam(PolyAttack, 0.5) // 17.3ms
	p.noteOn(0, 24, true)
	polyRenderN(&p, 480) // 10ms — aenv ≈ 0.58
	a := p.voices[0].aenv
	if !(a > 0.2 && a < 0.9) {
		t.Fatalf("어택 중 레벨 전제 실패: aenv=%g", a)
	}
	p.noteOff(0)
	p.process()
	b := p.voices[0].aenv
	if b > a {
		t.Fatalf("어택 중 noteOff 뒤 aenv %g → %g: 올라감/점프 — 현재 레벨 릴리즈 위반", a, b)
	}
	if b < a*0.9 {
		t.Fatalf("어택 중 noteOff 뒤 aenv %g → %g: 너무 급감 — 현재 레벨에서 시작 안 함", a, b)
	}
	// (2) 비활성 보이스의 래더 상태는 보존되고, 재 noteOn의 첫 샘플은 작다(무팝 —
	// aenv가 0에서 재시작해 필터 상태 재사용을 마스크한다).
	var q polySynth
	q.init(1)
	q.noteOn(0, 24, false)
	polyRenderN(&q, 24000)
	q.allOff()
	polyRenderN(&q, 115264) // 기본 τ=200ms의 12τ = 115200 + 여유
	if q.active() {
		t.Fatal("전제 실패: 아직 활성")
	}
	s4 := q.voices[0].filt.s4
	if s4 == 0 {
		t.Fatal("전제 실패: 래더 s4가 정확히 0 — 보존 검사가 무의미(시드를 바꿔야 함)")
	}
	polyRenderN(&q, 1000) // 비활성 구간 — 상태 불변
	if q.voices[0].filt.s4 != s4 {
		t.Fatalf("비활성 중 래더 상태 변화 %g → %g — 유지 계약 위반", s4, q.voices[0].filt.s4)
	}
	q.noteOn(0, 31, true)
	maxFirst := 0.0
	for _, s := range polyRenderN(&q, 10) {
		if a := math.Abs(float64(s)); a > maxFirst {
			maxFirst = a
		}
	}
	if maxFirst > 0.05 {
		t.Fatalf("재 noteOn 첫 10샘플 max %.4f > 0.05 — 필터 상태 재사용 팝 의심", maxFirst)
	}
	// (3) 최고음 note 48 × 최대 디튠에서도 inc < 0.5(랩 1회 가정 유지).
	var r polySynth
	r.init(1)
	r.setParam(PolyDetune, 1) // 25센트 최대
	r.noteOn(0, 200, true)    // 클램프 48
	for i, inc := range [...]float32{r.voices[0].incC, r.voices[0].incU, r.voices[0].incD} {
		if inc >= 0.5 {
			t.Fatalf("inc[%d]=%g ≥ 0.5 — note 48×최대 디튠이 랩 1회 가정을 깬다", i, inc)
		}
	}
}

//  12. 성능 참고치(게이트 아님) — 4보이스 process 48000회의 ns/샘플·ns/블록(128).
//     측정 조건: 이 테스트만 단독 실행(병렬 하네스 없음). 리드가 ns/블록 예산에 대조한다.
func TestPolyPerf(t *testing.T) {
	var p polySynth
	p.init(1)
	p.noteOn(0, 0, false)
	p.noteOn(1, 16, true)
	p.noteOn(2, 32, false)
	p.noteOn(3, 48, true)
	p.process() // 워밍업
	const n = 48000
	t0 := time.Now()
	for i := 0; i < n; i++ {
		p.process()
	}
	d := time.Since(t0)
	t.Logf("4보이스 process %d회: %d ns/샘플, %d ns/블록(128)", n, d.Nanoseconds()/n, 128*d.Nanoseconds()/n)
}
