// url_v2_test.go — URL v2 계약 ↔ 단언 표(새 Cmd 4종 직렬화). 계약 원본: docs/impl-plan-2026-09-05.md §12.5.
//
// | 계약 | 단언 테스트 |
// |---|---|
// | 접두사 "v2."·버전 바이트 2 · v1 접두사는 "지원하지 않는 버전" err로 거부(note 의미 변경) | TestV2PrefixAndV1Rejection |
// | 새 Kind 왕복: SetKey A(1B) · SetChord A,B,C(3B) · BassMode A,B,C(3B) · Transport A(1B) — D·V는 0 | TestV2RoundTripNewKinds |
// | 범위 밖 값(255 등)은 그대로 보존 — 정규화는 엔진 Apply가 한다(url.go 상단 원칙) | TestV2RoundTripNewKinds |
// | Kind < NumCmdKinds 전부 프레이밍 보존(미래 Kind가 readCmd에 빠지면 즉시 실패) | TestV2AllKindsFraming |
// | 새 Kind 필드 중간 잘림 → 읽은 것까지 유효 + error | TestV2TruncatedNewKindFields |
// | 알 수 없는 Kind(≥ NumCmdKinds) → 0바이트로 건너뛰고 계속 | TestV2UnknownKindStillSkips |
// | 같은 스텝 같은 마디 SetChord — 마지막 것만(스텝 양자화의 자연 결과와 같은 규칙) | TestV2SetChordSameStepReduction |
// | 재인코딩 고정점 — 새 Kind 혼합 로그에서 감량 0..3단계 | TestV2FixedPointWithNewKinds |
// | Replay·ReplaySteps가 새 Kind를 Apply 그대로 적용 | TestV2ReplayAppliesNewKinds |
//
// 3-클래스 입력 방어 표:
// | 클래스 | 단언 |
// |---|---|
// | 악의(거대 varint 델타, 계약 밖 Kind 바이트) | TestDecodeHostileVarint(session_test.go), TestV2UnknownKindStillSkips |
// | 손상(base64·헤더·새 Kind 필드 잘림) | TestDecodeRejectsBadSchema(session_test.go), TestV2TruncatedNewKindFields |
// | 구버전 스키마(v1) | TestV2PrefixAndV1Rejection |
package session

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/midagedev/revirth/engine"
)

// v2Log — 새 Kind 4종이 섞인 로그. 스텝 겹침 방지: 블록 간격 60 > 스텝당 43.3블록(130BPM).
// Transport A=0(정지)이 있어 재생 계열 테스트에는 쓰지 않는다(정지는 시계를 동결한다).
func v2Log() *Log {
	l := &Log{Word: "노동요", Seed: SeedFromWord("노동요")}
	l.Append(0, Human, engine.Cmd{Kind: engine.SetKey, A: 5})
	l.Append(60, Human, engine.Cmd{Kind: engine.SetChord, A: 3, B: 5, C: engine.ChordSeventh})
	l.Append(120, Human, engine.Cmd{Kind: engine.BassMode, A: 1, B: engine.ModeArp, C: engine.DirDown})
	l.Append(180, Human, engine.Cmd{Kind: engine.Transport, A: 0})
	l.Append(240, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.7})
	l.Append(300, Resident, engine.Cmd{Kind: engine.BassStep, A: 0, B: 4, C: 21, D: engine.StepGate})
	l.Append(360, Human, engine.Cmd{Kind: engine.Trigger, A: uint8(engine.CP)})
	return l
}

func TestV2PrefixAndV1Rejection(t *testing.T) {
	url := EncodeURL(v2Log())
	if !strings.HasPrefix(url, "v2.") {
		t.Fatalf("접두사: %q, want \"v2.\"", url[:8])
	}
	if _, err := DecodeURL(url); err != nil {
		t.Fatalf("v2 디코드: %v", err)
	}
	// v1 문자열 거부 — Phase 2에서 BassStep note가 절대음→도수로 바뀌어 v1 재생은 다른 소리가 난다.
	l1, err := DecodeURL("v1." + strings.TrimPrefix(url, "v2."))
	if err == nil || l1 != nil {
		t.Fatalf("v1 접두사 거부 안 됨: log=%v err=%v", l1, err)
	}
	if !strings.Contains(err.Error(), "v1") || !strings.Contains(err.Error(), "지원하지 않는 버전") {
		t.Errorf("v1 거부 문구에 \"v1\"·\"지원하지 않는 버전\" 필요: %q", err.Error())
	}
	if _, err := DecodeURL("v3." + strings.TrimPrefix(url, "v2.")); err == nil {
		t.Error("v3 접두사 거부 안 됨(접두사 불일치)")
	}
}

func TestV2RoundTripNewKinds(t *testing.T) {
	cases := []engine.Cmd{
		{Kind: engine.SetKey, A: 5},
		{Kind: engine.SetKey, A: 255}, // 범위 밖 루트 — 그대로 보존
		{Kind: engine.SetChord, A: 3, B: 6, C: engine.ChordSeventh},
		{Kind: engine.SetChord, A: 255, B: 255, C: 255},
		{Kind: engine.BassMode, A: 1, B: engine.ModeChord, C: engine.DirUpDown},
		{Kind: engine.BassMode, A: 0, B: 255, C: 255},
		{Kind: engine.Transport, A: 1},
		{Kind: engine.Transport, A: 255},
	}
	l := &Log{}
	for i := range cases {
		l.Append(uint32(i)*60, Human, cases[i]) // 스텝 겹침 없음 → 감량 대상 아님
	}
	d, err := DecodeURL(EncodeURL(l))
	if err != nil {
		t.Fatalf("디코드: %v", err)
	}
	if len(d.Entries) != len(cases) {
		t.Fatalf("엔트리 %d개, want %d(새 Kind는 감량 대상 아님)", len(d.Entries), len(cases))
	}
	for i, c := range cases {
		got := d.Entries[i].Cmd
		if got.Kind != c.Kind || got.A != c.A || got.B != c.B || got.C != c.C {
			t.Errorf("케이스 %d(%v): 필드 왕복 %+v, want %+v", i, c.Kind, got, c)
		}
		if got.D != 0 || got.V != 0 {
			t.Errorf("케이스 %d(%v): D·V는 0이어야 한다: D=%d V=%v", i, c.Kind, got.D, got.V)
		}
	}
}

func TestV2AllKindsFraming(t *testing.T) {
	cmds := []engine.Cmd{
		{Kind: engine.SetParam, A: uint8(engine.Tempo), V: 1},
		{Kind: engine.BassStep, A: 1, B: 7, C: 24, D: engine.StepGate | engine.StepSlide},
		{Kind: engine.DrumStep, A: uint8(engine.SD), B: 3, D: engine.StepAccent},
		{Kind: engine.SelectPattern, A: 0, B: 4},
		{Kind: engine.Mute, A: uint8(engine.CH), B: 1},
		{Kind: engine.Trigger, A: uint8(engine.CY)},
		{Kind: engine.Drop},
		{Kind: engine.ResetPos},
		{Kind: engine.SetKey, A: 9},
		{Kind: engine.SetChord, A: 7, B: 4, C: engine.ChordSeventh},
		{Kind: engine.BassMode, A: 0, B: engine.ModeArp, C: engine.DirUpDown},
		{Kind: engine.Transport, A: 1},
	}
	l := &Log{}
	for i := range cmds {
		l.Append(uint32(i)*60, Human, cmds[i])
	}
	d, err := DecodeURL(EncodeURL(l))
	if err != nil {
		t.Fatalf("디코드: %v", err)
	}
	if len(d.Entries) != len(cmds) {
		t.Fatalf("엔트리 %d개, want %d — Kind 직렬화 프레이밍 깨짐", len(d.Entries), len(cmds))
	}
	seen := map[engine.CmdKind]bool{}
	for i, c := range cmds {
		got := d.Entries[i].Cmd
		seen[got.Kind] = true
		if got.A != c.A || got.B != c.B || got.C != c.C || got.D != c.D {
			t.Errorf("Kind %v 필드 왕복: %+v, want %+v", c.Kind, got, c)
		}
		if c.Kind == engine.SetParam {
			if got.V != float32(quantizeV(c.V))/engine.ParamSteps {
				t.Errorf("SetParam 값 왕복: %v", got.V)
			}
		} else if got.V != 0 {
			t.Errorf("Kind %v: V는 0이어야 한다: %v", c.Kind, got.V)
		}
	}
	for k := engine.CmdKind(0); k < engine.NumCmdKinds; k++ {
		if !seen[k] {
			t.Errorf("Kind %d(NumCmdKinds=%d)가 직렬화 검증 표에 없다 — appendCmd/readCmd 케이스를 추가하라", k, engine.NumCmdKinds)
		}
	}
}

func TestV2TruncatedNewKindFields(t *testing.T) {
	// 3바이트 필드 Kind(SetChord)가 마지막 엔트리 — 끝 1·2바이트를 자르면 필드 잘림.
	l3 := &Log{}
	l3.Append(0, Human, engine.Cmd{Kind: engine.SetKey, A: 5})
	l3.Append(60, Human, engine.Cmd{Kind: engine.SetChord, A: 2, B: 5, C: engine.ChordSeventh})
	raw3, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(EncodeURL(l3), urlPrefix))
	if err != nil {
		t.Fatal(err)
	}
	for cut := 1; cut <= 2; cut++ {
		d, err := DecodeURL(urlPrefix + base64.RawURLEncoding.EncodeToString(raw3[:len(raw3)-cut]))
		if err == nil {
			t.Fatalf("SetChord cut=%d: 잘린 스트림인데 error 없음", cut)
		}
		if d == nil || len(d.Entries) != 1 {
			t.Errorf("SetChord cut=%d: 부분 결과 엔트리 %d개, want 1(읽은 것까지 유효)", cut, entryCount(d))
		}
	}
	// 3바이트 필드(BassMode)가 스트림 중간 엔트리여도 같은 규칙.
	lm := &Log{}
	lm.Append(0, Human, engine.Cmd{Kind: engine.SetKey, A: 5})
	lm.Append(60, Human, engine.Cmd{Kind: engine.BassMode, A: 0, B: engine.ModeChord, C: engine.DirDown})
	rawm, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(EncodeURL(lm), urlPrefix))
	if err != nil {
		t.Fatal(err)
	}
	if d, err := DecodeURL(urlPrefix + base64.RawURLEncoding.EncodeToString(rawm[:len(rawm)-1])); err == nil || entryCount(d) != 1 {
		t.Errorf("BassMode cut=1: err=%v 엔트리=%d, want error+1", err, entryCount(d))
	}
	// 1바이트 필드 Kind(SetKey) — 필드 바이트가 없으면 역시 잘림.
	lk := &Log{}
	lk.Append(0, Human, engine.Cmd{Kind: engine.SetKey, A: 5})
	rawk, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(EncodeURL(lk), urlPrefix))
	if err != nil {
		t.Fatal(err)
	}
	if d, err := DecodeURL(urlPrefix + base64.RawURLEncoding.EncodeToString(rawk[:len(rawk)-1])); err == nil || entryCount(d) != 0 {
		t.Errorf("SetKey cut=1: err=%v 엔트리=%d, want error+0", err, entryCount(d))
	}
}

func TestV2UnknownKindStillSkips(t *testing.T) {
	// 계약 밖 Kind 바이트 뒤에 정상 Transport(1바이트 필드) — 건너뛰기가 프레이밍을 깨뜨리면 바로 드러난다.
	for _, kind := range []byte{byte(engine.NumCmdKinds), 0xFF} {
		payload := []byte{2} // 버전
		payload = append(payload, 7)                    // seed varint
		payload = append(payload, 0)                    // 단어 길이 0
		payload = append(payload, 0, kind)              // step 델타 0 + 알 수 없는 Kind
		payload = append(payload, 0, byte(engine.Transport)) // 델타 0 + Kind Transport
		payload = append(payload, 1)                        // Transport.A = 1
		d, err := DecodeURL(urlPrefix + base64.RawURLEncoding.EncodeToString(payload))
		if err != nil {
			t.Fatalf("kind 0x%02X 건너뛰기 실패: %v", kind, err)
		}
		if len(d.Entries) != 1 || d.Entries[0].Cmd.Kind != engine.Transport || d.Entries[0].Cmd.A != 1 {
			t.Errorf("kind 0x%02X: 엔트리 %+v, want Transport{A:1} 1개", kind, cmdsOf(d))
		}
	}
}

func cmdsOf(l *Log) []engine.Cmd {
	var cs []engine.Cmd
	if l != nil {
		for _, e := range l.Entries {
			cs = append(cs, e.Cmd)
		}
	}
	return cs
}

func TestV2SetChordSameStepReduction(t *testing.T) {
	// 블록 44·45·46은 130BPM에서 전부 스텝 1(43.269..86.538).
	l := &Log{}
	l.Append(44, Human, engine.Cmd{Kind: engine.SetChord, A: 2, B: 1, C: 0})
	l.Append(45, Human, engine.Cmd{Kind: engine.SetChord, A: 3, B: 4, C: engine.ChordSeventh})
	l.Append(46, Human, engine.Cmd{Kind: engine.SetChord, A: 2, B: 5, C: 0})
	d, err := DecodeURL(EncodeURL(l))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Entries) != 2 {
		t.Fatalf("엔트리 %d개, want 2(마디 2는 마지막 B=5만, 마디 3은 생존)", len(d.Entries))
	}
	if c := d.Entries[1].Cmd; c.A != 2 || c.B != 5 || c.C != 0 {
		t.Errorf("마디 2의 마지막 값이 아니라 %+v", c)
	}
	if c := d.Entries[0].Cmd; c.A != 3 || c.B != 4 || c.C != engine.ChordSeventh {
		t.Errorf("다른 마디(3)가 같은 스텝 감량에 섞여 삭제됨: %+v", c)
	}

	// 다른 스텝의 같은 마디는 둘 다 산다(감량은 스텝 내에서만).
	l2 := &Log{}
	l2.Append(44, Human, engine.Cmd{Kind: engine.SetChord, A: 2, B: 1, C: 0})
	l2.Append(88, Human, engine.Cmd{Kind: engine.SetChord, A: 2, B: 4, C: engine.ChordSeventh}) // 스텝 2(86.538..)
	d2, err := DecodeURL(EncodeURL(l2))
	if err != nil {
		t.Fatal(err)
	}
	if len(d2.Entries) != 2 || d2.Entries[1].Cmd.B != 4 {
		t.Errorf("다른 스텝 같은 마디가 감량됨: %d개 %+v", len(d2.Entries), cmdsOf(d2))
	}

	// A%ChordBars가 같으면 같은 마디(10%8=2) — 엔진 Apply의 마디 해석과 같은 기준.
	l3 := &Log{}
	l3.Append(44, Human, engine.Cmd{Kind: engine.SetChord, A: 2, B: 1, C: 0})
	l3.Append(45, Human, engine.Cmd{Kind: engine.SetChord, A: 10, B: 6, C: engine.ChordSeventh})
	d3, err := DecodeURL(EncodeURL(l3))
	if err != nil {
		t.Fatal(err)
	}
	if len(d3.Entries) != 1 || d3.Entries[0].Cmd.A != 10 || d3.Entries[0].Cmd.B != 6 {
		t.Errorf("같은 마디(A%%8) 감량 안 됨: %d개 %+v", len(d3.Entries), cmdsOf(d3))
	}
}

func TestV2FixedPointWithNewKinds(t *testing.T) {
	l := v2Log()
	// 같은 스텝 중간값 추가 — 감량(SetParam·SetChord)이 실제로 일하는 로그로 고정점을 잰다.
	l.Append(240, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.1})
	l.Append(245, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.5}) // 같은 스텝 → 감량
	l.Append(300, Human, engine.Cmd{Kind: engine.SetChord, A: 0, B: 2, C: 0})
	l.Append(301, Human, engine.Cmd{Kind: engine.SetChord, A: 0, B: 6, C: engine.ChordSeventh}) // 같은 스텝 같은 마디 → 감량
	for level := 0; level <= 3; level++ {
		url := encodePayload(l, level)
		d, err := DecodeURL(url)
		if err != nil {
			t.Fatalf("감량 %d단계 디코드: %v", level, err)
		}
		if again := EncodeURL(d); again != url {
			t.Errorf("감량 %d단계 고정점 위반(재인코딩 %d자 vs %d자)", level, len(again), len(url))
		}
	}
	d, err := DecodeURL(EncodeURL(l))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(d.Entries); n != len(l.Entries)-2 {
		t.Errorf("엔트리 %d개, want %d(중간값 2개 감량)", n, len(l.Entries)-2)
	}
}

func TestV2ReplayAppliesNewKinds(t *testing.T) {
	// Replay(블록 로그) — Transport 정지 포함. 정지는 시계를 동결하므로 ReplaySteps가 아니라
	// 고정 블록 루프인 Replay로 재생한다.
	l := &Log{Seed: 1, Word: "a"}
	l.Append(0, Human, engine.Cmd{Kind: engine.SetKey, A: 5})
	l.Append(60, Human, engine.Cmd{Kind: engine.SetChord, A: 3, B: 5, C: engine.ChordSeventh})
	l.Append(120, Human, engine.Cmd{Kind: engine.BassMode, A: 0, B: engine.ModeArp, C: engine.DirDown})
	l.Append(180, Human, engine.Cmd{Kind: engine.Transport, A: 0})
	e := engine.New(1)
	if !e.Playing() {
		t.Fatal("엔진 초기 상태는 재생이어야 한다")
	}
	Replay(e, l, 300, nil)
	if got := e.KeyRoot(); got != 5 {
		t.Errorf("SetKey 적용 안 됨: KeyRoot=%d, want 5", got)
	}
	if deg, flags := e.Chord(3); deg != 5 || flags != engine.ChordSeventh {
		t.Errorf("SetChord 적용 안 됨: bar3=(%d,%d), want (5,%d)", deg, flags, engine.ChordSeventh)
	}
	if mode, dir := e.Mode(engine.BassA); mode != engine.ModeArp || dir != engine.DirDown {
		t.Errorf("BassMode 적용 안 됨: (%d,%d), want (%d,%d)", mode, dir, engine.ModeArp, engine.DirDown)
	}
	if e.Playing() {
		t.Error("Transport 정지 적용 안 됨: 여전히 재생 중")
	}

	// ReplaySteps(URL 디코드 엔트리) — 스텝 경계 적용. 정지가 없는 로그로.
	l2 := &Log{Seed: 1, Word: "a"}
	l2.Append(0, Human, engine.Cmd{Kind: engine.SetKey, A: 9})
	l2.Append(60, Human, engine.Cmd{Kind: engine.SetChord, A: 3, B: 6, C: 0})
	l2.Append(120, Human, engine.Cmd{Kind: engine.BassMode, A: 1, B: engine.ModeChord, C: engine.DirUpDown})
	d, err := DecodeURL(EncodeURL(l2))
	if err != nil {
		t.Fatal(err)
	}
	e2 := engine.New(1)
	ReplaySteps(e2, d.Entries, 8, nil)
	if got := e2.KeyRoot(); got != 9 {
		t.Errorf("ReplaySteps SetKey: KeyRoot=%d, want 9", got)
	}
	if deg, _ := e2.Chord(3); deg != 6 {
		t.Errorf("ReplaySteps SetChord: bar3 deg=%d, want 6", deg)
	}
	if mode, dir := e2.Mode(engine.BassB); mode != engine.ModeChord || dir != engine.DirUpDown {
		t.Errorf("ReplaySteps BassMode: (%d,%d), want (%d,%d)", mode, dir, engine.ModeChord, engine.DirUpDown)
	}
}
