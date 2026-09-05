// harmony_test.go — P2 화성 계약(스펙 §계약·§12.4) ↔ 단언 테스트.
//
//	계약             | 단언 테스트                                    | FAIL-first(2026-09-06 실측)
//	-----------------+-----------------------------------------------+--------------------------------------------------
//	SetKey 1회        | TestSetKeyOnce                                 | 구현 전 "SetKey 0개(1이어야)"
//	코드 진행          | TestChordProgression                           | 구현 전 "SetChord 방출 바가 없다"
//	BassMode 페이즈   | TestBassModePhases, TestBassModePatterns       | 구현 전 "첫 바 BassMode 0개" — 패턴 반쪽은
//	                  |                                               | 같은 항목의 Cmd 부재 FAIL로 선행 실측
//	도수 가중·범위     | TestResidentDrivesEngine30Min(resident_test)   | 도수화 전 절대음 분포 이탈(같은 표 참고)
//	화성 잠금          | TestHarmonyLock                                | 구현 전 "잠금 없는 구동에 화성 Cmd가 없다"
//
// 진행표·모드 표는 스펙 수치를 테스트에 베껴 둔다(구현 상수가 아님 — TestSetParamBudget 관례).
// 같은 이유로 어느 진행표가 골라지는지(cycleSeed 추첨)는 단언하지 않고, 6표 중 하나·
// 같은 사이클 내 일관성만 검증한다.
package resident

import (
	"testing"

	"github.com/midagedev/revirth/engine"
)

// specProgressions — §12.4 진행표(도수, 마이너).
var specProgressions = [6][engine.ChordBars]uint8{
	{0, 0, 5, 6, 0, 0, 3, 4}, // i i VI VII | i i iv v
	{0, 5, 2, 6, 0, 5, 2, 6}, // i VI III VII | i VI III VII
	{0, 3, 6, 2, 5, 1, 4, 0}, // i iv VII III | VI ii v i
	{0, 0, 0, 0, 5, 5, 6, 6}, // i i i i | VI VI VII VII
	{0, 6, 5, 6, 0, 6, 5, 4}, // i VII VI VII | i VII VI v
	{0, 4, 5, 3, 0, 4, 5, 6}, // i v VI iv | i v VI VII
}

// specBassMode — 페이즈 → (모드, 방향). Breakdown CHORD의 방향은 엔진이 무시(스펙 미지정 → DirUp).
var specBassMode = [numPhases]struct{ m, d uint8 }{
	{engine.ModeBass, engine.DirUp},
	{engine.ModeArp, engine.DirUp},
	{engine.ModeArp, engine.DirUpDown},
	{engine.ModeChord, engine.DirUp},
}

// ---- 계약 10: SetKey 정확히 1회 ----

func TestSetKeyOnce(t *testing.T) {
	for _, seed := range []uint32{0xC0FFEE, 0, 7} {
		norm := seed
		if norm == 0 { // New과 같은 정규화(0x9E3779B9) 뒤의 seed%12가 계약값
			norm = 0x9E3779B9
		}
		want := uint8(norm % engine.NumKeys)
		barDur := barDurOf(Rush)
		r := New(seed, Rush, Config{}) // 기본 포모도로 — 세션 경계(600s) 통과 포함
		n, atBar, val := 0, -1, uint8(255)
		for _, tr := range drive(r, 400, barDur) {
			for _, c := range tr.cmds {
				if c.Kind == engine.SetKey {
					n++
					atBar = tr.bar
					val = c.A
				}
			}
		}
		if n != 1 {
			t.Fatalf("seed %#x: SetKey %d개(세션당 정확히 1이어야)", seed, n)
		}
		if val != want {
			t.Fatalf("seed %#x: SetKey A=%d, want %d(norm%%12)", seed, val, want)
		}
		if atBar != 0 {
			t.Fatalf("seed %#x: SetKey가 바 %d에 나왔다(세션 첫 바=0이어야)", seed, atBar)
		}
	}
}

// ---- 계약 10: 코드 진행(사이클 시작·Drop·Breakdown 진입 바) ----

func TestChordProgression(t *testing.T) {
	const bars = 400
	const seed = uint32(0xBADA55)
	barDur := barDurOf(Rush)
	cfg := normConfig(Config{})
	r := New(seed, Rush, Config{})
	var chordAt [bars][]engine.Cmd
	var phaseAt [bars]Phase
	var entryAt [bars]bool
	last := Phase(255)
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		for _, c := range r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true}) {
			if c.Kind == engine.SetChord {
				chordAt[b] = append(chordAt[b], c)
			}
		}
		phaseAt[b] = r.Phase()
		entryAt[b] = last == 255 || phaseAt[b] != last
		last = phaseAt[b]
		for s := 1; s < 16; s++ {
			r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	// 1) 방출 바 수: Intro·Drop·Breakdown 진입 바에만, 세 종류 모두 관측되어야 한다.
	perPhase := [numPhases]int{}
	for b := 0; b < bars; b++ {
		if len(chordAt[b]) == 0 {
			continue
		}
		if !entryAt[b] || phaseAt[b] == Build {
			t.Fatalf("바 %d(페이즈 %d 진입 %v): SetChord는 Intro/Drop/Breakdown 진입 바에만 허용", b, phaseAt[b], entryAt[b])
		}
		perPhase[phaseAt[b]]++
	}
	for ph := 0; ph < int(numPhases); ph++ {
		n := perPhase[ph]
		if (ph == int(Intro) || ph == int(Drop) || ph == int(Breakdown)) && n == 0 {
			t.Fatalf("%d 페이즈 진입 바의 SetChord가 한 번도 없다", ph)
		}
	}
	// 2) 바당 정확히 8개·A는 0..7 순서·도수는 진행표 6개 중 하나·같은 사이클이면 같은 진행·7th 규칙.
	type cyc struct{ s, c int }
	perCycle := map[cyc][engine.ChordBars]uint8{}
	sevenths := 0
	for b := 0; b < bars; b++ {
		cs := chordAt[b]
		if len(cs) == 0 {
			continue
		}
		if len(cs) != 8 {
			t.Fatalf("바 %d: SetChord %d개(정확히 8이어야)", b, len(cs))
		}
		var deg [engine.ChordBars]uint8
		for i, c := range cs {
			if c.A != uint8(i) {
				t.Fatalf("바 %d: SetChord[%d].A=%d(바 인덱스 순서여야)", b, i, c.A)
			}
			deg[i] = c.B
		}
		match := false
		for _, p := range specProgressions {
			if deg == p {
				match = true
				break
			}
		}
		if !match {
			t.Fatalf("바 %d: 진행표 6개 중 일치 없음 %v", b, deg)
		}
		sess, cycIdx, _, _ := cycleAt(seed, cfg, float64(b)*barDur)
		key := cyc{sess, cycIdx}
		if prev, ok := perCycle[key]; ok && prev != deg {
			t.Fatalf("사이클 (%d,%d): 진행이 바뀌었다 %v → %v", sess, cycIdx, prev, deg)
		}
		perCycle[key] = deg
		for i, c := range cs {
			want7 := phaseAt[b] == Drop && (deg[i] == 3 || deg[i] == 4 || deg[i] == 6)
			if want7 && c.C != engine.ChordSeventh {
				t.Fatalf("바 %d 마디 %d(도수 %d): Drop 7th 누락", b, i, deg[i])
			}
			if !want7 && c.C != 0 {
				t.Fatalf("바 %d 마디 %d(도수 %d): 7th가 있으면 안 되는 방출(C=%d, 페이즈 %d)", b, i, deg[i], c.C, phaseAt[b])
			}
			if c.C == engine.ChordSeventh {
				sevenths++
			}
		}
	}
	if len(perCycle) < 2 {
		t.Fatalf("관측한 사이클 %d개 — 일관성 검사에 부족", len(perCycle))
	}
	if sevenths == 0 {
		t.Fatal("Drop 재방출의 7th가 하나도 관측되지 않았다")
	}
}

// ---- 계약 10: BassMode 페이즈 연동 ----

func TestBassModePhases(t *testing.T) {
	const bars = 400
	barDur := barDurOf(Rush)
	r := New(0xFACE, Rush, Config{})
	var modeAt [bars][]engine.Cmd
	var phaseAt [bars]Phase
	var entryAt [bars]bool
	last := Phase(255)
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		for _, c := range r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true}) {
			if c.Kind == engine.BassMode {
				modeAt[b] = append(modeAt[b], c)
			}
		}
		phaseAt[b] = r.Phase()
		entryAt[b] = last == 255 || phaseAt[b] != last
		last = phaseAt[b]
		for s := 1; s < 16; s++ {
			r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	perPhase := [numPhases]int{}
	for b := 0; b < bars; b++ {
		ms := modeAt[b]
		if len(ms) == 0 {
			continue
		}
		if !entryAt[b] {
			t.Fatalf("바 %d(페이즈 %d): BassMode는 페이즈 진입 바에만 허용", b, phaseAt[b])
		}
		if len(ms) > 1 {
			t.Fatalf("바 %d: BassMode %d개(진입 바당 1개)", b, len(ms))
		}
		c := ms[0]
		if c.A != 1 {
			t.Fatalf("바 %d: BassMode A=%d(파트 B=1이어야)", b, c.A)
		}
		want := specBassMode[phaseAt[b]]
		if c.B != want.m || c.C != want.d {
			t.Fatalf("바 %d(페이즈 %d): BassMode=(%d,%d), want (%d,%d)", b, phaseAt[b], c.B, c.C, want.m, want.d)
		}
		perPhase[phaseAt[b]]++
	}
	if len(modeAt[0]) != 1 {
		t.Fatalf("첫 바 BassMode %d개(초기 모드 명시로 1개)", len(modeAt[0]))
	}
	for ph := 0; ph < int(numPhases); ph++ {
		if perPhase[ph] == 0 {
			t.Fatalf("%d 페이즈의 BassMode가 한 번도 없다", ph)
		}
	}
}

// ---- 계약 12: 화성 잠금 ----

func TestHarmonyLock(t *testing.T) {
	barDur := barDurOf(DeepFocus)
	// 새너티(FAIL-first 고리): 잠금 없는 구동에 화성 Cmd가 있어야 이 테스트가 의미 있다.
	s := New(99, DeepFocus, Config{})
	harm := 0
	for _, tr := range drive(s, 40, barDur) {
		for _, c := range tr.cmds {
			if c.Kind == engine.SetKey || c.Kind == engine.SetChord || c.Kind == engine.BassMode {
				harm++
			}
		}
	}
	if harm == 0 {
		t.Fatal("잠금 없는 40바 구동에 화성 Cmd가 없다 — 잠금 단언이 공허하다")
	}

	const bars = 900 // 30분(DeepFocus 바 2s)
	r := New(0xC0FFEE, DeepFocus, Config{})
	r.LockHarmony()
	if !r.HarmonyLocked() {
		t.Fatal("LockHarmony 뒤 HarmonyLocked()가 false")
	}
	harm, bass, drum, drop := 0, 0, 0, 0
	for _, tr := range drive(r, bars, barDur) {
		for _, c := range tr.cmds {
			switch c.Kind {
			case engine.SetKey, engine.SetChord, engine.BassMode:
				harm++
			case engine.BassStep:
				bass++
			case engine.DrumStep:
				drum++
			case engine.Drop:
				drop++
			}
		}
	}
	if harm != 0 {
		t.Fatalf("LockHarmony 뒤 30분에 화성 Cmd %d개(0이어야)", harm)
	}
	if bass < 1000 || drum < 1000 {
		t.Fatalf("잠금 뒤 패턴 정지: BassStep %d·DrumStep %d(계속 나와야)", bass, drum)
	}
	if drop == 0 {
		t.Fatal("잠금 뒤 Drop이 한 번도 없다(계속되어야)")
	}
	// Resume은 노브 잠금만 푼다 — 화성 잠금은 세션 내 영구(§12.2).
	r.Resume()
	if !r.HarmonyLocked() {
		t.Fatal("Resume 뒤 화성 잠금이 풀렸다(세션 내 영구여야)")
	}
	for _, tr := range drive(r, 50, barDur) {
		for _, c := range tr.cmds {
			if c.Kind == engine.SetKey || c.Kind == engine.SetChord || c.Kind == engine.BassMode {
				t.Fatalf("Resume 뒤 화성 Cmd %d가 나왔다(건드리지 않아야)", c.Kind)
			}
		}
	}
}

// ---- 계약 10(패턴 반쪽): 페이즈별 B 패턴 모양 ----
//
// 쓰기 바(emitPatterns)의 페이즈는 생성 바의 페이즈와 같다 — 진입 바는 재생성이
// 우선이라 쓰기가 다음 바로 미뤄지므로(onBar). 이 불변식을 전제로 쓰기 바 페이즈로
// B 패턴을 검증한다.

func TestBassModePatterns(t *testing.T) {
	const bars = 400
	barDur := barDurOf(Rush)
	r := New(0xD15EA5E, Rush, Config{})
	type bstep struct {
		note, flags uint8
	}
	var phaseAt [bars]Phase
	var writeB [bars][engine.Steps]bstep
	var writeA [bars][engine.Steps]bstep
	writes := 0
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		cs := r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		phaseAt[b] = r.Phase()
		sawB := false
		for _, c := range cs {
			if c.Kind == engine.BassStep {
				if c.A == 1 {
					writeB[b][c.B] = bstep{c.C, c.D}
					sawB = true
				} else {
					writeA[b][c.B] = bstep{c.C, c.D}
				}
			}
		}
		if sawB {
			writes++
		}
		for s := 1; s < 16; s++ {
			r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	if writes < 50 {
		t.Fatalf("패턴 쓰기 바 %d개 — 검증에 부족", writes)
	}
	perPhase := [numPhases]int{}
	for b := 0; b < bars; b++ {
		var anyB bool
		for st := 0; st < engine.Steps; st++ {
			if writeB[b][st].flags&engine.StepGate != 0 {
				anyB = true
				break
			}
		}
		if !anyB {
			continue // 이 바에 B 쓰기가 없다
		}
		ph := phaseAt[b]
		perPhase[ph]++
		for st := 0; st < engine.Steps; st++ {
			s := writeB[b][st]
			if s.flags&engine.StepGate == 0 {
				continue
			}
			switch ph {
			case Build, Drop: // ARP: note 14 고정·슬라이드 없음(액센트 accP 허용)
				if s.note != 14 {
					t.Fatalf("바 %d(ARP) 스텝 %d: B 노트 %d(14여야)", b, st, s.note)
				}
				if s.flags&engine.StepSlide != 0 {
					t.Fatalf("바 %d(ARP) 스텝 %d: B 슬라이드 금지 위반", b, st)
				}
			case Breakdown: // CHORD: 스텝 0·8·12만·액센트 0
				if st != 0 && st != 8 && st != 12 {
					t.Fatalf("바 %d(CHORD): 스텝 %d가 게이트(0·8·12만)", b, st)
				}
				if s.note != 14 || s.flags&engine.StepAccent != 0 {
					t.Fatalf("바 %d(CHORD) 스텝 %d: note %d·액센트 %d", b, st, s.note, s.flags&engine.StepAccent)
				}
			default: // Intro BASS: A가 게이트된 스텝과 겹치지 않는다(오프비트 보완)
				if writeA[b][st].flags&engine.StepGate != 0 {
					t.Fatalf("바 %d(BASS) 스텝 %d: A·B가 같은 스텝에 게이트", b, st)
				}
			}
		}
		if ph == Breakdown { // 세 스탭이 모두 있다
			for _, st := range [...]int{0, 8, 12} {
				if writeB[b][st].flags&engine.StepGate == 0 {
					t.Fatalf("바 %d(CHORD): 스탭 스텝 %d가 비었다", b, st)
				}
			}
		}
	}
	for ph := 0; ph < int(numPhases); ph++ {
		if perPhase[ph] == 0 {
			t.Fatalf("%d 페이즈의 B 패턴 쓰기가 한 번도 없다", ph)
		}
	}
}
