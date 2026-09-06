// state.go — 제어 상태 직렬화(키프레임). 리드 소유(docs/impl-plan-2026-09-05.md §2.3·§12가 원본).
//
// 키프레임은 바 경계의 제어 상태다: 파라미터·패턴·슬롯·뮤트·조성·코드 트랙·모드·트랜스포트.
// 보이스 내부 상태(필터 메모리·엔벨로프·딜레이 버퍼)는 포함하지 않는다 — 복원은 바 경계에서만 의미가 있다.
// 레이아웃 v3(리틀엔디언, 고정 StateSize 바이트 — §13.1 파라미터 33→59로 v2에서 +52):
//
//	[0..2)          magic 'J','3'
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
// 키 %12, 도수 %7, 모드·방향 범위 밖은 0). v2('J','2', 696바이트) 이하는 거부한다 — 새
// 파라미터(믹서·버스 26개)의 기본값 해석이 없어 반쪽 상태가 되고, 지속 저장된
// 키프레임은 없다(메모리·리플레이 전용 — 로그 재생이 정본). v1('J','1', 684바이트)도
// 거부다(v1 당시부터 note 의미가 절대음→도수로 바뀌어 호환이 없다).
// 이 파일에는 곱셈-덧셈이 없다.
package engine

const (
	stateMagic0 = 'J'
	stateMagic1 = '3'

	offParams  = 2
	offBassPat = offParams + 2*int(NumParams)        // 120
	offDrumPat = offBassPat + 2*PatternSlots*Steps*2 // 632
	offSlots   = offDrumPat + NumDrums*Steps         // 728
	offMute    = offSlots + 2                        // 730
	offKey     = offMute + 1                         // 731
	offMode    = offKey + 1                          // 732 (2바이트)
	offPlaying = offMode + 2                         // 734
	offChord   = offPlaying + 1                      // 735 (8바이트)
	StateSize  = offChord + ChordBars + 5            // 748
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
	return true
}
