// layout.go — 레이아웃 JSON 로더. 좌표의 단일 소유자는 JSON이다(Go에 픽셀 상수 금지).
//
// 기기 뷰: app/assets/device/layout.json — tools/rack/wire.py가 만든 것(720×1280 논리 좌표).
// 방 뷰:   app/assets/room/layout.json — 아래 RoomLayout 스키마(비전 라운드가 플레이트를 보고 기록).
package core

import (
	"encoding/json"

	"github.com/midagedev/jangdan/engine"
)

// DeviceLayout — tools/rack/wire.py 출력 + sprites 표.
type DeviceLayout struct {
	Size     [2]float64 `json:"size"`
	Knobs    []Knob     `json:"knobs"`
	Buttons  []Button   `json:"buttons"`
	Pads     []Named    `json:"pads"`
	Plates   []For      `json:"plates"`
	Panels   []Named    `json:"panels"`
	LEDs     []LED      `json:"leds"`
	Displays []For      `json:"displays"`
	Scope    struct {
		Rect Rect `json:"rect"`
	} `json:"scope"`
	Display struct {
		Rect Rect `json:"rect"`
	} `json:"display"`
	// ChordTrack — 코드 트랙 띠(8마디 셀, 앱이 그린다; §12). 헤더 아래 어두운 빈 판(실측 std ≈ 7~12).
	ChordTrack struct {
		Rect Rect `json:"rect"`
	} `json:"chord_track"`
	Sprites map[string]Sprite `json:"sprites"`
}

type Knob struct {
	Name    string
	Section string
	CX, CY  float64
	R       float64
}

// UnmarshalJSON — cx/cy 두 필드(태그 하나로 못 받아 수동).
func (k *Knob) UnmarshalJSON(b []byte) error {
	var raw struct {
		Name    string  `json:"name"`
		Section string  `json:"section"`
		CX      float64 `json:"cx"`
		CY      float64 `json:"cy"`
		R       float64 `json:"r"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*k = Knob{Name: raw.Name, Section: raw.Section, CX: raw.CX, CY: raw.CY, R: raw.R}
	return nil
}

type Button struct {
	Name    string `json:"name"`
	Section string `json:"section"`
	Rect    Rect   `json:"rect"`
}

type Named struct {
	Name string `json:"name"`
	Rect Rect   `json:"rect"`
}

type For struct {
	For  string `json:"for"`
	Rect Rect   `json:"rect"`
}

type LED struct {
	CX float64 `json:"cx"`
	CY float64 `json:"cy"`
	R  float64 `json:"r"`
}

type Sprite struct {
	Size [2]int `json:"size"`
	R    int    `json:"r"`
}

// RoomLayout — 방 뷰 영역. 좌표는 플레이트 픽셀(720×1280). 각 영역은 사물↔신호 표(기획서 05절)의 사물 하나.
type RoomLayout struct {
	Size       [2]float64  `json:"size"`
	Plates     RoomPlates  `json:"plates"`
	Lamp       RoomLamp    `json:"lamp"`
	Skylight   Rect        `json:"skylight"` // 비가 내리는 창 영역(셰이더 클립)
	Windows    []Rect      `json:"windows"`  // 창밖 마을 불빛(각각 켜고 끄는 작은 창)
	Mug        RoomMug     `json:"mug"`
	Cat        RoomActor   `json:"cat"`
	Character  RoomActor   `json:"character"`
	Device     Rect        `json:"device"`      // 책상 위 기기(탭하면 기기 뷰)
	DeviceLEDs []LED       `json:"device_leds"` // 방 뷰에서도 읽히는 16스텝 LED 위치(없으면 device rect 하단에 자동 배치)
	Scope      Rect        `json:"scope"`       // 기기의 작은 스코프(파형)
	Radiator   Rect        `json:"radiator"`
	Records    []Rect      `json:"records"`   // 벽에 기댄 레코드(순서 교체 연출)
	SeedText   Rect        `json:"seed_text"` // 시드 단어를 새기는 자리
	Palette    RoomPalette `json:"palette"`
}

type RoomPlates struct {
	Night     string `json:"night"` // 파일명(app/assets/room/ 아래)
	Evening   string `json:"evening"`
	Afternoon string `json:"afternoon"`
}

type RoomLamp struct {
	Bulb   [2]float64 `json:"bulb"`   // 광원 중심
	Radius float64    `json:"radius"` // 빛 풀 반경(호흡 셰이더)
	Cone   Rect       `json:"cone"`   // 빛이 닿는 책상 영역
}

type RoomMug struct {
	Rect  Rect       `json:"rect"`
	Steam [2]float64 `json:"steam"` // 김이 오르는 시작점
}

type RoomActor struct {
	Rect   Rect       `json:"rect"`   // 스프라이트가 놓이는 영역(플레이트에서 잘라낸 크기)
	Poses  []string   `json:"poses"`  // 포즈 스프라이트 파일명(app/assets/room/ 아래), 0번 = 기본
	Anchor [2]float64 `json:"anchor"` // 회전·흔들림 기준점(플레이트 좌표)
}

type RoomPalette struct {
	LampWarm  string `json:"lamp_warm"` // hex
	Ink       string `json:"ink"`
	Rain      string `json:"rain"`
	WindowLit string `json:"window_lit"`
}

func LoadDeviceLayout(b []byte) (*DeviceLayout, error) {
	var l DeviceLayout
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// RearLayout — 랙 뒷면(§14.3, tools/rack/rear.py 산출). 앞면과 같은 좌표계(720×2000)라
// 스크롤 규칙이 하나다. 잭 좌표의 단일 소유자는 이 JSON이고, 포트 수는 engine kindPorts의
// 전사다(게이트 TestRearLayoutPorts가 두 표의 일치를 잰다).
type RearLayout struct {
	Size    [2]float64   `json:"size"`
	Panels  []Named      `json:"panels"`
	Plates  []For        `json:"plates"`
	Devices []RearDevice `json:"devices"`
}

// RearDevice — 뒷면 행 하나 = 장치 하나. Slot은 기본 랙의 슬롯 번호이고, 그 슬롯에 다른
// 종류가 놓이면 뷰는 실제 포트 수까지만 그린다(레이아웃이 상한).
type RearDevice struct {
	Slot  int    `json:"slot"`
	Name  string `json:"name"`
	Rect  Rect   `json:"rect"`
	Plate Rect   `json:"plate"`
	In    []Jack `json:"in"`
	Out   []Jack `json:"out"`
}

// Jack — 잭 하나. Name은 앱이 폰트로 올리는 포트 라벨(engine/rack.go 포트 주석 전사).
type Jack struct {
	Name   string  `json:"name"`
	Port   int     `json:"port"`
	CX, CY float64 `json:"-"`
	R      float64 `json:"r"`
}

// UnmarshalJSON — cx/cy 두 필드(Knob과 같은 사정).
func (j *Jack) UnmarshalJSON(b []byte) error {
	var raw struct {
		Name string  `json:"name"`
		Port int     `json:"port"`
		CX   float64 `json:"cx"`
		CY   float64 `json:"cy"`
		R    float64 `json:"r"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*j = Jack{Name: raw.Name, Port: raw.Port, CX: raw.CX, CY: raw.CY, R: raw.R}
	return nil
}

func LoadRearLayout(b []byte) (*RearLayout, error) {
	var l RearLayout
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// RearDeviceAt — 슬롯의 뒷면 행. 없으면 nil.
func (l *RearLayout) RearDeviceAt(slot int) *RearDevice {
	for i := range l.Devices {
		if l.Devices[i].Slot == slot {
			return &l.Devices[i]
		}
	}
	return nil
}

func LoadRoomLayout(b []byte) (*RoomLayout, error) {
	var l RoomLayout
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// polyKnob — 섹션 "poly" 노브 이름 → 장치 로컬 파라미터 k(engine/poly.go 표).
var polyKnob = map[string]int{
	"CUTOFF": engine.PolyCutoff, "RESO": engine.PolyReso, "ENV": engine.PolyEnvMod, "ATTACK": engine.PolyAttack,
	"DECAY": engine.PolyDecay, "RELEASE": engine.PolyRelease, "DETUNE": engine.PolyDetune, "LEVEL": engine.PolyLevel,
}

// KnobDevParam — 장치 로컬 파라미터 노브(§14.1 DeviceParam): 섹션·이름 → (슬롯, k). 지금은 섹션
// "poly"(슬롯 engine.SlotPoly)만. 알 수 없으면 false. KnobParam과 함께 UI↔엔진 매핑의 단일 소유자 —
// 한 노브는 둘 중 하나에만 속한다(둘 다 false면 New가 레이아웃 오류로 거부한다).
func KnobDevParam(section, name string) (slot, k int, ok bool) {
	if section != "poly" {
		return 0, 0, false
	}
	k, ok = polyKnob[name]
	if !ok {
		return 0, 0, false
	}
	return engine.SlotPoly, k, true
}

// DevParamDefault — 장치 로컬 파라미터 기본값(섀도 미러가 아직 없을 때 노브 표시 폴백). 슬롯 종류를
// 모르므로 폴리 슬롯만 안다(SlotPoly 외 0).
func DevParamDefault(slot, k int) float32 {
	if slot == engine.SlotPoly && k >= 0 && k < engine.PolyParams {
		return engine.DefaultPolyParams()[k]
	}
	return 0
}

// KnobParam — 기기 레이아웃의 노브 이름·섹션 → 엔진 ParamID. 알 수 없으면 false.
// 이 표가 UI↔엔진 매핑의 단일 소유자다(view/device는 이 함수를 쓴다).
func KnobParam(section, name string) (engine.ParamID, bool) {
	var base engine.ParamID
	switch section {
	case "basslineA":
		base = engine.BassAParams
	case "basslineB":
		base = engine.BassBParams
	case "drums": // "BD_LEVEL" / "BD_TUNE" — 접미사 화이트리스트(오타 이름이 LEVEL로 묵히는 결함 차단)
		if len(name) < 4 {
			return 0, false
		}
		v, ok := drumIndex[name[:2]]
		if !ok {
			return 0, false
		}
		id := engine.DrumParams + engine.ParamID(2*v)
		switch name[3:] {
		case "LEVEL":
		case "TUNE":
			id++
		default:
			return 0, false
		}
		return id, true
	case "fx":
		switch name {
		case "DELAY":
			return engine.Delay, true
		case "DRIVE":
			return engine.Drive, true
		case "COMP":
			return engine.Comp, true
		case "MASTER":
			return engine.Master, true
		case "TEMPO":
			return engine.Tempo, true
		}
		return 0, false
	case "mixer": // §13.1 — REV_<채널> 8 + LEVEL_A/B + CHO_A/B
		if id, ok := mixerRevSend[name]; ok {
			return id, true
		}
		switch name {
		case "LEVEL_A":
			return engine.BassALevel, true
		case "LEVEL_B":
			return engine.BassBLevel, true
		case "CHO_A":
			return engine.ChoSendA, true
		case "CHO_B":
			return engine.ChoSendB, true
		}
		return 0, false
	case "fx2": // §13.1 — 리버브·코러스 파라미터 6
		switch name {
		case "REV_SIZE":
			return engine.RevSize, true
		case "REV_DAMP":
			return engine.RevDamp, true
		case "REV_MIX":
			return engine.RevMix, true
		case "CHO_RATE":
			return engine.ChoRate, true
		case "CHO_DEPTH":
			return engine.ChoDepth, true
		case "CHO_MIX":
			return engine.ChoMix, true
		}
		return 0, false
	default:
		return 0, false
	}
	off, ok := bassOffset[name]
	if !ok {
		return 0, false
	}
	return base + off, true
}

var bassOffset = map[string]engine.ParamID{"TUNE": engine.BTune, "CUTOFF": engine.BCutoff, "RESO": engine.BReso, "ENV": engine.BEnvMod, "DECAY": engine.BDecay, "ACCENT": engine.BAccent}
var drumIndex = map[string]int{"BD": 0, "SD": 1, "CH": 2, "OH": 3, "CP": 4, "CY": 5}

// mixerRevSend — 믹서 REV 센드 노브 이름 → RevSend(part)(§13.1 35..42). 드럼 채널은
// drumIndex와 같은 보이스 순서다. 구성 시 1회 읽는 표라 핫 루프 무할당 규칙 밖이다.
var mixerRevSend = map[string]engine.ParamID{
	"REV_A":  engine.RevSend(engine.BassA),
	"REV_B":  engine.RevSend(engine.BassB),
	"REV_BD": engine.RevSend(engine.BD),
	"REV_SD": engine.RevSend(engine.SD),
	"REV_CH": engine.RevSend(engine.CH),
	"REV_OH": engine.RevSend(engine.OH),
	"REV_CP": engine.RevSend(engine.CP),
	"REV_CY": engine.RevSend(engine.CY),
}

// PadPart — 패드 이름 → 엔진 Part(2..7). 알 수 없으면 false.
func PadPart(name string) (engine.Part, bool) {
	v, ok := drumIndex[name]
	if !ok {
		return 0, false
	}
	return engine.BD + engine.Part(v), true
}
