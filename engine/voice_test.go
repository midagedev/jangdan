// voice_test.go — 베이스라인 보이스 수치 게이트(T1a 소유 파일). bassVoice를 직접 만들어
// setParam·noteOn/noteOff·process를 구동한다. 테스트 파일은 math 자유(엔진 규칙 — 게이트
// 대상은 비테스트 코드). 측정은 단순 DFT(4096점, Hann)·보간 영교차·RMS로 한다.
//
// 계약 ↔ 단언 표(태스크 스펙 "수치 게이트" ↔ 이 파일):
//
// | 계약              | 단언                                          | FAIL-first |
// |-------------------|-----------------------------------------------|------------|
// | 컷오프 단조성      | TestVoiceCutoffMonotonic: q 0.2/0.5/0.8 스펙트럼 중심 단조 증가 | BReso→0.9로 두면 공진이 중심을 뒤흔들어 실패 확인 |
// | 레조넌스 피크      | TestVoiceResonancePeak: ±1/3옥타브 대역 에너지비 ≥ 1.5배 | BReso 매핑 1.6→0.2로 낮추면 1.1배 미만으로 실패 확인 |
// | 자기발진 유계      | TestVoiceSelfOscBounded: noteOff 후 2초 |y| ≤ 1.0, NaN 0 | 필터 상태 클램프 제거 시 발산·NaN으로 실패 |
// | PolyBLEP          | TestVoicePolyBLEPHF: 12kHz↑ 에너지 ≥ 6dB 낮음(나이브 원본) + 매칭 필터 기준 ≥ 2dB | corr 비활성화 0dB·부호 반전 −3.4dB로 매칭 필터 기준 실패 확인(정상 4.1dB) |
// | 사각파 짝수 고조파 | TestVoiceSquareEven: 짝/기수 에너지 ≥ 20dB     | BWave가 사각을 무시(항상 톱니)하면 20dB 미달 실패 |
// | 액센트            | TestVoiceAccent: 첫 20ms RMS ≥ 1.3배          | lv 보너스 0.6→0.1로 낮추면 1.12배로 실패 확인 |
// | 슬라이드          | TestVoiceSlide: 도달 ≤ 1센트·ZCR ±1.5%·중간은 사이 | slideInc 부호 반전 시 도달 검사에서 즉시 실패 |
// | 릴리즈            | TestVoiceRelease: 384샘플 ≤ 40%, 960샘플 ≤ 5% | relDec를 0.9999로 두면 960샘플 90%로 실패 |
// | 진폭 상한         | TestVoiceAmplitudeCorners: 257코너×note 0/36×액센트 max ≤ 1.0, 유한 | 출력 게인 0.24→2.4로 두면 즉시 실패 |
// | 무할당            | TestVoiceNoAllocs: process/noteOn/noteOff/setParam AllocsPerRun == 0 | noteOn에 make(1) 삽입 시 실패(원리는 engine_test #3와 동일) |
// | 결정론            | TestVoiceDeterminism: 같은 시퀀스 두 번 비트 동일 | 시퀀스에 파라미터 변경 하나를 더 넣으면 불일치 실패 |
package engine

import (
	"math"
	"testing"
)

// renderVoiceN — n샘플 렌더(dropOct=0).
func renderVoiceN(v *bassVoice, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = v.process(0)
	}
	return out
}

var twRe, twIm []float64 // DFT 트윌 테이블 캐시(테스트 순차 실행 가정)

// dftMag — n점 실수 DFT 크기 스펙트럼(트윌 캐시, O(n²) — 게이트용으로 충분).
func dftMag(x []float32) []float64 {
	n := len(x)
	if len(twRe) != n {
		twRe = make([]float64, n)
		twIm = make([]float64, n)
		for i := range twRe {
			ph := 2 * math.Pi * float64(i) / float64(n)
			twRe[i] = math.Cos(ph)
			twIm[i] = math.Sin(ph)
		}
	}
	out := make([]float64, n/2)
	for k := 1; k < n/2; k++ {
		var re, im float64
		for i := 0; i < n; i++ {
			j := i * k % n
			re += float64(x[i]) * twRe[j]
			im -= float64(x[i]) * twIm[j]
		}
		out[k] = math.Sqrt(re*re + im*im)
	}
	return out
}

// hannWindow — x 사본에 Hann 창을 적용해 반환.
func hannWindow(x []float32) []float32 {
	y := make([]float32, len(x))
	for i := range y {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(x)-1))
		y[i] = float32(w) * x[i]
	}
	return y
}

// binHz — 스펙트럼 bin k의 주파수.
func binHz(mag []float64, k int) float64 {
	return float64(k) * 48000 / float64(2*len(mag))
}

// centroid — 크기 가중 스펙트럼 중심(Hz).
func centroid(mag []float64) float64 {
	var num, den float64
	for k := 1; k < len(mag); k++ {
		num += binHz(mag, k) * mag[k]
		den += mag[k]
	}
	return num / den
}

// bandE — [f0, f1] 대역 에너지(크기 제곱합).
func bandE(mag []float64, f0, f1 float64) float64 {
	var e float64
	for k := 1; k < len(mag); k++ {
		f := binHz(mag, k)
		if f >= f0 && f <= f1 {
			e += mag[k] * mag[k]
		}
	}
	return e
}

// totalE — 전(양) 대역 에너지.
func totalE(mag []float64) float64 {
	var e float64
	for k := 1; k < len(mag); k++ {
		e += mag[k] * mag[k]
	}
	return e
}

// zeroCrossFreq — 선형 보간 상승 영교차의 평균 주기로 주파수 추정. 교차 < 2면 false.
func zeroCrossFreq(x []float32) (float64, bool) {
	var ts []float64
	for i := 0; i+1 < len(x); i++ {
		if x[i] < 0 && x[i+1] >= 0 {
			t := float64(i) + float64(-x[i])/float64(x[i+1]-x[i])
			ts = append(ts, t)
		}
	}
	if len(ts) < 2 {
		return 0, false
	}
	return 48000 * float64(len(ts)-1) / (ts[len(ts)-1] - ts[0]), true
}

// 1. 컷오프 단조성 — EnvMod=0에서 컷오프 q가 오르면 스펙트럼 중심이 단조 증가한다.
//    정상 상태 관측: 트리거 0.5s 뒤 4096샘플(τ=2s라 진폭은 78%로 살아있다).
func TestVoiceCutoffMonotonic(t *testing.T) {
	prev := 0.0
	for _, q := range [...]float32{0.2, 0.5, 0.8} {
		var v bassVoice
		v.init(1)
		v.setParam(int(BEnvMod), 0)
		v.setParam(int(BReso), 0)
		v.setParam(int(BDecay), 1) // τ = 2s
		v.setParam(int(BCutoff), q)
		v.noteOn(12, false, false, 12, 48000)
		buf := renderVoiceN(&v, 28096)
		c := centroid(dftMag(hannWindow(buf[24000:28096])))
		t.Logf("cutoff q=%.1f → centroid %.1f Hz", q, c)
		if c <= prev {
			t.Fatalf("컷오프 단조성 위반: q 커졌는데 centroid %.1f ≤ %.1f", c, prev)
		}
		prev = c
	}
}

// 2. 레조넌스 피크 — Reso 0.9는 0.1보다 컷오프 ±1/3옥타브 대역 에너지 비율이 ≥ 1.5배.
//    컷오프 q=0.5 → 200·40^0.5 = 1264.9Hz, 대역 [1003.6, 1594.3]Hz.
func TestVoiceResonancePeak(t *testing.T) {
	ratio := func(reso float32) float64 {
		var v bassVoice
		v.init(1)
		v.setParam(int(BEnvMod), 0)
		v.setParam(int(BDecay), 1)
		v.setParam(int(BCutoff), 0.5)
		v.setParam(int(BReso), reso)
		v.noteOn(12, false, false, 12, 48000)
		buf := renderVoiceN(&v, 5096)
		mag := dftMag(hannWindow(buf[1000:5096]))
		return bandE(mag, 1003.6, 1594.3) / totalE(mag)
	}
	lo, hi := ratio(0.1), ratio(0.9)
	t.Logf("band ratio reso 0.1=%.3f 0.9=%.3f ratio=%.2f", lo, hi, hi/lo)
	if hi/lo < 1.5 {
		t.Fatalf("레조넌스 피크 부족: 0.9/0.1 대역비 %.2f < 1.5", hi/lo)
	}
}

// 3. 자기발진 유계 — Reso=1에서 게이트 1회 뒤 noteOff 후 2초 동안 |y| ≤ 1.0, NaN 없음.
func TestVoiceSelfOscBounded(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BCutoff), 0.45)
	v.setParam(int(BEnvMod), 0.5)
	v.setParam(int(BDecay), 0.4)
	v.setParam(int(BReso), 1)
	v.noteOn(12, true, false, 12, 14400)
	pre := renderVoiceN(&v, 14400)
	v.noteOff()
	tail := renderVoiceN(&v, 96000)
	peak := 0.0
	for _, s := range tail {
		if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
			t.Fatal("자기발진 꼬리에 NaN/Inf")
		}
		if a := math.Abs(float64(s)); a > peak {
			peak = a
		}
	}
	headPeak := 0.0
	for _, s := range pre {
		if a := math.Abs(float64(s)); a > headPeak {
			headPeak = a
		}
	}
	t.Logf("게이트 중 peak=%.3f, noteOff 후 2s peak=%.3f", headPeak, peak)
	if peak > 1.0 {
		t.Fatalf("자기발진 발산: noteOff 후 peak %.3f > 1.0", peak)
	}
}

// 4. PolyBLEP — 톱니 note 36(261.63Hz), 컷오프 8kHz, Reso 0에서 12kHz↑ 대역 에너지가
//    테스트에서 직접 만든 나이브 톱니보다 ≥ 6dB 낮다.
func TestVoicePolyBLEPHF(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BEnvMod), 0)
	v.setParam(int(BReso), 0)
	v.setParam(int(BDecay), 1)
	v.setParam(int(BCutoff), 1) // 8000Hz
	v.noteOn(36, false, false, 36, 48000)
	buf := renderVoiceN(&v, 4352)
	poly := hannWindow(buf[256:4352])
	f0 := 440 * math.Pow(2, float64(36+24-69)/12)
	naive := make([]float32, 4096)
	for i := range naive {
		ph := math.Mod(float64(i)*f0/48000, 1)
		naive[i] = float32(2*ph - 1)
	}
	hiEnergy := func(x []float32) float64 {
		mag := dftMag(x)
		return bandE(mag, 12000, 24000)
	}
	en, eb := hiEnergy(hannWindow(naive)), hiEnergy(poly)
	db := 10 * math.Log10(en/eb)
	t.Logf("(스펙 게이트) 12kHz↑ 절대 에너지 naive=%.4g poly=%.4g → %.1f dB 낮음", en, eb, db)
	if db < 6 {
		t.Fatalf("PolyBLEP 효과 부족: 12kHz↑ 에너지 차 %.1f dB < 6 dB", db)
	}
	// 보강: 나이브 톱니를 동일한 4단 원폴(같은 g)로 통과시킨 뒤 대역 에너지 "비율"로
	// 비교해 필터 롤오프의 기여를 상쇄한다. 실측: 정상 4.1dB, corr 비활성화 0dB,
	// corr 부호 반전 −3.4dB — 임계 2dB가 결함을 가른다. 참고: 스펙의 "6dB"는 필터
	// 롤오프가 지배하며(상단 게이트), 이미지 억제 자체는 1/n² 가중 때문에 ~1.5dB 분량이다.
	g := 1 - math.Exp(-2*math.Pi*8000/48000)
	ref := make([]float32, 4096)
	var st [4]float64
	for i := range ref {
		ph := math.Mod(float64(i)*f0/48000, 1)
		x := 2*ph - 1
		for j := 0; j < 4; j++ {
			st[j] += g * (x - st[j])
			x = st[j]
		}
		ref[i] = float32(x)
	}
	frac := func(x []float32) float64 {
		mag := dftMag(x)
		return bandE(mag, 12000, 24000) / totalE(mag)
	}
	refFrac, polyFrac := frac(hannWindow(ref)), frac(poly)
	dbf := 10 * math.Log10(refFrac/polyFrac)
	t.Logf("(보강) 매칭 필터 기준 대역비 naive=%.3g poly=%.3g → %.1f dB", refFrac, polyFrac, dbf)
	if dbf < 2 {
		t.Fatalf("PolyBLEP 효과 부족(매칭 필터 기준): %.1f dB < 2 dB", dbf)
	}
}

// 5. 사각파 — BWave=1에서 짝수 고조파(2f,4f) 에너지가 기수(f,3f)보다 ≥ 20dB 낮다.
//    note 12 = 65.406Hz, 창 4096(bin 11.72Hz) — 각 고조파 ±2bin 적분.
func TestVoiceSquareEven(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BEnvMod), 0)
	v.setParam(int(BReso), 0)
	v.setParam(int(BDecay), 1)
	v.setParam(int(BCutoff), 1)
	v.setParam(int(BWave), 1)
	v.noteOn(12, false, false, 12, 48000)
	buf := renderVoiceN(&v, 4352)
	mag := dftMag(hannWindow(buf[256:4352]))
	f0 := 440 * math.Pow(2, float64(12+24-69)/12)
	harm := func(n int) float64 {
		center := f0 * float64(n) / (48000 / float64(2*len(mag)))
		lo, hi := int(math.Floor(center))-2, int(math.Floor(center))+2
		var e float64
		for k := lo; k <= hi; k++ {
			if k > 0 && k < len(mag) {
				e += mag[k] * mag[k]
			}
		}
		return e
	}
	odd := harm(1) + harm(3)
	even := harm(2) + harm(4)
	db := 10 * math.Log10(odd/even)
	t.Logf("기수=%.4g 짝수=%.4g → %.1f dB", odd, even, db)
	if db < 20 {
		t.Fatalf("사각파 짝수 고조파 억제 부족: %.1f dB < 20 dB", db)
	}
}

// 6. 액센트 — 같은 노트를 accent true/false로 트리거했을 때 첫 20ms(960샘플) RMS 비 ≥ 1.3.
func TestVoiceAccent(t *testing.T) {
	rms := func(accent bool) float64 {
		var v bassVoice
		v.init(1)
		v.setParam(int(BEnvMod), 0)
		v.setParam(int(BDecay), 1)
		v.setParam(int(BReso), 0.3)
		v.setParam(int(BCutoff), 0.6)
		v.setParam(int(BAccent), 1)
		v.noteOn(12, accent, false, 12, 48000)
		buf := renderVoiceN(&v, 960)
		var s float64
		for _, x := range buf {
			s += float64(x) * float64(x)
		}
		return math.Sqrt(s / 960)
	}
	plain, acc := rms(false), rms(true)
	t.Logf("RMS plain=%.4f accent=%.4f ratio=%.2f", plain, acc, acc/plain)
	if acc/plain < 1.3 {
		t.Fatalf("액센트 레벨 효과 부족: %.2f < 1.3", acc/plain)
	}
}

// 7. 슬라이드 — note 12→24, stepSamples=5538(130BPM). 스펙의 "마지막 128샘플 영교차"는
//    130.8Hz에서 교차가 0~1개뿐이라 측정 불가 — 스텝 끝 도달은 (a) 내부 inc0의 센트
//    오차 ≤ 1(계약 그 자체), (b) 스텝 종료 후 같은 피치 유지 구간의 보간 영교차 주파수
//    ±1.5%로 이중 검증한다. 스텝 절반에서는 두 주파수 사이.
func TestVoiceSlide(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BEnvMod), 0)
	v.setParam(int(BReso), 0)
	v.setParam(int(BDecay), 1)
	v.setParam(int(BCutoff), 1) // 파형 유지용 넓은 컷오프
	v.noteOn(12, false, true, 24, 5538)
	half := renderVoiceN(&v, 2769) // 스텝 절반까지
	if !(v.slideOff > 0.05 && v.slideOff < 0.95) {
		t.Fatalf("슬라이드 중간 오프셋 %.3f — 로그 보간이 아님(0.5 옥타브 기대)", v.slideOff)
	}
	rest := renderVoiceN(&v, 5538-2769+4096)
	if v.slideN != 0 {
		t.Fatalf("슬라이드 미종료: slideN=%d", v.slideN)
	}
	exp := 440 * math.Pow(2, float64(24+24-69)/12) / 48000
	cents := 1200 * math.Log2(float64(v.inc0)/exp)
	t.Logf("도달 오차 %.3f센트(inc0=%.9g exp=%.9g)", cents, v.inc0, exp)
	if math.Abs(cents) > 1 {
		t.Fatalf("슬라이드 도달 오차 %.3f센트 > 1센트", cents)
	}
	fMid, ok := zeroCrossFreq(half[len(half)-2048:])
	// rest는 타임라인 2769부터 — 스텝 종료(5538)는 rest 인덱스 2769. 종료 후 창은
	// [6050, 9634) 타임라인, 3584샘플 ≈ 주기 9.8개(130.8Hz).
	fEnd, ok2 := zeroCrossFreq(rest[3281 : 3281+3584])
	if !ok || !ok2 {
		t.Fatalf("영교차 부족 mid=%v end=%v", ok, ok2)
	}
	f12 := 440 * math.Pow(2, float64(12+24-69)/12)
	f24 := 440 * math.Pow(2, float64(24+24-69)/12)
	t.Logf("f12=%.2f fmid=%.2f f24=%.2f fend=%.2f", f12, fMid, f24, fEnd)
	if math.Abs(fEnd/f24-1) > 0.015 {
		t.Fatalf("슬라이드 끝 주파수 %.2fHz — nextNote %.2fHz ±1.5%% 밖", fEnd, f24)
	}
	if !(fMid > f12*1.05 && fMid < f24*0.95) {
		t.Fatalf("슬라이드 중간 주파수 %.2fHz — 두 주파수 사이 아님(%.2f~%.2f)", fMid, f12, f24)
	}
}

// 8. 릴리즈 — noteOff 뒤 384샘플(스펙 게이트 지점) |y| ≤ 직전 피크 40%, 960샘플 ≤ 5%.
func TestVoiceRelease(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BEnvMod), 0)
	v.setParam(int(BReso), 0)
	v.setParam(int(BDecay), 1)
	v.setParam(int(BCutoff), 0.6)
	v.noteOn(12, false, false, 12, 48000)
	pre := renderVoiceN(&v, 14400)
	v.noteOff()
	post := renderVoiceN(&v, 1440)
	pk := 0.0
	for _, s := range pre[len(pre)-384:] {
		if a := math.Abs(float64(s)); a > pk {
			pk = a
		}
	}
	a384 := math.Abs(float64(post[384]))
	a960 := math.Abs(float64(post[960]))
	t.Logf("peak=%.4f 384샘플=%.4f(%.1f%%) 960샘플=%.5f(%.1f%%)", pk, a384, 100*a384/pk, a960, 100*a960/pk)
	if a384 > 0.4*pk {
		t.Fatalf("릴리즈 384샘플 %.1f%% > 40%%", 100*a384/pk)
	}
	if a960 > 0.05*pk {
		t.Fatalf("릴리즈 960샘플 %.1f%% > 5%%", 100*a960/pk)
	}
}

// 9. 진폭 상한 — 8파라미터 {0,1} 코너 256개 + all-0.5, 각 note 0/36 × 액센트 유무로
//    1초 렌더 → max|y| ≤ 1.0, NaN/Inf 0.
func TestVoiceAmplitudeCorners(t *testing.T) {
	worst := 0.0
	combos := 0
	for i := 0; i <= 256; i++ { // 256 코너 + all-0.5
		vals := [8]float32{}
		for k := 0; k < 8; k++ {
			switch {
			case i == 256:
				vals[k] = 0.5
			case (i>>uint(k))&1 == 1:
				vals[k] = 1
			default:
				vals[k] = 0
			}
		}
		for _, note := range [...]uint8{0, 36} {
			for _, acc := range [...]bool{false, true} {
				var v bassVoice
				v.init(1)
				for k := 0; k < 8; k++ {
					v.setParam(k, vals[k])
				}
				v.noteOn(note, acc, false, note, 48000)
				for _, s := range renderVoiceN(&v, 48000) {
					f := float64(s)
					if math.IsNaN(f) || math.IsInf(f, 0) {
						t.Fatalf("코너 %d note %d acc %v: NaN/Inf", i, note, acc)
					}
					if a := math.Abs(f); a > worst {
						worst = a
					}
				}
				combos++
			}
		}
	}
	t.Logf("%d 조합 렌더, max|y|=%.4f", combos, worst)
	if worst > 1.0 {
		t.Fatalf("진폭 상한 위반: max|y|=%.4f > 1.0", worst)
	}
}

// 10. 무할당 — process/noteOn/noteOff/setParam 모두 AllocsPerRun == 0.
func TestVoiceNoAllocs(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BCutoff), 0.5)
	v.noteOn(12, false, true, 24, 5538)
	v.process(0)
	if n := testing.AllocsPerRun(1000, func() { v.process(0.5) }); n != 0 {
		t.Fatalf("process 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { v.noteOn(24, true, true, 12, 5538) }); n != 0 {
		t.Fatalf("noteOn 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { v.noteOff() }); n != 0 {
		t.Fatalf("noteOff 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { v.setParam(int(BDecay), 0.7) }); n != 0 {
		t.Fatalf("setParam 할당: %v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { v.noteOnChord(21, 24, 28, true) }); n != 0 {
		t.Fatalf("noteOnChord 할당: %v", n)
	}
}

// 12. CHORD 패러포닉 — noteOnChord의 세 inc는 각 톤의 baseInc와 비트 동일(같은 유도식),
//     합산 스케일 0.5 경로의 출력은 유계. 엔벨로프·액센트 공유는 trigger 경로 재사용으로
//     구조적으로 보장(별도 수치 게이트는 harmony_test 진폭 게이트가 담당).
func TestVoiceChordParaphonic(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.setParam(int(BCutoff), 0.45)
	v.noteOnChord(21, 24, 28, true)
	if !v.chord3 {
		t.Fatal("noteOnChord 뒤 chord3=false")
	}
	for i, want := range [...]struct{ n uint8; f *float32 }{
		{21, &v.inc0}, {24, &v.inc2}, {28, &v.inc3},
	} {
		var ref bassVoice
		ref.init(1)
		ref.noteOn(want.n, false, false, want.n, 48000)
		if *want.f != ref.inc0 {
			t.Fatalf("inc[%d](반음 %d)=%g ≠ 단음 baseInc %g — 유도식 불일치", i, want.n, *want.f, ref.inc0)
		}
	}
	peak := 0.0
	for _, s := range renderVoiceN(&v, 9600) {
		f := float64(s)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			t.Fatal("CHORD 렌더에 NaN/Inf")
		}
		if a := math.Abs(f); a > peak {
			peak = a
		}
	}
	if peak > 1.0 {
		t.Fatalf("CHORD 9600샘플 peak %.4f > 1.0", peak)
	}
	if peak == 0 {
		t.Fatal("CHORD 렌더가 무음 — 합산 경로 안 살아있음")
	}
	// noteOn(단음) 뒤에는 패러포닉 상태가 완전히 꺼진다.
	v.noteOn(21, false, false, 21, 48000)
	if v.chord3 || v.inc2 != 0 || v.inc3 != 0 {
		t.Fatalf("noteOn 뒤 chord3=%v inc2=%g inc3=%g — 단음 복귀 실패", v.chord3, v.inc2, v.inc3)
	}
}

// 13. 보조 오실레이터 비활성 기여 0 — chord3=false면 inc2/inc3/phase2/phase3 값이 무엇이든
//     process 출력은 비트 동일(분기 계약: 항을 더하지 않는다, +0이 아님).
func TestVoiceChordIgnoredWhenOff(t *testing.T) {
	run := func(poison bool) []float32 {
		var v bassVoice
		v.init(1)
		v.setParam(int(BCutoff), 0.45)
		v.noteOn(21, true, true, 28, 5538) // 슬라이드 포함 경로
		if poison {
			v.inc2, v.inc3 = v.inc0, v.inc0
			v.phase2, v.phase3 = 0.31, 0.73
		}
		return renderVoiceN(&v, 5538)
	}
	a, b := run(false), run(true)
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			t.Fatalf("샘플 %d: 더미 inc2/inc3가 출력을 바꿈(%v vs %v) — 분기 계약 위반", i, a[i], b[i])
		}
	}
}

// 14. 노트 도메인 — noteOn의 인자는 ResolveNote 출력(0..MaxSemis 48). 36 < n ≤ 48은
//     클램프되지 않고, 48 초과만 48로(구 MaxNote 36 클램프가 아님).
func TestVoiceNoteDomainMaxSemis(t *testing.T) {
	var v bassVoice
	v.init(1)
	v.noteOn(40, false, false, 48, 48000)
	if v.inc0 != v.baseInc(40) {
		t.Fatalf("noteOn(40) inc0=%g ≠ baseInc(40)=%g — 36으로 클램프된 구 도메인", v.inc0, v.baseInc(40))
	}
	v.noteOn(48, false, false, 0, 48000)
	hi := v.inc0
	v.noteOn(200, false, false, 0, 48000)
	if v.inc0 != hi {
		t.Fatalf("noteOn(200) inc0=%g ≠ noteOn(48)=%g — MaxSemis 클램프 실패", v.inc0, hi)
	}
	var slide bassVoice
	slide.init(1)
	slide.noteOn(0, false, true, 200, 5538) // nextNote 클램프도 MaxSemis
	if slide.slideInc <= 0 {
		t.Fatalf("nextNote 200 클램프 뒤 slideInc=%g — 목표가 48이어야 양수", slide.slideInc)
	}
}

// 11. 결정론 — 같은 시퀀스(파라미터 이력 포함) 두 번은 비트 동일.
func TestVoiceDeterminism(t *testing.T) {
	run := func() []float32 {
		var v bassVoice
		v.init(1)
		v.setParam(int(BCutoff), 0.5)
		v.setParam(int(BReso), 0.6)
		v.noteOn(12, false, false, 12, 5538)
		var out []float32
		out = append(out, renderVoiceN(&v, 5538)...)
		v.setParam(int(BCutoff), 0.8)
		v.noteOn(19, true, true, 24, 5538)
		out = append(out, renderVoiceN(&v, 5538)...)
		v.noteOff()
		out = append(out, renderVoiceN(&v, 4800)...)
		v.noteOn(24, false, false, 24, 48000)
		out = append(out, renderVoiceN(&v, 48000)...)
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
