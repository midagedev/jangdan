// state.go — 제어 상태 직렬화(키프레임). 리드 소유(docs/impl-plan-2026-09-05.md §2.3이 원본).
//
// 키프레임은 바 경계의 제어 상태다: 파라미터·패턴·슬롯·뮤트. 보이스 내부 상태(필터 메모리·
// 엔벨로프·딜레이 버퍼)는 포함하지 않는다 — 복원은 바 경계에서만 의미가 있다.
// 레이아웃(리틀엔디언, 고정 StateSize 바이트):
//   [0..2)          magic 'J','1'
//   [2..68)         params[33] uint16
//   [68..580)       bass 패턴 2파트 × 8슬롯 × 16스텝 × (note u8, flags u8)
//   [580..676)      drum 패턴 6보이스 × 16스텝 × flags u8
//   [676..678)      선택 슬롯 BassA, BassB
//   [678]           뮤트 비트(bit part)
//   [679..684)      예약(0)
// ReadState는 검증 후 폐기가 아니라 재정규화한다(note>MaxNote → MaxNote, flags 마스킹, 슬롯 &7).
// 이 파일에는 곱셈-덧셈이 없다.
package engine

const (
	stateMagic0 = 'J'
	stateMagic1 = '1'

	offParams   = 2
	offBassPat  = offParams + 2*int(NumParams)                        // 68
	offDrumPat  = offBassPat + 2*PatternSlots*Steps*2                 // 580
	offSlots    = offDrumPat + NumDrums*Steps                         // 676
	offMute     = offSlots + 2                                        // 678
	StateSize   = offMute + 1 + 5                                     // 684
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
	return true
}
