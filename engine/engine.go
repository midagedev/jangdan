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

// bassStep — 베이스 패턴 한 스텝. note 0..MaxNote = 도수 표기 octave*7+degree(harmony.go:
// 0 = 코드 루트(옥타브 0), 7 = 옥타브 1 루트), flags = StepGate|StepSlide|StepAccent.
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

	// 조성·코드 트랙·모드·트랜스포트(harmony.go 계약, §12)
	keyRoot    uint8            // 현재 키 루트(바 경계에 pendingKey에서 갱신)
	pendingKey uint8            // SetKey 대기값
	chordDeg   [ChordBars]uint8 // 마디별 코드 도수(0..6)
	chordFlags [ChordBars]uint8 // 마디별 ChordSeventh
	mode       [2]uint8         // 파트별 ModeBass/ModeArp/ModeChord
	dir        [2]uint8         // 파트별 아르페지오 방향
	arpIdx     [2]uint8         // 아르페지오 진행 인덱스(코드 톤 수로 순환)
	arpDown    [2]bool          // DirUpDown 현재 하강 중
	playing    bool             // Transport: false면 위치 동결(렌더는 계속 — 딜레이 꼬리)

	// 드롭
	dropPending bool
	dropEnv     float32 // 1→0, 8바 선형
	dropDec     float32 // 샘플당 감쇠량

	// 직전 블록 신호
	flags uint32
	peak  float32
	// 파트별 블록 피크(프리 FX 출력 abs 최대, Part 순 BassA BassB BD SD CH OH CP CY).
	// Render가 마스터 peak과 같은 자리에서 모은다 — sample의 partOut(베이스)과
	// drumKit.last(드럼 항)이 샘플별 원본이다. 라인별 LED·VU 미터의 원본.
	levels [8]float32

	// 장치 인스턴스(종류별 고정 배열 — rack.go kindCap)와 랙(슬롯·케이블·위상 순서).
	// 어느 슬롯이 어느 인스턴스인지는 rack.kind/inst가 소유한다(§14.1).
	bass  [2]bassVoice
	lvl   [2]float32 // 베이스 채널 레벨(BassALevel/BassBLevel — 기본 1.0은 mul32(x,1)==x 항등)
	drums drumKit
	fx    fxChain
	rev   reverb
	cho   chorus
	poly  [1]polySynth
	rack  rack

	polyHeld [1]bool // 폴리 인스턴스별: 직전 스텝이 게이트였다(타이 판정 — 런타임 상태, 직렬화 안 함)
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
	e.poly[0].init(seed ^ 0x2545F491)
	e.rack.buildDefault() // 기본 랙(§14.1) — 아래 DefaultParams 적용이 결속 케이블 게인을 채운다
	e.initDevDefaults(SlotPoly)
	e.playing = true
	e.keyRoot = uint8(seed % NumKeys) // 세션 조성은 시드가 고른다(resident도 같은 식 seed%12로 SetKey를 낸다)
	e.pendingKey = e.keyRoot
	e.chordDeg = [ChordBars]uint8{0, 0, 5, 6, 0, 0, 3, 4} // i i VI VII | i i iv v
	e.genInitialPatterns()
	d := DefaultParams()
	for i := 0; i < int(NumParams); i++ {
		e.applyParam(ParamID(i), d[i])
	}
	e.dropDec = float32(1.0 / (8.0 * 16.0 * e.samplesPerStep)) // 8바
}

// genInitialPatterns — seed로 슬롯 0 패턴을 채운다(나머지 슬롯은 빈 패턴 = 게이트 없음).
// 레지던트가 곧 덮어쓰지만, 손이 없는 첫 300ms에도 소리가 나야 한다.
// 베이스 note는 도수(harmony.go): A = 옥타브 1(7..13), B = 옥타브 0(0..6). 도수 가중
// 루트 0.5 · 코드 톤(2·4) 0.3 · 경과음(1·3·5·6) 0.2 — 첫 바(레지던트가 덮기 전)부터
// 조성 안의 소리가 난다. 도수는 코드 기준이라 가중표는 마디·코드 트랙과 무관하다.
func (e *Engine) genInitialPatterns() {
	// 도수 분포(8비트 0..255): <128 루트(0.5) · <217 3rd(2) · <230 5th(4) — 코드 톤
	// 합계 ≈0.3 · <243/256/255 경과음 1·3·5(≈0.05씩) · 나머지 6(7th 경과).
	// 밀도 0.75·액센트 0.25·슬라이드 0.15는 각각 독립 비트 필드에서(상관 없게).
	for p := 0; p < 2; p++ {
		for st := 0; st < Steps; st++ {
			v := e.rng.next()
			var deg uint8
			switch r := v & 0xFF; {
			case r < 128:
				deg = 0
			case r < 217:
				deg = 2
			case r < 230:
				deg = 4
			case r < 243:
				deg = 1
			case r < 252:
				deg = 3
			case r < 255:
				deg = 5
			default:
				deg = 6
			}
			n := deg
			if p == 0 {
				n = deg + 7 // 베이스 A는 옥타브 1
			}
			f := uint8(0)
			if (v>>8)&0xFF < 192 {
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
	// 폴리 리드(SlotPoly) 초기 패턴: 오프비트 8분(2·6·10·14), 옥타브 3 — rng를 소비하지 않는다
	// (베이스·드럼 초기 패턴 바이트 불변). 레지던트가 페이즈 진입 바에 덮어쓴다(resident/poly.go).
	for _, st := range [...]int{2, 6, 10, 14} {
		e.rack.devPat[SlotPoly][st] = bassStep{note: 3 * NumDegrees, flags: StepGate}
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
	case id == BassALevel:
		e.lvl[0] = q
	case id == BassBLevel:
		e.lvl[1] = q
	case id == RevSize:
		e.rev.setSize(q)
	case id == RevDamp:
		e.rev.setDamp(q)
	case id == ChoRate:
		e.cho.setRate(q)
	case id == ChoDepth:
		e.cho.setDepth(q)
	case id >= BassALevel: // 센드·리턴(RevSend·DelaySend·ChoSend·RevMix·ChoMix) — 결속 케이블 게인만
	default: // Delay, Drive, Comp, Master
		e.fx.setParam(int(id-Delay), q)
	}
	e.rack.setBound(id, q) // 이 파라미터에 결속된 케이블(기본 랙의 센드·리턴, 사용자 결속)의 게인 유도
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
	case SetKey:
		e.pendingKey = c.A % NumKeys
	case SetChord:
		e.chordDeg[c.A%ChordBars] = c.B % NumDegrees
		e.chordFlags[c.A%ChordBars] = c.C & ChordSeventh
	case BassMode:
		if c.A > 1 {
			return
		}
		m, d := c.B, c.C
		if m >= NumModes {
			m = ModeBass
		}
		if d >= NumDirs {
			d = DirUp
		}
		if m != e.mode[c.A] {
			e.arpIdx[c.A], e.arpDown[c.A] = 0, false
		}
		e.mode[c.A], e.dir[c.A] = m, d
	case Transport:
		if c.A != 0 {
			if !e.playing {
				e.playing = true
				e.stepIdx, e.stepPos, e.started = 0, 0, false // 다음 Render에서 스텝 0부터(바 유지)
			}
		} else if e.playing {
			e.playing = false
			e.bass[0].noteOff()
			e.bass[1].noteOff()
			e.poly[0].allOff()
			e.polyHeld[0] = false
		}
	// 장치 그래프(§14.1, rack.go). 실패(점유·범위 밖·순환·표 가득)는 무동작.
	case AddDevice:
		if e.rack.addDevice(int(c.A), DeviceKind(c.B)) {
			e.initDevDefaults(int(c.A))
		}
	case RemoveDevice:
		if int(c.A) < RackSlots && e.rack.kind[c.A] == KindPoly {
			e.poly[e.rack.inst[c.A]].allOff() // 빠진 장치가 울리지 않게(재장착 시 조용히 시작)
			e.polyHeld[e.rack.inst[c.A]] = false
		}
		e.rack.removeDevice(int(c.A))
	case Connect:
		n, _ := quantize(c.V)
		if e.rack.connect(c.A, c.C&0x0F, c.B, c.C>>4, ParamID(c.D), n) && ParamID(c.D) < NumParams {
			e.rack.setBound(ParamID(c.D), e.params[c.D]) // 결속 케이블은 지금 값으로 게인 유도
		}
	case Disconnect:
		e.rack.disconnect(c.A, c.C&0x0F, c.B, c.C>>4)
	case DeviceParam:
		n, _ := quantize(c.V)
		if e.rack.setDevParam(int(c.A), int(c.B), n) {
			e.applyDevParam(int(c.A), int(c.B))
		}
	case DeviceStep:
		e.rack.setDevStep(int(c.A), c.B, c.C, c.D)
	}
}

// applyDevParam — 슬롯 로컬 파라미터 k의 저장값을 그 슬롯 장치의 계수로 유도한다(종류별 switch).
func (e *Engine) applyDevParam(slot, k int) {
	switch e.rack.kind[slot] {
	case KindPoly:
		e.poly[e.rack.inst[slot]].setParam(k, float32(e.rack.devParQ[slot][k])/ParamSteps)
	}
}

// initDevDefaults — 슬롯에 놓인 장치 종류의 기본 로컬 파라미터를 devParQ에 쓰고 계수를 유도한다
// (Reset·AddDevice). 로컬 파라미터가 없는 종류는 무동작.
func (e *Engine) initDevDefaults(slot int) {
	if slot < 0 || slot >= RackSlots {
		return
	}
	switch e.rack.kind[slot] {
	case KindPoly:
		d := DefaultPolyParams()
		for k := 0; k < PolyParams && k < DevParams; k++ {
			n, _ := quantize(d[k])
			e.rack.setDevParam(slot, k, n)
			e.applyDevParam(slot, k)
		}
	}
}

// polyStep — KindPoly 슬롯의 스텝 처리(§14.2): 게이트 스텝은 그 마디 코드 톤(3 또는 4)을
// 보이스 0..3에 배정(옥타브 = 패턴 note/7), 남는 보이스는 릴리즈. 게이트 없는 스텝은 전 보이스
// 릴리즈. 타이(StepGate|StepSlide)는 직전 스텝이 게이트였으면 유지(재트리거 없음), 아니면 게이트.
func (e *Engine) polyStep(st int) {
	for s := 0; s < RackSlots; s++ {
		if e.rack.kind[s] != KindPoly {
			continue
		}
		inst := e.rack.inst[s]
		p := &e.poly[inst]
		ps := e.rack.devPat[s][st]
		if ps.flags&StepGate == 0 {
			p.allOff()
			e.polyHeld[inst] = false
			continue
		}
		if ps.flags&StepSlide != 0 && e.polyHeld[inst] {
			continue // 타이 — 유지
		}
		deg, cflags := e.Chord(int(e.bar))
		seventh := cflags&ChordSeventh != 0
		n := int(ChordTones(cflags))
		oct := (ps.note / NumDegrees) * NumDegrees
		accent := ps.flags&StepAccent != 0
		for v := 0; v < polyVoices; v++ {
			if v < n {
				p.noteOn(v, ResolveNote(e.keyRoot, deg, oct+ChordToneDeg(uint8(v), seventh)), accent)
			} else {
				p.noteOff(v)
			}
		}
		e.polyHeld[inst] = true
		e.flags |= FlagPoly
		if accent {
			e.flags |= FlagAccent
		}
	}
}

// DevParamQ — 슬롯 로컬 파라미터 양자화 값(UI 표시용). 범위 밖 0.
func (e *Engine) DevParamQ(slot, k int) uint16 {
	if slot < 0 || slot >= RackSlots || k < 0 || k >= DevParams {
		return 0
	}
	return e.rack.devParQ[slot][k]
}

// DevStepAt — 슬롯 스텝 패턴(UI 표시용). 범위 밖 0,0.
func (e *Engine) DevStepAt(slot int, step int) (note, flags uint8) {
	if slot < 0 || slot >= RackSlots {
		return 0, 0
	}
	s := e.rack.devPat[slot][step&(Steps-1)]
	return s.note, s.flags
}

// 조성·코드·모드·트랜스포트 읽기(UI 표시용).
func (e *Engine) KeyRoot() uint8 { return e.pendingKey }

// SyncPending — 바 경계에 적용되는 대기값(슬롯·키)을 렌더 없이 즉시 반영한다. 호스트의 메인 스레드
// 섀도 엔진(렌더하지 않고 같은 Cmd만 적용해 UI 미러로 쓰는 인스턴스)이 바 경계 틱마다 부른다.
// 실제 렌더 엔진은 onStep(0)에서 같은 일을 하므로 두 인스턴스의 제어 상태가 일치한다.
func (e *Engine) SyncPending() {
	e.slot = e.pendingSlot
	e.keyRoot = e.pendingKey
}
func (e *Engine) Playing() bool { return e.playing }

// Chord — 코드 트랙 마디 bar(&7)의 (도수, 플래그).
func (e *Engine) Chord(bar int) (deg, flags uint8) {
	b := bar & (ChordBars - 1)
	return e.chordDeg[b], e.chordFlags[b]
}

// Mode — 파트의 (모드, 방향). 베이스 파트가 아니면 0,0.
func (e *Engine) Mode(p Part) (mode, dir uint8) {
	if p > BassB {
		return 0, 0
	}
	return e.mode[p], e.dir[p]
}

// 위치·신호 읽기(Render 뒤).
func (e *Engine) Block() uint64 { return e.block }
func (e *Engine) Step() int     { return e.stepIdx }
func (e *Engine) Bar() uint32   { return e.bar }
func (e *Engine) Flags() uint32 { return e.flags }
func (e *Engine) Peak() float32 { return e.peak }

// Level — 직전 Render 블록의 파트별 피크(프리 FX — 파트가 무음이면 0). p ≥ NumParts는 0.
func (e *Engine) Level(p Part) float32 {
	if p >= NumParts {
		return 0
	}
	return e.levels[p]
}
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
	for i := 0; i < int(NumParts); i++ { // 파트 피크도 블록마다 다시 모은다(마스터 peak와 같은 주기)
		e.levels[i] = 0
	}
	for i := 0; i < Block; i++ {
		if e.playing { // 정지 중에는 위치가 동결되고 보이스·FX 꼬리만 렌더된다
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
		}
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
		// 파트별 피크 — 파트 장치 출력 포트(프리 FX·레벨 반영)의 abs 최대(마스터 peak과
		// 같은 자리). 파트의 장치가 랙에 없으면 0.
		for p := 0; p < int(NumParts); p++ {
			s := e.rack.partSlot[p]
			if s == 0xFF {
				continue
			}
			if a := abs32(e.rack.port[s][e.rack.partPort[p]]); a > e.levels[p] {
				e.levels[p] = a
			}
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
		e.keyRoot = e.pendingKey
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
		if s.flags&StepGate == 0 {
			e.bass[p].noteOff()
			continue
		}
		accent := s.flags&StepAccent != 0
		deg, cflags := e.Chord(int(e.bar))
		switch e.mode[p] {
		case ModeArp:
			// ARP — 게이트된 스텝마다 코드 톤 하나(옥타브는 패턴 note/7, 도수는
			// 아르페지오가). 슬라이드 목표 = 다음 게이트 스텝의 톤(arpNotes).
			note, tgt := e.arpNotes(p, st, s.note, deg, cflags)
			e.bass[p].noteOn(note, accent, s.flags&StepSlide != 0, tgt, e.samplesPerStep)
		case ModeChord:
			// CHORD — 3음 패러포닉. 7th 없음 = 루트·3·5(도수 0·2·4), 7th = 3·5·7
			// (2·4·6 — 루트는 베이스 A가 맡는다). 슬라이드는 무시: 노트온만(voice.go 계약).
			oct := (s.note / NumDegrees) * NumDegrees
			d1, d2, d3 := oct+0, oct+2, oct+4
			if cflags&ChordSeventh != 0 {
				d1, d2, d3 = oct+2, oct+4, oct+6
			}
			e.bass[p].noteOnChord(
				ResolveNote(e.keyRoot, deg, d1),
				ResolveNote(e.keyRoot, deg, d2),
				ResolveNote(e.keyRoot, deg, d3),
				accent)
		default:
			// BASS — 패턴 도수를 그대로(코드 트랙이 화성을 정한다). 슬라이드 목표는
			// 다음 스텝이 속한 마디의 코드로 해석(st 15 → 다음 바).
			note := ResolveNote(e.keyRoot, deg, s.note)
			next := e.bassPat[p][e.slot[p]][(st+1)&(Steps-1)]
			nextDeg := deg
			if st == Steps-1 {
				nextDeg, _ = e.Chord(int(e.bar) + 1)
			}
			nextNote := ResolveNote(e.keyRoot, nextDeg, next.note)
			slideTo := s.flags&StepSlide != 0 && next.flags&StepGate != 0
			e.bass[p].noteOn(note, accent, slideTo, nextNote, e.samplesPerStep)
		}
		e.flags |= 1 << p
		if accent {
			e.flags |= FlagAccent
		}
	}
	e.polyStep(st)
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

// arpStep — 아르페지오 진행 규칙의 순수 계산(진행 커밋·슬라이드 peek 공용 — 한 몫의
// 로직이 두 경로에 쓰인다). (idx, down)에서 dir 방향으로 한 스텝 진행한 (idx', down').
// DirDown은 감소 순환(0 → n-1 랩), DirUpDown은 삼각파(꼭대기·바닥에서 반전).
func arpStep(idx uint8, down bool, dir uint8, n uint8) (uint8, bool) {
	if idx >= n {
		idx = n - 1
	}
	switch dir {
	case DirUp:
		idx++
		if idx >= n {
			idx = 0
		}
	case DirDown:
		if idx == 0 {
			idx = n - 1
		} else {
			idx--
		}
	default: // DirUpDown — n ≥ 3이라 1·n-2가 항상 유효
		if down {
			if idx == 0 {
				return 1, false
			}
			return idx - 1, true
		}
		if idx == n-1 {
			return n - 2, true
		}
		return idx + 1, false
	}
	return idx, down
}

// arpNotes — ARP 모드의 이번 톤 반음과 슬라이드 목표 반음, 그리고 진행(arpIdx/arpDown
// 커밋). 순서 계약(§12.1): Up [r,3,5,r,3,5] · Down [5,3,r,…] · UpDown [r,3,5,3,r,3] —
// 하강은 진입이 꼭대기에서 시작하도록 연주 *전*에 한 칸 진행하고, 상승 계열은 연주
// *뒤*에 진행한다. 게이트 없는 스텝은 진행하지 않고(호출 자체가 게이트된 스텝에서만
// 일어난다), 바 경계·코드 변경에서 리셋하지 않는다(Apply의 모드 변경 리셋만).
// 7th 해제로 톤 수가 4→3으로 줄면 idx를 n-1로 클램프해 순환이 끊기지 않게 한다.
func (e *Engine) arpNotes(p int, st int, patNote, chordDeg, cflags uint8) (note, tgt uint8) {
	n := ChordTones(cflags)
	seventh := cflags&ChordSeventh != 0
	idx, down := e.arpIdx[p], e.arpDown[p]
	if idx >= n {
		idx = n - 1
		e.arpIdx[p] = idx
	}
	if e.dir[p] == DirDown {
		idx, down = arpStep(idx, down, DirDown, n) // 연주 전 진행(꼭대기 진입)
	}
	oct := (patNote / NumDegrees) * NumDegrees
	note = ResolveNote(e.keyRoot, chordDeg, oct+ChordToneDeg(idx, seventh))
	// 다음 게이트 스텝이 연주할 톤(peek) — 이번 스텝의 진행까지 반영한 인덱스에서
	// 읽는다(Down은 이미 진행 완료 상태, 상승 계열은 진행 후 상태를 순수 계산).
	tIdx, tDown := arpStep(idx, down, e.dir[p], n)
	tgt = e.arpPeek(p, st, tIdx)
	if e.dir[p] == DirDown {
		e.arpIdx[p], e.arpDown[p] = idx, down
	} else {
		e.arpIdx[p], e.arpDown[p] = tIdx, tDown
	}
	return note, tgt
}

// arpPeek — 다음 게이트 스텝이 연주할 반음(arpIdx·arpDown은 바꾸지 않는다). idx는
// 이번 스텝의 진행까지 반영한 값. 16스텝 안에는 반드시 게이트가 있다(이번 스텝 자신이
// 다음 바에 돌아온다) — 루프가 다 돌면 0을 반환한다(정규화 계약, 도달 불가).
// 다음 바의 스텝은 pendingSlot 패턴·그 바의 코드에서 읽는다(바 경계에서 적용될 값).
func (e *Engine) arpPeek(p int, st int, idx uint8) uint8 {
	for k := 1; k <= Steps; k++ {
		ust := st + k
		ss := ust & (Steps - 1)
		bar, slot := e.bar, e.slot[p]
		if ust >= Steps {
			bar, slot = e.bar+1, e.pendingSlot[p]
		}
		if e.bassPat[p][slot][ss].flags&StepGate == 0 {
			continue
		}
		deg, cflags := e.Chord(int(bar))
		oct := (e.bassPat[p][slot][ss].note / NumDegrees) * NumDegrees
		return ResolveNote(e.keyRoot, deg, oct+ChordToneDeg(idx, cflags&ChordSeventh != 0))
	}
	return 0
}

// sample — 장치 그래프 한 샘플(§14.1, rack.go). 위상 순서대로 각 장치의 입력 포트를
// 케이블 합으로 만들고(첫 케이블 대입·이후 덧셈·죽은 src 건너뜀) 종류별 process를 불러
// 출력 포트에 쓴다. Main 장치의 두 입력이 엔진 출력이다(busClamp — dry 최대 0.98998 <
// 0.99026이라 dry 단독에는 항등). 베이스라인에는 드롭 컷오프 부스트(옥타브), FX에는
// 드롭 드라이브 부스트. 기본 랙에서는 옛 고정 체인(베이스 합 → FX → 리턴 합)과 연산
// 순서가 같아 바이트가 같다(fx2_test 기본값 해시가 게이트).
func (e *Engine) sample() (float32, float32) {
	r := &e.rack
	var in [MaxInPorts]float32
	var outL, outR float32
	for k := 0; k < r.nOrder; k++ {
		s := int(r.order[k])
		if !r.live[s] {
			continue
		}
		if kindPorts[r.kind[s]][0] > 0 {
			in = [MaxInPorts]float32{}
			r.sumInputs(s, &in)
		}
		switch r.kind[s] {
		case KindBass:
			i := r.inst[s]
			r.port[s][0] = mul32(e.bass[i].process(e.dropEnv), e.lvl[i])
		case KindDrums:
			d, bd := e.drums.process(&e.noise)
			r.port[s][0], r.port[s][1] = d, bd
			for v := 0; v < NumDrums; v++ {
				r.port[s][2+v] = e.drums.last[v]
			}
		case KindFx:
			r.port[s][0], r.port[s][1] = e.fx.process(in[0], in[1], in[2], e.dropEnv, in[3])
		case KindReverb:
			r.port[s][0], r.port[s][1] = e.rev.process(in[0])
		case KindChorus:
			r.port[s][0], r.port[s][1] = e.cho.process(in[0])
		case KindMain:
			outL, outR = busClamp(in[0]), busClamp(in[1])
		case KindPoly:
			r.port[s][0] = e.poly[r.inst[s]].process()
		}
	}
	return outL, outR
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
