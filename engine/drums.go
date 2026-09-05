// drums.go — 드럼 키트(T1b 소유 파일). engine.go가 호출하는 메서드 집합이 계약이다:
//
//	init(seed)                          New/Reset
//	setParam(voice, which int, q float32)  voice 0..5 = BD SD CH OH CP CY, which 0=Level 1=Tune
//	trigger(voice int, accent bool)     원샷(시퀀서·패드·드롭)
//	process(noise *xorshift32) (mix, bd float32)   샘플 1개. mix = 6보이스 합(레벨 적용), bd = BD 단독(사이드체인용)
//
// 현재 내용은 스탠드인(BD 사인 스윕, 나머지는 노이즈+HPF+엔벨로프 변형)이다. T1b 라운드가
// 브리지드-T BD·SD 톤+노이즈·6비트 금속 노이즈 햇·CP 4연타·CY 밴드 2개로 교체한다
// (docs/impl-plan-2026-09-05.md §2.6). 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴.
package engine

const bdFreqDecay float32 = 0.99973271
const bdAmpDecay float32 = 0.99994048

type drumVoice struct {
	phase, freq, amp, hpz float32
	on                    bool
	level, tune           float32
	decay                 float32
	hp                    float32
}

type drumKit struct {
	v [NumDrums]drumVoice
}

func (d *drumKit) init(seed uint32) {
	*d = drumKit{}
	// 스탠드인 감쇠: CH 50ms, OH 300ms, SD 150ms, CP 120ms, CY 900ms (샘플당 계수)
	dec := [NumDrums]float32{bdAmpDecay, 0.99986, 0.99958344, 0.99993, 0.99983, 0.99998}
	hp := [NumDrums]float32{0, 0.3, 0.6, 0.6, 0.4, 0.7}
	for i := range d.v {
		d.v[i].decay = dec[i]
		d.v[i].hp = hp[i]
		d.v[i].level = 0.8
		d.v[i].tune = 0.5
	}
}

func (d *drumKit) setParam(voice, which int, q float32) {
	if voice < 0 || voice >= NumDrums {
		return
	}
	if which == 0 {
		d.v[voice].level = q
	} else {
		d.v[voice].tune = q
	}
}

func (d *drumKit) trigger(voice int, accent bool) {
	if voice < 0 || voice >= NumDrums {
		return
	}
	v := &d.v[voice]
	v.phase = 0
	v.freq = mul32(v.tune, 80) + 70
	v.amp = 1
	if accent {
		v.amp = 1.3
	}
	v.on = true
}

func (d *drumKit) process(n *xorshift32) (float32, float32) {
	var mix, bd float32
	for i := range d.v {
		v := &d.v[i]
		if !v.on {
			continue
		}
		v.amp = v.amp * v.decay
		var s float32
		if i == 0 {
			v.freq = v.freq * bdFreqDecay
			if v.freq < 50 {
				v.freq = 50
			}
			v.phase = v.phase + v.freq/SampleRate
			if v.phase >= 1 {
				v.phase -= 1
			}
			h := v.phase - 0.5
			s = sin5(mul32(twoPiF, h))
		} else {
			u := float32(n.next()&0xFFFF) / 65536.0
			nn := mul32(2, u) - 1
			dd := nn - v.hpz
			v.hpz = mul32(v.hp, dd) + v.hpz
			s = v.hpz
		}
		s = mul32(s, v.amp)
		s = mul32(s, v.level)
		if i == 0 {
			bd = s
		}
		mix += s
	}
	return mix, bd
}
