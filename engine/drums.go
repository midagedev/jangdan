// 드럼 — BD(사인 + 피치 스윕 + 앰프 엔벨로프), CH(노이즈 → 1극 HPF → 짧은 엔벨로프).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 궁 — FMA 융합 차단 계약(approx.go
// 파일 주석의 실측 근거 참조. 특히 이 파일의 HPF 한 줄이 정체성 변환 패턴으로
// FMADDS로 융합돼 네이티브·wasm 해시가 갈렸던 최초의 발견 지점이다).
package engine

// 감쇠 계수는 exp(−1/(τ·SR))을 상수로 사전 계산해 박아둔다(핫 루프 밖).
// BD 피치: 110Hz → 50Hz 플로어, 시정수 ~20ms.
// BD 앰프: 시정수 ~0.35s(스탠드인 — 데시션은 후속 라운드).
// CH 앰프: 시정수 ~50ms. HPF 계수 0.6.
const bdFreqDecay float32 = 0.99973271
const bdAmpDecay float32 = 0.99994048
const chDecay float32 = 0.99958344
const chHPCoef float32 = 0.6

// bdVoice — 사인(위상 누적 + 5차 다항 근사) + 지수 피치 스윕 + 지수 앰프 엔벨로프.
type bdVoice struct {
	phase float32
	freq  float32
	amp   float32
	on    bool
}

func (v *bdVoice) trigger() {
	v.phase = 0
	v.freq = 110
	v.amp = 1
	v.on = true
}

func (v *bdVoice) process() float32 {
	if !v.on {
		return 0
	}
	v.freq = v.freq * bdFreqDecay
	if v.freq < 50 {
		v.freq = 50
	}
	v.amp = v.amp * bdAmpDecay
	v.phase = v.phase + v.freq/SampleRate
	if v.phase >= 1 {
		v.phase -= 1
	}
	h := v.phase - 0.5 // [-0.5, 0.5)
	x := mul32(twoPiF, h) // [-π, π)
	s := sin5(x)
	return mul32(s, v.amp)
}

// chVoice — xorshift 노이즈 → 1극 하이패스 → 짧은 지수 엔벨로프.
type chVoice struct {
	amp float32
	hpz float32
	on  bool
}

func (v *chVoice) trigger() {
	v.amp = 1
	v.on = true
}

func (v *chVoice) process(n *xorshift32) float32 {
	if !v.on {
		return 0
	}
	v.amp = v.amp * chDecay
	u := float32(n.next()&0xFFFF) / 65536.0 // [0,1)
	w := mul32(2, u)
	nn := w - 1 // [-1,1)
	d := nn - v.hpz
	v.hpz = mul32(chHPCoef, d) + v.hpz
	return v.hpz
}
