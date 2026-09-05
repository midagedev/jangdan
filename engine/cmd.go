// cmd.go — 파트와 명령(Cmd). 리드 소유(docs/impl-plan-2026-09-05.md §2.2가 원본).
//
// 엔진에 닿는 모든 변경은 Cmd 하나로 통일된다 — 사람 손·레지던트·리플레이가 같은 경로를
// 쓰므로 로그가 정본이 된다. Apply는 무할당이고 범위 밖 입력은 정규화한다(무시가 아니라
// 클램프·마스킹; 파트 범위 밖만 무동작). 이 파일에는 곱셈-덧셈이 없다.
package engine

type Part uint8

const (
	BassA Part = iota
	BassB
	BD
	SD
	CH
	OH
	CP
	CY
	NumParts
)

type CmdKind uint8

const (
	SetParam      CmdKind = iota // A=ParamID                        V=값(0..1)
	BassStep                     // A=Part(0|1) B=step C=note(0..36) D=flags(StepGate|StepSlide|StepAccent)
	DrumStep                     // A=Part(2..7) B=step               D=flags(StepGate|StepAccent)
	SelectPattern                // A=Part(0|1) B=slot(0..7)         — 다음 바 경계에 적용
	Mute                         // A=Part B=1 뮤트 / 0 해제
	Trigger                      // A=Part(2..7)                     — 패드 즉시 원샷
	Drop                         //                                  — 다음 바 경계에 발동
	ResetPos                     //                                  — 스텝 0·바 0(리플레이 시작)
	NumCmdKinds
)

// 스텝 플래그(BassStep.D / DrumStep.D).
const (
	StepGate   uint8 = 1 << 0
	StepSlide  uint8 = 1 << 1
	StepAccent uint8 = 1 << 2
)

// 패턴 상수.
const (
	Steps       = 16
	PatternSlots = 8
	MaxNote     = 36 // C1..C4 (note 24 = C3 기준음)
)

// Cmd — 12바이트 고정. 직렬화(session)는 이 필드 순서를 그대로 쓴다.
type Cmd struct {
	Kind       CmdKind
	A, B, C, D uint8
	V          float32
}

// Flags() 비트(직전 Render 블록에서 일어난 일).
const (
	FlagBar    uint32 = 1 << 8  // 바 경계(스텝 0 진입)
	FlagDrop   uint32 = 1 << 9  // 드롭 발동
	FlagAccent uint32 = 1 << 10 // 액센트 노트 트리거
	// bit 0..7 = 해당 Part 트리거
)
