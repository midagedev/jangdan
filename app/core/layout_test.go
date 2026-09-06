// layout_test.go — KnobParam 전체 매핑 표 단언(P4-scroll — 스펙 ⑤). 레이아웃 JSON의
// 노브 47개 전부가 기대 ParamID와 정확히 일치하고, 매핑끼리 겹치지 않으며(서로 다른 두
// 노브가 같은 파라미터를 움직이면 믹서 노브를 돌려도 다른 노브가 함께 움직인다),
// 알 수 없는 이름은 (0,false)로 거짓을 돌려준다.
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
	// 레이아웃 JSON의 노브 전부가 표와 정확히 일치(JSON이 바뀌면 여기가 먼저 빨간다).
	l, err := LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		t.Fatalf("레이아웃 파싱: %v", err)
	}
	if len(l.Knobs) != len(wantKnob) {
		t.Fatalf("레이아웃 노브 %d개(표 %d항)", len(l.Knobs), len(wantKnob))
	}
	for _, k := range l.Knobs {
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
