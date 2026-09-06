// state.go — 제어 상태 직렬화(키프레임). 리드 소유(docs/impl-plan-2026-09-05.md §2.3·§12가 원본).
//
// 키프레임은 바 경계의 제어 상태다: 파라미터·패턴·슬롯·뮤트·조성·코드 트랙·모드·트랜스포트.
// 보이스 내부 상태(필터 메모리·엔벨로프·딜레이 버퍼)는 포함하지 않는다 — 복원은 바 경계에서만 의미가 있다.
// 레이아웃 v5(리틀엔디언, 고정 StateSize 바이트 — v3 748바이트 뒤에 랙(§14.1)을 덧붙인다):
//
//	[0..2)          magic 'J','5'
//	[2..120)        params[59] uint16
//	[120..632)      bass 패턴 2파트 × 8슬롯 × 16스텝 × (note u8 = 도수 표기, flags u8)
//	[632..728)      drum 패턴 6보이스 × 16스텝 × flags u8
//	[728..730)      선택 슬롯 BassA, BassB
//	[730]           뮤트 비트(bit part)
//	[731]           keyRoot(0..11)
//	[732..734)      파트별 mode | dir<<2 (BassA, BassB)
//	[734]           playing(0|1)
//	[735..743)      코드 트랙 8마디 × (degree | flags<<3)
//	[743..748)      예약(0)
//
// ReadState는 검증 후 폐기가 아니라 재정규화한다(note>MaxNote → MaxNote, flags 마스킹, 슬롯 &7,
// 키 %12, 도수 %7, 모드·방향 범위 밖은 0). 랙은 빈 랙에서 슬롯·케이블을 하나씩 다시 놓으며
// 정규화한다(종류 범위 밖·인스턴스 중복·포트 범위 밖·순환 케이블은 버린다, Main이 없으면 놓는다 —
// 결속 케이블 게인은 파라미터에서 다시 유도). v4('J','4', 1424바이트) 이하는 거부한다 — 랙 표·장치
// 패턴이 없어 반쪽 상태가 되고, 지속 저장된 키프레임은 없다(메모리·리플레이 전용 — 로그 재생이 정본).
// 이 파일에는 곱셈-덧셈이 없다.
package engine

const (
	stateMagic0 = 'J'
	stateMagic1 = '5'

	offParams  = 2
	offBassPat = offParams + 2*int(NumParams)        // 120
	offDrumPat = offBassPat + 2*PatternSlots*Steps*2 // 632
	offSlots   = offDrumPat + NumDrums*Steps         // 728
	offMute    = offSlots + 2                        // 730
	offKey     = offMute + 1                         // 731
	offMode    = offKey + 1                          // 732 (2바이트)
	offPlaying = offMode + 2                         // 734
	offChord   = offPlaying + 1                      // 735 (8바이트)
	offRack    = offChord + ChordBars + 5            // 748 (v3 크기)
	offInst    = offRack + RackSlots                 // 764
	offNCables = offInst + RackSlots                 // 780
	offCables  = offNCables + 1                      // 781
	cableBytes = 6
	offDevPar  = offCables + RackCables*cableBytes // 1165
	DevParams  = 8                                 // 슬롯당 로컬 파라미터 수
	offDevPat  = offDevPar + RackSlots*DevParams*2 // 1421
	StateSize  = offDevPat + RackSlots*Steps*2 + 3 // 1936
)

// WriteState — 현재 제어 상태를 dst에 쓴다. len(dst) < StateSize이면 0을 돌려주고 아무것도 쓰지 않는다.
func (e *Engine) WriteState(dst []byte) int {
	if len(dst) < StateSize {
		return 0
	}
	for i := range dst[:StateSize] {
		dst[i] = 0
	}
	dst[0], dst[1] = stateMagic0, stateMagic1
	for i := 0; i < int(NumParams); i++ {
		n := e.paramQ[i]
		dst[offParams+2*i] = byte(n)
		dst[offParams+2*i+1] = byte(n >> 8)
	}
	o := offBassPat
	for p := 0; p < 2; p++ {
		for s := 0; s < PatternSlots; s++ {
			for st := 0; st < Steps; st++ {
				dst[o] = e.bassPat[p][s][st].note
				dst[o+1] = e.bassPat[p][s][st].flags
				o += 2
			}
		}
	}
	o = offDrumPat
	for v := 0; v < NumDrums; v++ {
		for st := 0; st < Steps; st++ {
			dst[o] = e.drumPat[v][st]
			o++
		}
	}
	dst[offSlots] = e.slot[0]
	dst[offSlots+1] = e.slot[1]
	dst[offMute] = e.mute
	dst[offKey] = e.pendingKey
	dst[offMode] = e.mode[0] | e.dir[0]<<2
	dst[offMode+1] = e.mode[1] | e.dir[1]<<2
	if e.playing {
		dst[offPlaying] = 1
	}
	for b := 0; b < ChordBars; b++ {
		dst[offChord+b] = e.chordDeg[b] | e.chordFlags[b]<<3
	}
	r := &e.rack
	for s := 0; s < RackSlots; s++ {
		dst[offRack+s] = byte(r.kind[s])
		dst[offInst+s] = r.inst[s]
	}
	dst[offNCables] = byte(r.nCables)
	for i := 0; i < r.nCables; i++ {
		c := &r.cables[i]
		o := offCables + cableBytes*i
		dst[o] = c.src
		dst[o+1] = c.dst
		dst[o+2] = c.sp | c.dp<<4
		dst[o+3] = byte(c.bind)
		dst[o+4] = byte(c.gainQ)
		dst[o+5] = byte(c.gainQ >> 8)
	}
	for s := 0; s < RackSlots; s++ {
		for k := 0; k < DevParams; k++ {
			n := r.devParQ[s][k]
			o := offDevPar + 2*(s*DevParams+k)
			dst[o] = byte(n)
			dst[o+1] = byte(n >> 8)
		}
		for st := 0; st < Steps; st++ {
			o := offDevPat + 2*(s*Steps+st)
			dst[o] = r.devPat[s][st].note
			dst[o+1] = r.devPat[s][st].flags
		}
	}
	return StateSize
}

// ReadState — src에서 제어 상태를 복원한다. 길이 부족·매직 불일치면 false(상태 불변).
// 값은 재정규화한다. 위치(스텝·바)는 건드리지 않는다 — 호출자가 바 경계에서 부른다.
func (e *Engine) ReadState(src []byte) bool {
	if len(src) < StateSize || src[0] != stateMagic0 || src[1] != stateMagic1 {
		return false
	}
	for i := 0; i < int(NumParams); i++ {
		n := uint16(src[offParams+2*i]) | uint16(src[offParams+2*i+1])<<8
		if n > ParamSteps {
			n = ParamSteps
		}
		e.setParamQ(ParamID(i), n)
	}
	o := offBassPat
	for p := 0; p < 2; p++ {
		for s := 0; s < PatternSlots; s++ {
			for st := 0; st < Steps; st++ {
				n := src[o]
				if n > MaxNote {
					n = MaxNote
				}
				e.bassPat[p][s][st] = bassStep{note: n, flags: src[o+1] & (StepGate | StepSlide | StepAccent)}
				o += 2
			}
		}
	}
	o = offDrumPat
	for v := 0; v < NumDrums; v++ {
		for st := 0; st < Steps; st++ {
			e.drumPat[v][st] = src[o] & (StepGate | StepAccent)
			o++
		}
	}
	e.slot[0] = src[offSlots] & (PatternSlots - 1)
	e.slot[1] = src[offSlots+1] & (PatternSlots - 1)
	e.pendingSlot[0], e.pendingSlot[1] = e.slot[0], e.slot[1]
	e.mute = src[offMute]
	e.keyRoot = src[offKey] % NumKeys
	e.pendingKey = e.keyRoot
	for p := 0; p < 2; p++ {
		m := src[offMode+p] & 3
		d := src[offMode+p] >> 2 & 3
		if m >= NumModes {
			m = ModeBass
		}
		if d >= NumDirs {
			d = DirUp
		}
		e.mode[p], e.dir[p] = m, d
		e.arpIdx[p], e.arpDown[p] = 0, false
	}
	e.playing = src[offPlaying] != 0
	for b := 0; b < ChordBars; b++ {
		e.chordDeg[b] = src[offChord+b] & 7 % NumDegrees
		e.chordFlags[b] = src[offChord+b] >> 3 & ChordSeventh
	}
	// 랙 — 빈 랙에서 다시 놓는다(placeDevice·connect가 범위·중복·순환을 거른다).
	r := &e.rack
	r.reset()
	for s := 0; s < RackSlots; s++ {
		r.placeDevice(s, DeviceKind(src[offRack+s]), src[offInst+s])
	}
	if !r.hasKind(KindMain) {
		if !r.placeDevice(SlotMain, KindMain, 0) {
			for s := 0; s < RackSlots && !r.placeDevice(s, KindMain, 0); s++ {
			}
		}
	}
	n := int(src[offNCables])
	if n > RackCables {
		n = RackCables
	}
	for i := 0; i < n; i++ {
		o := offCables + cableBytes*i
		r.connect(src[o], src[o+2]&0x0F, src[o+1], src[o+2]>>4, ParamID(src[o+3]), uint16(src[o+4])|uint16(src[o+5])<<8)
	}
	for s := 0; s < RackSlots; s++ {
		for k := 0; k < DevParams; k++ {
			o := offDevPar + 2*(s*DevParams+k)
			r.setDevParam(s, k, uint16(src[o])|uint16(src[o+1])<<8)
			e.applyDevParam(s, k)
		}
		for st := 0; st < Steps; st++ {
			o := offDevPat + 2*(s*Steps+st)
			r.setDevStep(s, uint8(st), src[o], src[o+1])
		}
	}
	r.rebind(&e.params)
	return true
}
