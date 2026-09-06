// poly.go — 폴리 리드 신스(P5-poly-dsp 소유 파일). engine.go(리드)가 호출하는 메서드 집합이 계약이다:
//
//	init(seed uint32)                          New/Reset — 상태 0, 계수 기본(기본값 유도는 DefaultPolyParams를 setParam과 같은 경로로 — 리드가 Reset에서 다시 적용해도 비트 동일)
//	setParam(k int, q float32)                 k = PolyCutoff..PolyLevel(0..7), q = 양자화된 0..1 (범위 밖 k 무시, NaN/음수 0, >1 → 1)
//	noteOn(voice int, note uint8, accent bool) voice 0..3(범위 밖 무시), note = C1 기준 반음 0..MaxSemis(48) 클램프
//	noteOff(voice int)                         그 보이스 게이트 오프 → 릴리즈
//	allOff()                                   전 보이스 릴리즈(Transport 정지)
//	process() float32                          샘플 1개(모노). 4보이스 합 × Level. 첫 noteOn 전에는 정확히 0(+0)
//	active() bool                              한 보이스라도 소리 중(릴리즈 포함)
//
// 구조(impl-plan §14.2 항목 1): 보이스 4개, 보이스마다 디튠 톱니 3개(중심·+d·−d — 슈퍼소
// 두께)가 보이스별 다이오드 래더(filter.go) 하나로 합산된다. 앰프 엔벌로프는 어택(선형)·
// 서스테인·릴리즈(지수), 필터 엔벌로프는 noteOn마다 fenv=1 재트리거하는 디케이형. 액센트 =
// 레벨 ×1.3(비액센트 목표 1/1.3, 액센트 1.0) + 필터 깊이 +0.5옥타브.
//
// 계수 유도 시점: 감쇠(릴리즈·필터 디케이)는 voice.go trigger()처럼 국소 전개 1 + u + u²/2
// (u = −1/τ샘플)로 setParam에서 1회 유도한다 — exp2 근사 오차(~1.5e-4)가 매 샘플 곱으로
// 누적되면 시정수가 어긋난다(τ 1000ms→469ms 사고, voice.go 파일 주석). 어택은 선형 증분
// 1/(attackSec·SampleRate), 디튠 비율 2^(±d/1200)도 setParam에서 유도한다. 핫 루프의 exp2는
// 컷오프 hz 계산 1회뿐 — voice.go·filter.go와 같은 기존 패턴.
//
// 베이스(voice.go)와의 차이 세 가지: ① 어택이 있는 게이트 서스테인 엔벌로프(베이스는
// 디케이형). ② 필터 엔벌로프는 릴리즈 중에도 계속 디케이한다(베이스는 동결 — 베이스 릴리즈
// τ 6ms라 상관없지만 폴리는 20ms..2s라 동결하면 필터 플룸이 릴리즈 내내 붙는다). ③ 재트리거·
// 게이트 온 모두 엔벌로프가 현재 레벨에서 이어진다(점프 없음 — 어택은 상승·하강 같은 증분).
// 위상은 어떤 noteOn에서도 리셋하지 않는다. 릴리즈가 1e-5 밑으로 떨어진 보이스는 aenv=0·
// 비활성 — process가 오실레이터·필터 계산을 건너뛰되 래더 상태는 남겨 다음 noteOn에서 이어 쓴다.
//
// 진폭 유도(계약): 보이스 하나 = 래더 출력(|s4| ≤ 4 — filter.go 상태 클램프) × 0.24 ≤ 0.96.
// 4보이스 합 ≤ 3.84 > 1 → 합에 유리식 소프트클립 polyClip(|x| ≤ 8에서 최대 1.20763)을 걸고
// ×0.82(fx.go outClipScale)·Level을 곱해 |출력| ≤ 0.99026 ≤ 1.0을 상수로 보장. 합에 0.5를
// 곱지 않는다(리드 재핀 2026-09-06: ×0.5에서 기본 랙 폴리 RMS −33.7 dB — 베이스 −30.5 아래로
// 묻혔다. 통상 4보이스 합 ≤ 0.6이라 클립은 −1 dB 안의 부드러운 압축이다). 1보이스 이론 상한
// 0.96·1.2076·0.82·Level — Level 0.8이면 0.76.
//
// 이 장치는 엔트로피가 없다(init의 seed는 그래프 시그니처 계약). 외부 바이트를 읽지 않는다.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
package engine

// 폴리 파라미터 k — 장치 로컬 0..7이다. params.go의 전역 ParamID에 추가하지 않는다
// (그래프 라운드의 몫 — 이 장치의 k 공간은 전역 ID와 독립이다).
const (
	PolyCutoff  = 0 // 200Hz..8kHz 지수 = 200·40^q(voice.go BCutoff와 같은 식)
	PolyReso    = 1 // 래더 k = 1.6·q(voice.go BReso — 상한 근거는 filter.go 안정성 유도)
	PolyEnvMod  = 2 // 필터 엔벌로프 깊이 0..4옥타브
	PolyAttack  = 3 // 앰프 어택 1ms..300ms 지수 = 0.001·300^q(0.5 ≈ 17ms)
	PolyDecay   = 4 // 필터 엔벌로프 디케이 시정수 30ms..2s 지수(voice.go BDecay와 같은 식)
	PolyRelease = 5 // 앰프 릴리즈 시정수 20ms..2s 지수 = 0.02·100^q
	PolyDetune  = 6 // 슈퍼소 디튠 0..25센트(선형 q·25) — 3오실레이터 중심·+d·−d
	PolyLevel   = 7 // 출력 레벨 0..1 선형(process 마지막에 곱)

	PolyParams = 8
)

// DefaultPolyParams — 폴리 기본값(리드가 Reset에서 setParam으로 적용한다).
// 어택 2.4ms·디튠 12.5센트의 살짝 웻한 트랜스 리드(사용자 2026-09-06).
func DefaultPolyParams() [PolyParams]float32 {
	return [PolyParams]float32{0.55, 0.35, 0.45, 0.15, 0.5, 0.5, 0.5, 0.8}
}

const (
	polyVoices  = 4    // 보이스 수
	polySilence = 1e-5 // 릴리즈 종료 역치 — 12τ(e^−12 = 6.1e-6)면 닿는다
)

// polyVoice — 폴리 보이스 1개. 위상 3개가 래더 하나로 합산된다.
type polyVoice struct {
	phC, phU, phD    float32 // 위상 3개(중심·+디튠·−디튠) — noteOn에서 리셋하지 않는다(연속성 계약)
	incC, incU, incD float32 // 위상 증분 — incU/incD = incC·detUp/detDown(noteOn에서 유도)

	aenv, fenv float32 // 앰프(어택·서스테인·릴리즈)·필터(noteOn 재트리거 디케이) 엔벌로프
	peak       float32 // 이 노트의 앰프 목표(액센트 1.0 / 비액센트 1/1.3)
	envDepth   float32 // 이 노트의 필터 엔벌로프 깊이(옥타브 — 4·PolyEnvMod + 액센트 0.5)
	gate       bool    // 게이트 온(어택·서스테인 중)
	on         bool    // 소리 중(릴리즈 포함) — process·active가 보는 비트

	filt ladderFilter // 보이스별 래더 — 비활성화 후에도 상태 유지, 다음 noteOn에서 이어 씀
}

// polySynth — 폴리 리드 신스 장치. 유도 계수는 전부 setParam이 채운다.
type polySynth struct {
	voices [polyVoices]polyVoice

	cutBaseHz float32 // 200·40^q
	envOct    float32 // 4·PolyEnvMod
	atkInc    float32 // 어택 선형 증분 = 1/(attackSec·SampleRate) — 0→1 소요가 attackSec
	fDec      float32 // 필터 엔벌로프 감쇠 계수(국소 전개)
	relCoef   float32 // 릴리즈 감쇠 계수(국소 전개)
	detUp     float32 // 2^(+d/1200), d = 25·q 센트
	detDown   float32 // 2^(−d/1200)
	level     float32 // PolyLevel
}

func (p *polySynth) init(seed uint32) {
	*p = polySynth{} // 전 상태 0 — 래더 0값이 곧 초기 상태(filter.go)
	// 계수 기본: DefaultPolyParams를 setParam과 같은 유도 경로로 적용한다(비트 동일성 —
	// 리드가 Reset에서 다시 setParam으로 적용해도 같은 상태가 되어야 한다).
	d := DefaultPolyParams()
	for k := 0; k < PolyParams; k++ {
		p.setParam(k, d[k])
	}
	_ = seed // 이 장치는 엔트로피 없음(결정론) — 시그니처는 그래프 계약
}

func (p *polySynth) setParam(k int, q float32) {
	if q != q || q < 0 { // NaN·음수 → 0(params.go quantize와 같은 방어)
		q = 0
	}
	if q > 1 {
		q = 1
	}
	switch k {
	case PolyCutoff:
		p.cutBaseHz = mul32(200, exp2(mul32(q, 5.3219281))) // log2(40) = 5.3219281
	case PolyReso:
		for i := 0; i < polyVoices; i++ {
			p.voices[i].filt.k = mul32(q, 1.6)
		}
	case PolyEnvMod:
		p.envOct = mul32(q, 4)
	case PolyAttack:
		sec := mul32(0.001, exp2(mul32(q, 8.2288187))) // log2(300) = 8.2288187
		p.atkInc = 1 / mul32(sec, SampleRate)
	case PolyDecay:
		sec := mul32(0.03, exp2(mul32(q, 6.0588937))) // log2(2s/30ms) = 6.0588937
		p.fDec = polyDecayCoef(mul32(sec, SampleRate))
	case PolyRelease:
		sec := mul32(0.02, exp2(mul32(q, 6.6438562))) // log2(100) = 6.6438562
		p.relCoef = polyDecayCoef(mul32(sec, SampleRate))
	case PolyDetune:
		d := mul32(q, 25)
		p.detUp = exp2(d / 1200)
		p.detDown = exp2(-d / 1200)
	case PolyLevel:
		p.level = q
	}
}

// polyDecayCoef — 지수 감쇠 계수를 국소 전개 1 + u + u²/2 (u = −1/τ샘플)로 유도한다.
// exp2 결과를 매 샘플 곱하면 근사 오차가 τ·SR샘플만큼 누적되어 시정수가 어긋난다
// (voice.go trigger 주석의 사고 기록). 전개 절단 오차는 u³/6 ≤ 1.9e-10(τ=20ms)으로 무시 가능.
func polyDecayCoef(tauSamp float32) float32 {
	u := -1 / tauSamp
	u2 := mul32(u, u)
	return mul32(u2, 0.5) + u + 1
}

// baseInc — note의 위상 증분. voice.go baseInc와 같은 공식(MIDI 24+note, 440Hz 기준)에서
// 튠·옥타브 곱만 뺀 것(폴리는 note 자체가 옥타브를 갖는다). 최고음 note 48 → 523.25Hz →
// inc 0.0109; 최대 디튠 2^(25/1200) = 1.0145를 쳐도 0.0111 ≪ 0.5 — 랩 1회 가정 유지.
func (*polySynth) baseInc(note uint8) float32 {
	m := int32(note) + 24
	o := float32(m-69) / 12
	f := mul32(exp2(o), 440)
	return f / SampleRate
}

func (p *polySynth) noteOn(voice int, note uint8, accent bool) {
	if voice < 0 || voice >= polyVoices {
		return
	}
	if note > MaxSemis {
		note = MaxSemis // 도메인 = ResolveNote 출력 0..MaxSemis(voice.go와 같은 클램프)
	}
	v := &p.voices[voice]
	inc := p.baseInc(note)
	v.incC = inc
	v.incU = mul32(inc, p.detUp)
	v.incD = mul32(inc, p.detDown)
	// 엔벌로프: aenv는 현재 레벨에서 이어진다(여기서 손대지 않는다 — 재트리거 무점프).
	v.fenv = 1
	v.envDepth = p.envOct
	if accent {
		v.envDepth += 0.5
		v.peak = 1
	} else {
		v.peak = 1 / 1.3 // 액센트 ×1.3의 역(상한 1.0 유지)
	}
	v.gate = true
	v.on = true
}

func (p *polySynth) noteOff(voice int) {
	if voice < 0 || voice >= polyVoices {
		return
	}
	p.voices[voice].gate = false // 릴리즈 진입 — aenv는 현재 레벨에서 감쇠
}

func (p *polySynth) allOff() {
	for i := 0; i < polyVoices; i++ {
		p.voices[i].gate = false // Transport 정지 — 전 보이스 릴리즈
	}
}

// silence — 보이스 상태만 즉시 0으로(계수·파라미터는 유지). 랙에서 뽑히거나 꽂힐 때 리드가
// 부른다 — sampler.go silence와 같은 이유·같은 계약(뽑힌 장치는 process()를 못 받아 릴리즈가
// 진행되지 않는다). 래더 상태도 함께 지운다: 재장착 시 옛 필터 상태에서 이어 쓰면 튄다.
func (p *polySynth) silence() {
	for i := 0; i < polyVoices; i++ {
		k := p.voices[i].filt.k // 레조넌스는 setParam이 채운 계수라 보존한다
		p.voices[i] = polyVoice{}
		p.voices[i].filt.k = k
	}
}

func (p *polySynth) active() bool {
	for i := 0; i < polyVoices; i++ {
		if p.voices[i].on {
			return true
		}
	}
	return false
}

// polySaw — PolyBLEP 톱니 1개. voice.go oscSample의 톱니 경로와 같은 식(corr는 같은
// 패키지 함수를 그대로 쓴다 — 복제 금지). 반환 [−1,1].
func polySaw(ph, inc float32) float32 {
	osc := mul32(2, ph) - 1
	return osc - corr(ph, inc)
}

// polyAvg3 — 세 오실레이터의 산술평균(3개 합 × 1/3). f64 합·나눗셈으로 정확히 계산한다:
// 세 값이 같으면(디튠 0) 반환값이 단일 톱니와 비트까지 같다(게이트 계약). 곱셈이 없어
// FMA 융합 여지도 없다(덧셈·나눗셈뿐 — mul32 장벽이 필요 없는 유일한 형태).
func polyAvg3(a, b, c float32) float32 {
	return float32((float64(a) + float64(b) + float64(c)) / 3)
}

// polyClip — 출력 단 유리식 소프트클립 x(27+x²)/(27+9x²), |x| ≤ 8 클램프(fx.go outClip의
// 유리식과 같은 식 — 스케일 곱은 호출측). |x| = 8에서 최대 1.20763.
func polyClip(x float32) float32 {
	if x > 8 {
		x = 8
	}
	if x < -8 {
		x = -8
	}
	x2 := mul32(x, x)
	return mul32(x, 27+x2) / (mul32(9, x2) + 27)
}

// voiceSample — 활성 보이스 1개 샘플. 신호 흐름은 voice.go process와 같다:
// 3오실레이터 평균 → 앰프 엔벌로프 → 보이스별 래더 → ×0.24.
func (p *polySynth) voiceSample(v *polyVoice) float32 {
	phC := v.phC + v.incC
	if phC >= 1 {
		phC -= 1
	}
	v.phC = phC
	phU := v.phU + v.incU
	if phU >= 1 {
		phU -= 1
	}
	v.phU = phU
	phD := v.phD + v.incD
	if phD >= 1 {
		phD -= 1
	}
	v.phD = phD
	osc := polyAvg3(polySaw(phC, v.incC), polySaw(phU, v.incU), polySaw(phD, v.incD))
	if v.gate {
		// 어택 — 현재 레벨에서 peak로 선형(0→1 소요 = attackSec). 재트리거로 peak가
		// 내려가는 경우(액센트→비액센트)도 같은 증분으로 내려간다(점프 없음).
		if v.aenv < v.peak {
			v.aenv += p.atkInc
			if v.aenv > v.peak {
				v.aenv = v.peak
			}
		} else if v.aenv > v.peak {
			v.aenv -= p.atkInc
			if v.aenv < v.peak {
				v.aenv = v.peak
			}
		}
	} else {
		// 릴리즈 — 현재 레벨에서 지수 감쇠. 1e-5 밑에서 종료(래더 상태는 유지).
		v.aenv = v.aenv * p.relCoef
		if v.aenv < polySilence {
			v.aenv = 0
			v.on = false
		}
	}
	v.fenv = v.fenv * p.fDec
	sig := mul32(osc, v.aenv)
	oct := mul32(v.fenv, v.envDepth)
	hz := mul32(p.cutBaseHz, exp2(oct))
	if hz > 16000 {
		hz = 16000
	}
	if hz < 20 {
		hz = 20
	}
	return mul32(v.filt.process(sig, hz), 0.24)
}

func (p *polySynth) process() float32 {
	var sum float32
	for i := 0; i < polyVoices; i++ {
		v := &p.voices[i]
		if !v.on {
			continue // 비활성 보이스는 오실레이터·필터 계산을 건너뛴다(래더 상태 유지)
		}
		sum += p.voiceSample(v)
	}
	// 합산 소프트클립(파일 주석 유도): clip(합) × 0.82 × Level — 모든 보이스가 꺼진
	// 창에는 합이 +0이라 출력도 정확히 +0(첫 noteOn 전 계약).
	return mul32(polyClip(sum), mul32(outClipScale, p.level))
}
