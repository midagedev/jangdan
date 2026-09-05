// voice.go — 베이스라인 보이스(T1a 소유 파일). engine.go가 호출하는 메서드 집합이 계약이다:
//
//	init(seed)                      New/Reset — 상태 0, 계수 기본
//	setParam(k int, q float32)      k = BTune..BOct(0..7), q = 양자화된 0..1
//	noteOn(note, accent, slide, nextNote, stepSamples)  스텝 트리거. slide면 stepSamples 동안 nextNote로 포르타멘토
//	noteOff()                       게이트 오프(릴리즈 8ms)
//	process(dropOct float32) float32   샘플 1개. dropOct(0..1)은 컷오프 +dropOct 옥타브 부스트
//
// 현재 내용은 스탠드인(나이브 톱니 + 4극 원폴 캐스케이드)이다. T1a 라운드가 다이오드 래더
// 4폴 근사·PolyBLEP·슬라이드·액센트·사각파로 교체한다(docs/impl-plan-2026-09-05.md §2.6).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
package engine

type bassVoice struct {
	phase      float32
	y1, y2, y3 float32
	y4         float32
	fenv, aenv float32
	inc        float32
	active     bool

	// setParam에서 유도되는 계수
	tuneMul   float32 // 2^(tune 반음/12)
	cutBaseHz float32
	resoK     float32
	envOct    float32
	ampDecay  float32
	fltDecay  float32
	accent    float32
	square    bool
	octMul    float32
}

func (v *bassVoice) init(seed uint32) {
	*v = bassVoice{tuneMul: 1, octMul: 1, cutBaseHz: 1000, ampDecay: 0.9999, fltDecay: 0.99997}
}

func (v *bassVoice) setParam(k int, q float32) {
	switch ParamID(k) {
	case BTune: // ±12반음
		semi := mul32(q, 24) - 12
		v.tuneMul = exp2(semi / 12)
	case BCutoff: // 200Hz..8kHz 지수: 200·40^q, log2(40)=5.3219281
		v.cutBaseHz = mul32(200, exp2(mul32(q, 5.3219281)))
	case BReso:
		v.resoK = mul32(q, 3.2)
	case BEnvMod:
		v.envOct = mul32(q, 4)
	case BDecay: // 30ms..2s 지수
		sec := mul32(0.03, exp2(mul32(q, 6.0588937)))
		v.ampDecay = exp2(float32(-1.0 / (float64(sec) * 48000.0 * 0.6931471805599453)))
		v.fltDecay = exp2(float32(-1.0 / (float64(sec) * 3.0 * 48000.0 * 0.6931471805599453)))
	case BAccent:
		v.accent = q
	case BWave:
		v.square = q >= 0.5
	case BOct:
		switch {
		case q < 1.0/3:
			v.octMul = 0.5
		case q < 2.0/3:
			v.octMul = 1
		default:
			v.octMul = 2
		}
	}
}

// noteFreq — note 0..MaxNote(24 = C3 = MIDI 48 = 130.81Hz) → Hz(튠·옥타브 포함).
func (v *bassVoice) noteFreq(note uint8) float32 {
	m := int32(note) + 24 // MIDI
	o := float32(m-69) / 12
	f := mul32(exp2(o), 440)
	f = mul32(f, v.tuneMul)
	return mul32(f, v.octMul)
}

func (v *bassVoice) noteOn(note uint8, accent, slide bool, nextNote uint8, stepSamples float64) {
	v.inc = v.noteFreq(note) / SampleRate
	lv := float32(0.4)
	if accent {
		lv += mul32(v.accent, 0.6)
	}
	v.aenv = lv
	v.fenv = 1
	v.active = true
}

func (v *bassVoice) noteOff() {}

func (v *bassVoice) process(dropOct float32) float32 {
	if !v.active {
		return 0
	}
	v.aenv = v.aenv * v.ampDecay
	v.fenv = v.fenv * v.fltDecay
	oct := mul32(v.envOct, v.fenv) + dropOct
	hz := mul32(v.cutBaseHz, exp2(oct))
	if hz > 16000 {
		hz = 16000
	}
	w := mul32(1.88875e-4, hz)
	g := 1 - exp2(-w)
	if g > 0.999 {
		g = 0.999
	}
	v.phase = v.phase + v.inc
	if v.phase >= 1 {
		v.phase -= 1
	}
	var osc float32
	if v.square {
		if v.phase < 0.5 {
			osc = 1
		} else {
			osc = -1
		}
	} else {
		osc = mul32(2, v.phase) - 1
	}
	fb := mul32(v.resoK, v.y4)
	x := osc + fb
	if x > 4 {
		x = 4
	}
	if x < -4 {
		x = -4
	}
	d := x - v.y1
	v.y1 = mul32(g, d) + v.y1
	d = v.y1 - v.y2
	v.y2 = mul32(g, d) + v.y2
	d = v.y2 - v.y3
	v.y3 = mul32(g, d) + v.y3
	d = v.y3 - v.y4
	v.y4 = mul32(g, d) + v.y4
	return mul32(v.y4, v.aenv)
}
