// harmony_test.go — Phase 2 화성 게이트(§12.1 ↔ 이 파일의 단언). 테스트 파일은 math 자유.
//
// 계약 ↔ 단언 표:
//
// | 계약                                | 단언                                              | FAIL-first |
// |-------------------------------------|---------------------------------------------------|------------|
// | ResolveNote 도수→반음 표             | TestResolveNoteTable: 표 전수 + 클램프/위반 입력    | 스켈레톤(골격 커밋) 복사본에서 이 파일을 돌리면 표가 통과함(리드 구현) — 진입 게이트는 TestBassDegreeInterpretation가 담당 |
// | onStep 도수 해석(BASS)               | TestBassDegreeInterpretation: inc0 = ResolveNote 주파수 | 스켈레톤에서 절대음(24+note)이라 실패 확인(/tmp/p2ab/skel 실측) |
// | 초기 패턴 도수화                     | TestInitialPatternDegrees: A∈7..13, B∈0..6, 루트 가중 | 스켈레톤의 12+펜타토닉(12..29)이라 도메인 단언 실패 확인(/tmp/p2ab/skel 실측) |
// | ARP 순서(Up/Down/UpDown/7th)         | TestArpOrder: 연속 게이트 반음 [r,3,5,r,3,5] 등     | 스켈레톤에 ARP 진행이 없어(모든 톤 = 패턴 노트) 실패 확인(/tmp/p2ab/skel 실측) |
// | ARP 게이트 없는 스텝 진행 없음        | TestArpOrder 갭 케이스: {0,3,7} 게이트도 [r,3,5,r…]  | 진행을 매 스텝 하게 바꾸면 갭 케이스가 실패(/tmp 변이 M2에서 진행 삭제 시 순서 케이스가 실패하는 것으로 측정) |
// | ARP 바 경계 리셋 없음                | TestArpOrder 바 경계 케이스: 코드가 바뀌어도 이어짐  | arpIdx를 st==0에서 0으로 리셋하면 실패(계산: idx2→36 vs 리셋 idx0→29) |
// | ARP 톤 수 4→3 클램프                 | TestArpToneShrink: 7th 해제 뒤 5th에서 재개         | 클램프 없이 마스킹에 맡기면 r(21)이 나와 실패 |
// | ARP 슬라이드 목표 peek(상태 불변)     | TestSlideTargetsAcrossBar: slideInc = 다음 게이트 톤 | 목표를 현재 코드로 해석하면 (33-21)이 (21±Δ)로 바뀌어 실패 |
// | BASS 슬라이드 목표 = 다음 마디 코드   | TestSlideTargetsAcrossBar: st15 목표가 bar+1 코드    | st==15 분기 제거 시 목표가 현 코드 해석으로 바뀌어 실패 |
// | CHORD inc 비율(0·2·4 / 2·4·6)        | TestChordMode: inc0:inc2:inc3 = ResolveNote 주파수비(1e-3) | noteOnChord가 inc2/inc3를 0으로 두면 비율 1이 되어 실패 |
// | CHORD MaxSemis 접힘 유계             | TestChordMode fold 케이스: 세 inc 동일·출력 유계     | (접힘 자체가 정규화 계약 — 유계 단언이 패닉/발산을 잡는다) |
// | BASS/ARP 보조 오실레이터 기여 0       | TestChordZeroContribution: chord3=false·inc2=inc3=0 + /tmp A/B 해시 동일 | noteOn이 chord3=true로 두면 voice_test TestVoiceChordIgnoredWhenOff가 실패 |
// | 진폭 상한(모드별 300초)              | TestAmplitudeAllModes: 3모드 peak ≤ 0.9903          | outClip 스케일 0.82→1.0으로 두면 실패(fx.go 상수 근거) |
// | 트랜스포트 정지(동결·aenv 감쇠)       | TestTransportStopPlay: 10블록 Step/Bar 불변·aenv 감소·FlagBar 없음 | Render의 playing 게이트 제거 시 위치 동결 단언 실패(/tmp 변이 M5 실측) |
// | 트랜스포트 재생(스텝 0·바 유지)       | TestTransportStopPlay: Step()==0, Bar() 유지, FlagBar | started=false 리셋 제거 시 Step()≠0으로 실패 |
// | 트랜스포트 멱등                      | TestTransportStopPlay: 재생 중 A:1 무동작·정지 중 A:0 무동작 | 가드 제거 시 재생 중 A:1이 스텝을 0으로 되돌려 실패 |
// | 상태 v3 왕복(P4-fx2 매직 승격)         | TestStateV3RoundTrip: key·mode·dir·playing·코드 8 보존 | WriteState의 코드 루프 건너뛰면 Chord(2) 불일치 실패(/tmp 변이 M6 실측) |
// | 상태 v3 'J','1' 거부·재정규화         | TestStateV3RoundTrip: v1 헤더 false, 도수 7→0, 모드·방향 3→0 | ReadState의 %7·범위 검사 제거 시 실패 |
// | 결정론(화성 Cmd 이력 포함)            | TestDeterminismWithHarmonyCmds: 300블록 비트 동일    | 두 실행 중 하나에 Cmd 하나를 더 넣으면 불일치 실패(기존 #8과 동일 원리) |
// | 무할당(ARP·CHORD 경로)               | TestHarmonyNoAllocs: Render·Apply AllocsPerRun==0    | noteOnChord에 make 1줄 삽입 시 실패(원리는 engine_test #3와 동일) |
package engine

import (
	"fmt"
	"math"
	"testing"
)

// wantInc — semis(C1 기준 반음)의 기대 inc0. 튠 0(×1)·옥타브 0(×1) 기준.
// voice exp2 근사 상대오차 ~1.5e-4를 감안해 비교 허용치는 1e-3.
func wantInc(semis uint8) float64 {
	return 440 * math.Pow(2, float64(24+int(semis)-69)/12) / SampleRate
}

func checkInc(t *testing.T, got float32, semis uint8, label string) {
	t.Helper()
	w := wantInc(semis)
	if d := math.Abs(float64(got)-w) / w; d > 1e-3 {
		t.Fatalf("%s: inc0=%.9g, 반음 %d 기대 %.9g (상대오차 %.2g > 1e-3)", label, got, semis, w, d)
	}
}

// 1. ResolveNote 표 — A 마이너(key 9) 표 전수 + 경계/위반. 기대값 근거는 각 줄.
func TestResolveNoteTable(t *testing.T) {
	cases := []struct {
		key, chord, note, want uint8
		why                    string
	}{
		{9, 0, 7, 21, "루트 A(9) + 옥타브 1(12) + degreeSemis(0,0)=0"},
		{9, 5, 0, 17, "VI=F: minorScale[5]=8 → 9+8"},
		{9, 5, 2, 21, "(5+2)%7=0, 옥타브 넘김 +12 → 9+12=21 = A(F 위 2도수)"},
		{9, 0, 36, 48, "9 + 12·5 + minorScale[1]=2 → 71 → MaxSemis 클램프"},
		{9, 0, 40, 48, "note>MaxNote → 36과 같은 경로(40%7과 무관하게 36으로 클램프)"},
		{0, 0, 0, 0, "C1 기준 원점"},
		{11, 6, 6, 31, "11 + degreeSemis(6,6): (6+6)래핑 i=5 → minorScale[5]=8+12 → 31"},
		{200, 200, 200, 48, "위반 입력: key%12=8, chord%7=4, note→36 → 8+60+8=76 → 클램프(정규화 계약)"},
	}
	for _, c := range cases {
		if got := ResolveNote(c.key, c.chord, c.note); got != c.want {
			t.Fatalf("ResolveNote(%d,%d,%d)=%d, want %d (%s)", c.key, c.chord, c.note, got, c.want, c.why)
		}
	}
	// 보조 단언: 코드 루트 위 도수 순회는 반음 단조 증가(마이너 음계 0,2,3,5,7,8,10).
	prev := -1
	for deg := uint8(0); deg < NumDegrees; deg++ {
		s := int(ResolveNote(9, 0, deg))
		if s <= prev {
			t.Fatalf("도수 %d 반음 %d — 음계 단조 아님(직전 %d)", deg, s, prev)
		}
		prev = s
	}
}

//  2. onStep 도수 해석 — BASS 모드의 noteOn 반음은 ResolveNote(keyRoot, 코드, note).
//     스켈레톤(절대음 noteOn)과 갈리는 진입 게이트.
func TestBassDegreeInterpretation(t *testing.T) {
	e := New(9) // keyRoot = 9 (A)
	e.Apply(Cmd{Kind: Mute, A: uint8(BassB), B: 1})
	for v := 0; v < NumDrums; v++ {
		e.Apply(Cmd{Kind: Mute, A: uint8(BD) + uint8(v), B: 1})
	}
	// 슬롯 0: 스텝 0만 게이트, note 7(옥타브 1 루트 도수). 기대 반음 = ResolveNote(9,0,7) = 21.
	e.Apply(Cmd{Kind: BassStep, A: 0, B: 0, C: 7, D: StepGate})
	buf := make([]float32, 2*Block)
	for i := 0; i < 200; i++ {
		e.Render(buf)
		if e.Flags()&1 != 0 {
			checkInc(t, e.bass[0].inc0, 21, "BASS 도수 해석(스텝 0)")
			return
		}
	}
	t.Fatal("200블록 안에 베이스 트리거 없음")
}

// 3. 초기 패턴 도수화 — A는 옥타브 1(7..13), B는 옥타브 0(0..6), 루트 가중 ≈0.5.
func TestInitialPatternDegrees(t *testing.T) {
	roots := 0
	for seed := uint32(1); seed <= 8; seed++ {
		e := New(seed)
		for st := 0; st < Steps; st++ {
			n, _ := e.BassStepAt(BassA, st)
			if n < 7 || n > 13 {
				t.Fatalf("seed %d A 스텝 %d note %d — 옥타브 1(7..13) 밖", seed, st, n)
			}
			if n == 7 {
				roots++
			}
			n2, _ := e.BassStepAt(BassB, st)
			if n2 > 6 {
				t.Fatalf("seed %d B 스텝 %d note %d — 옥타브 0(0..6) 밖", seed, st, n2)
			}
			if n2 == 0 {
				roots++
			}
		}
	}
	// 8시드 × 32스텝 = 256 표본, 루트 기대 ≈128. 균등(1/7≈37)이나 가중 붕괴(≤0.31)는 걸러진다.
	if roots < 80 {
		t.Fatalf("루트 도수 %d/256 — 가중 0.5가 안 살아 있음", roots)
	}
}

// harmonyFixture — 화성 게이트용 엔진. 시드 9(키 A), 파트 1·드럼 뮤트, 코드 트랙 전 마디
// (deg, seventh), 파트 0 슬롯 0은 gates의 스텝만 게이트(note는 note 인자). 모드는 별도 Apply.
func harmonyFixture(deg uint8, seventh bool, gates []int, note uint8) *Engine {
	e := New(9)
	fl := uint8(0)
	if seventh {
		fl = ChordSeventh
	}
	for b := 0; b < ChordBars; b++ {
		e.Apply(Cmd{Kind: SetChord, A: uint8(b), B: deg, C: fl})
	}
	e.Apply(Cmd{Kind: Mute, A: uint8(BassB), B: 1})
	for v := 0; v < NumDrums; v++ {
		e.Apply(Cmd{Kind: Mute, A: uint8(BD) + uint8(v), B: 1})
	}
	for st := 0; st < Steps; st++ {
		e.Apply(Cmd{Kind: BassStep, A: 0, B: uint8(st), C: note, D: 0}) // 생성 패턴 게이트 소거
	}
	for _, st := range gates {
		e.Apply(Cmd{Kind: BassStep, A: 0, B: uint8(st), C: note, D: StepGate})
	}
	return e
}

// collectTrigInc — 파트 0 트리거 블록마다 inc0을 모은다(슬라이드 없는 패턴 전제:
// inc0은 노트온 시점 값 그대로).
func collectTrigInc(e *Engine, want int) []float32 {
	buf := make([]float32, 2*Block)
	var got []float32
	for i := 0; i < 40000 && len(got) < want; i++ {
		e.Render(buf)
		if e.Flags()&1 != 0 {
			got = append(got, e.bass[0].inc0)
		}
	}
	return got
}

func checkIncSeq(t *testing.T, got []float32, want []uint8, label string) {
	t.Helper()
	if len(got) < len(want) {
		t.Fatalf("%s: 트리거 %d개 모으고도 기대 %d개보다 적음", label, len(got), len(want))
	}
	for i, s := range want {
		checkInc(t, got[i], s, fmt.Sprintf("%s[%d]", label, i))
	}
}

//  4. ARP 순서 — 스펙 표 그대로: Up [r,3,5,r,3,5] · Down [5,3,r,…] · UpDown [r,3,5,3,r,3] ·
//     7th UpDown [r,3,5,7,5,3,r]. 반음값은 A 마이너 i코드(9,0): r=21, 3=24, 5=28, 7=31.
func TestArpOrder(t *testing.T) {
	six := []int{0, 1, 2, 3, 4, 5}
	run := func(dir uint8, seventh bool, want []uint8, label string) {
		e := harmonyFixture(0, seventh, six, 7)
		e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: dir})
		checkIncSeq(t, collectTrigInc(e, len(want)), want, label)
	}
	run(DirUp, false, []uint8{21, 24, 28, 21, 24, 28}, "ARP Up")
	run(DirDown, false, []uint8{28, 24, 21, 28, 24, 21}, "ARP Down")
	run(DirUpDown, false, []uint8{21, 24, 28, 24, 21, 24}, "ARP UpDown")
	run(DirUpDown, true, []uint8{21, 24, 28, 31, 28, 24, 21}, "ARP UpDown 7th")

	// 게이트 없는 스텝은 진행하지 않는다 — 갭이 있어도 [r,3,5] 순서가 이어진다.
	e := harmonyFixture(0, false, []int{0, 3, 7}, 7)
	e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: DirUp})
	checkIncSeq(t, collectTrigInc(e, 6), []uint8{21, 24, 28, 21, 24, 28}, "ARP 갭")
}

//  5. ARP 바 경계 — 코드가 바뀌어도 아르페지오 인덱스는 리셋하지 않는다.
//     게이트 {0, 15}: st0(bar0, idx0 → r=21) · st15(bar0, idx1 → 3rd=24) ·
//     st0(bar1, idx2 → VI의 5도수 = ResolveNote(9,5,11)=36). 리셋이면 idx0 → 29(F).
func TestArpNoResetAtBar(t *testing.T) {
	e := New(9)
	e.Apply(Cmd{Kind: SetChord, A: 0, B: 0})
	e.Apply(Cmd{Kind: SetChord, A: 1, B: 5}) // bar1 = VI
	for b := 2; b < ChordBars; b++ {
		e.Apply(Cmd{Kind: SetChord, A: uint8(b), B: 0})
	}
	e.Apply(Cmd{Kind: Mute, A: uint8(BassB), B: 1})
	for v := 0; v < NumDrums; v++ {
		e.Apply(Cmd{Kind: Mute, A: uint8(BD) + uint8(v), B: 1})
	}
	for st := 0; st < Steps; st++ {
		e.Apply(Cmd{Kind: BassStep, A: 0, B: uint8(st), C: 7, D: 0}) // 생성 게이트 소거
	}
	for _, st := range []int{0, 15} {
		e.Apply(Cmd{Kind: BassStep, A: 0, B: uint8(st), C: 7, D: StepGate})
	}
	e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: DirUp})
	got := collectTrigInc(e, 4)
	checkIncSeq(t, got[:3], []uint8{21, 24, 36}, "ARP 바 경계(이어짐)")
	if math.Abs(float64(got[2])/wantInc(29)-1) < 0.01 {
		t.Fatal("바 경계에서 idx가 리셋된 값(29=F)이 나옴 — 리셋 금지 계약 위반")
	}
}

//  6. ARP 톤 수 4→3 — 7th 해제로 n이 줄면 idx를 n-1로 클램프해 5th에서 재개한다
//     (마스킹에 맡기면 r이 나와 순환이 뒤집힌다).
func TestArpToneShrink(t *testing.T) {
	e := harmonyFixture(0, true, []int{0, 1, 2, 3, 4, 5, 6, 7}, 7)
	e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: DirUp})
	checkIncSeq(t, collectTrigInc(e, 3), []uint8{21, 24, 28}, "7th 3톤") // idx: 0→1→2→3
	for b := 0; b < ChordBars; b++ {
		e.Apply(Cmd{Kind: SetChord, A: uint8(b), B: 0, C: 0}) // 7th 해제 → n=3
	}
	// idx 3은 클램프로 2(5th=28)로. 클램프 없이 마스킹하면 ChordToneDeg(3%3)=0 → 21.
	checkIncSeq(t, collectTrigInc(e, 3), []uint8{28, 21, 24}, "7th 해제 후")
}

// 7. 슬라이드 목표 — BASS는 st15에서 다음 마디 코드로, ARP는 다음 게이트 스텝의 톤으로(peek).
func TestSlideTargetsAcrossBar(t *testing.T) {
	// BASS: st15(게이트·슬라이드, note 7) → 다음 스텝 st0(bar1, VI) note 0.
	// 이번 반음 21, 목표 = ResolveNote(9,5,0) = 17(F).
	e := New(9)
	e.Apply(Cmd{Kind: SetChord, A: 0, B: 0})
	e.Apply(Cmd{Kind: SetChord, A: 1, B: 5})
	e.Apply(Cmd{Kind: Mute, A: uint8(BassB), B: 1})
	for v := 0; v < NumDrums; v++ {
		e.Apply(Cmd{Kind: Mute, A: uint8(BD) + uint8(v), B: 1})
	}
	for st := 0; st < Steps; st++ {
		e.Apply(Cmd{Kind: BassStep, A: 0, B: uint8(st), C: 7, D: 0}) // 생성 게이트 소거
	}
	e.Apply(Cmd{Kind: BassStep, A: 0, B: 0, C: 0, D: StepGate})
	e.Apply(Cmd{Kind: BassStep, A: 0, B: 15, C: 7, D: StepGate | StepSlide})
	buf := make([]float32, 2*Block)
	for i := 0; i < 40000; i++ {
		e.Render(buf)
		if e.Flags()&1 != 0 && e.bass[0].slideN > 0 {
			want := float32(int32(17)-int32(21)) / 12 / float32(int32(e.samplesPerStep))
			if e.bass[0].slideInc != want {
				t.Fatalf("BASS st15 슬라이드 목표 slideInc=%g, want %g(다음 바 VI 코드 기대)", e.bass[0].slideInc, want)
			}
			break
		}
	}
	if e.bass[0].slideN <= 0 {
		t.Fatal("st15 슬라이드 트리거가 관측 안 됨")
	}

	// ARP: 게이트가 st15뿐 → 다음 게이트는 다음 바의 st15(peek k=16). dir Up이므로
	// 목표 idx=1(3rd)을 bar1(VI) 코드로 해석: ResolveNote(9,5,9)=33. 이번 톤 21.
	e2 := harmonyFixture(0, false, []int{15}, 7)
	e2.Apply(Cmd{Kind: SetChord, A: 1, B: 5})
	e2.Apply(Cmd{Kind: BassStep, A: 0, B: 15, C: 7, D: StepGate | StepSlide})
	e2.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: DirUp})
	for i := 0; i < 40000; i++ {
		e2.Render(buf)
		if e2.Flags()&1 != 0 && e2.bass[0].slideN > 0 {
			want := float32(int32(33)-int32(21)) / 12 / float32(int32(e2.samplesPerStep))
			if e2.bass[0].slideInc != want {
				t.Fatalf("ARP 슬라이드 peek slideInc=%g, want %g(다음 바 3rd 톤 기대)", e2.bass[0].slideInc, want)
			}
			return
		}
	}
	t.Fatal("ARP 슬라이드 트리거가 관측 안 됨")
}

//  8. CHORD — 세 inc의 주파수 비율이 ResolveNote(0·2·4 / 7th 2·4·6)과 일치(허용 1e-3).
//     fold 케이스(옥타브 5)는 세 톤이 같은 반음(48)으로 접혀도 유계·무패닉이어야 한다.
func TestChordMode(t *testing.T) {
	e := harmonyFixture(0, false, []int{0, 4}, 7)
	e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeChord, C: DirUp})
	buf := make([]float32, 2*Block)
	ratio := func(a, b float32, semis float64) bool {
		return math.Abs(float64(a)/float64(b)-math.Pow(2, semis/12)) <= 1e-3*math.Pow(2, semis/12)
	}
	for i := 0; i < 200; i++ {
		e.Render(buf)
		if e.Flags()&1 != 0 {
			break
		}
	}
	if !e.bass[0].chord3 {
		t.Fatal("CHORD 트리거 뒤 chord3=false")
	}
	v := &e.bass[0]
	// 7th 없음: 루트·3·5 = 반음 21·24·28 → 비율 2^(3/12), 2^(7/12).
	if !ratio(v.inc2, v.inc0, 3) || !ratio(v.inc3, v.inc0, 7) {
		t.Fatalf("CHORD inc 비율 위반: inc0=%.9g inc2=%.9g inc3=%.9g", v.inc0, v.inc2, v.inc3)
	}
	// 7th: 3·5·7 = 반음 24·28·31 → inc0 기준 2^(4/12), 2^(7/12).
	for b := 0; b < ChordBars; b++ {
		e.Apply(Cmd{Kind: SetChord, A: uint8(b), B: 0, C: ChordSeventh})
	}
	for i := 0; i < 40000; i++ {
		e.Render(buf)
		if e.Flags()&1 != 0 {
			break
		}
	}
	if !ratio(v.inc2, v.inc0, 4) || !ratio(v.inc3, v.inc0, 7) {
		t.Fatalf("CHORD 7th inc 비율 위반: inc0=%.9g inc2=%.9g inc3=%.9g", v.inc0, v.inc2, v.inc3)
	}

	// fold: 옥타브 5(note 35)는 37·39가 MaxNote 36으로 클램프돼 세 톤이 같은 반음(48)에
	// 접힌다 — 정규화 계약(접힘 자체는 정상). 유계·무패닉을 단언.
	e3 := harmonyFixture(0, false, []int{0}, 35)
	e3.Apply(Cmd{Kind: BassMode, A: 0, B: ModeChord, C: DirUp})
	for i := 0; i < 200; i++ {
		e3.Render(buf)
		if e3.Flags()&1 != 0 {
			break
		}
	}
	f := &e3.bass[0]
	if f.inc0 != f.inc2 || f.inc2 != f.inc3 {
		t.Fatalf("fold 기대 위반: inc0=%.9g inc2=%.9g inc3=%.9g", f.inc0, f.inc2, f.inc3)
	}
	checkInc(t, f.inc0, 48, "CHORD fold(반음 48)")
	for i := 0; i < 100; i++ {
		e3.Render(buf)
	}
	if p := e3.Peak(); p > 1.0 || !(p == p) {
		t.Fatalf("CHORD fold 100블록 peak %g — 유계 위반", p)
	}
}

//  9. BASS/ARP에서 보조 오실레이터 기여 0 — chord3=false·inc2=inc3=0(분기 계약).
//     (보이스 레벨 비트 동일성은 voice_test TestVoiceChordIgnoredWhenOff가 지킨다.)
func TestChordZeroContribution(t *testing.T) {
	for _, mode := range [...]uint8{ModeBass, ModeArp} {
		e := harmonyFixture(0, false, []int{0}, 7)
		e.Apply(Cmd{Kind: BassMode, A: 0, B: mode, C: DirUp})
		buf := make([]float32, 2*Block)
		for i := 0; i < 200; i++ {
			e.Render(buf)
			if e.Flags()&1 != 0 {
				break
			}
		}
		v := &e.bass[0]
		if v.chord3 || v.inc2 != 0 || v.inc3 != 0 {
			t.Fatalf("모드 %d: chord3=%v inc2=%g inc3=%g — 단음 경로 오염", mode, v.chord3, v.inc2, v.inc3)
		}
	}
}

// 10. 진폭 상한 — 어느 모드에서든 300초 |sample| ≤ 0.9903(outClip 계약), 무음 아님.
func TestAmplitudeAllModes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode, dir uint8
	}{
		{"BASS", ModeBass, DirUp},
		{"ARP", ModeArp, DirUpDown},
		{"CHORD", ModeChord, DirUp},
	} {
		e := New(1)
		for b := 0; b < ChordBars; b++ {
			e.Apply(Cmd{Kind: SetChord, A: uint8(b), B: uint8(b % 2), C: ChordSeventh}) // 코드 밀도 최대(7th)
		}
		e.Apply(Cmd{Kind: BassMode, A: 0, B: tc.mode, C: tc.dir})
		e.Apply(Cmd{Kind: BassMode, A: 1, B: tc.mode, C: tc.dir})
		buf := make([]float32, 2*Block)
		peak := float32(0)
		var sumSq float64
		n := 0
		for i := 0; i < SampleRate*300/Block; i++ {
			e.Render(buf)
			for _, s := range buf {
				a := s
				if a < 0 {
					a = -a
				}
				if a > peak {
					peak = a
				}
				sumSq += float64(s * s)
				n++
			}
		}
		rms := math.Sqrt(sumSq / float64(n))
		t.Logf("%s 300s peak=%v rms=%v", tc.name, peak, rms)
		if peak > 0.9903 {
			t.Fatalf("%s 모드 300초 peak %v > 0.9903", tc.name, peak)
		}
		if rms <= 0.01 {
			t.Fatalf("%s 모드 300초 RMS %v ≤ 0.01 — 사실상 무음", tc.name, rms)
		}
	}
}

// 11. 트랜스포트 — 정지 = 위치 동결·aenv 감쇠·FlagBar 없음 · 재생 = 스텝 0·바 유지·FlagBar · 멱등.
func TestTransportStopPlay(t *testing.T) {
	e := New(3)
	for st := 0; st < Steps; st++ { // 두 파트 매 스텝 게이트 — aenv 관측을 동시 트리거로
		e.Apply(Cmd{Kind: BassStep, A: 0, B: uint8(st), C: 7, D: StepGate})
		e.Apply(Cmd{Kind: BassStep, A: 1, B: uint8(st), C: 0, D: StepGate})
	}
	buf := make([]float32, 2*Block)
	for i := 0; i < 5000; i++ {
		e.Render(buf)
		if e.Flags()&1 != 0 && e.Flags()&2 != 0 {
			break
		}
	}
	a0, a1 := e.bass[0].aenv, e.bass[1].aenv
	if a0 <= 0 || a1 <= 0 {
		t.Fatalf("정지 직전 aenv %g/%g — 트리거 직후여야 함", a0, a1)
	}
	e.Apply(Cmd{Kind: Transport, A: 0})
	if e.Playing() {
		t.Fatal("Transport 정지 뒤에도 playing")
	}
	step, bar := e.Step(), e.Bar()
	// 동결 창은 스텝 경계(≈ samplesPerStep 샘플)를 넘어야 한다 — 게이트 제거 변이(M5
	// 실측)는 경계를 안 넘는 짧은 창에서는 들키지 않는다.
	freeze := int(e.samplesPerStep/Block) + 5
	for i := 0; i < freeze; i++ {
		e.Render(buf)
		if e.Step() != step || e.Bar() != bar {
			t.Fatalf("정지 중 위치 이동: step %d→%d bar %d→%d", step, e.Step(), bar, e.Bar())
		}
		if e.Flags()&FlagBar != 0 {
			t.Fatal("정지 중 FlagBar — 레지던트가 쉬어야 하는데 깨운다")
		}
	}
	if !(e.bass[0].aenv < a0 && e.bass[1].aenv < a1) {
		t.Fatalf("정지 10블록 뒤 aenv %g/%g — 감쇠 없음(직전 %g/%g)", e.bass[0].aenv, e.bass[1].aenv, a0, a1)
	}
	// 멱등: 정지 중 재정지는 무동작.
	e.Apply(Cmd{Kind: Transport, A: 0})
	if !(!e.Playing() && e.Step() == step && e.Bar() == bar) {
		t.Fatal("정지 중 Transport A:0이 상태를 바꿈")
	}
	// 재생: 현재 바 유지, 다음 Render에서 스텝 0부터, FlagBar.
	e.Apply(Cmd{Kind: Transport, A: 1})
	e.Render(buf)
	if e.Step() != 0 || e.Bar() != bar {
		t.Fatalf("재생 첫 Render: step %d bar %d — want 0/%d", e.Step(), e.Bar(), bar)
	}
	if e.Flags()&FlagBar == 0 || !e.Playing() {
		t.Fatal("재생 첫 Render에 FlagBar 없음 또는 playing 아님")
	}
	// 멱등: 재생 중 A:1은 위치를 리셋하지 않는다.
	for e.Step() < 3 {
		e.Render(buf)
	}
	e.Apply(Cmd{Kind: Transport, A: 1})
	e.Render(buf)
	if e.Step() == 0 {
		t.Fatal("재생 중 Transport A:1이 스텝을 0으로 되돌림")
	}
}

// 12. 상태 v2 왕복 + 'J','1' 거부 + 범위 밖 바이트 재정규화.
func TestStateV3RoundTrip(t *testing.T) {
	a := New(5)
	a.Apply(Cmd{Kind: SetKey, A: 3})
	a.Apply(Cmd{Kind: SetChord, A: 2, B: 2, C: ChordSeventh})
	a.Apply(Cmd{Kind: BassMode, A: 1, B: ModeArp, C: DirDown})
	a.Apply(Cmd{Kind: Transport, A: 0})
	var buf [StateSize]byte
	if n := a.WriteState(buf[:]); n != StateSize {
		t.Fatalf("WriteState %d", n)
	}
	if buf[0] != 'J' || buf[1] != '4' {
		t.Fatalf("매직 %q%q — want J3(P4-fx2 v3)", buf[0], buf[1])
	}
	f := New(9)
	if !f.ReadState(buf[:]) {
		t.Fatal("ReadState false")
	}
	if f.KeyRoot() != 3 {
		t.Fatalf("key %d, want 3", f.KeyRoot())
	}
	if d, fl := f.Chord(2); d != 2 || fl != ChordSeventh {
		t.Fatalf("Chord(2)=%d,%d want 2,%d", d, fl, ChordSeventh)
	}
	if m, dr := f.Mode(BassB); m != ModeArp || dr != DirDown {
		t.Fatalf("Mode(B)=%d,%d want %d,%d", m, dr, ModeArp, DirDown)
	}
	if m, _ := f.Mode(BassA); m != ModeBass {
		t.Fatalf("Mode(A)=%d want ModeBass(미설정 보존)", m)
	}
	if f.Playing() {
		t.Fatal("playing 보존 실패(정지 상태였음)")
	}
	// v1 헤더 거부·짧은 버퍼 거부.
	v1 := buf
	v1[1] = '1'
	if f.ReadState(v1[:]) {
		t.Fatal("'J','1' 헤더를 받아들임 — v1은 거부 계약")
	}
	if f.ReadState(v1[:StateSize-1]) {
		t.Fatal("짧은 입력을 받아들임")
	}
	// 재정규화: 도수 7 → 0, 모드·방향 3 → 0, 키 200 → 8, playing 5 → true.
	mut := buf
	mut[offChord] = 7
	mut[offMode+1] = 0xFF
	mut[offKey] = 200
	mut[offPlaying] = 5
	if !f.ReadState(mut[:]) {
		t.Fatal("재정규화 대상 입력인데 ReadState false")
	}
	if d, _ := f.Chord(0); d != 0 {
		t.Fatalf("도수 7 → %d, want 0(%%7)", d)
	}
	if m, dr := f.Mode(BassB); m != ModeBass || dr != DirUp {
		t.Fatalf("모드·방향 3 → %d,%d, want 0,0", m, dr)
	}
	if f.KeyRoot() != 8 {
		t.Fatalf("키 200 → %d, want 8", f.KeyRoot())
	}
	if !f.Playing() {
		t.Fatal("playing 5 → false, want true(≠0)")
	}
}

// 13. 결정론 — 같은 seed·같은 화성 Cmd 이력(모드·코드·정지/재생 포함)은 비트 동일.
func TestDeterminismWithHarmonyCmds(t *testing.T) {
	run := func() []float32 {
		e := New(7)
		e.Apply(Cmd{Kind: SetKey, A: 4})
		for b := 0; b < ChordBars; b++ {
			e.Apply(Cmd{Kind: SetChord, A: uint8(b), B: uint8(b % NumDegrees), C: uint8(b%2) * ChordSeventh})
		}
		e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: DirUpDown})
		e.Apply(Cmd{Kind: BassMode, A: 1, B: ModeChord, C: DirDown})
		buf := make([]float32, 2*Block)
		total := make([]float32, 0, 2*Block*300)
		for i := 0; i < 300; i++ {
			switch i {
			case 50:
				e.Apply(Cmd{Kind: Transport, A: 0})
			case 90:
				e.Apply(Cmd{Kind: Transport, A: 1})
			case 150:
				e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeBass, C: DirUp})
			case 200:
				e.Apply(Cmd{Kind: SetChord, A: 3, B: 5, C: ChordSeventh})
			}
			e.Render(buf)
			total = append(total, buf...)
		}
		return total
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("길이 불일치 %d vs %d", len(a), len(b))
	}
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			t.Fatalf("샘플 %d 불일치 %v vs %v — 화성 Cmd 이력이 같은데 다름", i, a[i], b[i])
		}
	}
}

// 14. 무할당 — ARP·CHORD 렌더 경로와 새 Cmd 4종 Apply 경로 모두 AllocsPerRun == 0.
func TestHarmonyNoAllocs(t *testing.T) {
	for _, mode := range [...]uint8{ModeArp, ModeChord} {
		e := New(1)
		e.Apply(Cmd{Kind: BassMode, A: 0, B: mode, C: DirUpDown})
		e.Apply(Cmd{Kind: BassMode, A: 1, B: mode, C: DirDown})
		buf := make([]float32, 2*Block)
		e.Render(buf)
		n := testing.AllocsPerRun(1000, func() { e.Render(buf) }) // 1000블록 ≈ 스텝 23개 — 게이트 스텝 포함
		if n != 0 {
			t.Fatalf("모드 %d Render 할당: %v", mode, n)
		}
	}
	e := New(1)
	n := testing.AllocsPerRun(1000, func() {
		e.Apply(Cmd{Kind: SetChord, A: 1, B: 2, C: ChordSeventh})
		e.Apply(Cmd{Kind: SetKey, A: 3})
		e.Apply(Cmd{Kind: BassMode, A: 0, B: ModeArp, C: DirDown})
		e.Apply(Cmd{Kind: Transport, A: 1})
	})
	if n != 0 {
		t.Fatalf("화성 Cmd Apply 할당: %v", n)
	}
}
