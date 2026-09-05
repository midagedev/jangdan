// resident_test.go — 계약↔단언 표(스펙 §계약 1..9 ↔ 테스트).
//
//	계약        | 단언 테스트                                | FAIL-first 변이(전부 실측 FAIL 확인)
//	------------+--------------------------------------------+----------------------------------------------
//	1 API/버퍼  | TestTickWithoutBarStart, TestHand          | dispatch를 항상 onBar로(패턴 Cmd 누출); Hand 유지 1바→3바
//	2 생성기    | TestKickSkeleton, TestSlotRotation,         | 생성 노트 +1(스케일 이탈 — P2 도수화 전); 골격 12 제거;
//	            | TestDeterminism                             | 슬롯 기저 4→0
//	3 에너지    | TestPhaseRatios, TestPhaseDerivation,      | Intro 경계 0.15→0.20(점유 이탈);
//	            | TestCmdRanges                              | clampNote 해제+옥타브 +24(노트 범위)
//	4 바이브    | TestVibeTempo                              | Lofi 112→110 BPM
//	5 포모도로  | TestPomodoroTransitions                    | 강제 Build 16바→1바
//	6 잠금      | TestLockResume                             | 잠금 스킵 제거(방출 누출)
//	7 결정론    | TestDeterminism                            | New가 seed 무시(고정 시드)
//	8 범위/수  | TestCmdRanges, TestSetParamBudget          | 예산 8→9(바당 상한 초과; 테스트는 계약 수치 8과 비교)
//	9 금지어    | grep 게이트(검증 명령 참고, 소스에 없음)    |
//	10 화성     | TestSetKeyOnce, TestChordProgression,        | 구현 전 실측: SetKey 0개·SetChord 0개·
//	            | TestBassModePhases (harmony_test.go)         | 첫 바 BassMode 0개(2026-09-06 FAIL)
//	11 도수     | TestResidentDrivesEngine30Min(도수 분포),    | 도수화 전 절대음 분포 실측: 루트 0.030·
//	            | TestCmdRanges(note ≤ MaxNote)                | 코드톤 0.000·경과음 0.970(n=891, FAIL)
//	12 화성잠금 | TestHarmonyLock (harmony_test.go)            | 새너티 “화성 Cmd 없음” FAIL(잠금 전)
//
// 실산출물: TestResidentDrivesEngine30Min — 30분 Cmd를 engine.New에 실제 Apply하며
// Render해 NaN 0·peak ≤ 1.0·RMS > 0.01과 페이즈별 평균 RMS 표를 확인한다.
package resident

import (
	"math"
	"testing"

	"github.com/midagedev/revirth/engine"
)

// ---- 테스트 헬퍼 ----

func barDurOf(v Vibe) float64 { return 240.0 / vibeBPM(v) }

type tickRec struct {
	bar, step int
	barStart  bool
	cmds      []engine.Cmd
}

// drive — bars개 바를 16스텝씩 틱하고 Tick 결과를 복사해 기록한다
// (Tick 반환은 내부 버퍼 재사용이므로 반드시 복사).
func drive(r *Resident, bars int, barDur float64) []tickRec {
	out := make([]tickRec, 0, bars*16)
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		cs := r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		out = append(out, tickRec{b, 0, true, append([]engine.Cmd(nil), cs...)})
		for s := 1; s < 16; s++ {
			cs = r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
			out = append(out, tickRec{b, s, false, append([]engine.Cmd(nil), cs...)})
		}
	}
	return out
}

func encodeCmds(recs []tickRec) []byte {
	var out []byte
	for _, tr := range recs {
		out = append(out, byte(tr.bar), byte(tr.bar>>8), byte(tr.step))
		if tr.barStart {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		for _, c := range tr.cmds {
			out = append(out, byte(c.Kind), c.A, c.B, c.C, c.D)
			b := math.Float32bits(c.V)
			out = append(out, byte(b), byte(b>>8), byte(b>>16), byte(b>>24))
		}
	}
	return out
}

// ---- 계약 7: 결정론 ----

func TestDeterminism(t *testing.T) {
	const bars = 300
	barDur := barDurOf(Rush)
	a := drive(New(0xC0FFEE, Rush, Config{600, 600, 600}), bars, barDur)
	b := drive(New(0xC0FFEE, Rush, Config{600, 600, 600}), bars, barDur)
	if string(encodeCmds(a)) != string(encodeCmds(b)) {
		t.Fatal("같은 seed·Input 시퀀스가 다른 Cmd 시퀀스를 냈다")
	}
	c := drive(New(0x1234567, Rush, Config{600, 600, 600}), bars, barDur)
	if string(encodeCmds(a)) == string(encodeCmds(c)) {
		t.Fatal("다른 seed가 같은 Cmd 시퀀스를 냈다")
	}
}

// ---- 계약 6: MANUAL 잠금 / Resume ----

func TestLockResume(t *testing.T) {
	r := New(77, Rush, Config{600, 600, 600})
	r.Lock(engine.CutoffA)
	r.Lock(engine.ParamID(200)) // 범위 밖 ID: 무시되어야 한다
	if r.Locked(engine.ParamID(200)) {
		t.Fatal("범위 밖 ParamID가 잠금 상태로 보였다")
	}
	var cutA, others int
	for _, tr := range drive(r, 1000, barDurOf(Rush)) {
		for _, c := range tr.cmds {
			if c.Kind == engine.SetParam {
				if engine.ParamID(c.A) == engine.CutoffA {
					cutA++
				} else {
					others++
				}
			}
		}
	}
	if cutA != 0 {
		t.Fatalf("잠금된 CutoffA SetParam이 %d개 나왔다(0이어야)", cutA)
	}
	if others == 0 {
		t.Fatal("다른 파라미터의 SetParam이 하나도 없다")
	}
	r.Resume()
	cutA = 0
	for _, tr := range drive(r, 100, barDurOf(Rush)) {
		for _, c := range tr.cmds {
			if c.Kind == engine.SetParam && engine.ParamID(c.A) == engine.CutoffA {
				cutA++
			}
		}
	}
	if cutA == 0 {
		t.Fatal("Resume 후 CutoffA SetParam이 나오지 않는다")
	}
}

// ---- 계약 5: 포모도로 전이 ----

func TestPomodoroTransitions(t *testing.T) {
	barDur := barDurOf(Rush)         // 135BPM ≈ 1.7778s/바
	r := New(0xBEEF, Rush, Config{}) // 기본 25/5/5, 첫 세션 데모 5분
	phaseAt := [360]Phase{}
	dropBars := []int{}
	for b := 0; b < 360; b++ {
		now := float64(b) * barDur
		for _, c := range r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true}) {
			if c.Kind == engine.Drop {
				dropBars = append(dropBars, b)
			}
		}
		phaseAt[b] = r.Phase()
		for s := 1; s < 16; s++ {
			r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	for _, b := range []int{153, 160, 167} {
		if phaseAt[b] != Build {
			t.Fatalf("바 %d: Focus 종료 16바 전 강제 Build여야 한다(실제 %d)", b, phaseAt[b])
		}
	}
	if phaseAt[168] != Drop {
		t.Fatalf("바 168: Focus 종료 바 = Drop이어야 한다(실제 %d)", phaseAt[168])
	}
	for _, b := range []int{169, 300, 337} {
		if phaseAt[b] != Breakdown {
			t.Fatalf("바 %d: Rest 동안 Breakdown 고정이어야 한다(실제 %d)", b, phaseAt[b])
		}
	}
	if phaseAt[338] != Intro {
		t.Fatalf("바 338: Rest 종료 후 새 사이클 Intro이어야 한다(실제 %d)", phaseAt[338])
	}
	// 데모 Focus 종료 Drop이 300s ±1바 창에 정확히 한 번(자연 Drop 진입은 별개로 허용).
	var demoDrops []int
	for _, b := range dropBars {
		if math.Abs(float64(b)*barDur-300) <= barDur {
			demoDrops = append(demoDrops, b)
		}
	}
	if len(demoDrops) != 1 {
		t.Fatalf("300s ±1바 창의 종료 Drop이 %d개(1이어야): %v(전체 %v)", len(demoDrops), demoDrops, dropBars)
	}
	for _, b := range []int{153, 154, 160, 167} {
		if phaseAt[b] == Drop {
			t.Fatalf("바 %d: 강제 Build 구간에 Drop 페이즈", b)
		}
	}

	// 포모도로 접근자: now=650s → 세션 1 Focus, 남음 1450s.
	r2 := New(0xBEEF, Rush, Config{})
	r2.Tick(Input{Bar: 365, Step: 0, Now: 650, BarStart: true})
	ph, rem, sess := r2.Pomodoro()
	if ph != Focus || sess != 1 || math.Abs(rem-1450) > 1e-6 {
		t.Fatalf("Pomodoro()=(%d,%.3f,%d), want (Focus,1450,1)", ph, rem, sess)
	}

	// 경계 자동 잠금 해제: Focus→Rest 경계(≈300s)를 넘기면 잠금이 풀린다.
	r3 := New(0xBEEF, Rush, Config{})
	for b := 0; b <= 160; b++ {
		now := float64(b) * barDur
		r3.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		for s := 1; s < 16; s++ {
			r3.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	r3.Lock(engine.CutoffA)
	for b := 161; b <= 175; b++ {
		now := float64(b) * barDur
		r3.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		for s := 1; s < 16; s++ {
			r3.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	if r3.Locked(engine.CutoffA) {
		t.Fatal("포모도로 Focus→Rest 경계에서 잠금이 자동 해제되지 않았다")
	}
}

// ---- 계약 3: 페이즈 점유(30분 시뮬, 각 ±5%) ----

func TestPhaseRatios(t *testing.T) {
	// 포모도로를 중립화(600분 Focus)해 에너지 곡선만 잰다 — 기본 25/5/5면 Rest가
	// Breakdown 점유를 25% 밖으로 밀어 이 테스트의 기대값이 성립하지 않는다.
	barDur := barDurOf(Rush)
	r := New(0x5EED, Rush, Config{600, 600, 600})
	bars := int(30 * 60 / barDur) // 30분
	counts := [numPhases]int{}
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		counts[r.Phase()]++
		for s := 1; s < 16; s++ {
			r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	want := [numPhases]float64{0.15, 0.35, 0.25, 0.25}
	names := [numPhases]string{"Intro", "Build", "Drop", "Breakdown"}
	for p := 0; p < int(numPhases); p++ {
		got := float64(counts[p]) / float64(bars)
		if math.Abs(got-want[p]) > 0.05 {
			t.Fatalf("%s 점유 %.3f(%.1f%%±5%% 기대)", names[p], got, want[p]*100)
		}
		t.Logf("%s: %d/%d 바 = %.1f%% (기대 %.0f%%)", names[p], counts[p], bars, got*100, want[p]*100)
	}
}

// ---- 계약 4: 바이브별 Tempo ----

func TestVibeTempo(t *testing.T) {
	cases := []struct {
		v    Vibe
		bpm  float64
		name string
	}{
		{DeepFocus, 120, "DeepFocus"},
		{Rush, 135, "Rush"},
		{Lofi, 112, "Lofi"},
	}
	for _, tc := range cases {
		r := New(42, tc.v, Config{600, 600, 600})
		cs := r.Tick(Input{Bar: 0, Step: 0, Now: 0, BarStart: true})
		var tempos []float32
		for _, c := range cs {
			if c.Kind == engine.SetParam && engine.ParamID(c.A) == engine.Tempo {
				tempos = append(tempos, c.V)
			}
		}
		if len(tempos) != 1 {
			t.Fatalf("%s: 첫 바 Tempo SetParam %d개(1이어야)", tc.name, len(tempos))
		}
		want := float32((tc.bpm - 100) / 60)
		if math.Abs(float64(tempos[0]-want)) > 1e-6 {
			t.Fatalf("%s: Tempo q=%.6f, want %.6f", tc.name, tempos[0], want)
		}
		// 이후 바이브 변경: 다음 바 경계에 정확히 1개.
		if tc.v == DeepFocus {
			r.SetVibe(Lofi)
			found := 0
			barDur := barDurOf(DeepFocus)
			for b := 1; b <= 6; b++ {
				now := float64(b) * barDur
				for _, c := range r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true}) {
					if c.Kind == engine.SetParam && engine.ParamID(c.A) == engine.Tempo {
						found++
						if math.Abs(float64(c.V-0.2)) > 1e-6 {
							t.Fatalf("바이브 변경 Tempo q=%.6f, want 0.2", c.V)
						}
					}
				}
				for s := 1; s < 16; s++ {
					r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
				}
			}
			if found != 1 {
				t.Fatalf("SetVibe 후 Tempo SetParam %d개(다음 바에 1개여야)", found)
			}
		}
	}
}

// ---- 계약 8: 바당 SetParam ≤ 8, 파라미터당 ≥ 2스텝 간격 ----

func TestSetParamBudget(t *testing.T) {
	barDur := barDurOf(Lofi)
	r := New(9, Lofi, Config{600, 600, 600})
	r.SetVibe(Rush) // 바 0: 초기 Tempo + 바 1: 변경 Tempo — 가장 빠듯한 국면 포함
	perBar := map[int]int{}
	lastStep := map[[2]int]int{}
	for _, tr := range drive(r, 200, barDur) {
		n := 0
		for _, c := range tr.cmds {
			if c.Kind != engine.SetParam {
				continue
			}
			n++
			key := [2]int{tr.bar, int(c.A)}
			if prev, ok := lastStep[key]; ok && tr.step-prev < 2 {
				t.Fatalf("바 %d 파라미터 %d: 방출 간격 %d스텝(<2)", tr.bar, c.A, tr.step-prev)
			}
			lastStep[key] = tr.step
		}
		perBar[tr.bar] += n
	}
	for bar, n := range perBar {
		if n > 8 { // 계약 수치(구현 상수 아님)
			t.Fatalf("바 %d: SetParam %d개(≤8이어야)", bar, n)
		}
	}
}

// ---- 계약 8: Cmd 범위·반환 수 상한 ----

func TestCmdRanges(t *testing.T) {
	barDur := barDurOf(Rush)
	r := New(0xABCDEF, Rush, Config{}) // 기본 포모도로(전이·드롭 국면 포함)
	for _, tr := range drive(r, 1000, barDur) {
		if tr.barStart && len(tr.cmds) > 144 {
			t.Fatalf("바 %d: BarStart 틱 Cmd %d개(≤144)", tr.bar, len(tr.cmds))
		}
		if !tr.barStart && len(tr.cmds) > 8 {
			t.Fatalf("바 %d 스텝 %d: 비BarStart 틱 Cmd %d개(≤8)", tr.bar, tr.step, len(tr.cmds))
		}
		for _, c := range tr.cmds {
			switch c.Kind {
			case engine.SetParam:
				if c.A >= uint8(engine.NumParams) {
					t.Fatalf("SetParam 파라미터 %d(≥%d)", c.A, engine.NumParams)
				}
				if c.V < 0 || c.V > 1 || c.V != c.V {
					t.Fatalf("SetParam V=%v([0,1] 밖)", c.V)
				}
			case engine.BassStep:
				if c.A > 1 || c.B > 15 || c.C > engine.MaxNote || c.D & ^uint8(engine.StepGate|engine.StepSlide|engine.StepAccent) != 0 {
					t.Fatalf("BassStep 범위 밖: %+v", c)
				}
			case engine.DrumStep:
				if c.A < uint8(engine.BD) || c.A > uint8(engine.CY) || c.B > 15 || c.D & ^uint8(engine.StepGate|engine.StepAccent) != 0 {
					t.Fatalf("DrumStep 범위 밖: %+v", c)
				}
			case engine.SelectPattern:
				if c.A > 1 || c.B > 7 {
					t.Fatalf("SelectPattern 범위 밖: %+v", c)
				}
			case engine.SetKey:
				if c.A >= engine.NumKeys {
					t.Fatalf("SetKey 루트 %d(≥%d)", c.A, engine.NumKeys)
				}
			case engine.SetChord:
				if c.A >= engine.ChordBars || c.B >= engine.NumDegrees || c.C&^uint8(engine.ChordSeventh) != 0 {
					t.Fatalf("SetChord 범위 밖: %+v", c)
				}
			case engine.BassMode:
				if c.A != 1 || c.B >= engine.NumModes || c.C >= engine.NumDirs {
					t.Fatalf("BassMode 범위 밖: %+v", c)
				}
			case engine.Drop:
				// 필드 없음
			default:
				t.Fatalf("레지던트가 내지 않는 Kind %d", c.Kind)
			}
		}
	}
}

// ---- 계약 2: 드럼 킥 골격 ----

func TestKickSkeleton(t *testing.T) {
	barDur := barDurOf(Rush)
	recs := drive(New(31337, Rush, Config{600, 600, 600}), 120, barDur)
	bdByBar := map[int]map[int]bool{}
	writeBars := map[int]bool{}
	for _, tr := range recs {
		for _, c := range tr.cmds {
			if c.Kind == engine.BassStep {
				writeBars[tr.bar] = true
			}
			if c.Kind == engine.DrumStep && c.A == uint8(engine.BD) && c.D&engine.StepGate != 0 {
				m := bdByBar[tr.bar]
				if m == nil {
					m = map[int]bool{}
					bdByBar[tr.bar] = m
				}
				m[int(c.B)] = true
			}
		}
	}
	if len(writeBars) == 0 {
		t.Fatal("패턴 기록 바가 없다")
	}
	for bar := range writeBars {
		m := bdByBar[bar]
		if m == nil {
			t.Fatalf("바 %d: BD DrumStep이 없다", bar)
		}
		for _, st := range [...]int{0, 4, 8, 12} {
			if !m[st] {
				t.Fatalf("바 %d: 킥 골격 스텝 %d가 비었다(gate=%v)", bar, st, m)
			}
		}
	}
}

// ---- 계약 2: 슬롯 전환 순서 ----

func TestSlotRotation(t *testing.T) {
	barDur := barDurOf(Rush)
	recs := drive(New(2024, Rush, Config{600, 600, 600}), 80, barDur)
	var slots []uint8
	selectBars := map[int]uint8{}
	bassBars := map[int]bool{}
	for _, tr := range recs {
		for _, c := range tr.cmds {
			if c.Kind == engine.SelectPattern && c.A == 0 {
				slots = append(slots, c.B)
				selectBars[tr.bar] = c.B
			}
			if c.Kind == engine.SelectPattern && c.A == 1 {
				if selectBars[tr.bar] != c.B {
					t.Fatalf("바 %d: A/B가 다른 슬롯(%d vs %d)", tr.bar, selectBars[tr.bar], c.B)
				}
			}
			if c.Kind == engine.BassStep {
				bassBars[tr.bar] = true
			}
		}
	}
	if len(slots) < 6 {
		t.Fatalf("슬롯 전환 %d회 — 순환 검사에 부족", len(slots))
	}
	for i, s := range slots {
		want := uint8(4 + i&3)
		if s != want {
			t.Fatalf("슬롯 순서[%d]=%d, want %d(전체 %v)", i, s, want, slots)
		}
	}
	// 순서: SelectPattern 바 다음 바에 BassStep이 온다(현재 슬롯에만 쓸 수 있으므로).
	for bar := range selectBars {
		if !bassBars[bar+1] {
			t.Fatalf("바 %d SelectPattern 다음 바 %d에 BassStep이 없다", bar, bar+1)
		}
	}
}

// ---- 계약 1: 바 경계 없이 호출(오용 방어 1) ----

func TestTickWithoutBarStart(t *testing.T) {
	barDur := barDurOf(DeepFocus)
	r := New(8, DeepFocus, Config{600, 600, 600})
	for b := 0; b < 100; b++ {
		n := 0
		for s := 0; s < 16; s++ {
			cs := r.Tick(Input{Bar: uint32(b), Step: s, Now: float64(b)*barDur + float64(s)*barDur/16, BarStart: false})
			n += len(cs)
			for _, c := range cs {
				if c.Kind != engine.SetParam {
					t.Fatalf("BarStart 없는 틱이 %d Kind를 냈다", c.Kind)
				}
			}
		}
		if n > 8 { // 계약 수치(구현 상수 아님)
			t.Fatalf("바 %d: SetParam %d개(≤8)", b, n)
		}
	}
}

// ---- 오용 방어 3: Now 역행 ----

func TestNowReversal(t *testing.T) {
	barDur := barDurOf(Rush)
	r := New(11, Rush, Config{}) // 기본 25/5/5
	for b := 0; b < 6; b++ {
		now := float64(b) * barDur
		r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		for s := 1; s < 16; s++ {
			r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
		}
	}
	_, remAtMax, _ := r.Pomodoro()
	r.Tick(Input{Bar: 99, Step: 0, Now: 1.0, BarStart: true}) // 역행
	_, remAfter, sess := r.Pomodoro()
	if math.Abs(remAfter-remAtMax) > 1e-9 {
		t.Fatalf("Now 역행 후 remaining %.6f(최대 시점 %.6f여야)", remAfter, remAtMax)
	}
	if sess != 0 {
		t.Fatalf("세션 인덱스 %d(0이어야)", sess)
	}
	r.Tick(Input{Bar: 100, Step: 0, Now: 100 * barDur, BarStart: true}) // 정상 진행 재개
}

// ---- 계약 1: Hand ----

func TestHand(t *testing.T) {
	barDur := barDurOf(Rush)
	r := New(777, Rush, Config{600, 600, 600})
	handBar := -1
	for b := 0; b < 10 && handBar < 0; b++ {
		now := float64(b) * barDur
		r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		for s := 1; s < 16; s++ {
			cs := r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
			for _, c := range cs {
				if c.Kind == engine.SetParam {
					handBar = b
				}
			}
		}
	}
	if handBar < 0 {
		t.Fatal("10바 안에 노브 움직임이 없다")
	}
	// handBar의 마지막 틱(스텝 15) 직후와 다음 바까지 활성, +2바에 비활성.
	if id, ok := r.Hand(); !ok {
		t.Fatalf("움직임 직후 Hand 비활성(id=%d)", id)
	}
	r.Tick(Input{Bar: uint32(handBar + 1), Step: 0, Now: float64(handBar+1) * barDur, BarStart: true})
	if _, ok := r.Hand(); !ok {
		t.Fatal("다음 바에서 Hand가 비활성(1바 유지여야)")
	}
	r.Tick(Input{Bar: uint32(handBar + 2), Step: 0, Now: float64(handBar+2) * barDur, BarStart: true})
	if _, ok := r.Hand(); ok {
		t.Fatal("2바 뒤에도 Hand 활성(1바 유지 위반)")
	}
}

// ---- 계약 3: 페이즈 유도 단위 ----

func TestPhaseDerivation(t *testing.T) {
	cases := []struct {
		f    float64
		want Phase
	}{
		{0.0, Intro}, {0.05, Intro}, {0.149, Intro},
		{0.15, Build}, {0.49, Build},
		{0.5, Drop}, {0.74, Drop},
		{0.75, Breakdown}, {0.99, Breakdown},
	}
	for _, tc := range cases {
		if got := phaseOfFrac(tc.f); got != tc.want {
			t.Fatalf("phaseOfFrac(%v)=%d want %d", tc.f, got, tc.want)
		}
	}
	// 포모도로 우선: Rest → Breakdown, 종료 16바 → Build, 종료 바 → Drop.
	r := New(1, Rush, Config{600, 600, 600})
	barDur := barDurOf(Rush)
	if ph, _, _ := r.derive(0); ph != Intro {
		t.Fatalf("derive(0)=%d want Intro", ph)
	}
	if ph, _, _ := r.derive(barDur); ph != Intro { // 사이클 초반
		t.Fatalf("derive(1바)=%d want Intro", ph)
	}
	rp := New(1, Rush, Config{10, 5, 10})
	if ph, _, _ := rp.derive(601); ph != Breakdown { // 10분 Focus 끝나고 Rest
		t.Fatalf("Rest 시간 derive=%d want Breakdown", ph)
	}
	// 강제 Build 창: 자연 페이즈가 무엇이든 Build여야 한다. 창이 자연 Build 위에만
	// 걸리는 시드는 검증이 무의미하니, 자연 비-Build가 걸리는 시드를 찾아 쓴다.
	win := forcedBuildBars * barDur
	pickSeed := uint32(0)
	for s := uint32(1); s < 100; s++ {
		for d := barDur * 1.01; d < win; d += barDur / 8 {
			_, _, frac, _ := cycleAt(s, Config{10, 5, 10}, 600-d)
			if phaseOfFrac(frac) != Build {
				pickSeed = s
				break
			}
		}
		if pickSeed != 0 {
			break
		}
	}
	if pickSeed == 0 {
		t.Fatal("강제 Build 창이 자연 비-Build와 겹치는 시드를 못 찾았다")
	}
	rp = New(pickSeed, Rush, Config{10, 5, 10})
	for d := barDur * 1.01; d < win; d += barDur / 8 {
		now := 600 - d
		_, _, frac, _ := cycleAt(pickSeed, Config{10, 5, 10}, now)
		if ph, _, _ := rp.derive(now); ph != Build {
			t.Fatalf("종료 %.1f바 전: 강제 Build여야 한다(실제 %d, 자연 %d)", d/barDur, ph, phaseOfFrac(frac))
		}
	}
	// 창 밖(17바 전): 강제가 아니어야 한다 — 자연 페이즈와 일치 확인.
	for d := win; d < win+barDur; d += barDur / 8 {
		now := 600 - d
		_, _, frac, _ := cycleAt(pickSeed, Config{10, 5, 10}, now)
		want := phaseOfFrac(frac)
		if ph, _, _ := rp.derive(now); ph != want {
			t.Fatalf("종료 %.1f바 전(창 밖): 자연 %d여야 한다(실제 %d)", d/barDur, want, ph)
		}
	}
	if ph, _, _ := rp.derive(600 - barDur/2); ph != Drop {
		t.Fatalf("종료 바 Drop 아님(=%d)", ph)
	}
}

// ---- 실산출물: 30분 엔진 구동 ----

func TestResidentDrivesEngine30Min(t *testing.T) {
	const seed = uint32(0xC0FFEE)
	barDur := barDurOf(DeepFocus) // 120BPM → 바 2.0s, 30분 = 900바
	const bars = 900
	const blocksPerBar = 750 // 2s × 48000 / 128
	const perStep = blocksPerBar / 16
	const tail = blocksPerBar - perStep*16 // 750 − 736 = 14

	r := New(seed, DeepFocus, Config{}) // 기본 포모도로 25/5/5(첫 5분 데모)
	e := engine.New(seed)
	buf := make([]float32, 256)
	applied := 0

	names := [numPhases]string{"Intro", "Build", "Drop", "Breakdown"}
	var sumSq [numPhases]float64
	var nSamp [numPhases]int64
	var barsPer [numPhases]int
	var total float64
	var totalN int64
	nan := 0
	peak := float32(0)
	phaseSeen := [numPhases]bool{}
	var degRoot, degChord, degPass int // 계약 11: 베이스 A 게이트 스텝 도수 분포

	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		for _, c := range r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true}) {
			e.Apply(c)
			applied++
			if c.Kind == engine.BassStep && c.A == 0 && c.D&engine.StepGate != 0 {
				if c.C > engine.MaxNote {
					t.Fatalf("바 %d: 파트 A 노트 %d > MaxNote %d", b, c.C, engine.MaxNote)
				}
				switch c.C % 7 {
				case 0:
					degRoot++
				case 2, 4:
					degChord++
				default: // 1·3·5·6
					degPass++
				}
			}
		}
		ph := r.Phase()
		phaseSeen[ph] = true
		barsPer[ph]++
		for s := 0; s < 16; s++ {
			if s > 0 {
				for _, c := range r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false}) {
					e.Apply(c)
					applied++
				}
			}
			for i := 0; i < perStep; i++ {
				e.Render(buf)
				scan(t, buf, &sumSq[ph], &nSamp[ph], &total, &totalN, &nan, &peak)
			}
		}
		for i := 0; i < tail; i++ {
			e.Render(buf)
			scan(t, buf, &sumSq[ph], &nSamp[ph], &total, &totalN, &nan, &peak)
		}
	}

	if nan != 0 {
		t.Fatalf("NaN 샘플 %d개", nan)
	}
	if applied < 10000 {
		t.Fatalf("적용한 Cmd %d개 — 30분 구동치가 아니다(레지던트가 침묵했나)", applied)
	}
	if peak > 1.0 {
		t.Fatalf("peak %.4f > 1.0", peak)
	}
	rms := math.Sqrt(total / float64(totalN))
	if rms <= 0.01 {
		t.Fatalf("전체 RMS %.5f ≤ 0.01", rms)
	}
	// 계약 11: 도수 가중 — 루트 0.5·코드톤 0.3·경과음 0.2, 각 ±0.08(계약 수치).
	degN := degRoot + degChord + degPass
	if degN < 400 {
		t.Fatalf("도수 분포 표본 %d개 — 너무 적다", degN)
	}
	for _, tc := range []struct {
		name string
		got  int
		want float64
	}{
		{"루트", degRoot, 0.5},
		{"코드톤(2·4)", degChord, 0.3},
		{"경과음(1·3·5·6)", degPass, 0.2},
	} {
		got := float64(tc.got) / float64(degN)
		if math.Abs(got-tc.want) > 0.08 {
			t.Fatalf("%s 점유 %.3f(%.1f±0.08 기대, 표본 %d)", tc.name, got, tc.want, degN)
		}
		t.Logf("도수 분포 | %s %d/%d = %.3f(기대 %.1f)", tc.name, tc.got, degN, got, tc.want)
	}
	t.Logf("전체: %d바 %.1f초, Cmd %d개 적용, 샘플 %d, RMS %.4f, peak %.4f, NaN %d",
		bars, float64(bars)*barDur, applied, totalN, rms, peak, nan)
	for p := 0; p < int(numPhases); p++ {
		if !phaseSeen[p] {
			t.Fatalf("30분 시뮬에서 %s 페이즈가 한 번도 없다", names[p])
		}
		prms := math.Sqrt(sumSq[p] / float64(nSamp[p]))
		t.Logf("페이즈별 RMS | %-10s 바=%3d 샘플=%8d rms=%.4f", names[p], barsPer[p], nSamp[p], prms)
	}
}

func scan(t *testing.T, buf []float32, ss *float64, n *int64, tot *float64, tn *int64, nan *int, peak *float32) {
	t.Helper()
	for _, v := range buf {
		if v != v {
			*nan++
			continue
		}
		a := v
		if a < 0 {
			a = -a
		}
		if a > *peak {
			*peak = a
		}
		*ss += float64(v) * float64(v)
		*n++
		*tot += float64(v) * float64(v)
		*tn++
	}
}
