// sampler.go — 샘플러 재생 장치(P5-sampler-dev 소유 파일). engine.go(리드)가 호출하는 메서드
// 집합이 계약이다:
//
//	init(seed uint32)               New/Reset — 상태 0, 계수 기본(DefaultSamplerParams를 setParam과 같은 경로로), ensurePack 1회
//	setParam(k int, q float32)      k = SmpSelect..SmpLevel(0..7), q = 양자화된 0..1 (범위 밖 k 무시, NaN/음수 0, >1 → 1)
//	noteOn(note uint8, accent bool) note = C1 기준 반음 0..MaxSemis(48) 클램프. 보이스 라운드로빈 배정
//	noteOff()                       루프 모드 보이스만 게이트 오프(원샷은 무시 — 끝까지 운다)
//	allOff()                        전 보이스 릴리즈(Transport 정지 — 원샷도 이때는 릴리즈, 위치 유지)
//	process() float32               샘플 1개(모노). 첫 noteOn 전에는 정확히 +0
//	active() bool                   한 보이스라도 소리 중(릴리즈 포함)
//
// 구조: 보이스 4개(smpVoices). noteOn은 꺼진 보이스 중 번호가 가장 작은 것에 배정하고, 전부
// 켜져 있으면 시작 순번 카운터(startSeq)가 가장 오래된 보이스를 뺏는다. 보이스는 시작 시점의
// 팩 슬롯(sel)·재생비(inc)·루프 모드(loop)를 물고 간다 — 울리는 중에 SmpSelect·SmpTune·SmpLoop를
// 바꿔도 그 보이스는 시작 조건으로 끝까지 읽는다(다음 noteOn부터 새 값).
//
// 재생: pos는 슬롯 상대 프레임(소수 포함). 읽기는 선형 보간 s = s0·(1−f) + s1·f — 볼록 조합이라
// |s| ≤ 슬롯 피크. i+1이 슬롯 끝을 넘으면 마지막 표본을 되풀이한다(유계 보장). 재생비
// inc = exp2((note − root + tune)/12)는 noteOn에서만 유도한다(voice.go와 같은 관례 — exp2는
// 트리거 경로에만 둔다). 원샷(그 슬롯이 loop == n이거나 시작 시 SmpLoop < 0.5)은 pos ≥ n−1에서
// 보이스 종료. 루프(시작 시 SmpLoop ≥ 0.5 && 슬롯 loop < n)는 pos ≥ n에서 pos −= n − loop로
// 되돌린다 — 게이트가 풀려도(릴리즈 중에도) 계속 돈다.
//
// 엔벌로프: 어택은 선형 증분 1/(attackSec·SampleRate)로 0→peak(액센트 1.0 / 비액센트 1/1.3 —
// poly.go와 같은 규칙), 도달하면 서스테인(peak 유지). 릴리즈는 국소 전개 계수(poly.go
// polyDecayCoef를 그대로 쓴다 — exp2 감쇠 계수의 누적 오차 사고는 voice.go 파일 주석)를 매
// 샘플 곱하고 aenv < smpSilence(1e-5)에서 보이스 종료. 톤은 보이스 합에 1폴 LP 하나(장치당
// 하나, 보이스마다가 아님) — 계수는 setParam에서 filter.go와 같은 식으로 1회 유도하고 편차에
// 곱해 상태에 누적되지 않는다.
//
// 진폭 유도(계약): 팩 슬롯 피크 0.9(samplerpack.go 정규화 계약) × 보간 볼록조합 ≤ 0.9 ×
// aenv ≤ peak ≤ 1 → 보이스 하나 ≤ 0.9, 4보이스 합 ≤ 3.6 ≤ 8. 합에 유리식 소프트클립
// polyClip(|x| ≤ 8에서 최대 1.20763)을 걸고 ×0.82(fx.go outClipScale)·Level을 곱해
// |출력| ≤ 1.20763·0.82·1.0 = 0.99026 ≤ 1.0을 상수로 보장.
//
// 침묵 계약: init 직후 첫 noteOn 전에는 process()가 정확히 +0(비트)이다 — 비활성 보이스는
// 계산을 건너뛰고, 0값 LP → polyClip(+0) → ×0.82×Level 체인이 +0을 보존한다(poly.go와 같은 규칙).
//
// 이 장치는 엔트로피가 없다. init의 seed는 그래프 시그니처 계약일 뿐이고 팩은 seed 무관한
// 불변 데이터다(samplerpack.go — 같은 팩이면 같은 바이트).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
package engine

// 샘플러 파라미터 k — 장치 로컬 0..7이다. params.go의 전역 ParamID에 추가하지 않는다
// (그래프 라운드의 몫 — poly.go와 같은 독립 k 공간 계약).
const (
	SmpSelect  = 0 // 팩 슬롯 0..7 = int(mul32(q, 7) + 0.5)
	SmpTune    = 1 // ±12반음 = mul32(q−0.5, 24) — 0.5가 원음
	SmpStart   = 2 // 시작 오프셋(슬롯 상대 프레임) = uint32(mul32(q, n−1)) — n은 noteOn 시점 슬롯 길이
	SmpLoop    = 3 // q ≥ 0.5 → 루프 모드(단 그 팩 슬롯이 loop < n일 때만 실제로 돈다)
	SmpAttack  = 4 // 앰프 어택 0.5ms..200ms 지수 = 0.0005·400^q
	SmpRelease = 5 // 앰프 릴리즈 시정수 20ms..2s 지수 = 0.02·100^q
	SmpTone    = 6 // 출력 1폴 LP 컷오프 200Hz..16kHz 지수 = 200·80^q
	SmpLevel   = 7 // 출력 레벨 0..1 선형

	SmpParams = 8
)

// DefaultSamplerParams — 샘플러 기본값(리드가 Reset에서 setParam으로 적용한다).
func DefaultSamplerParams() [SmpParams]float32 {
	return [SmpParams]float32{0, 0.5, 0, 0, 0.05, 0.3, 0.85, 0.8}
}

const (
	smpVoices  = 4    // 보이스 수
	smpSilence = 1e-5 // 릴리즈 종료 역치 — 12τ(e^−12 = 6.1e-6)면 닿는다(poly.go와 같은 값)
)

// smpVoice — 샘플러 보이스 1개. sel·inc·loop은 noteOn 시점에 물고 간다.
type smpVoice struct {
	pos  float32 // 재생 위치(슬롯 상대 프레임, 소수 포함)
	inc  float32 // 재생비 = exp2((note−root+tune)/12) — noteOn에서 유도
	sel  uint8   // 시작 시점에 물고 간 팩 슬롯 번호
	seq  uint32  // 시작 순번(스틸 판정 — 작을수록 오래된 것)
	aenv float32 // 앰프 엔벌로프(0→peak 선형 어택, 서스테인, 지수 릴리즈)
	peak float32 // 이 노트의 목표 진폭(액센트 1.0 / 비액센트 1/1.3)
	gate bool    // 게이트 온(어택·서스테인 중) — 원샷은 noteOff로 꺼지지 않는다
	loop bool    // 시작 시점의 루프 모드(SmpLoop ≥ 0.5 && 슬롯 loop < n)
	on   bool    // 소리 중(릴리즈 포함) — process·active가 보는 비트
}

// samplerDev — 샘플러 재생 장치. 유도 계수는 전부 setParam이 채운다.
type samplerDev struct {
	voices   [smpVoices]smpVoice
	startSeq uint32 // noteOn 시작 순번 카운터(스틸: 가장 오래된 보이스)

	sel     uint8   // SmpSelect — 다음 noteOn부터 적용되는 현재 슬롯
	tune    float32 // SmpTune 유도 ±12반음
	startQ  float32 // SmpStart q(오프셋 프레임은 noteOn에서 슬롯 길이에 맞춰 유도)
	loopQ   float32 // SmpLoop q(≥ 0.5 → 루프 모드)
	atkInc  float32 // 어택 선형 증분 = 1/(attackSec·SampleRate) — 0→1 소요가 attackSec
	relCoef float32 // 릴리즈 감쇠 계수(국소 전개 — polyDecayCoef)
	lpCoef  float32 // 출력 1폴 LP 계수(filter.go와 같은 식)
	lp      float32 // LP 상태 — 장치 하나(보이스 합에 하나)
	level   float32 // SmpLevel
}

func (s *samplerDev) init(seed uint32) {
	*s = samplerDev{} // 전 상태 0 — LP 0값이 곧 초기 상태
	ensurePack()      // 팩 굽기는 최초 1회(samplerpack.go — Reset마다 재굽기 금지)
	// 계수 기본: DefaultSamplerParams를 setParam과 같은 유도 경로로 적용한다(비트 동일성 —
	// 리드가 Reset에서 다시 setParam으로 적용해도 같은 상태가 되어야 한다. poly.go와 같은 관례).
	d := DefaultSamplerParams()
	for k := 0; k < SmpParams; k++ {
		s.setParam(k, d[k])
	}
	_ = seed // 이 장치는 엔트로피 없음 — 팩은 seed 무관(samplerpack.go), 시그니처는 그래프 계약
}

func (s *samplerDev) setParam(k int, q float32) {
	if q != q || q < 0 { // NaN·음수 → 0(params.go quantize와 같은 방어)
		q = 0
	}
	if q > 1 {
		q = 1
	}
	switch k {
	case SmpSelect:
		s.sel = uint8(int(mul32(q, 7) + 0.5)) // q ≤ 1 → 최대 int(7.5) = 7, 하한 0 — 클램프 불필요
	case SmpTune:
		s.tune = mul32(q-0.5, 24)
	case SmpStart:
		s.startQ = q
	case SmpLoop:
		s.loopQ = q
	case SmpAttack:
		sec := mul32(0.0005, exp2(mul32(q, 8.6438562))) // log2(400) = 8.6438562
		s.atkInc = 1 / mul32(sec, SampleRate)
	case SmpRelease:
		sec := mul32(0.02, exp2(mul32(q, 6.6438562))) // log2(100) = 6.6438562(poly.go PolyRelease와 같은 식)
		s.relCoef = polyDecayCoef(mul32(sec, SampleRate))
	case SmpTone:
		hz := mul32(200, exp2(mul32(q, 6.3219281))) // log2(80) = 6.3219281
		// g = 1 − e^{−2π·hz/48000} = 1 − exp2(−hz·2π/(48000·ln2)) — filter.go와 같은 유도.
		// 편차에 곱하는 형태라 exp2 근사 오차가 상태에 누적되지 않는다(filter.go 주석).
		w := mul32(hz, 1.888762e-4) // 2π/(48000·0.6931472)
		s.lpCoef = 1 - exp2(-w)
	case SmpLevel:
		s.level = q
	}
}

func (s *samplerDev) noteOn(note uint8, accent bool) {
	if note > MaxSemis {
		note = MaxSemis // 도메인 = ResolveNote 출력 0..MaxSemis(voice.go·poly.go와 같은 클램프)
	}
	// 라운드로빈 배정: 꺼진 보이스 중 번호가 가장 작은 것, 없으면 시작 순번이 가장 오래된 것.
	vi := -1
	for i := 0; i < smpVoices; i++ {
		if !s.voices[i].on {
			vi = i
			break
		}
	}
	if vi < 0 {
		vi = 0
		for i := 1; i < smpVoices; i++ {
			if s.voices[i].seq < s.voices[vi].seq {
				vi = i
			}
		}
	}
	v := &s.voices[vi]
	e := &packTab[s.sel]
	v.sel = s.sel
	v.pos = float32(uint32(mul32(s.startQ, float32(e.n-1))))
	oct := (float32(int32(note)-int32(e.root)) + s.tune) / 12
	v.inc = exp2(oct)
	v.aenv = 0
	v.peak = 1
	if !accent {
		v.peak = 1 / 1.3 // 액센트 ×1.3의 역(상한 1.0 유지 — poly.go와 같은 규칙)
	}
	v.gate = true
	v.loop = s.loopQ >= 0.5 && e.loop < e.n
	v.on = true
	s.startSeq++
	v.seq = s.startSeq
}

func (s *samplerDev) noteOff() {
	for i := 0; i < smpVoices; i++ {
		v := &s.voices[i]
		if v.loop {
			v.gate = false // 루프 모드만 게이트 오프 — 원샷은 무시(끝까지 운다)
		}
	}
}

func (s *samplerDev) allOff() {
	for i := 0; i < smpVoices; i++ {
		s.voices[i].gate = false // Transport 정지 — 원샷도 이때는 릴리즈로(위치 유지)
	}
}

// silence — 보이스 상태만 즉시 0으로(계수·파라미터는 유지). 랙에서 뽑히거나 꽂힐 때 리드가
// 부른다: 뽑힌 장치는 process()를 못 받으므로 allOff(릴리즈)만으로는 aenv가 영영 안 줄고
// "울리는 중"으로 얼어붙는다(2026-09-07 TestSamplerRackRemoveSilences가 잡은 결함). 재장착이
// 조용히 시작한다는 계약은 여기서 지켜진다.
func (s *samplerDev) silence() {
	for i := 0; i < smpVoices; i++ {
		s.voices[i] = smpVoice{}
	}
	s.lp = 0
}

func (s *samplerDev) active() bool {
	for i := 0; i < smpVoices; i++ {
		if s.voices[i].on {
			return true
		}
	}
	return false
}

func (s *samplerDev) process() float32 {
	var sum float32
	for i := 0; i < smpVoices; i++ {
		v := &s.voices[i]
		if !v.on {
			continue // 비활성 보이스는 계산을 건너뛴다(첫 noteOn 전 +0 계약)
		}
		if v.gate {
			// 어택 — 0에서 peak로 선형(0→1 소요 = attackSec). 이 장치의 보이스는 noteOn마다
			// 새로 시작하므로 상승 분기만 있다(poly.go의 하강 재트리거 분기는 해당 없음).
			if v.aenv < v.peak {
				v.aenv += s.atkInc
				if v.aenv > v.peak {
					v.aenv = v.peak
				}
			}
		} else {
			// 릴리즈 — 현재 레벨에서 지수 감쇠. 1e-5 밑에서 종료.
			v.aenv = v.aenv * s.relCoef
			if v.aenv < smpSilence {
				v.aenv = 0
				v.on = false
				continue
			}
		}
		e := &packTab[v.sel]
		i0 := uint32(v.pos)
		frac := v.pos - float32(i0)
		s0 := packBuf[e.off+i0]
		s1 := s0
		if i0+1 < e.n {
			s1 = packBuf[e.off+i0+1] // 슬롯 끝을 넘으면 마지막 표본 되풀이(유계)
		}
		sum += mul32(s0-mul32(s0, frac)+mul32(s1, frac), v.aenv)
		pos := v.pos + v.inc
		if v.loop {
			fn := float32(e.n)
			for pos >= fn {
				pos -= float32(e.n - e.loop) // inc 최대 2^4까지 — 루프 폭이 좁으면 수회 반복
			}
		} else if pos >= float32(e.n-1) {
			v.on = false // 원샷 종료 — noteOff와 무관하게 끝까지 읽은 뒤 꺼진다
		}
		v.pos = pos
	}
	// 보이스 합 → 장치 단 하나의 1폴 LP → 소프트클립 → ×0.82·Level(파일 주석 유도).
	d := sum - s.lp
	s.lp = mul32(s.lpCoef, d) + s.lp
	return mul32(polyClip(s.lp), mul32(outClipScale, s.level))
}
