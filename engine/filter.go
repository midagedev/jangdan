// filter.go — 다이오드 래더 4폴 근사(T1a 소유 파일). voice.go가 호출한다.
//
// 구조(기획서 §2.6): u = in − k·softclip(hpf100(s4)) → 1폴 4단 캐스케이드 → 출력 s4.
//   - 피드백 경로의 1차 HPF(≈100Hz)가 DC 폭주를 막는다(루프가 DC에서 닫히지 않게).
//   - 비선형은 유리식 소프트클립 x(27+x²)/(27+9x²) — 기울기 ≤ 1, 분모 ≥ 27.
//   - 음 피드백이므로 컷오프(각 단 −45°, 합 −180°)에서 루프가 양실수가 되어 공진 피크가 생긴다.
//
// 안정성 유도(매직 넘버 근거):
//   - 동일폴 4단의 컷오프점 크기는 0.25 → 저컷에서 한계 k = 4.
//   - 고컷에서는 1샘플 피드백 지연이 위상을 당겨 4θ+ω = 2π 지점이 위험해진다. 계수를
//     g ≤ 0.8로 클램프하면(폴 z ≥ 0.2, 컷오프 ≈ 8.5kHz 이상 포화) 그 지점 |H| ≤ 0.556,
//     한계 k ≥ 1.80 — kmax = 1.6(voice.go BReso 매핑)이 전 대역에서 11% 여유로
//     자기발진 직전을 유지한다.
//   - 상태 클램프 |s| ≤ 4(계약)와 유리식 포화로 어떤 파라미터에서도 발산·NaN 없음.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
package engine

// aHP100 — 피드백 HPF(1차 100Hz) 계수. 유도: y[i] = a·(y[i−1] + x[i] − x[i−1])에서
// a = e^{−2π·100/48000} = exp2(−(2π·100/48000)/ln2) = exp2(−0.0188873) = 0.9869953.
const aHP100 float32 = 0.9869953

// ladderFilter — 0값이 곧 초기 상태(모든 메모리 0). k는 setParam(BReso)이 유도한다.
type ladderFilter struct {
	s1, s2, s3, s4 float32 // 단 상태(출력은 s4)
	hpY, hpX1      float32 // 피드백 HPF 상태
	k              float32 // 레조넌스 피드백량(0..1.6)
}

// process — 샘플 1개. hz는 컷오프(클램프 20..16000). 반환 |·| ≤ 4(상태 클램프).
func (f *ladderFilter) process(in, hz float32) float32 {
	if hz > 16000 {
		hz = 16000
	}
	if hz < 20 {
		hz = 20
	}
	// g = 1 − e^{−2π·hz/48000} = 1 − exp2(−hz·2π/(48000·ln2)).
	// 계수는 매 샘플 새로 계산되고 상태에 곱해 누적되지 않으므로 exp2 근사 오차가 쌓이지 않는다.
	w := mul32(hz, 1.888762e-4) // 유도: 2π/(48000·0.6931472) = 1.888762e-4
	g := 1 - exp2(-w)
	if g > 0.8 {
		g = 0.8 // 고컷 위상 정합점 안정화(파일 주석 유도)
	}
	// 피드백 HPF(100Hz) — DC 차단
	hpin := f.s4
	hp := mul32(aHP100, f.hpY+hpin-f.hpX1)
	f.hpX1 = hpin
	f.hpY = hp
	// 유리식 소프트클립 x(27+x²)/(27+9x²)
	x2 := mul32(hp, hp)
	num := mul32(hp, x2+27)
	den := mul32(9, x2) + 27
	u := in - mul32(f.k, num/den)
	if u > 4 {
		u = 4
	}
	if u < -4 {
		u = -4
	}
	// 4단 원폴 캐스케이드(동일 계수)
	d := u - f.s1
	f.s1 = mul32(g, d) + f.s1
	d = f.s1 - f.s2
	f.s2 = mul32(g, d) + f.s2
	d = f.s2 - f.s3
	f.s3 = mul32(g, d) + f.s3
	d = f.s3 - f.s4
	f.s4 = mul32(g, d) + f.s4
	// 자기발진 유계(계약): 상태 |·| ≤ 4 — voice의 출력 게인 0.24와 함께 |출력| ≤ 0.96
	if f.s1 > 4 {
		f.s1 = 4
	} else if f.s1 < -4 {
		f.s1 = -4
	}
	if f.s2 > 4 {
		f.s2 = 4
	} else if f.s2 < -4 {
		f.s2 = -4
	}
	if f.s3 > 4 {
		f.s3 = 4
	} else if f.s3 < -4 {
		f.s3 = -4
	}
	if f.s4 > 4 {
		f.s4 = 4
	} else if f.s4 < -4 {
		f.s4 = -4
	}
	return f.s4
}
