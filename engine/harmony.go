// harmony.go — 조성·코드 트랙·도수 해석. 리드 소유(계약 원본: docs/impl-plan-2026-09-05.md §12).
//
// Phase 2부터 베이스 패턴의 note는 절대 음이 아니라 **코드 기준 도수**다:
//
//	note = octave*7 + degree   (0..MaxNote, octave 0..5, degree 0..6)
//	degree 0 = 현재 마디 코드의 루트, 1..6 = 그 위로 이어지는 조성 음계 음(0·2·4가 코드 톤, 6이 7th)
//
// 조성은 자연 마이너(에올리안) 하나이고 루트(keyRoot 0..11, 0 = C)만 바뀐다. 코드 트랙은
// ChordBars(8)마디 순환이며 각 마디에 다이어토닉 도수(0..6)와 7th 플래그 하나를 가진다.
// 같은 패턴이라도 코드를 바꾸면 소리가 따라온다 — 패턴은 리듬·윤곽, 코드 트랙은 화성.
//
// ResolveNote가 (keyRoot, chordDeg, note) → C1 기준 반음(0..MaxSemis)을 돌려준다. 곱셈-덧셈이
// 정수뿐이라 FMA 계약과 무관하다. 범위 밖 입력은 전부 마스킹·클램프(정규화 계약).
package engine

const (
	ChordBars  = 8  // 코드 트랙 길이(마디)
	NumDegrees = 7  // 다이어토닉 도수 수
	NumKeys    = 12 // 루트 피치 클래스 수
	MaxSemis   = 48 // ResolveNote 상한(C1 기준 반음, C5)

	ChordSeventh uint8 = 1 << 0 // SetChord.C / Chord() flags — 7th 추가(코드 톤 4개)
)

// 베이스 모드(BassMode.B)와 아르페지오 방향(BassMode.C).
const (
	ModeBass  uint8 = 0 // 패턴 도수를 그대로 연주(기본)
	ModeArp   uint8 = 1 // 게이트된 스텝마다 코드 톤을 순서대로(옥타브는 패턴 note/7)
	ModeChord uint8 = 2 // 3음 패러포닉 스탭(루트·3·5, 7th면 3·5·7)
	NumModes  uint8 = 3

	DirUp     uint8 = 0
	DirDown   uint8 = 1
	DirUpDown uint8 = 2
	NumDirs   uint8 = 3
)

// minorScale — 자연 마이너 반음 오프셋(루트 기준).
var minorScale = [NumDegrees]uint8{0, 2, 3, 5, 7, 8, 10}

// degreeSemis — 코드 루트 도수 chordDeg에서 위로 deg번째 음계 음의 반음 오프셋(키 루트 기준,
// 한 옥타브 넘으면 +12). 예: chordDeg 5(VI), deg 2 → 음계 인덱스 0(옥타브 넘음) → 12.
func degreeSemis(chordDeg, deg uint8) uint8 {
	i := int(chordDeg%NumDegrees) + int(deg%NumDegrees)
	oct := uint8(0)
	if i >= NumDegrees {
		i -= NumDegrees
		oct = 12
	}
	return minorScale[i] + oct
}

// ResolveNote — 도수 note를 C1 기준 반음으로. note > MaxNote는 MaxNote, 결과는 0..MaxSemis 클램프.
func ResolveNote(keyRoot, chordDeg, note uint8) uint8 {
	if note > MaxNote {
		note = MaxNote
	}
	s := int(keyRoot%NumKeys) + 12*int(note/NumDegrees) + int(degreeSemis(chordDeg, note%NumDegrees))
	if s > MaxSemis {
		s = MaxSemis
	}
	return uint8(s)
}

// ChordToneDeg — k번째 코드 톤의 도수(0→루트, 1→3rd, 2→5th, 3→7th). k는 코드 톤 수로 마스킹.
func ChordToneDeg(k uint8, seventh bool) uint8 {
	n := uint8(3)
	if seventh {
		n = 4
	}
	return (k % n) * 2
}

// ChordTones — 코드 톤 수(3 또는 4).
func ChordTones(flags uint8) uint8 {
	if flags&ChordSeventh != 0 {
		return 4
	}
	return 3
}
