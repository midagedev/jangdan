// harmony.go — 화성 명령: 조성 선언·코드 진행표·B 모드 페이즈 연동.
//
// Phase 2 화성(§12.4)의 레지던트 쪽 반쪽이다. 조성·코드 트랙·B 모드의 의미는
// engine/harmony.go가 소유하고, 여기서는 **언제 무엇을 내보낼지**만 정한다:
//
//   - 세션 첫 바: SetKey 1개(엔진 초기값과 같은 seed%12 — 로그가 자기서술적이 된다)
//   - 사이클 시작(Intro 진입) 바: 진행표 6개 중 하나를 cycleSeed로 골라 SetChord×8
//   - Drop 진입 바: 같은 진행을 7th(도수 3·4·6 = iv·v·VII)로 재방출
//   - Breakdown 진입 바: 7th 없이 재방출
//   - 페이즈 진입 바(첫 바 포함): BassMode A=1(모드·방향은 bassModeFor 표)
//
// derive-don't-store: 진행표 선택·7th 여부는 매 번 (cycleSeed, phase)에서 유도한다.
// 저장 상태는 없다(방출 순간의 유도뿐). 사람이 화성을 만지면 LockHarmony로 이 전체가
// 침묵한다(resident.go onBar의 게이트).
package resident

import "github.com/midagedev/jangdan/engine"

// progressions — 마이너 8마디 코드 진행표(도수; §12.4 계약 수치).
var progressions = [6][engine.ChordBars]uint8{
	{0, 0, 5, 6, 0, 0, 3, 4}, // i i VI VII | i i iv v
	{0, 5, 2, 6, 0, 5, 2, 6}, // i VI III VII | i VI III VII
	{0, 3, 6, 2, 5, 1, 4, 0}, // i iv VII III | VI ii v i
	{0, 0, 0, 0, 5, 5, 6, 6}, // i i i i | VI VI VII VII
	{0, 6, 5, 6, 0, 6, 5, 4}, // i VII VI VII | i VII VI v
	{0, 4, 5, 3, 0, 4, 5, 6}, // i v VI iv | i v VI VII
}

const numProgressions = 6

// progressionAt — 사이클 시드 → 진행표. 같은 사이클의 Intro/Drop/Breakdown 방출이
// 같은 표를 얻는 것도 이 함수 하나로 보장된다(저장 없음).
func progressionAt(cycleSeed uint32) *[engine.ChordBars]uint8 {
	return &progressions[xs32(cycleSeed^0x5F3759DF)%numProgressions]
}

// bassModeFor — 페이즈 → 베이스 B (모드, 방향). 방출(resident.go)과 패턴 생성
// (generator.go)이 같은 표를 쓴다 — 단일 소유자. Breakdown CHORD의 방향은 엔진이
// 무시하므로 DirUp으로 고정(스펙 미지정).
func bassModeFor(ph Phase) (mode, dir uint8) {
	switch ph {
	case Intro:
		return engine.ModeBass, engine.DirUp
	case Build:
		return engine.ModeArp, engine.DirUp
	case Drop:
		return engine.ModeArp, engine.DirUpDown
	default: // Breakdown
		return engine.ModeChord, engine.DirUp
	}
}

// seventhFor — Drop 재방출에서 7th를 붙일 마디 판정(도수 3·4·6 = iv·v·VII).
func seventhFor(deg uint8) uint8 {
	if deg == 3 || deg == 4 || deg == 6 {
		return engine.ChordSeventh
	}
	return 0
}

// emitChordTrack — 진행표를 SetChord×8로 방출. seventh가 true면 도수 3·4·6 마디에
// ChordSeventh. 호출은 harmonyLocked 게이트 안에서만(resident.go onBar).
func (r *Resident) emitChordTrack(cycleSeed uint32, seventh bool) {
	prog := progressionAt(cycleSeed)
	for bar := 0; bar < engine.ChordBars; bar++ {
		c := engine.Cmd{Kind: engine.SetChord, A: uint8(bar), B: (*prog)[bar]}
		if seventh {
			c.C = seventhFor((*prog)[bar])
		}
		r.emit(c)
	}
}
