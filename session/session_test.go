// session_test.go — 계약 ↔ 단언 표. 계약 원본: 작업 스펙(T2) 및 docs/impl-plan-2026-09-05.md §5.
//
// | 계약 | 단언 테스트 |
// |---|---|
// | Author 값 0..3(Human/Resident/ReplayAuthor/System) | TestAuthorValues |
// | Append 블록 단조 — 과거 블록 클램프 + Reordered++ | TestAppendMonotonicClamp |
// | AddKeyframe — StateSize 미만 무시, 블록 단조 | TestAddKeyframeGuards |
// | Since — block 이하 마지막 키프레임 + 그 이후 엔트리 | TestSinceBoundaries |
// | SeedFromWord: "acid"=="ACID "=="a c i d", ≠"acud", "노동요" 고정값, 빈→0x9E3779B9, 0→1 | TestSeedFromWord, TestFNV1a32Vectors |
// | URL: v2.+base64url, 헤더(버전·seed·단어), 엔트리 스트림 | TestEncodeDecodeRoundTrip |
// | Author 미기입(디코드 시 ReplayAuthor), 키프레임 미기입 | TestEncodeDecodeRoundTrip |
// | 스텝 양자화 — 템포 변경 추적(engine.BPMOf 공식, float64 누적) | TestStepQuantizationFollowsTempo |
// | 같은 스텝 같은 파라미터 SetParam — 마지막 값만 | TestSetParamSameStepReduction |
// | EncodeURLBudget — 2/4/8스텝당 1개 단계 선택, 예산 준수 | TestEncodeURLBudgetLevels |
// | 왕복 고정점 Encode→Decode→Encode 바이트 동일(감량 0..3단계) | TestURLFixedPoint |
// | 디코드 재정규화: 잘림→부분+err, 알 수 없는 Kind→건너뜀 | TestDecodeTruncatedStream, TestDecodeUnknownKindSkips |
// | 버전 불일치·접두사·base64·거대 varint 거부 | TestDecodeRejectsBadSchema, TestDecodeHostileVarint |
// | URL v2 — 새 Kind 4종·v1 거부·같은 스텝 같은 마디 SetChord 감량(§12.5) | url_v2_test.go 표 참조 |
// | Replay — 두 엔진 같은 로그 → 샘플 바이트 동일, 수동 루프와 동일 | TestReplayDeterministic, TestReplayMatchesManual |
// | ReplaySteps — 스텝 경계 블록 적용(Step()/Bar() 감시) | TestReplayStepsAppliesAtStepBoundaries |
// | jdsess -print URL → DecodeURL 엔트리 수 일치 | TestJdsessPrintURLDecodes |
// | nil 로그 인코딩 방어 | TestNilLogEncodes |
package session

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/midagedev/jangdan/engine"
)

// ---- 시드 단어 ----

func TestSeedFromWord(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want uint32
	}{
		{"acid", "acid", 0x550683A8},
		{"대문자+공백 무시", "ACID ", 0x550683A8},
		{"글자 사이 공백 무시", "a c i d", 0x550683A8},
		{"다른 단어는 다른 시드", "acud", 0x54E86E94},
		{"한글 안정값", "노동요", 0xF59957DA},
		{"빈 문자열", "", 0x9E3779B9},
		{"공백만 → 정규화 결과 빈 → 같은 규칙", "   ", 0x9E3779B9},
	}
	for _, c := range cases {
		if got := SeedFromWord(c.in); got != c.want {
			t.Errorf("%s: SeedFromWord(%q)=0x%08X, want 0x%08X", c.name, c.in, got, c.want)
		}
	}
	if SeedFromWord("acid") == SeedFromWord("acud") {
		t.Error("acid와 acud가 같은 시드")
	}
}

func TestFNV1a32Vectors(t *testing.T) {
	if got := fnv1a32(""); got != 0x811C9DC5 {
		t.Errorf("fnv1a32(\"\")=0x%08X, want 오프셋 기저 0x811C9DC5", got)
	}
	if got := fnv1a32("foobar"); got != 0xBF9CF968 {
		t.Errorf("fnv1a32(\"foobar\")=0x%08X, want 공개 벡터 0xBF9CF968", got)
	}
	if fnvOrOne(0) != 1 {
		t.Error("fnvOrOne(0)은 1이어야 한다(엔진 seed 0 대체 회피)")
	}
}

// ---- 로그·키프레임 ----

func TestAuthorValues(t *testing.T) {
	if Human != 0 || Resident != 1 || ReplayAuthor != 2 || System != 3 {
		t.Errorf("Author 값 계약 위반: %d %d %d %d", Human, Resident, ReplayAuthor, System)
	}
}

func TestAppendMonotonicClamp(t *testing.T) {
	var l Log
	l.Append(10, Human, engine.Cmd{Kind: engine.Drop})
	l.Append(12, Human, engine.Cmd{Kind: engine.Drop})
	l.Append(5, Human, engine.Cmd{Kind: engine.Drop}) // 과거 → 12로 클램프
	l.Append(12, Resident, engine.Cmd{Kind: engine.Drop})
	if l.Reordered != 1 {
		t.Errorf("Reordered=%d, want 1", l.Reordered)
	}
	for i, e := range l.Entries {
		if i > 0 && e.Block < l.Entries[i-1].Block {
			t.Fatalf("엔트리 %d 블록 역행: %d < %d", i, e.Block, l.Entries[i-1].Block)
		}
	}
	if l.Entries[2].Block != 12 {
		t.Errorf("과거 블록 엔트리가 클램프되지 않음: %d", l.Entries[2].Block)
	}
}

func TestAddKeyframeGuards(t *testing.T) {
	var l Log
	full := make([]byte, engine.StateSize)
	l.AddKeyframe(30, full[:engine.StateSize-1]) // 짧은 state → 무시
	if len(l.Keyframes) != 0 {
		t.Errorf("StateSize 미만 state로 키프레임이 생김: %d", len(l.Keyframes))
	}
	l.AddKeyframe(30, full)
	l.AddKeyframe(20, full) // 과거 블록 → 클램프
	if len(l.Keyframes) != 2 || l.Keyframes[1].Block != 30 {
		t.Errorf("키프레임 단조 위반: %+v", l.Keyframes)
	}
}

func TestSinceBoundaries(t *testing.T) {
	var l Log
	for _, b := range []uint32{5, 15, 25, 35} {
		l.Append(b, Human, engine.Cmd{Kind: engine.Drop})
	}
	full := make([]byte, engine.StateSize)
	l.AddKeyframe(10, full)
	l.AddKeyframe(30, full)

	kf, entries := l.Since(9) // 키프레임 이전 → nil + 전부
	if kf != nil || len(entries) != 4 {
		t.Errorf("Since(9): kf=%v entries=%d, want nil/4", kf, len(entries))
	}
	kf, entries = l.Since(10) // 경계 포함(이하)
	if kf == nil || kf.Block != 10 || len(entries) != 3 || entries[0].Block != 15 {
		t.Errorf("Since(10): kf.Block=%v entries=%v", kf.Block, blocksOf(entries))
	}
	kf, entries = l.Since(29)
	if kf == nil || kf.Block != 10 || len(entries) != 3 {
		t.Errorf("Since(29): kf.Block=%v entries=%d, want 10/3", kf.Block, len(entries))
	}
	kf, entries = l.Since(30)
	if kf == nil || kf.Block != 30 || len(entries) != 1 || entries[0].Block != 35 {
		t.Errorf("Since(30): kf.Block=%v entries=%v", kf.Block, blocksOf(entries))
	}
	kf, _ = l.Since(1000)
	if kf == nil || kf.Block != 30 {
		t.Errorf("Since(1000): kf.Block=%v, want 30", kf.Block)
	}
}

func blocksOf(es []Entry) []uint32 {
	var bs []uint32
	for _, e := range es {
		bs = append(bs, e.Block)
	}
	return bs
}

// ---- URL 인코딩·디코딩 ----

func sampleLog() *Log {
	l := &Log{Word: "노동요", Seed: SeedFromWord("노동요")}
	l.Append(0, Human, engine.Cmd{Kind: engine.SetParam, A: uint8(engine.Tempo), V: 1}) // 160 BPM
	l.Append(0, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.1})
	l.Append(1, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.3}) // 같은 스텝 → 감량
	l.Append(36, Human, engine.Cmd{Kind: engine.BassStep, A: 1, B: 7, C: 24, D: engine.StepGate | engine.StepSlide})
	l.Append(36, Resident, engine.Cmd{Kind: engine.DrumStep, A: uint8(engine.SD), B: 3, D: engine.StepGate | engine.StepAccent})
	l.Append(70, Human, engine.Cmd{Kind: engine.SelectPattern, A: 0, B: 4})
	l.Append(71, Human, engine.Cmd{Kind: engine.Mute, A: uint8(engine.CH), B: 1})
	l.Append(72, Human, engine.Cmd{Kind: engine.Trigger, A: uint8(engine.CP)})
	l.Append(73, System, engine.Cmd{Kind: engine.Drop})
	l.Append(74, Human, engine.Cmd{Kind: engine.ResetPos})
	return l
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	l := sampleLog()
	full := make([]byte, engine.StateSize)
	l.AddKeyframe(10, full) // URL에 없어야 한다

	url := EncodeURL(l)
	if !strings.HasPrefix(url, "v2.") {
		t.Fatalf("접두사: %q", url[:8])
	}
	d, err := DecodeURL(url)
	if err != nil {
		t.Fatalf("DecodeURL: %v", err)
	}
	if d.Seed != l.Seed || d.Word != l.Word {
		t.Errorf("헤더 왕복: seed %d/%d word %q/%q", d.Seed, l.Seed, d.Word, l.Word)
	}
	if len(d.Keyframes) != 0 {
		t.Errorf("키프레임이 URL에 실림: %d", len(d.Keyframes))
	}
	// 11개 엔트리 중 같은 스텝 같은 파라미터 1개 감량 → 10개.
	if len(d.Entries) != len(l.Entries)-1 {
		t.Errorf("엔트리 수 %d, want %d(감량 후)", len(d.Entries), len(l.Entries)-1)
	}
	kindCount := map[engine.CmdKind]int{}
	for _, e := range d.Entries {
		if e.Author != ReplayAuthor {
			t.Errorf("디코드 저자=%d, want ReplayAuthor(2)", e.Author)
		}
		kindCount[e.Cmd.Kind]++
	}
	for k, want := range map[engine.CmdKind]int{
		engine.SetParam: 2, engine.BassStep: 1, engine.DrumStep: 1, engine.SelectPattern: 1,
		engine.Mute: 1, engine.Trigger: 1, engine.Drop: 1, engine.ResetPos: 1,
	} {
		if kindCount[k] != want {
			t.Errorf("Kind %d 개수 %d, want %d", k, kindCount[k], want)
		}
	}
	// 필드 왕복: 마지막 SetParam(5)은 값 0.3의 양자화.
	if got := d.Entries[1].Cmd.V; got != float32(quantizeV(0.3))/engine.ParamSteps {
		t.Errorf("SetParam 값 왕복: %v", got)
	}
	if bs := d.Entries[2].Cmd; bs.Kind != engine.BassStep || bs.A != 1 || bs.B != 7 || bs.C != 24 || bs.D != engine.StepGate|engine.StepSlide {
		t.Errorf("BassStep 필드 왕복: %+v", bs)
	}
}

func TestURLFixedPoint(t *testing.T) {
	for level := 0; level <= 3; level++ {
		url := encodePayload(sampleLog(), level)
		d, err := DecodeURL(url)
		if err != nil {
			t.Fatalf("감량 %d단계 디코드: %v", level, err)
		}
		if again := EncodeURL(d); again != url {
			t.Errorf("감량 %d단계 고정점 위반: 재인코딩이 원본과 다름(len %d vs %d)", level, len(again), len(url))
		}
	}
}

func TestStepQuantizationFollowsTempo(t *testing.T) {
	// 내부 시계: 기본 130 BPM → 스텝당 43.269..블록. 160 BPM은 35.15625블록(정확한 2진).
	clk := newStepClock()
	if got := clk.stepOf(43); got != 0 {
		t.Errorf("130BPM 블록 43 → 스텝 %d, want 0(경계 43.269)", got)
	}
	if got := clk.stepOf(44); got != 1 {
		t.Errorf("130BPM 블록 44 → 스텝 %d, want 1", got)
	}
	clk2 := newStepClock()
	clk2.stepOf(0)
	clk2.setTempoQ(engine.ParamSteps) // 160 BPM
	if got := clk2.stepOf(75); got != 2 {
		t.Errorf("160BPM 블록 75 → 스텝 %d, want 2(경계 35.15625, 70.3125)", got)
	}
	if clk2.start != 70.3125 {
		t.Errorf("160BPM 스텝2 시작 블록 %v, want 70.3125(정확한 2진)", clk2.start)
	}

	// URL을 통한 관측: 같은 블록 75가 템포에 따라 다른 스텝 → 다른 디코드 블록.
	slow := &Log{}
	slow.Append(75, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.5})
	fast := &Log{}
	fast.Append(0, Human, engine.Cmd{Kind: engine.SetParam, A: uint8(engine.Tempo), V: 1})
	fast.Append(75, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.5})
	db, err := DecodeURL(EncodeURL(slow))
	if err != nil {
		t.Fatal(err)
	}
	if db.Entries[0].Block != 44 { // 130BPM 스텝1 시작 블록 ceil(43.269)
		t.Errorf("템포 없음: 디코드 블록 %d, want 44", db.Entries[0].Block)
	}
	db, err = DecodeURL(EncodeURL(fast))
	if err != nil {
		t.Fatal(err)
	}
	if db.Entries[1].Block != 71 { // 160BPM 스텝2 시작 블록 ceil(70.3125)
		t.Errorf("160BPM: 디코드 블록 %d, want 71", db.Entries[1].Block)
	}

	// 로그 중간 템포 변경: 기본 시계로 스텝1(43.269)에서 100 BPM(56.25블록)으로.
	// 다음 경계 43.269+56.25=99.519 → 블록 130은 스텝2, 디코드 블록 ceil(99.519)=100.
	// (템포를 추적하지 않으면 스텝3의 ceil(129.808)=130이 나온다.)
	mid := &Log{}
	mid.Append(0, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.5})
	mid.Append(50, Human, engine.Cmd{Kind: engine.SetParam, A: uint8(engine.Tempo), V: 0}) // 100 BPM
	mid.Append(130, Human, engine.Cmd{Kind: engine.SetParam, A: 6, V: 0.5})
	db, err = DecodeURL(EncodeURL(mid))
	if err != nil {
		t.Fatal(err)
	}
	if db.Entries[2].Block != 100 {
		t.Errorf("중간 템포 변경: 디코드 블록 %d, want 100(템포 미추적이면 130)", db.Entries[2].Block)
	}
}

func TestSetParamSameStepReduction(t *testing.T) {
	l := &Log{}
	l.Append(44, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.1})
	l.Append(45, Human, engine.Cmd{Kind: engine.SetParam, A: 6, V: 0.2})
	l.Append(46, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.3})
	d, err := DecodeURL(EncodeURL(l))
	if err != nil {
		t.Fatal(err)
	}
	// 44..46은 130BPM에서 전부 스텝1(43.269..86.538) → 파라미터별 마지막만: A6@0.2, A5@0.3.
	if len(d.Entries) != 2 {
		t.Fatalf("엔트리 %d개, want 2(같은 스텝 같은 파라미터 감량)", len(d.Entries))
	}
	if d.Entries[0].Cmd.A != 6 || d.Entries[1].Cmd.A != 5 {
		t.Errorf("순서 보존 위반: A=%d,%d", d.Entries[0].Cmd.A, d.Entries[1].Cmd.A)
	}
	if got := d.Entries[1].Cmd.V; got != float32(quantizeV(0.3))/engine.ParamSteps {
		t.Errorf("마지막 값이 아니라 %v", got)
	}
	for _, e := range d.Entries {
		if e.Block != 44 { // 스텝1 시작 블록 ceil(43.269)
			t.Errorf("디코드 블록 %d, want 44", e.Block)
		}
	}
}

func TestEncodeURLBudgetLevels(t *testing.T) {
	// 130BPM에서 스텝 0..7에 파라미터 7을 한 번씩(스텝당 1개 → 감량 0단계에서는 전부 생존).
	l := &Log{}
	for _, b := range []uint32{0, 44, 87, 131, 174, 217, 261, 304} {
		l.Append(b, Human, engine.Cmd{Kind: engine.SetParam, A: 7, V: float32(b) / 1000})
	}
	lens := make([]int, 4)
	for level := 0; level <= 3; level++ {
		lens[level] = len(encodePayload(l, level))
	}
	if !(lens[0] > lens[1] && lens[1] > lens[2] && lens[2] > lens[3]) {
		t.Fatalf("감량 단계별 길이 단조 감소 아님: %v", lens)
	}
	if url, lvl := EncodeURLBudget(l, 1<<30); lvl != 0 || url != encodePayload(l, 0) {
		t.Errorf("여유 예산: 단계 %d, want 0", lvl)
	}
	url, lvl := EncodeURLBudget(l, lens[1]) // 1단계 결과 이내 → 1단계
	if lvl != 1 || len(url) > lens[1] {
		t.Errorf("예산 %d: 단계 %d 길이 %d, want 1/%d", lens[1], lvl, len(url), lens[1])
	}
	d, err := DecodeURL(url)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Entries) != 4 { // 2스텝당 1개 → 스텝 {0,1}{2,3}{4,5}{6,7} 그룹 4개
		t.Errorf("1단계 감량 후 엔트리 %d개, want 4", len(d.Entries))
	}
	if url, lvl := EncodeURLBudget(l, 1); lvl != maxBudgetLevel || len(url) != len(encodePayload(l, maxBudgetLevel)) {
		t.Errorf("불가능 예산: 단계 %d, want %d(상한) 및 길이 일치", lvl, maxBudgetLevel)
	}
	// 상한 단계(4096스텝당)에서는 8스텝짜리 로그가 파라미터별 값 1개로 줄어든다.
	if got := len(encodePayload(l, maxBudgetLevel)); got >= lens[0] {
		t.Errorf("상한 감량이 오히려 길어짐: %d vs %d", got, lens[0])
	}
}

func TestDecodeTruncatedStream(t *testing.T) {
	l := &Log{Seed: 7, Word: "ab"}
	for _, b := range []uint32{0, 200, 400} {
		l.Append(b, Human, engine.Cmd{Kind: engine.Mute, A: 1, B: 1})
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(EncodeURL(l), "v2."))
	if err != nil {
		t.Fatal(err)
	}
	cut := "v2." + base64.RawURLEncoding.EncodeToString(raw[:len(raw)-1]) // 마지막 바이트만 잘림
	d, err := DecodeURL(cut)
	if err == nil {
		t.Fatal("잘린 스트림인데 error 없음")
	}
	if d == nil || len(d.Entries) != 2 {
		t.Errorf("부분 결과: nil=%v 엔트리 %d개, want 2개(읽은 것까지 유효)", d == nil, entryCount(d))
	}
	if d != nil && (d.Seed != 7 || d.Word != "ab") {
		t.Errorf("부분 로그 헤더: seed %d word %q", d.Seed, d.Word)
	}
}

func entryCount(l *Log) int {
	if l == nil {
		return -1
	}
	return len(l.Entries)
}

func TestDecodeUnknownKindSkips(t *testing.T) {
	// 수제 페이로드: 헤더 + 알 수 없는 Kind(0x77) 1개 + 정상 BassStep 1개(델타 0).
	payload := []byte{2}               // 버전
	payload = append(payload, 7)       // seed varint
	payload = append(payload, 0)       // 단어 길이 0
	payload = append(payload, 0, 0x77) // step 델타 0 + 알 수 없는 Kind
	payload = append(payload, 0, 1)    // 델타 0 + Kind BassStep
	payload = append(payload, 1, 7, 24, engine.StepGate)
	d, err := DecodeURL("v2." + base64.RawURLEncoding.EncodeToString(payload))
	if err != nil {
		t.Fatalf("알 수 없는 Kind 건너뛰기 실패: %v", err)
	}
	if len(d.Entries) != 1 || d.Entries[0].Cmd.Kind != engine.BassStep {
		t.Errorf("엔트리 %d개, want BassStep 1개", len(d.Entries))
	}
}

func TestDecodeRejectsBadSchema(t *testing.T) {
	l := sampleLog()
	good, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(EncodeURL(l), "v2."))

	if d, err := DecodeURL("v3." + base64.RawURLEncoding.EncodeToString(good)); err == nil || d != nil {
		t.Errorf("v3 접두사 거부 안 됨: %v", err)
	}
	bad := append([]byte(nil), good...)
	bad[0] = 1 // 버전 바이트 불일치(v1 페이로드)
	if d, err := DecodeURL("v2." + base64.RawURLEncoding.EncodeToString(bad)); err == nil || d != nil {
		t.Errorf("버전 1 페이로드 거부 안 됨: %v", err)
	}
	if d, err := DecodeURL("v2.!!!"); err == nil || d != nil {
		t.Error("잘못된 base64 거부 안 됨")
	}
	if d, err := DecodeURL(""); err == nil || d != nil {
		t.Error("빈 입력 거부 안 됨")
	}
	if d, err := DecodeURL("v2."); err == nil || d != nil {
		t.Error("빈 페이로드 거부 안 됨")
	}
}

func TestDecodeHostileVarint(t *testing.T) {
	// 정당하지만 거대한 step 델타(2^35) — 시계 전진 루프를 끊고 오류로 거부해야 한다.
	payload := []byte{2, 7, 0}
	payload = append(payload, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01) // 델타 2^35
	payload = append(payload, 1, 1, 1)                            // Kind + 필드
	d, err := DecodeURL("v2." + base64.RawURLEncoding.EncodeToString(payload))
	if err == nil {
		t.Fatal("거대 델타 거부 안 됨")
	}
	if d == nil {
		t.Fatal("부분 로그도 없음(엔트리 없이라도 헤더는 유효)")
	}
}

func TestNilLogEncodes(t *testing.T) {
	url := EncodeURL(nil)
	d, err := DecodeURL(url)
	if err != nil {
		t.Fatalf("nil 로그 URL 디코드: %v", err)
	}
	if len(d.Entries) != 0 || len(d.Keyframes) != 0 {
		t.Errorf("nil 로그 URL이 비어 있지 않음: %d/%d", len(d.Entries), len(d.Keyframes))
	}
}

// ---- 재생 ----

func feedHash(h interface{ Write([]byte) (int, error) }, buf []float32) {
	var b4 [4]byte
	for _, s := range buf {
		binary.LittleEndian.PutUint32(b4[:], math.Float32bits(s))
		h.Write(b4[:])
	}
}

func TestReplayDeterministic(t *testing.T) {
	l := sampleLog()
	l.Append(100, Human, engine.Cmd{Kind: engine.SetParam, A: 0, V: 0.9})

	hashes := make([]string, 2)
	for i := range hashes {
		var e = engine.New(l.Seed)
		h := sha256.New()
		Replay(e, l, 300, func(_ uint32, buf []float32) { feedHash(h, buf) })
		hashes[i] = fmt.Sprintf("%x", h.Sum(nil))
	}
	if hashes[0] != hashes[1] {
		t.Errorf("두 엔진 재생 해시 불일치: %s vs %s", hashes[0], hashes[1])
	}
}

func TestReplayMatchesManual(t *testing.T) {
	l := sampleLog()
	l.Append(100, Human, engine.Cmd{Kind: engine.SetParam, A: 0, V: 0.9})

	h := sha256.New()
	Replay(engine.New(l.Seed), l, 300, func(_ uint32, buf []float32) { feedHash(h, buf) })

	// 수동 루프(동일 의미): ResetPos → 블록마다 Apply 후 Render.
	e := engine.New(l.Seed)
	e.Apply(engine.Cmd{Kind: engine.ResetPos})
	buf := make([]float32, 2*engine.Block)
	h2 := sha256.New()
	i := 0
	for b := uint32(0); b < 300; b++ {
		for i < len(l.Entries) && l.Entries[i].Block <= b {
			e.Apply(l.Entries[i].Cmd)
			i++
		}
		e.Render(buf)
		feedHash(h2, buf)
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != fmt.Sprintf("%x", h2.Sum(nil)) {
		t.Error("Replay와 수동 루프 결과 불일치(적용 시점 오류 가능)")
	}
}

func TestReplayStepsAppliesAtStepBoundaries(t *testing.T) {
	// 파라미터 5(기본 0.4)를 블록 200(130BPM 스텝4, 시작 블록 173)에 0.9로.
	l := &Log{}
	l.Append(200, Human, engine.Cmd{Kind: engine.SetParam, A: 5, V: 0.9})
	d, err := DecodeURL(EncodeURL(l))
	if err != nil {
		t.Fatal(err)
	}
	if d.Entries[0].Block != 174 {
		t.Fatalf("스텝4 시작 블록 %d, want 174", d.Entries[0].Block)
	}

	e := engine.New(1)
	ReplaySteps(e, d.Entries, 2, nil) // 스텝 0..1만 — 엔트리(스텝4) 미적용
	if got := e.Param(5); got == d.Entries[0].Cmd.V {
		t.Errorf("스텝 2에서 스텝 4 엔트리가 적용됨: %v", got)
	}
	e = engine.New(1)
	ReplaySteps(e, d.Entries, 8, nil) // 스텝 0..7 — 스텝 4 지나 적용됨
	if got := e.Param(5); got != d.Entries[0].Cmd.V {
		t.Errorf("스텝 경계 적용 안 됨: param=%v, want %v", got, d.Entries[0].Cmd.V)
	}

	// 두 엔진 결정론.
	hashes := make([]string, 2)
	for i := range hashes {
		en := engine.New(1)
		h := sha256.New()
		ReplaySteps(en, d.Entries, 16, func(_ uint32, buf []float32) { feedHash(h, buf) })
		hashes[i] = fmt.Sprintf("%x", h.Sum(nil))
	}
	if hashes[0] != hashes[1] {
		t.Errorf("ReplaySteps 두 엔진 해시 불일치: %s vs %s", hashes[0], hashes[1])
	}
}

// ---- cmd/jdsess 산출물 게이트 ----

func TestJdsessPrintURLDecodes(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(thisFile))
	cmd := exec.Command("go", "run", "./cmd/jdsess", "-print")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./cmd/jdsess -print: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		t.Fatalf("-print 출력 형식(2줄 기대): %q", string(out))
	}
	url := strings.TrimSpace(lines[0])
	var enc, raw int
	if _, err := fmt.Sscanf(strings.TrimSpace(lines[1]), "entries=%d raw=%d", &enc, &raw); err != nil {
		t.Fatalf("entries 라인 파싱: %v (%q)", err, lines[1])
	}
	// §12.5 추가분 포함: 노브 200×20 + 스텝 100 + 뮤트 20 + 드롭 3 + 코드 8×3사이클 + 모드 12 + 트랜스포트 4 + 키 1.
	if want := 200*20 + 100 + 20 + 3 + 3*engine.ChordBars + 12 + 4 + 1; raw != want {
		t.Errorf("합성 엔트리 raw=%d, want %d", raw, want)
	}
	l, err := DecodeURL(url)
	if err != nil {
		t.Fatalf("jdsess URL 디코드: %v", err)
	}
	if len(l.Entries) != enc {
		t.Errorf("URL 엔트리 %d개, jdsess 보고 %d개 — 불일치", len(l.Entries), enc)
	}
	if url2 := EncodeURL(l); url2 != url {
		t.Errorf("jdsess URL 고정점 위반(재인코딩 %d자 vs %d자)", len(url2), len(url))
	}
}
