// drums.go — 드럼 키트(T1b 소유 파일). engine.go가 호출하는 메서드 집합이 계약이다:
//
//	init(seed)                          New/Reset
//	setParam(voice, which int, q float32)  voice 0..5 = BD SD CH OH CP CY, which 0=Level 1=Tune
//	trigger(voice int, accent bool)     원샷(시퀀서·패드·드롭)
//	process(noise *xorshift32) (mix, bd float32)   샘플 1개. mix = 6보이스 합(레벨 적용), bd = BD 단독(사이드체인용)
//	                                               process 후 last[v] = 이번 샘플 v보이스의 mix 기여(레벨 미터 원본)
//
// 보이스(docs/impl-plan-2026-09-05.md §2.6):
//   BD — 브리지드-T 근사: 사인 피치 스윕(시작 f0×3 → f0, 시정수 25ms) + 앰프 지수 디케이
//        (시정수 300ms) + 트리거 클릭(τ 0.7ms 노이즈 버스트 → 3kHz 원폴 LP). Tune = f0 45..90Hz 지수.
//        액센트 = 레벨 ×1.3 + 스윕 시작 ×1.2(×3.6).
//   SD — 톤 2개(f, 1.6f — τ 17.4ms = −40dB@80ms) + 노이즈 2폴 밴드패스(중심 ~2kHz,
//        τ 32.6ms = −40dB@150ms). Tune = 톤 f 150..300Hz 지수.
//   CH/OH — 공통 금속 노이즈원(metalBus) → HPF 원폴(Tune = 컷 4k..10kHz). CH τ 8.7ms
//        (−40dB@40ms), OH τ 76ms(−40dB@350ms). OH 트리거는 CH을 초크하지 않는다(단순화).
//   CP — 노이즈 → 밴드패스(중심 ~1.2kHz) → 4연타 엔벨로프(간격 Tune 6..14ms, 첫 3개
//        τ 2.5ms·마지막 꼬리 τ 39ms = −40dB@180ms).
//   CY — 금속 노이즈원 → 밴드 2개(중심 ~3kHz·~8kHz, Tune ×0.7..×1.4) 합, τ 217ms
//        (−40dB@1.0s) + 트리거 노이즈 어택(τ 10ms).
//
// 노이즈 독립성: process는 외부 xorshift에서 **항상 6회**(보이스별 고정 슬롯) 추첨한다.
// 활성 보이스만 추첨하면 한 보이스의 소리가 다른 보이스의 트리거 이력에 의존하게 된다
// (drums_test.go 사이드체인·결정론 게이트가 잡는다). 트리거 시점 추첨(금속 디튠)은
// drumKit 내부 rng(시드 파생). 금속 버스도 보이스 상태와 무관하게 매 샘플 진행한다.
//
// 진폭 계약: 보이스 원본 피크 ≤ ~0.5, 6보이스 합 ≤ ~2.4 → 액센트 ×1.3을 곱해 단독 ≤ 1.0,
// 동시 mix ≤ 3.0(FX가 클립). 감쇠·필터 계수는 전부 init/setParam에서 exp2·sin5 유도로
// 계산하고(핫 루프 밖), 리터럴에는 유도식을 주석으로 남긴다. "디케이 Nms"는 −40dB
// 도달 시간(시정수 = N/ln100), BD 앰프만 예외로 시정수 300ms(스펙 명시).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴.
package engine

// ---- 공통 헬퍼(이 파일 소유 소문자 함수) ----

// decayCal — exp2 다항 근사의 계통 오차 보정. exp2는 참값의 0.9999143..0.9999159배를
// 돌려준다(2026-09-05 실측, decayCoef 인수 범위 |x| ≤ 5.4e-3에서 frac≈1 부근 — 거의
// 일정한 곱셈 편차). 감쇠 계수는 샘플마다 곱해져 이 오차가 누적된다(순수 효과 τ≈245ms의
// 추가 감쇠 — CY τ217ms가 −40dB 469ms에 도달하는 것으로 실측됐다). 이 상수를 곱해
// dec^N = e^(−N/(tau·SR))를 샘플당 ±2e-6 이내로 맞춘다(짧은 τ의 잔차는 무해하다).
const decayCal = 1.0000857

// decayCoef — 시정수 tau(초)의 샘플당 감쇠. dec^N = e^(−N/(tau·SR)), 즉 tau가 진짜
// 시정수가 된다(voice.go BDecay와 같은 유도 + decayCal 보정).
func decayCoef(tau float64) float32 {
	return mul32(exp2(float32(-1.0/(tau*SampleRate*0.6931471805599453))), decayCal)
}

// lpCoef — 원폴 LP 계수 g = 1 − e^(−2π·fc/SR) = 1 − exp2(−w·log2e), w = fc·2π/SR.
func lpCoef(hz float32) float32 {
	w := mul32(hz, 1.30899694e-4) // 2π/48000
	g := 1 - exp2(mul32(w, -1.44269504))
	if g > 0.99 {
		g = 0.99
	}
	if g < 0 {
		g = 0
	}
	return g
}

// noise01 — xorshift 추첨값 → 균등 [0,1).
func noise01(x uint32) float32 {
	return float32(x&0xFFFF) / 65536.0
}

// noiseBipolar — 추첨값 → 균등 [−1,1).
func noiseBipolar(x uint32) float32 {
	return mul32(2, noise01(x)) - 1
}

// ladder6 — 균등 [0,1)을 63단계 계단에 올린다(6비트 양자화 — 이산 논리 금속 회로의
// 출력 래더 근사). 금속 노이즈원의 난수 성분이고 metalNoise가 테스트 훅이다.
func ladder6(u float32) float32 {
	return float32(int(mul32(u, 63))) / 63
}

// metalNoise — 금속 노이즈원 난수 1개(테스트 훅: 고유 레벨 수 ≤ 64를 단언한다).
func metalNoise(x *xorshift32) float32 {
	return ladder6(noise01(x.next()))
}

// oscSine — 위상 누산 사인. 위상 p ∈ [0,4)(1주기 = 4)를 4사분면으로 접어 sin5를
// [0, π/2]에서만 부른다 — sin5는 ±π에서 0이 아니라(≈ ∓0.075) 주기 경계마다 −0.15
// 계단이 생기고, 사분면 접기는 그 불연속을 없긴다(경계 오차 ≤ 4e-6).
func oscSine(p float32) float32 {
	fl := floorf(p)
	q := int32(fl) & 3
	f := p - fl // [0,1)
	a := mul32(f, 1.57079637) // π/2
	switch q {
	case 0:
		return sin5(a)
	case 1:
		return sin5(1.57079637 - a)
	case 2:
		return -sin5(a)
	default:
		return -sin5(1.57079637 - a)
	}
}

// bandLP2 — 2폴 밴드패스: LP2(hi) − LP2(lo). 응답 피크 ≈ √(hi·lo), 양쪽 12dB/oct.
// 체임버린 SVF 대신 이 구성을 쓰는 이유: SVF는 고역(fc 8k+, Tune ×1.4)에서 좁은
// 대역이 불안정해진다(안정 조건 q > 2·sin(π·fc/SR)) — 원폴 캐스케이드는 fc 전 범위에서
// 무조건 안정이다.
type bandLP2 struct {
	y1, y2 float32 // hi쪽 원폴 2단
	z1, z2 float32 // lo쪽 원폴 2단
	ghi, glo float32
}

func (b *bandLP2) set(hi, lo float32) {
	b.ghi = lpCoef(hi)
	b.glo = lpCoef(lo)
}

func (b *bandLP2) process(x float32) float32 {
	d := x - b.y1
	b.y1 = mul32(b.ghi, d) + b.y1
	d = b.y1 - b.y2
	b.y2 = mul32(b.ghi, d) + b.y2
	d = x - b.z1
	b.z1 = mul32(b.glo, d) + b.z1
	d = b.z1 - b.z2
	b.z2 = mul32(b.glo, d) + b.z2
	return b.y2 - b.z2
}

// ---- 금속 노이즈원(CH·OH·CY 공통) ----

// metalRatio — 6개 사각파의 비조화 주파수 비(기본 ~320Hz). 이산 논리 햇 회로 구조.
var metalRatio = [6]float32{1, 1.34, 1.66, 2.05, 2.4, 2.8}

// metalBus — 사각파 6개 합을 원폴 HP(3.4kHz — 기본성분 제거)로 통과시키고 6비트
// 계단 난수를 섞는 공통 금속 노이즈원. 매 샘플 진행(보이스 상태 무관 — 독립성 계약).
type metalBus struct {
	ph  [6]float32 // 위상 [0,4)
	inc [6]float32
	lp  float32 // HP용 원폴 LP 상태(HP 출력 = x − lp)
	g   float32
}

func (m *metalBus) init() {
	for i := range m.inc {
		m.inc[i] = mul32(mul32(320, metalRatio[i]), 8.3333333e-5) // hz·4/SR
	}
	m.g = lpCoef(3400)
}

// detune — 트리거 시 내부 rng로 계단 난수 6개를 뽑아 각 사각파를 ±0.7% 미세 조정.
// 히트마다 금속 질감이 살짝 다르다(결정론: 시드·트리거 시퀀스의 함수).
func (m *metalBus) detune(x *xorshift32) {
	for i := range m.inc {
		d := metalNoise(x) - 0.5
		base := mul32(mul32(320, metalRatio[i]), 8.3333333e-5)
		m.inc[i] = mul32(base, mul32(d, 0.014)+1)
	}
}

// sample — 금속원 샘플 1개. nn은 이번 샘플의 계단 난수를 [−1,1)로 접은 것.
func (m *metalBus) sample(nn float32) float32 {
	s := float32(0)
	for i := range m.ph {
		m.ph[i] = m.ph[i] + m.inc[i]
		if m.ph[i] >= 4 {
			m.ph[i] -= 4
		}
		if m.ph[i] < 2 {
			s = s + 1
		} else {
			s = s - 1
		}
	}
	x := mul32(s, 1.0/6.0) // 사각파 6개 합 → [−1,1]
	d := x - m.lp
	m.lp = mul32(m.g, d) + m.lp
	return x - m.lp + mul32(nn, 0.40)
}

// ---- 보이스 ----

// bdVoice — 브리지드-T 근사 킥.
type bdVoice struct {
	phase, sweep, amp float32
	clickA, clickLP   float32
	acc               float32 // 액센트 배율 1 / 1.3
	on                bool
	// init/setParam 유도 계수
	f0, sweepDec, ampDec, clickDec, clickG float32
	level                                  float32
}

func (v *bdVoice) trigger(acc float32, accent bool) {
	v.phase = 0
	v.sweep = 2 // f = f0·(1+sweep): 시작 ×3 → 1
	if accent {
		v.sweep = 2.6 // 액센트: 시작 ×3.6(= ×3의 1.2배)
	}
	v.amp = 1
	v.clickA = 1
	v.acc = acc
	v.on = true
}

func (v *bdVoice) process(nn float32) float32 {
	if !v.on {
		return 0
	}
	v.amp = mul32(v.amp, v.ampDec)
	v.sweep = mul32(v.sweep, v.sweepDec)
	v.phase = v.phase + mul32(mul32(v.f0, v.sweep+1), 8.3333333e-5)
	if v.phase >= 4 {
		v.phase -= 4
	}
	s := oscSine(v.phase)
	// 클릭: 0.7ms 노이즈 버스트 → 3kHz 원폴 LP
	v.clickA = mul32(v.clickA, v.clickDec)
	d := mul32(nn, v.clickA) - v.clickLP
	v.clickLP = mul32(v.clickG, d) + v.clickLP
	if v.amp < 1e-6 && v.clickA < 1e-6 {
		v.on = false
		return 0
	}
	return mul32(v.amp, s+mul32(0.30, v.clickLP)) // 스케일 0.36은 키트 process에서(보이스 합 예산)
}

// sdVoice — 톤 2개 + 노이즈 밴드패스 스네어.
type sdVoice struct {
	ph1, ph2 float32
	aT, aN   float32 // 톤·노이즈 엔벨로프
	bp       bandLP2
	acc      float32
	on       bool
	// init/setParam 유도
	inc1, inc2, toneDec, noiseDec float32
	level                         float32
}

func (v *sdVoice) trigger(acc float32) {
	v.ph1, v.ph2 = 0, 0
	v.aT, v.aN = 1, 1
	v.acc = acc
	v.on = true
}

func (v *sdVoice) process(nn float32) float32 {
	if !v.on {
		return 0
	}
	v.aT = mul32(v.aT, v.toneDec)
	v.aN = mul32(v.aN, v.noiseDec)
	v.ph1 = v.ph1 + v.inc1
	if v.ph1 >= 4 {
		v.ph1 -= 4
	}
	v.ph2 = v.ph2 + v.inc2
	if v.ph2 >= 4 {
		v.ph2 -= 4
	}
	if v.aT < 1e-6 && v.aN < 1e-6 {
		v.on = false
		return 0
	}
	tone := mul32(v.aT, mul32(0.34, oscSine(v.ph1))+mul32(0.20, oscSine(v.ph2)))
	noise := mul32(v.aN, v.bp.process(nn))
	return mul32(tone+noise, 0.33)
}

// hatVoice — CH/OH 공용: 금속원 → 원폴 HPF → 엔벨로프.
type hatVoice struct {
	env float32
	lp  float32
	acc float32
	on  bool
	// init/setParam 유도
	dec, hpG, gain float32
	level          float32
}

func (v *hatVoice) trigger(acc float32) {
	v.env = 1
	v.acc = acc
	v.on = true
}

func (v *hatVoice) process(src float32) float32 {
	if !v.on {
		return 0
	}
	v.env = mul32(v.env, v.dec)
	d := src - v.lp
	v.lp = mul32(v.hpG, d) + v.lp
	if v.env < 1e-6 {
		v.on = false
		return 0
	}
	return mul32(v.env, mul32(v.gain, src-v.lp))
}

// cpHitAmp — 4연타 각 히트의 시작 진폭(첫 3개 짧게, 마지막이 긴 꼬리).
var cpHitAmp = [4]float32{1, 0.85, 0.75, 0.9}

// cpVoice — 박수: 노이즈 → 밴드패스 → 4연타 엔벨로프.
type cpVoice struct {
	env       float32
	stage     int8 // 다음 히트 인덱스(1..3), 4면 끝
	countdown int32
	curDec    float32
	bp        bandLP2
	acc       float32
	on        bool
	// init/setParam 유도
	interval                     int32
	fastDec, tailDec             float32
	level                        float32
}

func (v *cpVoice) trigger(acc float32) {
	v.env = cpHitAmp[0]
	v.stage = 1
	v.countdown = v.interval
	v.curDec = v.fastDec
	v.acc = acc
	v.on = true
}

func (v *cpVoice) process(nn float32) float32 {
	if !v.on {
		return 0
	}
	if v.countdown > 0 {
		v.countdown--
		if v.countdown == 0 && v.stage < 4 {
			v.env = cpHitAmp[v.stage]
			if v.stage == 3 {
				v.curDec = v.tailDec
			}
			v.stage++
			if v.stage < 4 {
				v.countdown = v.interval
			}
		}
	}
	v.env = mul32(v.env, v.curDec)
	if v.env < 1e-6 {
		v.on = false
		return 0
	}
	return mul32(v.env, mul32(1.2, v.bp.process(nn)))
}

// cyVoice — 심벌: 금속원 → 밴드 2개 합 + 어택 노이즈.
type cyVoice struct {
	env, atk float32
	bp1, bp2 bandLP2
	acc      float32
	on       bool
	// init/setParam 유도
	dec, atkDec float32
	level       float32
}

func (v *cyVoice) trigger(acc float32) {
	v.env, v.atk = 1, 1
	v.acc = acc
	v.on = true
}

func (v *cyVoice) process(src, nn float32) float32 {
	if !v.on {
		return 0
	}
	v.env = mul32(v.env, v.dec)
	v.atk = mul32(v.atk, v.atkDec)
	if v.env < 1e-6 {
		v.on = false
		return 0
	}
	x := src + mul32(mul32(v.atk, nn), 0.8)
	sum := v.bp1.process(x) + v.bp2.process(x)
	return mul32(v.env, mul32(0.35, sum))
}

// ---- 키트 ----

type drumKit struct {
	bd    bdVoice
	sd    sdVoice
	ch    hatVoice
	oh    hatVoice
	cp    cpVoice
	cy    cyVoice
	metal metalBus
	rng   xorshift32 // 트리거 시점 추첨(금속 디튠)
	last  [6]float32 // 직전 샘플 각 보이스의 mix 기여(× acc × level 반영 — engine.Level이 블록 피크로 모은다)
}

func (d *drumKit) init(seed uint32) {
	*d = drumKit{}
	if seed == 0 {
		seed = 0x9E3779B9
	}
	d.rng = xorshift32{seed ^ 0x10245477} // 외부 노이즈 스트림과 다른 사례값
	d.metal.init()
	// 감쇠 계수(유도: "디케이 Nms" = −40dB 도달, 시정수 = N/ln100 = N/4.6052)
	d.bd.sweepDec = decayCoef(0.025)  // 피치 스윕 시정수 25ms(스펙)
	d.bd.ampDec = decayCoef(0.3)      // 앰프 시정수 300ms(스펙 — 시정수 표기)
	d.bd.clickDec = decayCoef(0.0007) // 클릭 0.7ms(2ms 버스트의 실효 시정수)
	d.bd.clickG = lpCoef(3000)
	d.sd.toneDec = decayCoef(0.01737)  // 80ms/4.6052
	d.sd.noiseDec = decayCoef(0.03257) // 150ms/4.6052
	d.sd.bp.set(3300, 1200)            // 중심 √(3.3k·1.2k) ≈ 2kHz
	d.ch.dec = decayCoef(0.008686)     // 40ms/4.6052
	d.oh.dec = decayCoef(0.076013)     // 350ms/4.6052
	d.ch.gain = 0.50
	d.oh.gain = 0.55
	d.cp.fastDec = decayCoef(0.0025) // 첫 3연타(계곡이 조용해야 연타가 또렷하다)
	d.cp.tailDec = decayCoef(0.039)  // 180ms/4.6052
	d.cp.bp.set(3600, 400)           // 중심 √(3.6k·0.4k) ≈ 1.2kHz(넓은 스커트)
	d.cy.dec = decayCoef(0.217147)   // 1.0s/4.6052
	d.cy.atkDec = decayCoef(0.010)
	for v := 0; v < NumDrums; v++ {
		d.setParam(v, 0, 0.8) // Level 기본(엔진 기본 파라미터와 같은 값)
		d.setParam(v, 1, 0.5) // Tune 기본 → 계수 유도
	}
}

func (d *drumKit) setParam(voice, which int, q float32) {
	if voice < 0 || voice >= NumDrums {
		return
	}
	if q != q || q < 0 { // NaN 방어 포함
		q = 0
	}
	if q > 1 {
		q = 1
	}
	if which == 0 {
		switch voice {
		case 0:
			d.bd.level = q
		case 1:
			d.sd.level = q
		case 2:
			d.ch.level = q
		case 3:
			d.oh.level = q
		case 4:
			d.cp.level = q
		case 5:
			d.cy.level = q
		}
		return
	}
	switch voice {
	case 0: // f0 = 45·2^q(45..90Hz, q=0.5 → 63.6Hz)
		d.bd.f0 = mul32(45, exp2(q))
	case 1: // 톤 f = 150·2^q(150..300Hz)
		f := mul32(150, exp2(q))
		d.sd.inc1 = mul32(f, 8.3333333e-5)
		d.sd.inc2 = mul32(mul32(1.6, f), 8.3333333e-5)
	case 2, 3: // HPF 컷 = 4000·2.5^q(4k..10kHz, q=0.5 → 6.3kHz), log2(2.5) = 1.3219281
		g := lpCoef(mul32(4000, exp2(mul32(q, 1.3219281))))
		if voice == 2 {
			d.ch.hpG = g
		} else {
			d.oh.hpG = g
		}
	case 4: // 연타 간격 = 6ms·(14/6)^q(6..14ms), log2(14/6) = 1.2223943
		sec := mul32(0.006, exp2(mul32(q, 1.2223943)))
		d.cp.interval = int32(mul32(sec, SampleRate))
	case 5: // 밴드 중심 ×(0.7·2^q)(×0.7..×1.4)
		m := mul32(0.7, exp2(q))
		d.cy.bp1.set(mul32(4200, m), mul32(2100, m)) // 중심 ~3kHz
		d.cy.bp2.set(mul32(11000, m), mul32(5500, m)) // 중심 ~8kHz
	}
}

func (d *drumKit) trigger(voice int, accent bool) {
	if voice < 0 || voice >= NumDrums {
		return
	}
	acc := float32(1)
	if accent {
		acc = 1.3
	}
	switch voice {
	case 0:
		d.bd.trigger(acc, accent)
	case 1:
		d.sd.trigger(acc)
	case 2:
		d.ch.trigger(acc)
		d.metal.detune(&d.rng)
	case 3:
		d.oh.trigger(acc)
		d.metal.detune(&d.rng)
	case 4:
		d.cp.trigger(acc)
	case 5:
		d.cy.trigger(acc)
		d.metal.detune(&d.rng)
	}
}

// process — 보이스별 고정 슬롯 6회 추첨(활성 여부와 무관) → 금속원 1회 진행 →
// 각 보이스 원본 × 액센트 × 레벨을 mix에 합산. bd는 BD의 mix 기여와 동일(사이드체인).
// 각 항을 last[v]에도 남긴다(레벨 미터 원본) — mix에 더하는 피연산자를 변수로 옮겨
// 더하는 것은 같은 연산·같은 순서라 출력 바이트가 불변이다.
func (d *drumKit) process(n *xorshift32) (float32, float32) {
	var w [6]uint32
	for i := range w {
		w[i] = n.next()
	}
	m := d.metal.sample(noiseBipolar(w[3]))
	bd := d.bd.process(noiseBipolar(w[0]))
	bd = mul32(mul32(bd, d.bd.acc), mul32(0.36, d.bd.level))
	d.last[0] = bd
	mix := bd
	t := mul32(mul32(d.sd.process(noiseBipolar(w[1])), d.sd.acc), d.sd.level)
	d.last[1] = t
	mix = mix + t
	t = mul32(mul32(d.ch.process(m), d.ch.acc), d.ch.level)
	d.last[2] = t
	mix = mix + t
	t = mul32(mul32(d.oh.process(m), d.oh.acc), d.oh.level)
	d.last[3] = t
	mix = mix + t
	t = mul32(mul32(d.cp.process(noiseBipolar(w[2])), d.cp.acc), d.cp.level)
	d.last[4] = t
	mix = mix + t
	t = mul32(mul32(d.cy.process(m, noiseBipolar(w[4])), d.cy.acc), d.cy.level)
	d.last[5] = t
	mix = mix + t
	return mix, bd
}
