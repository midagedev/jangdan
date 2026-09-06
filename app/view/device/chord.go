// chord.go — P2 화성 UI 상태기계: 코드 트랙 띠(8마디 셀 + 도수 선택기)와 베이스 B 모드 순환.
// 계약 원본: docs/impl-plan-2026-09-05.md §12.3.
//
// derive-don't-store: 띠 라벨은 Bridge.Chord에서 매 프레임 읽고 문자열만 캐시한다
// (값 변화 시에만 재구성 = 할당), B 모드 표시값·순환 시작점도 Bridge.Mode에서 매 프레임
// 유도한다(뷰가 모드를 저장하지 않는다). 뷰 상태는 선택기의 열림·대상 마디·무조작 시각뿐.
//
// 화성 API 구 브리지 호환: jsBridge.Chord/Mode/KeyRoot는 호스트(window.jd)에 해당
// 함수가 없으면 syscall/js 패닉으로 앱 전체가 죽는다(실측 2026-09-06 캡처: 기기 뷰 첫
// Update에 "property chord is not a function" → Go program exited). 화성 읽기는 전부
// 아래 래치 가드를 지난다 — 첫 패닉을 1회 받아 harmonyOK를 끊고 이후엔 기본값만 돌려준다
// (P2 호스트가 API를 갖추면 패닉이 없어 그대로 동작). 구조적 수정 자리는 bridge_js.go의
// 함수 존재 검사지만 app/core는 이 라운드 읽기 전용 — 코어 라운드에서 흡수할 과도 방어다.
package device

import (
	"strconv"

	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// 수치 계약(스펙 P2-device origin).
const (
	chordGap        = 3.0  // 셀 간 간격(px)
	chordSelTout    = 6.0  // 선택기 무조작 닫힘(초)
	dispKnobValDur  = 2.0  // B 표시창: 노브 접촉 뒤 값 표시 지속(초)
	chordLabelScale = 0.62 // 코드 트랙 라벨 스케일(버튼 0.45보다 크게 — 비전 FIX 2026-09-06: 10px 로마 숫자는 코드 줄로 안 읽힘)
)

// 도수 로마 숫자 표기(0..6 — 자연 마이너의 1·2·4·5도는 소문자, 3·6·7도는 대문자).
var romanDeg = [engine.NumDegrees]string{"i", "ii", "III", "iv", "v", "VI", "VII"}

// 키 이름(루트 0..11, 0 = C) — 하단 표시창은 여기에 "m"을 붙인다.
var keyNames = [engine.NumKeys]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// chordCellCache — 셀 라벨 캐시. deg·flags가 변화할 때만 text를 재구성한다.
type chordCellCache struct {
	deg, flags uint8
	init       bool
	text       string
}

// chordState — 코드 트랙 띠 상태(선택기).
type chordState struct {
	open  bool
	bar   int     // 선택 중인 마디 0..7
	lastT float64 // 마지막 상호작용 시각(ctx.Now)
	cells [engine.ChordBars]chordCellCache

	barLbl  string // 선택기 8번째 셀 라벨("B<bar> 7") — 열릴 때 1회만 구성
	barLblN int    // barLbl을 구성한 마디(-1 = 없음)
}

// bridgeChord/bridgeMode/bridgeKey — 화성 읽기 래치 가드(위 헤더 참조). 정상 상태는
// 불 검사 1개 + 직접 호출, 패닉 시 1회 복구 후 기본값(모드 BASS·키 C·도수 0).
func (v *View) bridgeChord(b core.Bridge, bar int) (deg, flags uint8) {
	if !v.harmonyOK {
		return 0, 0
	}
	defer func() {
		if recover() != nil {
			v.harmonyOK = false
			deg, flags = 0, 0
		}
	}()
	return b.Chord(bar)
}

func (v *View) bridgeMode(b core.Bridge, p engine.Part) (mode, dir uint8) {
	if !v.harmonyOK {
		return 0, 0
	}
	defer func() {
		if recover() != nil {
			v.harmonyOK = false
			mode, dir = 0, 0
		}
	}()
	return b.Mode(p)
}

func (v *View) bridgeKey(b core.Bridge) int {
	if !v.harmonyOK {
		return 0
	}
	defer func() {
		if recover() != nil {
			v.harmonyOK = false
		}
	}()
	return b.KeyRoot()
}

// initChord — newView에서 셀 기하를 계산한다(좌표 소유권은 레이아웃 JSON).
// 8셀 균등 분할 + 셀 간 chordGap: 셀폭 = (띠 폭 − 7×간격)/8, 마지막 셀이 띠 끝에 정확히 닿는다.
func (v *View) initChord() {
	v.chordRect = v.layout.ChordTrack.Rect
	w := (v.chordRect[2] - chordGap*float64(engine.ChordBars-1)) / float64(engine.ChordBars)
	for i := range v.chordCells {
		v.chordCells[i] = core.Rect{v.chordRect[0] + float64(i)*(w+chordGap), v.chordRect[1], w, v.chordRect[3]}
	}
	v.chord.barLblN = -1
}

// chordCellAt — 띠 안 좌표의 셀 인덱스(0..7). 띠 밖은 -1. 셀 사이 간격(3px)은 가까운 셀로.
func (v *View) chordCellAt(x, y float64) int {
	r := v.chordRect
	if !r.Contains(x, y) || r[2] <= 0 {
		return -1
	}
	pitch := v.chordCells[1][0] - v.chordCells[0][0] // 셀폭 + 간격
	if pitch <= 0 {
		return -1
	}
	i := int((x - r[0]) / pitch)
	if i < 0 {
		i = 0
	}
	if i > engine.ChordBars-1 {
		i = engine.ChordBars - 1
	}
	if x < v.chordCells[i][0] && i > 0 {
		if x-(v.chordCells[i-1][0]+v.chordCells[i-1][2]) < v.chordCells[i][0]-x {
			i--
		}
	}
	if end := v.chordCells[i][0] + v.chordCells[i][2]; x >= end && i < engine.ChordBars-1 {
		if x-end < v.chordCells[i+1][0]-x {
			i++
		}
	}
	return i
}

// tapChordBand — 띠 안 눌림. 닫힘: 셀 = 마디 → 선택기 연다(송신 없음). 열림: 셀 0..6 =
// 도수 확정(SetChord 송신 후 닫힘), 셀 7 = 7th 토글(도수 유지, 즉시 송신, 열림 유지).
func (v *View) tapChordBand(ctx *core.Ctx, cell int) {
	if cell < 0 {
		return
	}
	st := &v.chord
	if !st.open {
		st.open, st.bar, st.lastT = true, cell, ctx.Now
		if st.barLblN != cell {
			b := append(v.scratch[:0], 'B')
			b = strconv.AppendInt(b, int64(cell), 10)
			b = append(b, ' ', '7')
			st.barLbl = string(b) // 재구성 = 할당 — 마디가 바뀔 때만
			st.barLblN = cell
		}
		return
	}
	st.lastT = ctx.Now
	deg, flags := v.bridgeChord(ctx.Bridge, st.bar)
	if cell < engine.NumDegrees {
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SetChord, A: uint8(st.bar), B: uint8(cell), C: flags}, core.Human)
		st.open = false
		return
	}
	ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SetChord, A: uint8(st.bar), B: deg, C: flags ^ engine.ChordSeventh}, core.Human)
}

// chordIdleClose — 선택기 무조작 chordSelTout초 뒤 닫힘(송신 없음). Update에서 매 프레임.
func (v *View) chordIdleClose(now float64) {
	if v.chord.open && now-v.chord.lastT >= chordSelTout {
		v.chord.open = false
	}
}

// cacheChord — 셀 라벨 캐시. Bridge.Chord를 매 프레임 읽고 값이 변한 셀만 재구성한다.
// 도수는 mod 7로 재정규화(범위 밖 값도 "?"가 아니라 도수 라벨로 그린다 — 3클래스 입력 방어).
func (v *View) cacheChord(ctx *core.Ctx) {
	for i := range v.chord.cells {
		deg, flags := v.bridgeChord(ctx.Bridge, i)
		c := &v.chord.cells[i]
		if c.init && c.deg == deg && c.flags == flags {
			continue
		}
		c.deg, c.flags, c.init = deg, flags, true
		b := append(v.scratch[:0], romanDeg[deg%engine.NumDegrees]...)
		if flags&engine.ChordSeventh != 0 {
			b = append(b, '7')
		}
		c.text = string(b) // 재구성 = 할당 — 값 변화 시에만
		v.rebuilds++
	}
}

// nextBassMode — 모드 순환 BASS → ARP UP → ARP DN → ARP UD → CHORD → BASS의 다음 값.
// 현재 값은 호출부가 Bridge.Mode에서 읽은 것. 범위 밖 mode·dir은 나머지로 정규화한다.
func nextBassMode(mode, dir uint8) (uint8, uint8) {
	switch mode % engine.NumModes {
	case engine.ModeArp:
		switch dir % engine.NumDirs {
		case engine.DirUp:
			return engine.ModeArp, engine.DirDown
		case engine.DirDown:
			return engine.ModeArp, engine.DirUpDown
		default:
			return engine.ModeChord, engine.DirUp
		}
	case engine.ModeChord:
		return engine.ModeBass, engine.DirUp
	default:
		return engine.ModeArp, engine.DirUp
	}
}

// bassModeName — B 표시창 모드 문자열. 정규화는 nextBassMode와 같은 규칙.
func bassModeName(mode, dir uint8) string {
	switch mode % engine.NumModes {
	case engine.ModeArp:
		switch dir % engine.NumDirs {
		case engine.DirDown:
			return "ARP DN"
		case engine.DirUpDown:
			return "ARP UD"
		default:
			return "ARP UP"
		}
	case engine.ModeChord:
		return "CHORD"
	default:
		return "BASS"
	}
}

// tapBassModeDisplay — B 표시창 탭: 모드 순환 송신. 현재 값은 브리지에서 유도(저장 없음).
func (v *View) tapBassModeDisplay(ctx *core.Ctx) {
	mode, dir := v.bridgeMode(ctx.Bridge, engine.BassB)
	nm, nd := nextBassMode(mode, dir)
	ctx.Bridge.Cmd(engine.Cmd{Kind: engine.BassMode, A: uint8(engine.BassB), B: nm, C: nd}, core.Human)
}

// appendKey — 키 이름(루트 + "m")을 b에 붙인다. key는 0..11로 정규화.
func appendKey(b []byte, key int) []byte {
	k := key % engine.NumKeys
	if k < 0 {
		k += engine.NumKeys
	}
	return append(b, keyNames[k]+"m"...)
}
