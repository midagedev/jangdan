// samplerpack_test.go — 내장 팩 굽기 게이트(P5-sampler-pack 라운드).
//
// 계약 정본은 라운드 스펙: 슬롯별 수치 표(8줄) + 전 슬롯 공통 계약 10개. 게이트 배치:
//
//	계약                                  | 게이트                      | 유도(FAIL-first)
//	--------------------------------------+-----------------------------+----------------------------------------
//	표 정합(누적 off·Σn=packFrames·loop≤n) | TestPackTable               | packTab 리터럴과 스펙 표 직접 대조
//	피크 0.85..0.95·DC ≤0.01·RMS ≥0.02    | TestPackBakeCommon          | 슬롯별 측정값 그대로 비교
//	원샷 조용한 끝·슬롯5 루프 이음        | TestPackBakeCommon          | 끝 샘플·마지막 256프레임·양 끝 RMS 비
//	결정론(두 번 구우면 Float32bits 동일) | TestPackBakeCommon          | 별도 버퍼 2회 + 전역 packBuf와 3방향 대조
//	재굽기 없음(packReady 가드)           | TestPackBakeCommon          | 전역 오염 → ensurePack 재호출 → 오염 유지
//	무할당(AllocsPerRun == 0)             | TestPackBakeCommon          | bakePack에 make 1줄 삽입 시 1.0으로 실패 확인
//	슬롯별 정체성(ZCR·τ·유지·덕·스웰 등)  | TestPackSlotCharacter       | 아래 유도 기록
//	슬롯 간 변별(정규화 교차상관 ≤ 0.60)  | TestPackSlotDistinct        | 동일 파형 복제 시 1.0 근처로 실패
//
// 유도(FAIL-first 실측, 2026-09-07 — 물은 뒤 원복해 전 PASS 확인):
//   - BELL 감쇠 τ를 전 부분음 0.5s로 물으면 포락 τ 게이트(≥0.80) 실패 — "BELL 포락
//     τ 0.501s < 0.80". 감쇠비 폭 게이트도 같이 떨어진다(1.02 < 1.5).
//   - bakeBell의 pkClose 호출을 지우면 조용한 끝 게이트 실패 — "마지막 샘플 |0.208|
//     > 0.01"·"마지막 256프레임 max 0.208 > 0.045".
//   - bakePack에 탈출 할당(new → 패키지 변수) 1줄을 넣으면 무할당 게이트 실패 —
//     "할당 1.000회/호출". (탈출하지 않는 make는 스택에 놓여 게이트가 안 잡는다 —
//     게이트가 잡는 것은 힙 할당이다.)
//   - bakePluck 여재를 흰 잡음으로 바꾸면 ZCR 게이트 실패 — "ZCR 3302 — 계약
//     261±60 밖". 1폴 평균의 조화 손실이 낮은 고조파를 못 죽여 밝은 질감이 끝까지
//     남는다(사인 위주 여재를 택한 근거 — samplerpack.go bakePluck 주석).
//
// 테스트 파일은 math를 자유롭게 쓴다(게이트는 네이티브에서만 돈다 — 엔진 규칙과 다름).
package engine

import (
	"math"
	"testing"
)

// ---- 측정 헬퍼(이 파일 소유) ----

// pkT — 구운 팩의 슬롯 i 슬라이스(최초 호출이 굽는다).
func pkT(i int) []float32 {
	ensurePack()
	e := packTab[i]
	return packBuf[e.off : e.off+e.n]
}

func pkPeak(s []float32) float64 {
	var p float64
	for _, x := range s {
		p = math.Max(p, math.Abs(float64(x)))
	}
	return p
}

func pkMean(s []float32) float64 {
	var m float64
	for _, x := range s {
		m += float64(x)
	}
	return m / float64(len(s))
}

// pkR — RMS over [a, b).
func pkR(s []float32, a, b int) float64 {
	var p float64
	for j := a; j < b; j++ {
		p += float64(s[j]) * float64(s[j])
	}
	return math.Sqrt(p / float64(b-a))
}

// pkZ — ZCR = 부호 바뀐 인접 표본 쌍 수 / (n / 48000).
func pkZ(s []float32) float64 {
	c := 0
	for j := 1; j < len(s); j++ {
		if (s[j-1] >= 0) != (s[j] >= 0) {
			c++
		}
	}
	return float64(c) / (float64(len(s)) / 48000.0)
}

// pkTauFit — RMS 창(2400프레임 = 0.05s)의 ln 기울기 최소제곱으로 포락 시정수 추정.
// [skipStart, n−skipEnd) 영역만 피팅 — 어택과 닫는 램프(declick)는 포락 계약 밖이다.
func pkTauFit(s []float32, skipStart, skipEnd int) float64 {
	const w = 2400
	n := len(s)
	var st, sy, stt, sty float64
	c := 0
	for a := skipStart; a+w <= n-skipEnd; a += w {
		r := pkR(s, a, a+w)
		if r < 1e-12 {
			continue
		}
		t := float64(a+w/2) / 48000.0
		st += t
		sy += math.Log(r)
		stt += t * t
		sty += t * math.Log(r)
		c++
	}
	if c < 2 {
		return math.NaN()
	}
	slope := (sty*float64(c) - st*sy) / (stt*float64(c) - st*st)
	return -1 / slope
}

// pkG — Goertzel 진폭. s의 [a, b) 창에서 주파수 hz 성분 진폭(2/N 스케일).
func pkG(s []float32, a, b int, hz float64) float64 {
	w := 2 * math.Pi * hz / 48000.0
	coeff := 2 * math.Cos(w)
	var q1, q2 float64
	for j := a; j < b; j++ {
		q0 := coeff*q1 - q2 + float64(s[j])
		q2 = q1
		q1 = q0
	}
	return 2 * math.Sqrt(q1*q1+q2*q2-2*q1*q2*math.Cos(w)) / float64(b-a)
}

// pkCorr — 지연 0 정규화 교차상관(짧은 쪽 길이만큼).
func pkCorr(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sa, sb, sab float64
	for j := 0; j < n; j++ {
		x, y := float64(a[j]), float64(b[j])
		sa += x * x
		sb += y * y
		sab += x * y
	}
	return sab / math.Sqrt(sa*sb)
}

// pkPeaks — [a,b) 창 스펙트럼(80..1300Hz, 1Hz 스텝 Goertzel)에서 국소 최대 중
// 임계(thr×최대) 위를 진폭 내림차순으로 m개 이하. 사각창 사이드로브(−13dB ≈ 0.22)를
// 잡지 않도록 임계 0.22 — 최약 부분음(창 평균 감쇠 감함)이 최강의 ~0.26으로 위,
// 사이드로브(~0.19)는 아래로 떨어진다. 인접 15Hz 이내 후보는 주로브에 병합.
func pkPeaks(s []float32, a, b int, thr float64, m int) []float64 {
	const lo, hi = 80.0, 1300.0
	var mag []float64
	best := 0.0
	for f := lo; f <= hi; f++ {
		v := pkG(s, a, b, f)
		mag = append(mag, v)
		best = math.Max(best, v)
	}
	var out []float64
	for i := 1; i < len(mag)-1 && len(out) < m; i++ {
		if mag[i] >= mag[i-1] && mag[i] > mag[i+1] && mag[i] >= thr*best {
			f := lo + float64(i)
			if len(out) > 0 && f-out[len(out)-1] < 15 {
				continue // 인접 사이드로브 병합
			}
			out = append(out, f)
		}
	}
	// 진폭 내림차순 정렬(선택 정렬 — m ≤ 7)
	for i := 0; i < len(out); i++ {
		for k := i + 1; k < len(out); k++ {
			if pkG(s, a, b, out[k]) > pkG(s, a, b, out[i]) {
				out[i], out[k] = out[k], out[i]
			}
		}
	}
	return out
}

// ---- 게이트 1/4: 표 정합 ----

// TestPackTable — packTab 리터럴이 스펙 표와 정확히 같은지(길이·기준음·루프·누적 off).
// 리터럴이 정본이므로 이 게이트는 "스펙 ↔ 코드" 번역 오류를 잡는다.
func TestPackTable(t *testing.T) {
	wantN := [packSlots]uint32{48000, 72000, 48000, 9600, 12000, 38400, 33600, 28800}
	wantRoot := [packSlots]uint8{24, 36, 12, 24, 24, 24, 24, 12}
	wantLoop := [packSlots]uint32{48000, 72000, 48000, 9600, 12000, 0, 33600, 28800}
	var acc uint32
	for i := 0; i < packSlots; i++ {
		e := packTab[i]
		if e.off != acc {
			t.Errorf("packTab[%d].off = %d, 누적 %d와 다름", i, e.off, acc)
		}
		if e.n != wantN[i] {
			t.Errorf("packTab[%d].n = %d, 스펙 %d와 다름", i, e.n, wantN[i])
		}
		if e.root != wantRoot[i] {
			t.Errorf("packTab[%d].root = %d, 스펙 %d와 다름", i, e.root, wantRoot[i])
		}
		if e.loop != wantLoop[i] {
			t.Errorf("packTab[%d].loop = %d, 스펙 %d와 다름", i, e.loop, wantLoop[i])
		}
		if e.loop > e.n {
			t.Errorf("packTab[%d].loop = %d > n = %d", i, e.loop, e.n)
		}
		acc += e.n
	}
	if acc != packFrames {
		t.Errorf("스롯 n 합 = %d, packFrames = %d와 다름", acc, packFrames)
	}
	wantNames := [packSlots]string{"PLUCK", "BELL", "VOX", "WOOD", "SHAKE", "TAPE", "BREATH", "SUB"}
	if PackNames != wantNames {
		t.Errorf("PackNames = %v, 스펙 %v와 다름", PackNames, wantNames)
	}
}

// ---- 게이트 2/4: 전 슬롯 공통 계약 + 결정론 + 재굽기 + 무할당 ----

// TestPackBakeCommon — 공통 계약 1~5(피크·DC·RMS·끝·이음)·7(결정론)·8(재굽기 없음)·
// 9(무할당)을 단언한다.
func TestPackBakeCommon(t *testing.T) {
	for i := 0; i < packSlots; i++ {
		e := packTab[i]
		s := pkT(i)
		name := PackNames[i]
		peak := pkPeak(s)
		if peak < 0.85 || peak > 0.95 {
			t.Errorf("%s 피크 %.4f — 계약 0.85..0.95 밖", name, peak)
		}
		if m := math.Abs(pkMean(s)); m > 0.01 {
			t.Errorf("%s |평균| %.5f > 0.01", name, m)
		}
		if r := pkR(s, 0, len(s)); r < 0.02 {
			t.Errorf("%s RMS %.5f < 0.02 (죽은 슬롯)", name, r)
		}
		n := len(s)
		if e.loop == e.n { // 원샷: 끝이 조용하다
			if last := math.Abs(float64(s[n-1])); last > 0.01 {
				t.Errorf("%s 마지막 샘플 |%.5f| > 0.01", name, last)
			}
			tail := pkPeak(s[n-256:])
			if tail > 0.05*peak {
				t.Errorf("%s 마지막 256프레임 max %.5f > 피크×0.05 = %.5f", name, tail, 0.05*peak)
			}
		} else { // 슬롯5 TAPE: 루프 이음
			seam := math.Abs(float64(s[n-1]) - float64(s[e.loop]))
			if seam > 0.05 {
				t.Errorf("TAPE 이음 |끝−첫| %.5f > 0.05", seam)
			}
			ratio := pkR(s, n-512, n) / pkR(s, int(e.loop), int(e.loop)+512)
			if ratio < 0.7 || ratio > 1.43 {
				t.Errorf("TAPE 루프 RMS 비 %.4f — 계약 0.7..1.43 밖", ratio)
			}
		}
		t.Logf("%s peak=%.4f mean=%+.5f rms=%.4f", name, peak, pkMean(s), pkR(s, 0, n))
	}

	// 계약 7: 결정론 — 별도 버퍼 두 번 + 전역 packBuf까지 3방향 비트 동일.
	var a, b [packFrames]float32
	bakePack(&a)
	bakePack(&b)
	diff := 0
	for j := 0; j < packFrames; j++ {
		if math.Float32bits(a[j]) != math.Float32bits(b[j]) || math.Float32bits(a[j]) != math.Float32bits(packBuf[j]) {
			diff++
		}
	}
	if diff != 0 {
		t.Errorf("재굽기 비트 차이 %d프레임 — 결정론 위반", diff)
	}

	// 계약 8: 재굽기 없음 — 오염 후 재호출해도 오염이 유지되어야 한다(가드가 막는다).
	const probe = 4321
	saved := packBuf[probe]
	packBuf[probe] = 0.5
	ensurePack()
	if packBuf[probe] != 0.5 {
		t.Errorf("ensurePack 재호출이 팩을 다시 구웠다(가드 위반)")
	}
	packBuf[probe] = saved // 원상복구(결정론상 구운 값과 동일)

	// 계약 9: 무할당.
	var sink [packFrames]float32
	allocs := testing.AllocsPerRun(3, func() { bakePack(&sink) })
	if allocs != 0 {
		t.Errorf("bakePack 할당 %.3f회/호출 — 0이어야 한다", allocs)
	}
}

// ---- 게이트 3/4: 슬롯별 정체성 ----

// TestPackSlotCharacter — 스펙 표의 슬롯별 수치 계약. 측정값을 Logf로 남겨
// 보고 표(피크·RMS·ZCR·τ)의 원본이 된다.
func TestPackSlotCharacter(t *testing.T) {
	// 0 PLUCK — 카플러스-스트롱: ZCR 261±60, 포락 τ 0.20..0.60s.
	pluck := pkT(0)
	zPluck := pkZ(pluck)
	if zPluck < 201 || zPluck > 321 {
		t.Errorf("PLUCK ZCR %.1f — 계약 261±60 밖", zPluck)
	}
	tauPluck := pkTauFit(pluck, 2400, 800) // 닫는 램프 320 + 여유 480 제외
	if tauPluck < 0.20 || tauPluck > 0.60 {
		t.Errorf("PLUCK 포락 τ %.3fs — 계약 0.20..0.60 밖", tauPluck)
	}
	t.Logf("PLUCK zcr=%.1f tau=%.3f", zPluck, tauPluck)

	// 1 BELL — τ ≥ 0.80s, ZCR ≥ PLUCK×1.5, 비조화 부분음 ≥5, 부분음별 감쇠 상이.
	bell := pkT(1)
	tauBell := pkTauFit(bell, 4800, 19200) // 클릭(0.1s)·릴리즈 램프(0.35s) 제외
	if tauBell < 0.80 {
		t.Errorf("BELL 포락 τ %.3fs < 0.80", tauBell)
	}
	zBell := pkZ(bell)
	if zBell < 1.5*zPluck {
		t.Errorf("BELL ZCR %.1f < PLUCK ZCR %.1f × 1.5 = %.1f", zBell, zPluck, 1.5*zPluck)
	}
	peaks := pkPeaks(bell, 2400, 16800, 0.22, 7) // 0.05..0.35s 스펙트럼
	if len(peaks) < 6 {
		t.Fatalf("BELL 부분음 탐지 %d개 — 최저(기저) 포함 6개 이상 필요: %v", len(peaks), peaks)
	}
	fRoot := peaks[0] // 기저 = 최저 주파수 피크(peaks는 진폭순이므로 직접 최소 탐색)
	for _, f := range peaks {
		if f < fRoot {
			fRoot = f
		}
	}
	inh := 0
	for _, f := range peaks {
		harm := false
		for k := 1; k <= 20; k++ {
			if math.Abs(f-float64(k)*fRoot) <= 20 {
				harm = true
				break
			}
		}
		if !harm {
			inh++
		}
	}
	if inh < 5 {
		t.Errorf("BELL 비조화 부분음 %d개(기저 %.1fHz, 피크 %v) — 5개 미달", inh, fRoot, peaks)
	}
	// 부분음별 감쇠: 상위 4개 부분음의 [0.05,0.35)→[0.70,1.00)s 진폭비가 1.5배 이상 벌어진다.
	rMin, rMax := math.MaxFloat64, 0.0
	for _, f := range peaks[:4] {
		r := pkG(bell, 33600, 48000, f) / pkG(bell, 2400, 16800, f)
		rMin = math.Min(rMin, r)
		rMax = math.Max(rMax, r)
		t.Logf("BELL 부분음 %.0fHz 감쇠비 %.3f", f, r)
	}
	if rMax/rMin < 1.5 {
		t.Errorf("BELL 부분음 감쇠비 폭 %.2f (<1.5) — 감쇠가 부분음마다 다르지 않다", rMax/rMin)
	}
	t.Logf("BELL zcr=%.1f tau=%.3f peaks=%v", zBell, tauBell, peaks)

	// 2 VOX — 유지형: RMS(0.55~0.65s)/RMS(0.05~0.15s) ≥ 0.50.
	vox := pkT(2)
	hold := pkR(vox, 26400, 31200) / pkR(vox, 2400, 7200)
	if hold < 0.50 {
		t.Errorf("VOX 유지비 %.3f < 0.50 — 감쇠형이 됐다", hold)
	}
	t.Logf("VOX hold=%.3f zcr=%.1f", hold, pkZ(vox))

	// 3 WOOD — 피크 이후 0.05s 안에 RMS가 피크의 20% 밑으로.
	wood := pkT(3)
	ip := 0
	for j := range wood {
		if math.Abs(float64(wood[j])) > math.Abs(float64(wood[ip])) {
			ip = j
		}
	}
	if ip+2400 > len(wood) {
		t.Fatalf("WOOD 피크 위치 %d — 덕 창이 슬롯 밖", ip)
	}
	duck := pkR(wood, ip+1200, ip+2400) / pkPeak(wood)
	if duck > 0.20 {
		t.Errorf("WOOD 피크 후 0.05s RMS 비 %.3f > 0.20", duck)
	}
	t.Logf("WOOD peakAt=%.1fms duck=%.3f", float64(ip)/48, duck)

	// 4 SHAKE — ZCR ≥ 3000.
	shake := pkT(4)
	if z := pkZ(shake); z < 3000 {
		t.Errorf("SHAKE ZCR %.1f < 3000", z)
	} else {
		t.Logf("SHAKE zcr=%.1f", z)
	}

	// 5 TAPE — 8등분 창 RMS 변동계수 ≤ 0.50, ZCR ≥ 2000(이음은 공통 게이트).
	tape := pkT(5)
	var rs [8]float64
	w := len(tape) / 8
	var mR float64
	for k := 0; k < 8; k++ {
		rs[k] = pkR(tape, k*w, (k+1)*w)
		mR += rs[k]
	}
	mR /= 8
	var v float64
	for k := 0; k < 8; k++ {
		v += (rs[k] - mR) * (rs[k] - mR)
	}
	cov := math.Sqrt(v/8) / mR
	if cov > 0.50 {
		t.Errorf("TAPE 창 RMS 변동계수 %.3f > 0.50", cov)
	}
	if z := pkZ(tape); z < 2000 {
		t.Errorf("TAPE ZCR %.1f < 2000", z)
	} else {
		t.Logf("TAPE cov=%.3f zcr=%.1f", cov, z)
	}

	// 6 BREATH — 스웰: RMS(앞 20%) < RMS(40~60%) × 0.7.
	breath := pkT(6)
	front := pkR(breath, 0, len(breath)/5)
	mid := pkR(breath, int(0.4*float64(len(breath))), int(0.6*float64(len(breath))))
	if front >= 0.7*mid {
		t.Errorf("BREATH 앞 20%% RMS %.4f ≥ 중앙 RMS %.4f × 0.7 — 스웰이 아니다", front, mid)
	}
	t.Logf("BREATH front/mid=%.3f", front/mid)

	// 7 SUB — ZCR ≤ 200.
	sub := pkT(7)
	if z := pkZ(sub); z > 200 {
		t.Errorf("SUB ZCR %.1f > 200", z)
	} else {
		t.Logf("SUB zcr=%.1f", z)
	}
}

// ---- 게이트 4/4: 슬롯 간 변별 ----

// TestPackSlotDistinct — 서로 다른 두 슬롯의 정규화 교차상관(지연 0)이 전부 |ρ| ≤ 0.60.
// 같은 파형을 복사해 길이만 바꾸면 1 근처로 뜬다.
func TestPackSlotDistinct(t *testing.T) {
	maxAbs, maxi, maxj := 0.0, -1, -1
	for i := 0; i < packSlots; i++ {
		for j := i + 1; j < packSlots; j++ {
			c := pkCorr(pkT(i), pkT(j))
			if math.Abs(c) > math.Abs(maxAbs) {
				maxAbs, maxi, maxj = c, i, j
			}
			if math.Abs(c) > 0.60 {
				t.Errorf("%s×%s 교차상관 %+.3f — |ρ| ≤ 0.60 위반", PackNames[i], PackNames[j], c)
			}
		}
	}
	t.Logf("최대 쌍 교차상관 %+.4f (%s×%s)", maxAbs, PackNames[maxi], PackNames[maxj])
}
