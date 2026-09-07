// layout_test.go — KnobParam 전체 매핑 표 단언(P4-scroll — 스펙 ⑤). 레이아웃 JSON의
// 전역 노브 47개 전부가 기대 ParamID와 정확히 일치하고, 매핑끼리 겹치지 않으며(서로 다른 두
// 노브가 같은 파라미터를 움직이면 믹서 노브를 돌려도 다른 노브가 함께 움직인다),
// 알 수 없는 이름은 (0,false)로 거짓을 돌려준다.
// P5-poly: 레이아웃 v4에 장치 로컬 poly 노브 8종, P5-sampler: v5에 sampler 8종이 추가됐다
// (총 63) — KnobDevParam 표는 TestKnobDevParamTable·TestDevParamDefault 소관이고, 여기선
// 두 표의 교집합 없음만 단언한다.
package core

import (
	"testing"

	"github.com/midagedev/jangdan/app/assets"
	"github.com/midagedev/jangdan/engine"
)

// wantKnob — 노브 47개(§13.1 파라미터 전부)의 기대 매핑. 표 자체가 계약 문서다.
var wantKnob = map[[2]string]engine.ParamID{
	// 베이스라인 A(0..7)·B(8..15)
	{"basslineA", "TUNE"}:   engine.BassAParams + engine.BTune,
	{"basslineA", "CUTOFF"}: engine.BassAParams + engine.BCutoff,
	{"basslineA", "RESO"}:   engine.BassAParams + engine.BReso,
	{"basslineA", "ENV"}:    engine.BassAParams + engine.BEnvMod,
	{"basslineA", "DECAY"}:  engine.BassAParams + engine.BDecay,
	{"basslineA", "ACCENT"}: engine.BassAParams + engine.BAccent,
	{"basslineB", "TUNE"}:   engine.BassBParams + engine.BTune,
	{"basslineB", "CUTOFF"}: engine.CutoffB,
	{"basslineB", "RESO"}:   engine.BassBParams + engine.BReso,
	{"basslineB", "ENV"}:    engine.BassBParams + engine.BEnvMod,
	{"basslineB", "DECAY"}:  engine.BassBParams + engine.BDecay,
	{"basslineB", "ACCENT"}: engine.BassBParams + engine.BAccent,
	// 드럼 보이스 v: Level = DrumParams+2v, Tune = +1(16..27)
	{"drums", "BD_LEVEL"}: engine.BDLevel,
	{"drums", "BD_TUNE"}:  engine.BDTune,
	{"drums", "SD_LEVEL"}: engine.SDLevel,
	{"drums", "SD_TUNE"}:  engine.DrumParams + 3,
	{"drums", "CH_LEVEL"}: engine.CHLevel,
	{"drums", "CH_TUNE"}:  engine.DrumParams + 5,
	{"drums", "OH_LEVEL"}: engine.OHLevel,
	{"drums", "OH_TUNE"}:  engine.DrumParams + 7,
	{"drums", "CP_LEVEL"}: engine.CPLevel,
	{"drums", "CP_TUNE"}:  engine.DrumParams + 9,
	{"drums", "CY_LEVEL"}: engine.CYLevel,
	{"drums", "CY_TUNE"}:  engine.DrumParams + 11,
	// fx(28..32)
	{"fx", "DELAY"}:  engine.Delay,
	{"fx", "DRIVE"}:  engine.Drive,
	{"fx", "COMP"}:   engine.Comp,
	{"fx", "MASTER"}: engine.Master,
	{"fx", "TEMPO"}:  engine.Tempo,
	// 믹서(§13.1 33..50 중 채널 스트립): REV 센드 8 = RevSend(part), 레벨 2, 코러스 센드 2
	{"mixer", "REV_A"}:   engine.RevSend(engine.BassA),
	{"mixer", "REV_B"}:   engine.RevSend(engine.BassB),
	{"mixer", "REV_BD"}:  engine.RevSend(engine.BD),
	{"mixer", "REV_SD"}:  engine.RevSend(engine.SD),
	{"mixer", "REV_CH"}:  engine.RevSend(engine.CH),
	{"mixer", "REV_OH"}:  engine.RevSend(engine.OH),
	{"mixer", "REV_CP"}:  engine.RevSend(engine.CP),
	{"mixer", "REV_CY"}:  engine.RevSend(engine.CY),
	{"mixer", "LEVEL_A"}: engine.BassALevel,
	{"mixer", "LEVEL_B"}: engine.BassBLevel,
	{"mixer", "CHO_A"}:   engine.ChoSendA,
	{"mixer", "CHO_B"}:   engine.ChoSendB,
	// fx2(§13.1 43..50): 리버브 3 + 코러스 3
	{"fx2", "REV_SIZE"}:  engine.RevSize,
	{"fx2", "REV_DAMP"}:  engine.RevDamp,
	{"fx2", "REV_MIX"}:   engine.RevMix,
	{"fx2", "CHO_RATE"}:  engine.ChoRate,
	{"fx2", "CHO_DEPTH"}: engine.ChoDepth,
	{"fx2", "CHO_MIX"}:   engine.ChoMix,
}

func TestKnobParamFullTable(t *testing.T) {
	if len(wantKnob) != 47 {
		t.Fatalf("기대 표 %d항(47 예상 — 레이아웃 노브 수와 함께 갱신)", len(wantKnob))
	}
	// 매핑 충돌 없음(단사): 서로 다른 노브가 같은 ParamID를 가리키면 안 된다.
	seen := make(map[engine.ParamID][2]string, len(wantKnob))
	for key, id := range wantKnob {
		if id < 0 || id >= engine.NumParams {
			t.Fatalf("%v → ParamID %d(0..%d 범위 밖)", key, id, engine.NumParams-1)
		}
		if prev, dup := seen[id]; dup {
			t.Fatalf("ParamID %d 이중 매핑: %v ↔ %v", id, prev, key)
		}
		seen[id] = key
	}
	// 레이아웃 JSON의 전역 노브 전부가 표와 정확히 일치(JSON이 바뀌면 여기가 먼저 빨간다).
	// poly·sampler 8종씩은 장치 로컬 표 소관 — 전역 매핑이 아니라는 분리만 여기서 잰다.
	// 제외 목록은 하드코딩이다: 세 번째 장치 섹션이 오면 이 분기를 지나 "표에 없음"으로
	// 빨간다(제품 devSection을 그대로 쓰면 새 섹션이 자동으로 빠져 의도가 죽는다).
	localSection := map[string]bool{"poly": true, "sampler": true}
	l, err := LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	n := 0
	for _, k := range l.Knobs {
		if localSection[k.Section] {
			if _, ok := KnobParam(k.Section, k.Name); ok {
				t.Fatalf("KnobParam이 %s 노브 %q를 전역에 매핑(단일 소유자 위반)", k.Section, k.Name)
			}
			continue
		}
		n++
		key := [2]string{k.Section, k.Name}
		want, ok := wantKnob[key]
		if !ok {
			t.Fatalf("노브 %q/%q가 기대 표에 없음 — 표 갱신 필요", k.Section, k.Name)
		}
		got, ok := KnobParam(k.Section, k.Name)
		if !ok || got != want {
			t.Fatalf("KnobParam(%q,%q) = (%d,%v)(%d,true 예상)", k.Section, k.Name, got, ok, want)
		}
	}
	if n != len(wantKnob) {
		t.Fatalf("전역 노브 %d개(표 %d항 — poly·sampler 제외 레이아웃 전부)", n, len(wantKnob))
	}
}

func TestKnobParamUnknown(t *testing.T) {
	for _, c := range [][2]string{
		{"mixer", "REV_X"}, {"mixer", "LEVEL_C"}, {"mixer", "REV_"}, {"mixer", "TUNE"},
		{"fx2", "REV_X"}, {"fx2", "CHO_X"}, {"fx2", "DELAY"}, {"fx2", "REV_SIZ"},
		{"fx", "REVERB"}, {"fx", "TEMPO2"},
		{"drums", "XX_LEVEL"}, {"drums", "BD"}, {"drums", "BD_SLIDE"},
		{"basslineA", "RESONANCE"}, {"basslineB", "REV_A"},
		{"noise", "TUNE"}, {"", "TUNE"},
	} {
		if id, ok := KnobParam(c[0], c[1]); ok {
			t.Fatalf("KnobParam(%q,%q) = (%d,true)((0,false) 예상)", c[0], c[1], id)
		}
	}
	// RevSend 산술(§13.1): 베이스 A가 첫 센드, CY가 마지막 — 매핑 표의 근거 재확인.
	if engine.RevSend(engine.BassA) != engine.RevSendBase || engine.RevSend(engine.CY) != engine.RevSendBase+7 {
		t.Fatalf("RevSend 범위 = %d..%d(%d 기대)", engine.RevSend(engine.BassA), engine.RevSend(engine.CY), engine.RevSendBase)
	}
}

// wantDevSection — 장치 로컬 파라미터 섹션의 기대 매핑(P5-poly "poly", P5-sampler
// "sampler" — §14.1): 섹션 → (슬롯, 파라미터 수, 노브 이름→k). 전역 47종과 합쳐 레이아웃
// v5의 63노브를 전부 덮는다(두 표의 교집합 없음 — 단일 소유자).
var wantDevSection = map[string]struct {
	slot   int
	params int
	knob   map[string]int
}{
	"poly": {engine.SlotPoly, engine.PolyParams, map[string]int{
		"CUTOFF": engine.PolyCutoff, "RESO": engine.PolyReso, "ENV": engine.PolyEnvMod, "ATTACK": engine.PolyAttack,
		"DECAY": engine.PolyDecay, "RELEASE": engine.PolyRelease, "DETUNE": engine.PolyDetune, "LEVEL": engine.PolyLevel,
	}},
	"sampler": {engine.SlotSampler, engine.SmpParams, map[string]int{
		"SELECT": engine.SmpSelect, "TUNE": engine.SmpTune, "START": engine.SmpStart, "LOOP": engine.SmpLoop,
		"ATTACK": engine.SmpAttack, "RELEASE": engine.SmpRelease, "TONE": engine.SmpTone, "LEVEL": engine.SmpLevel,
	}},
}

func TestKnobDevParamTable(t *testing.T) {
	if len(wantDevSection) != len(devSection) {
		t.Fatalf("기대 섹션 %d개(제품 레지스트리 %d개와 같아야 — 갱신 필요)", len(wantDevSection), len(devSection))
	}
	// 섹션별: k 중복 없음(두 노브가 같은 장치 파라미터를 움직이면 한 노브를 돌려도 다른 노브가
	// 함께 움직인다)·레지스트리와 정확히 일치·전역 표와 교집합 없음.
	for sec, want := range wantDevSection {
		if _, ok := devSection[sec]; !ok {
			t.Fatalf("기대 섹션 %q가 제품 레지스트리에 없음", sec)
		}
		if want.slot < 0 || want.slot >= engine.RackSlots {
			t.Fatalf("섹션 %q 슬롯 %d(0..%d 범위 밖)", sec, want.slot, engine.RackSlots-1)
		}
		seen := make(map[int]string, len(want.knob))
		for name, k := range want.knob {
			if k < 0 || k >= want.params {
				t.Fatalf("%s/%q → k %d(0..%d 범위 밖)", sec, name, k, want.params-1)
			}
			if prev, dup := seen[k]; dup {
				t.Fatalf("k %d 이중 매핑: %q ↔ %q", k, prev, name)
			}
			seen[k] = name
			slot, got, ok := KnobDevParam(sec, name)
			if !ok || slot != want.slot || got != k {
				t.Fatalf("KnobDevParam(%q,%q) = (%d,%d,%v)((%d,%d,true) 예상)", sec, name, slot, got, ok, want.slot, k)
			}
			// 전역 표와의 교집합 없음 — 한 노브는 두 표 중 정확히 하나에 속한다.
			if _, ok := KnobParam(sec, name); ok {
				t.Fatalf("%q가 전역 KnobParam에도 매핑(단일 소유자 위반)", name)
			}
		}
	}
	// 레이아웃의 장치 로컬 노브는 소속 섹션 표와 정확히 1:1(JSON이 바뀌면 여기가 먼저 빨간다).
	l, err := LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	got := map[string]int{}
	for _, k := range l.Knobs {
		want, ok := wantDevSection[k.Section]
		if !ok {
			continue
		}
		got[k.Section]++
		wk, ok := want.knob[k.Name]
		if !ok {
			t.Fatalf("%s 노브 %q가 기대 표에 없음 — 표 갱신 필요", k.Section, k.Name)
		}
		if _, g, ok := KnobDevParam(k.Section, k.Name); !ok || g != wk {
			t.Fatalf("KnobDevParam(%q,%q) k = (%d,%v)(%d,true 예상)", k.Section, k.Name, g, ok, wk)
		}
	}
	for sec, want := range wantDevSection {
		if got[sec] != len(want.knob) {
			t.Fatalf("레이아웃 %s 노브 %d개(표 %d항)", sec, got[sec], len(want.knob))
		}
	}
	// 모르는 이름·다른 섹션 → false(newView의 레이아웃 오류 경로). 전역 이름·대소문자 변주,
	// 섹션 간 이름 혼용(폴리 "ATTACK"을 샘플러에)도 매핑이 아니다.
	for _, c := range [][2]string{
		{"poly", "TEMPO"}, {"poly", "CUTOFF2"}, {"poly", ""}, {"poly", "cutoff"},
		{"sampler", "SELECT2"}, {"sampler", "CUTOFF"}, {"sampler", "select"},
		{"poly", "SELECT"}, {"sampler", "DETUNE"},
		{"fx", "CUTOFF"}, {"mixer", "LEVEL"}, {"basslineA", "ATTACK"}, {"", "CUTOFF"},
	} {
		if slot, k, ok := KnobDevParam(c[0], c[1]); ok {
			t.Fatalf("KnobDevParam(%q,%q) = (%d,%d,true)(false 예상)", c[0], c[1], slot, k)
		}
	}
}

func TestDevParamDefault(t *testing.T) {
	// 아는 슬롯: 엔진 기본값 표와 정확히 일치 — 표시 폴백(미러 부재)이 엔진 Reset과 같은 값을 준다.
	def := engine.DefaultPolyParams()
	for k := 0; k < engine.PolyParams; k++ {
		if got := DevParamDefault(engine.SlotPoly, k); got != def[k] {
			t.Fatalf("DevParamDefault(SlotPoly,%d) = %v(%v 예상)", k, got, def[k])
		}
	}
	sdef := engine.DefaultSamplerParams()
	for k := 0; k < engine.SmpParams; k++ {
		if got := DevParamDefault(engine.SlotSampler, k); got != sdef[k] {
			t.Fatalf("DevParamDefault(SlotSampler,%d) = %v(%v 예상)", k, got, sdef[k])
		}
	}
	// 범위 밖 방어: 다른 슬롯·k 경계 밖은 0 — knobValue의 NaN/음수 폴백이 변칙 값을 만들지 않는다.
	for _, c := range [][2]int{{engine.SlotBassA, 0}, {engine.SlotPoly, engine.PolyParams}, {engine.SlotPoly, -1}, {engine.SlotSampler, engine.SmpParams}, {engine.SlotSampler, -1}, {engine.SlotSampler + 1, 0}, {-1, 0}} {
		if got := DevParamDefault(c[0], c[1]); got != 0 {
			t.Fatalf("DevParamDefault(%d,%d) = %v(0 예상)", c[0], c[1], got)
		}
	}
}

// TestRearLayoutPorts — 뒷면 잭 표(§14.3)와 엔진 포트 표의 일치. 이 둘이 어긋나면 케이블이
// 없는 잭에 꽂히거나(연결 무동작) 있는 포트가 화면에서 사라진다 — 어느 쪽도 화면만 보고는
// 원인을 못 찾는다. 기본 랙 슬롯의 종류로 검사한다(rear.json은 기본 랙 기준 상한이다).
func TestRearLayoutPorts(t *testing.T) {
	l, err := LoadRearLayout(assets.DeviceRearJSON)
	if err != nil {
		t.Fatalf("rear.json 파싱: %v", err)
	}
	dl, err := LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("layout.json 파싱: %v", err)
	}
	if l.Size != dl.Size {
		t.Fatalf("뒷면 크기 %v ≠ 앞면 %v — 좌표계가 하나여야 스크롤 규칙이 하나다", l.Size, dl.Size)
	}
	e := engine.New(1)
	seen := map[int]bool{}
	for i := range l.Devices {
		d := &l.Devices[i]
		if seen[d.Slot] {
			t.Fatalf("슬롯 %d 행이 둘", d.Slot)
		}
		seen[d.Slot] = true
		nIn, nOut := engine.KindPorts(e.Kind(d.Slot))
		if len(d.In) != int(nIn) || len(d.Out) != int(nOut) {
			t.Errorf("슬롯 %d(%s) 잭 in %d out %d, 엔진 포트 in %d out %d",
				d.Slot, d.Name, len(d.In), len(d.Out), nIn, nOut)
		}
		for _, j := range append(append([]Jack{}, d.In...), d.Out...) {
			if j.R < 12 {
				t.Errorf("슬롯 %d 잭 %s 반지름 %v < 12(히트 28px 하한)", d.Slot, j.Name, j.R)
			}
			if !d.Rect.Contains(j.CX-j.R, j.CY-j.R) || !d.Rect.Contains(j.CX+j.R, j.CY+j.R) {
				t.Errorf("슬롯 %d 잭 %s(%v,%v)가 행 %v 밖", d.Slot, j.Name, j.CX, j.CY, d.Rect)
			}
		}
	}
	// 기본 랙의 장치가 전부 뒷면에 있어야 한다(케이블 양 끝이 그려질 자리가 있다는 뜻).
	for s := 0; s < engine.RackSlots; s++ {
		if e.Kind(s) != engine.KindNone && l.RearDeviceAt(s) == nil {
			t.Errorf("슬롯 %d(종류 %d)의 뒷면 행 없음", s, e.Kind(s))
		}
	}
	// 잭 히트 영역(r+4)이 서로 겹치지 않는다 — 겹치면 드래그가 엉뚱한 포트에 꽂힌다.
	type hit struct {
		x, y, r float64
		who     string
	}
	var hits []hit
	for i := range l.Devices {
		d := &l.Devices[i]
		for _, j := range append(append([]Jack{}, d.In...), d.Out...) {
			hits = append(hits, hit{j.CX, j.CY, j.R + 4, d.Name + "/" + j.Name})
		}
	}
	for i := 0; i < len(hits); i++ {
		for k := i + 1; k < len(hits); k++ {
			dx, dy := hits[i].x-hits[k].x, hits[i].y-hits[k].y
			if dx*dx+dy*dy < (hits[i].r+hits[k].r)*(hits[i].r+hits[k].r) {
				t.Errorf("잭 히트 겹침: %s ↔ %s", hits[i].who, hits[k].who)
			}
		}
	}
}
