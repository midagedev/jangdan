// 패키지 engine — 장단(Jangdan) DSP·시퀀서. 순수 Go, 외부 의존 0, math는
// Float32bits/Float32frombits/Abs/Sqrt만 허용. 계약 원본: docs/impl-plan-2026-09-05.md §2.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석 참조).
// 게이트: tools/check-fma.sh. 핫 루프(Render/Apply) 무할당: 할당은 New에서만. 결정론: 같은
// seed·같은 Cmd 이력(같은 블록 위치) → 어느 플랫폼에서든 같은 샘플.
//
// 파일 소유: engine.go·params.go·cmd.go·state.go·approx.go = 리드. voice.go/filter.go = 베이스라인,
// drums.go = 드럼, fx.go = 이펙트 — 각 파일은 아래 engine.go가 호출하는 메서드 집합을 구현한다.
package engine

const SampleRate = 48000
const Block = 128

// bassStep — 베이스 패턴 한 스텝. note 0..MaxNote(24 = C3), flags = StepGate|StepSlide|StepAccent.
type bassStep struct {
	note  uint8
	flags uint8
}

// Engine — 모든 상태. 맵·슬라이스·인터페이스 없음(무할당·결정론 계약). 딜레이 버퍼도
// fxChain 안의 고정 배열이라 New 한 번이 유일한 할당이다.
type Engine struct {
	params [NumParams]float32 // 양자화된 값 n/ParamSteps
	paramQ [NumParams]uint16  // 양자화 정수(직렬화 정본)

	seed  uint32
	rng   xorshift32 // 초기 패턴 생성(New/Reset에서만 소진)
	noise xorshift32 // 런타임 노이즈(드럼)

	bassPat     [2][PatternSlots][Steps]bassStep
	drumPat     [NumDrums][Steps]uint8
	slot        [2]uint8 // 현재 슬롯
	pendingSlot [2]uint8 // 다음 바 경계에 적용될 슬롯
	mute        uint8    // bit part

	// 시퀀서 위치
	stepIdx        int
	bar            uint32
	block          uint64
	stepPos        float64 // 현재 스텝 내 샘플 위치
	samplesPerStep float64
	started        bool // 첫 스텝 트리거 전(Render 첫 호출에서 스텝 0 발동)

	// 드롭
	dropPending bool
	dropEnv     float32 // 1→0, 8바 선형
	dropDec     float32 // 샘플당 감쇠량

	// 직전 블록 신호
	flags uint32
	peak  float32

	bass  [2]bassVoice
	drums drumKit
	fx    fxChain
}

// New — 유일한 할당 지점. seed에서 초기 패턴을 만들고 기본 파라미터를 적용한다.
func New(seed uint32) *Engine {
	e := &Engine{}
	e.Reset(seed)
	return e
}

// Reset — 무할당 재초기화(-gc=leaking에서 New 재호출 대신 쓴다). 위치·패턴·파라미터·보이스 전부.
func (e *Engine) Reset(seed uint32) {
	*e = Engine{}
	if seed == 0 {
		seed = 0x9E3779B9
	}
	e.seed = seed
	e.rng = xorshift32{seed}
	e.noise = xorshift32{seed ^ 0x5BF03635}
	e.bass[0].init(seed)
	e.bass[1].init(seed ^ 0x7F4A7C15)
	e.drums.init(seed ^ 0x3C6EF372)
	e.fx.init()
	e.genInitialPatterns()
	d := DefaultParams()
	for i := 0; i < int(NumParams); i++ {
		e.applyParam(ParamID(i), d[i])
	}
	e.dropDec = float32(1.0 / (8.0 * 16.0 * e.samplesPerStep)) // 8바
}

// genInitialPatterns — seed로 슬롯 0 패턴을 채운다(나머지 슬롯은 빈 패턴 = 게이트 없음).
// 레지던트가 곧 덮어쓰지만, 손이 없는 첫 300ms에도 소리가 나야 한다.
func (e *Engine) genInitialPatterns() {
	// 마이너 펜타토닉(반음 오프셋) 위에서 베이스 A: 밀도 0.75, 액센트 0.25, 슬라이드 0.15
	scale := [...]uint8{0, 3, 5, 7, 10, 12, 15, 17}
	for p := 0; p < 2; p++ {
		for st := 0; st < Steps; st++ {
			v := e.rng.next()
			n := 24 + scale[(v>>8)&7] - 12 // C2 기준
			f := uint8(0)
			if v&0xFF < 192 {
				f |= StepGate
			}
			if (v>>16)&0xFF < 64 {
				f |= StepAccent
			}
			if (v>>24)&0xFF < 40 {
				f |= StepSlide
			}
			if p == 1 && st%2 == 1 { // B는 오프비트를 비운다
				f &^= StepGate
			}
			e.bassPat[p][0][st] = bassStep{note: n, flags: f}
		}
	}
	// 드럼 골격: BD 1·5·9·13, SD 5·13, CH 짝수, OH 3·11, CP 13(확률), CY 0(첫 바만은 아님 — 드롭이 친다)
	for _, st := range [...]int{0, 4, 8, 12} {
		e.drumPat[0][st] = StepGate
	}
	e.drumPat[0][int((e.rng.next()>>4)%16)] |= StepGate
	e.drumPat[1][4], e.drumPat[1][12] = StepGate, StepGate|StepAccent
	for st := 0; st < Steps; st += 2 {
		e.drumPat[2][st] = StepGate
	}
	e.drumPat[2][int((e.rng.next()>>4)%16)] |= StepGate
	e.drumPat[3][2], e.drumPat[3][10] = StepGate, StepGate
	if e.rng.next()&1 == 1 {
		e.drumPat[4][12] = StepGate
	}
}

// SetParam — Apply(SetParam) 단축.
func (e *Engine) SetParam(id ParamID, v float32) { e.applyParam(id, v) }

// Param — 현재(양자화된) 값. id ≥ NumParams이면 0.
func (e *Engine) Param(id ParamID) float32 {
	if id >= NumParams {
		return 0
	}
	return e.params[id]
}

// ParamQ — 양자화 정수(0..ParamSteps).
func (e *Engine) ParamQ(id ParamID) uint16 {
	if id >= NumParams {
		return 0
	}
	return e.paramQ[id]
}

func (e *Engine) applyParam(id ParamID, v float32) {
	if id >= NumParams {
		return
	}
	n, _ := quantize(v)
	e.setParamQ(id, n)
}

// setParamQ — 양자화 정수로 설정하고 계수를 유도한다(핫 루프 밖). 유도 계산도 결정론 규칙을 따른다.
func (e *Engine) setParamQ(id ParamID, n uint16) {
	if n > ParamSteps {
		n = ParamSteps
	}
	q := float32(n) / ParamSteps
	e.paramQ[id] = n
	e.params[id] = q
	switch {
	case id < BassBParams:
		e.bass[0].setParam(int(id-BassAParams), q)
	case id < DrumParams:
		e.bass[1].setParam(int(id-BassBParams), q)
	case id < Delay:
		k := int(id - DrumParams)
		e.drums.setParam(k/2, k%2, q)
	case id == Tempo:
		e.samplesPerStep = SampleRate * 60.0 / BPMOf(q) / 4.0
		e.fx.setTempo(e.samplesPerStep)
		e.dropDec = float32(1.0 / (8.0 * 16.0 * e.samplesPerStep))
	default: // Delay, Drive, Comp, Master
		e.fx.setParam(int(id-Delay), q)
	}
}

// Apply — 명령 적용. 무할당. 범위 밖은 정규화(step&15, slot&7, note 클램프), 파트 범위 밖은 무동작.
func (e *Engine) Apply(c Cmd) {
	switch c.Kind {
	case SetParam:
		e.applyParam(ParamID(c.A), c.V)
	case BassStep:
		if c.A > 1 {
			return
		}
		n := c.C
		if n > MaxNote {
			n = MaxNote
		}
		e.bassPat[c.A][e.slot[c.A]][c.B&(Steps-1)] = bassStep{note: n, flags: c.D & (StepGate | StepSlide | StepAccent)}
	case DrumStep:
		if c.A < uint8(BD) || c.A >= uint8(NumParts) {
			return
		}
		e.drumPat[c.A-uint8(BD)][c.B&(Steps-1)] = c.D & (StepGate | StepAccent)
	case SelectPattern:
		if c.A > 1 {
			return
		}
		e.pendingSlot[c.A] = c.B & (PatternSlots - 1)
	case Mute:
		if c.A >= uint8(NumParts) {
			return
		}
		if c.B != 0 {
			e.mute |= 1 << c.A
		} else {
			e.mute &^= 1 << c.A
		}
	case Trigger:
		if c.A < uint8(BD) || c.A >= uint8(NumParts) {
			return
		}
		e.drums.trigger(int(c.A-uint8(BD)), false)
		e.flags |= 1 << c.A
	case Drop:
		e.dropPending = true
	case ResetPos:
		e.stepIdx, e.bar, e.stepPos, e.started = 0, 0, 0, false
	}
}

// 위치·신호 읽기(Render 뒤).
func (e *Engine) Block() uint64  { return e.block }
func (e *Engine) Step() int      { return e.stepIdx }
func (e *Engine) Bar() uint32    { return e.bar }
func (e *Engine) Flags() uint32  { return e.flags }
func (e *Engine) Peak() float32  { return e.peak }
func (e *Engine) Slot(p Part) uint8 {
	if p > BassB {
		return 0
	}
	return e.slot[p]
}
func (e *Engine) Muted(p Part) bool { return p < NumParts && e.mute&(1<<p) != 0 }

// BassStepAt — 현재 슬롯의 스텝(UI 표시용). p>1이면 0,0.
func (e *Engine) BassStepAt(p Part, step int) (note, flags uint8) {
	if p > BassB {
		return 0, 0
	}
	s := e.bassPat[p][e.slot[p]][step&(Steps-1)]
	return s.note, s.flags
}

// DrumStepAt — 드럼 스텝 플래그. 드럼 파트가 아니면 0.
func (e *Engine) DrumStepAt(p Part, step int) uint8 {
	if p < BD || p >= NumParts {
		return 0
	}
	return e.drumPat[p-BD][step&(Steps-1)]
}

// Render — len(out) == 2*Block 스테레오 인터리브를 채운다. 길이가 다르면 무동작(패닉 금지).
// 힙 할당 0. 블록 시작에 flags를 지우고 블록 안의 사건을 OR로 쌓는다.
func (e *Engine) Render(out []float32) {
	if len(out) != 2*Block {
		return
	}
	e.flags = 0 // 블록 신호는 Render가 소유한다(Trigger 명령의 비트는 그 블록 렌더 전에 세워지므로 유지되지 않는다 — 호스트가 패드 lit을 직접 그린다)
	peak := float32(0)
	for i := 0; i < Block; i++ {
		if !e.started {
			e.started = true
			e.stepPos = 0
			e.stepIdx = 0
			e.onStep(0, true)
		} else if e.stepPos >= e.samplesPerStep {
			e.stepPos -= e.samplesPerStep
			e.stepIdx = (e.stepIdx + 1) & (Steps - 1)
			e.onStep(e.stepIdx, false)
		}
		e.stepPos++
		if e.dropEnv > 0 {
			e.dropEnv -= e.dropDec
			if e.dropEnv < 0 {
				e.dropEnv = 0
			}
		}
		l, r := e.sample()
		out[2*i] = l
		out[2*i+1] = r
		if a := abs32(l); a > peak {
			peak = a
		}
		if a := abs32(r); a > peak {
			peak = a
		}
	}
	e.peak = peak
	e.block++
}

// onStep — 스텝 경계 처리: 바 경계(슬롯 교체·드롭 발동)와 파트 트리거. first = 재생 첫 스텝(bar 0 유지).
func (e *Engine) onStep(st int, first bool) {
	if st == 0 {
		if !first {
			e.bar++
		}
		e.flags |= FlagBar
		e.slot = e.pendingSlot
		if e.dropPending {
			e.dropPending = false
			e.dropEnv = 1
			e.mute = 0
			e.drums.trigger(int(CY-BD), true)
			e.flags |= FlagDrop | 1<<CY
		}
	}
	for p := 0; p < 2; p++ {
		if e.mute&(1<<p) != 0 {
			e.bass[p].noteOff()
			continue
		}
		s := e.bassPat[p][e.slot[p]][st]
		if s.flags&StepGate != 0 {
			next := e.bassPat[p][e.slot[p]][(st+1)&(Steps-1)]
			slideTo := s.flags&StepSlide != 0 && next.flags&StepGate != 0
			e.bass[p].noteOn(s.note, s.flags&StepAccent != 0, slideTo, next.note, e.samplesPerStep)
			e.flags |= 1 << p
			if s.flags&StepAccent != 0 {
				e.flags |= FlagAccent
			}
		} else {
			e.bass[p].noteOff()
		}
	}
	for v := 0; v < NumDrums; v++ {
		part := uint8(BD) + uint8(v)
		if e.mute&(1<<part) != 0 {
			continue
		}
		f := e.drumPat[v][st]
		if f&StepGate != 0 {
			e.drums.trigger(v, f&StepAccent != 0)
			e.flags |= 1 << part
		}
	}
}

// sample — 보이스 합 → FX 체인. 베이스라인에는 드롭 컷오프 부스트(옥타브), FX에는 드롭 드라이브 부스트.
func (e *Engine) sample() (float32, float32) {
	b := e.bass[0].process(e.dropEnv) + e.bass[1].process(e.dropEnv)
	d, bd := e.drums.process(&e.noise)
	return e.fx.process(b, d, bd, e.dropEnv)
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
