// voice.go — 베이스라인 보이스(T1a 소유 파일). engine.go가 호출하는 메서드 집합이 계약이다:
//
//	init(seed)                      New/Reset — 상태 0, 계수 기본
//	setParam(k int, q float32)      k = BTune..BOct(0..7), q = 양자화된 0..1
//	noteOn(note, accent, slide, nextNote, stepSamples)  스텝 트리거. slide면 stepSamples 동안 nextNote로 포르타멘토
//	noteOnChord(n1, n2, n3, accent) CHORD 모드 3음 패러포닉 트리거(아래 계약)
//	noteOff()                       게이트 오프(지수 릴리즈)
//	process(dropOct float32) float32   샘플 1개. dropOct(0..1)은 컷오프 +dropOct 옥타브 부스트
//
// note 인자 도메인(Phase 2, harmony.go): 엔진이 ResolveNote 결과(C1 기준 반음
// 0..MaxSemis 48)를 넣는다. 클램프도 MaxSemis 기준(구 MaxNote 36이 아니다).
//
// 제품 보이스(docs/impl-plan-2026-09-05.md §2.6): 톱니/사각 PolyBLEP 오실레이터, 다이오드
// 래더 4폴(filter.go), 앰프·필터 디케이 엔벨로프, 액센트(레벨 +0.6·BAccent, 필터 깊이
// +1옥타브·BAccent, 디케이 ×0.5), 슬라이드(로그 주파수 보간 포르타멘토).
//
// 슬라이드 구현: inc를 매 샘플 비율 곱하는 대신 기준 노트 상대 옥타브 오프셋을
// 샘플마다 등가 증분(slideInc)해 inc = inc0·exp2(offset)로 계산한다. 이유: exp2 상대
// 오차 ~1.5e-4가 매 샘플 곱으로 누적되면 5538샘플(130BPM 한 스텝)에서 주파수 오차
// ~48%로 커진다(τ 1000ms→469ms 사고와 같은 계열 — 감쇠 계수 국소 전개 규칙의 원인).
// 절대 오프셋 방식의 오차는 exp2 1회분(~0.26센트)에 머문다.
//
// CHORD 모드(§12.1): 오실레이터 3개가 필터 하나 앞에서 합산된다(스케일 0.5 — 3음
// 합이 단음 대비 진폭 상한을 넘지 않게). 엔벨로프·액센트·필터는 단음 noteOn과 같은
// 것을 공유한다. 슬라이드는 없다 — 노트온만(패러포닉 슬라이드는 화성을 흐려 제외).
// BASS/ARP 모드에서 보조 오실레이터는 기여 0이다: process가 chord3 분기로 항을
// 아예 더하지 않는다(+0이 아니라 분기 — 단음 경로 비트 동일성 게이트가 지킨다).
//
// 릴리즈: 계약 문장은 "8ms 지수 릴리즈", 수치 게이트는 384샘플 ≤40%·960샘플 ≤5%.
// τ=384샘플이면 960샘플에서 e^−2.5 = 8.2%로 5% 게이트를 만족하지 못한다(스펙 내부
// 충돌) — 게이트를 택해 τ = 288샘플(6ms): 384샘플→26.4%, 960샘플→3.6%.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
package engine

type bassVoice struct {
	// 오실레이터
	phase    float32 // 위상 누산 [0,1)
	inc0     float32 // 기준 노트 위상 증분(튠·옥타브 포함, 슬라이드 오프셋 적용 전)
	slideOff float32 // 현재 피치 오프셋(옥타브, inc0 기준) — 슬라이드 중에만 ≠0
	slideInc float32 // 오프셋 증분(옥타브/샘플) — 로그 보간의 등가 선형 증분
	slideN   int32   // 남은 슬라이드 샘플 수
	note     uint8   // 직전 기준 노트(슬라이드 잔여 오프셋 환산용)
	square   bool    // BWave ≥ 0.5

	// 패러포닉 보조 오실레이터(CHORD 모드 전용). BASS/ARP에선 chord3=false라
	// process가 이 경로를 건너뛴다(파일 주석 계약).
	phase2, phase3 float32
	inc2, inc3     float32
	chord3         bool

	// 엔벨로프(트리거 시 값 설정, 샘플마다 감쇠)
	aenv, fenv float32
	ampDec     float32 // 앰프 감쇠 계수(노트별 — 액센트 τ×0.5 반영)
	fltDec     float32 // 필터 감쇠 계수(앰프의 3배 느림)
	relDec     float32 // 릴리즈 계수(τ=288샘플, 파일 주석 유도)
	releasing  bool
	active     bool    // 첫 noteOn 전에만 false — 릴리즈 후에도 링이 살아있어야 한다
	envDepth   float32 // 이 노트의 필터 엔벨로프 깊이(옥타브, 액센트 보너스 포함)

	// setParam 유도 계수
	tuneMul   float32 // 2^(튠 반음/12)
	cutBaseHz float32 // 200·40^q
	envOct    float32 // 4·BEnvMod
	tauSamp   float32 // 앰프 디케이 시정수(샘플) 30ms..2s 지수
	accent    float32 // BAccent
	octMul    float32 // BOct 3단

	filt ladderFilter
}

func (v *bassVoice) init(seed uint32) {
	*v = bassVoice{
		tuneMul:   1,
		octMul:    1,
		cutBaseHz: 1000,
		tauSamp:   24000, // 기본 τ 500ms — Reset이 DefaultParams로 곧 덮어쓴다
		ampDec:    0.9999,
		fltDec:    0.99997,
		envDepth:  2,
		relDec:    0.9965338, // 유도: 1 − 1/288 + 1/(2·288²) = 0.9965338 (τ=288샘플)
	}
	_ = seed // 보이스는 엔트로피 없음(결정론) — 시그니처는 engine.Reset 계약
}

func (v *bassVoice) setParam(k int, q float32) {
	switch ParamID(k) {
	case BTune: // ±12반음(0.5 = 0)
		semi := mul32(q, 24) - 12
		v.tuneMul = exp2(semi / 12)
	case BCutoff: // 200Hz..8kHz 지수: 200·40^q, log2(40) = 5.3219281
		v.cutBaseHz = mul32(200, exp2(mul32(q, 5.3219281)))
	case BReso: // 0..자기발진 직전: k = 1.6·q(안정성 유도는 filter.go 파일 주석)
		v.filt.k = mul32(q, 1.6)
	case BEnvMod: // 엔벨로프 모드 0..4옥타브
		v.envOct = mul32(q, 4)
	case BDecay: // 30ms..2s 지수 — 시정수만 저장, 감쇠 계수는 noteOn에서 유도(액센트 ×0.5 때문)
		sec := mul32(0.03, exp2(mul32(q, 6.0588937))) // log2(2s/30ms) = 6.0588937
		v.tauSamp = mul32(sec, SampleRate)
	case BAccent:
		v.accent = q
	case BWave: // <0.5 톱니, ≥0.5 사각
		v.square = q >= 0.5
	case BOct: // 3단: <1/3 → −1옥타브, <2/3 → 0, 그 외 +1
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

// baseInc — note의 위상 증분(튠·옥타브 포함). note 0..MaxSemis(48) → MIDI 24+note
// (24 = C1 = 32.70Hz, 72 = C5 = 523.25Hz). 최대 inc ≈ 0.0109 < 0.022이라 랩 1회로 충분.
func (v *bassVoice) baseInc(note uint8) float32 {
	m := int32(note) + 24
	o := float32(m-69) / 12
	f := mul32(exp2(o), 440)
	f = mul32(f, v.tuneMul)
	return mul32(f, v.octMul) / SampleRate
}

// trigger — 비레가토 noteOn의 엔벨로프 시작. 감쇠 계수는 국소 전개 1 + u + u²/2
// (u = −1/τ샘플)로 계산한다: exp2 결과를 매 샘플 곱하면 상대 오차 ~1.5e-4가
// τ·SR샘플만큼 누적되어 시정수가 크게 어긋난다(τ 1000ms→469ms 사고). 전개 절단
// 오차는 u³/6 ≤ 5.6e-11(τ=30ms)로 무시 가능.
func (v *bassVoice) trigger(accent bool) {
	lv := float32(0.4)
	depth := v.envOct
	tau := v.tauSamp
	if accent {
		lv += mul32(0.6, v.accent) // 레벨 +0.6·BAccent
		depth += v.accent          // 필터 엔벨로프 깊이 +1옥타브·BAccent
		tau = mul32(tau, 0.5)      // 디케이 ×0.5
	}
	if lv > 1 {
		lv = 1
	}
	v.aenv = lv
	v.fenv = 1
	v.envDepth = depth
	u := -1 / tau
	u2 := mul32(u, u)
	v.ampDec = mul32(u2, 0.5) + u + 1
	uf := -1 / mul32(tau, 3) // 필터 디케이는 앰프의 3배 느림
	uf2 := mul32(uf, uf)
	v.fltDec = mul32(uf2, 0.5) + uf + 1
}

// freezePitch — 슬라이드 종료 시 잔여 오프셋을 inc0에 베이크해 상태를 단순화한다.
// 베이크 1회의 exp2 오차 ~1.5e-4(≈0.26센트) — 누적 아님.
func (v *bassVoice) freezePitch() {
	if v.slideOff != 0 {
		v.inc0 = mul32(v.inc0, exp2(v.slideOff))
		v.slideOff = 0
	}
	v.slideInc = 0
	v.slideN = 0
}

// corr — PolyBLEP 2샘플 보정항(Välimäki 다항). t는 에지 기준 위상 거리(에지가
// t=0/t=1 경계에 놓인다), dt는 위상 증분. 반환은 [−1,1] — 하강 에지는 빼고,
// 상승 에지는 더해 쓴다(보정 후 불연속이 ±2→0으로 매끈해진다).
func corr(t, dt float32) float32 {
	if t < dt {
		u := t / dt
		u2 := mul32(u, u)
		return mul32(u, 2) - u2 - 1
	}
	if t > 1-dt {
		u := (1 - t) / dt
		u2 := mul32(u, u)
		return 1 - mul32(u, 2) + u2
	}
	return 0
}

func (v *bassVoice) noteOn(note uint8, accent, slide bool, nextNote uint8, stepSamples float64) {
	if note > MaxSemis {
		note = MaxSemis // 도메인 = ResolveNote 출력 0..MaxSemis(파일 주석)
	}
	if nextNote > MaxSemis {
		nextNote = MaxSemis
	}
	// 슬라이드 노트도 자기 음에서 시작한다(계약: "현재 노트에서 nextNote로"). 잔여
	// 슬라이드 오프셋은 베이크해 새 노트로 점프 — 연속 슬라이드 체인에서는 직전
	// 글라이드가 이 노트에 도달해 있으므로 점프가 무음(no-op)이다.
	v.freezePitch()
	v.note = note
	v.inc0 = v.baseInc(note)
	v.chord3 = false // 단음 경로 — 보조 오실레이터 기여 0(분기로 건너뜀, 파일 주석)
	v.inc2, v.inc3 = 0, 0
	if slide {
		n := int32(stepSamples)
		if n < 1 {
			n = 1
		}
		v.slideOff = 0
		tgt := float32(int32(nextNote)-int32(note)) / 12 // 목표 옥타브 오프셋(반음은 정수라 무손실)
		v.slideInc = tgt / float32(n)
		v.slideN = n
		// 레가토(계약): 앰프·필터 엔벨로프 재트리거 없이 이어진다. 단 첫 스텝이
		// slide면 무성음이 되므로 한 번도 트리거한 적 없을 때만 시작한다.
		if !v.active {
			v.trigger(accent)
		}
	} else {
		v.trigger(accent) // slide=false: 즉시 점프 + 재트리거
	}
	v.releasing = false
	v.active = true
}

// noteOnChord — CHORD 모드 3음 패러포닉 트리거. 엔진이 ResolveNote 결과(C1 기준
// 반음) 3개를 준다. 세 오실레이터가 필터 하나 앞에서 합산되고(스케일 0.5),
// 엔벨로프·액센트·필터는 단음 noteOn과 같은 경로를 공유한다. 슬라이드는 없다 —
// 노트온만(CHORD 모드 계약, 파일 주석).
func (v *bassVoice) noteOnChord(n1, n2, n3 uint8, accent bool) {
	if n1 > MaxSemis {
		n1 = MaxSemis
	}
	if n2 > MaxSemis {
		n2 = MaxSemis
	}
	if n3 > MaxSemis {
		n3 = MaxSemis
	}
	v.freezePitch()
	v.note = n1
	v.inc0 = v.baseInc(n1)
	v.inc2 = v.baseInc(n2)
	v.inc3 = v.baseInc(n3)
	v.chord3 = true
	v.trigger(accent)
	v.releasing = false
	v.active = true
}

func (v *bassVoice) noteOff() {
	v.freezePitch() // 포르타멘토 정지 — 현재 피치 유지
	v.releasing = true
}

// oscSample — 위상 1개의 PolyBLEP 파형 샘플(톱니/사각). 단음과 패러포닉 합산이
// 같은 파형 코드를 쓴다(두 경로 드리프트 금지). 곱셈-덧셈은 mul32 규칙 그대로.
func (v *bassVoice) oscSample(ph, inc float32) float32 {
	if v.square {
		var osc float32
		if ph < 0.5 {
			osc = 1
		} else {
			osc = -1
		}
		osc = osc + corr(ph, inc) // p=0 상승 에지 보정(가산)
		pe := ph + 0.5
		if pe >= 1 {
			pe -= 1
		}
		return osc - corr(pe, inc) // p=0.5 하강 에지 보정(감산)
	}
	osc := mul32(2, ph) - 1
	return osc - corr(ph, inc) // p=1→0 하강 에지 보정(감산)
}

func (v *bassVoice) process(dropOct float32) float32 {
	if !v.active {
		return 0
	}
	// 오실레이터 — 슬라이드 중엔 절대 오프셋에서 inc 계산(파일 주석: 곱누적 오차 회피)
	inc := v.inc0
	if v.slideN > 0 {
		v.slideN--
		v.slideOff += v.slideInc
		if v.slideN == 0 {
			v.freezePitch() // 도달 — 잔여 오프셋 베이크(1회)
			inc = v.inc0
		} else {
			inc = mul32(v.inc0, exp2(v.slideOff))
		}
	}
	ph := v.phase + inc
	if ph >= 1 {
		ph -= 1 // inc ≤ 0.022(최고음)라 한 번의 랩으로 충분
	}
	v.phase = ph
	var osc float32
	if v.chord3 {
		// CHORD — 보조 2오실레이터 합산(파일 주석 계약). inc2/inc3는 슬라이드가
		// 없으므로 상수다.
		ph2 := v.phase2 + v.inc2
		if ph2 >= 1 {
			ph2 -= 1
		}
		v.phase2 = ph2
		ph3 := v.phase3 + v.inc3
		if ph3 >= 1 {
			ph3 -= 1
		}
		v.phase3 = ph3
		sum := v.oscSample(ph, inc) + v.oscSample(ph2, v.inc2) + v.oscSample(ph3, v.inc3)
		osc = mul32(sum, 0.5)
	} else {
		osc = v.oscSample(ph, inc) // 단음 경로 — 보조 오실레이터 항 없음(비트 동일성)
	}
	// 엔벨로프 — 릴리즈 중엔 앰프만 감쇠(필터 엔벨로프 동결, τ=288샘플)
	if v.releasing {
		v.aenv = v.aenv * v.relDec
	} else {
		v.aenv = v.aenv * v.ampDec
		v.fenv = v.fenv * v.fltDec
	}
	sig := mul32(osc, v.aenv)
	oct := mul32(v.fenv, v.envDepth) + dropOct
	hz := mul32(v.cutBaseHz, exp2(oct))
	if hz > 16000 {
		hz = 16000
	}
	if hz < 20 {
		hz = 20
	}
	// 래더 상태 클램프 |s| ≤ 4(filter.go) × 게인 0.24 → |출력| ≤ 0.96 < 1.0(계약 5)
	return mul32(v.filt.process(sig, hz), 0.24)
}
