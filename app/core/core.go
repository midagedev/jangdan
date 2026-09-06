// Package core — 앱(Ebitengine) 공용 층. 리드 소유. 계약 원본: docs/impl-plan-2026-09-05.md §4·§7.
//
// 뷰(view/device, view/room)는 이 패키지만 의존한다: 브리지(엔진·호스트), 틱 스냅샷(엔진 신호),
// 명령 싱크(저자 포함), 레이아웃 로더, 텔레메트리. syscall/js는 bridge_js.go에만 있다.
// 논리 좌표계는 720×1280(세로), 백킹은 DPR에 따른다(Ebitengine Layout이 논리 크기를 돌려준다).
package core

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/jangdan/engine"
)

const (
	LogicalW = 720
	LogicalH = 1280
)

// Author — 명령의 저자(session.Author와 같은 값; 호스트 로그가 기록한다).
type Author uint8

const (
	Human Author = iota
	Resident
	Replay
	System
)

// Tick — 호스트가 워클릿에서 받아 둔 최신 엔진 스냅샷(§4 tick 메시지). 오디오가 시작 전이면 Started=false.
type Tick struct {
	Started bool
	Block   float64 // 워클릿이 렌더한 블록 수(추정 현재)
	Step    int
	Bar     uint32
	Flags   uint32 // engine.Flag*, 직전 틱 이후 누적 OR
	Peak    float32
	CtxTime float64 // AudioContext.currentTime(초)
	Playing bool    // 트랜스포트(engine.Playing) — Started 전에는 false
	// Levels — 파트별(engine.Part 순: BassA BassB BD SD CH OH CP CY) 블록 피크(0..1 근처, 프리 FX).
	// 직전 Tick() 이후 누적 최대(호스트가 틱마다 OR 대신 max로 모은다). 라인별 활동 LED·VU의 원본.
	// 구 호스트(levels 없음)는 전부 0 — 미터가 꺼진 채로 그린다(입력 방어).
	Levels [NumLevels]float32
}

// NumLevels — Tick.Levels 길이(베이스 2 + 드럼 6 = engine.Part 수).
const NumLevels = 8

// Bridge — JS 호스트(app/web/host.js)와의 접점. 데스크톱 빌드는 no-op 구현.
// 메서드는 전부 논블로킹이며 호스트가 없으면 조용히 무동작(값은 0/false).
type Bridge interface {
	Start()                          // 사용자 제스처 뒤 오디오 시작(중복 호출 무해)
	Cmd(c engine.Cmd, a Author)      // 엔진에 명령(호스트가 블록 예약·로그 기록)
	Tick() Tick                      // 최신 스냅샷(프레임당 1회 호출)
	Scope(dst []byte) bool           // 파형 256 Float32 = 1024바이트(리틀엔디언) 복사
	Param(id engine.ParamID) float32 // 호스트가 미러링한 현재 파라미터 값(레지던트·리플레이 반영)
	DevParam(slot, k int) float32    // 장치 로컬 파라미터 미러 값(§14.1 DeviceParam; 폴리 리드 노브) — 미러 부재 시 음수
	// 랙 위상(§14.3 뒷면 케이블 뷰). RackRev가 달라졌을 때만 Cables를 다시 부른다 —
	// 프레임마다 케이블 64줄을 JS로 되읽지 않기 위한 계약이다(값 자체는 무의미, 변화만 본다).
	RackRev() uint32
	RackKind(slot int) engine.DeviceKind // 슬롯의 장치 종류(범위 밖·미러 부재는 KindNone)
	Cables(dst []RackCable) int          // 케이블 표를 dst에 복사(최대 len(dst)) — 복사 수 반환, 무할당
	BassStep(p engine.Part, step int) (note, flags uint8)
	DrumStep(p engine.Part, step int) uint8
	Muted(p engine.Part) bool
	Slot(p engine.Part) uint8
	KeyRoot() int                          // 조성 루트 0..11(0 = C)
	Chord(bar int) (deg, flags uint8)      // 코드 트랙 마디 bar&7의 (도수 0..6, engine.ChordSeventh)
	Mode(p engine.Part) (mode, dir uint8)  // 베이스 파트 모드(engine.ModeBass/Arp/Chord)·방향
	Hint(state int)                        // 첫 접촉 캡션 상태(호스트 DOM이 문구를 가진다): 0 없음 1 탭 전 2 기기 안내 3 기기 뷰 첫 진입
	Telemetry(event string, value float64) // 이벤트 1건(호스트가 배치 전송)
	Replay(seconds float64)                // 마지막 N초 리플레이 요청(3·2·1 뒤 재생; 호스트가 처리)
	SeedWord() string                      // DOM 오버레이의 시드 단어(빈 문자열 가능)
	ReducedMotion() bool                   // prefers-reduced-motion
	Hidden() bool                          // document.hidden — 렌더 정지 판단
	CleanScreen() bool                     // 클린 스크린 토글(DOM 버튼) — 뷰가 잡동사니를 숨긴다
	WallClock() (hour, min, sec int)       // 로컬 벽시계(벽시계 드롭용)
	Frame(ms float64)                      // 계측: Update 시작~Draw 끝
	FirstFrame()
	AllocPerFrame(bytes float64)
}

// RackCable — 뒷면 뷰가 보는 케이블 한 줄(engine.Cable의 UI 사본). Bind는 결속
// 파라미터 ID이고 engine.Unbound(= NumParams)면 비결속 — 게인의 정본이 gainQ라는 뜻이다.
type RackCable struct {
	Src, SP uint8
	Dst, DP uint8
	Bind    uint8
	Gain    float32
}

// Ctx — 프레임마다 뷰에 넘기는 공용 상태. main.go가 채우고 뷰는 읽기만(Cmd는 Bridge 경유).
type Ctx struct {
	Bridge Bridge
	Tick   Tick     // 이 프레임의 스냅샷
	DT     float64  // 직전 프레임과의 간격(초)
	Now    float64  // 앱 시작 후 초
	Font   *FontSet // 라벨 폰트(core/font.go)

	// 입력 — main.go가 프레임 시작에 수집해 넘긴다(뷰가 ebiten 입력 API를 직접 부르지 않아도 되게).
	Pointers []Pointer // 이 프레임의 활성 포인터(마우스 1 + 터치 N). 재사용 슬라이스 — 보관 금지.

	// 레지던트·세션 정보(방 뷰 연출용) — main.go가 채운다.
	ResidentHand      engine.ParamID
	ResidentHandOn    bool
	Energy            float32 // 0..1
	Phase             uint8   // 0 Intro 1 Build 2 Drop 3 Breakdown
	PomodoroRest      bool
	PomodoroRemainSec float64
	ManualLocked      bool // 사용자가 잡은 노브가 하나 이상
	CleanScreen       bool
}

// Pointer — 논리 좌표의 포인터 상태. ID: 마우스 = -1, 터치 = ebiten.TouchID.
type Pointer struct {
	ID           int
	X, Y         float64
	JustPressed  bool
	Pressed      bool
	JustReleased bool
}

// View — 화면 하나. Update는 입력 처리·상태 갱신, Draw는 그리기만(Draw에서 Cmd 금지).
type View interface {
	Update(ctx *Ctx)
	Draw(screen *ebiten.Image, ctx *Ctx)
}

// Rect — 레이아웃 JSON의 [x, y, w, h].
type Rect [4]float64

func (r Rect) Contains(x, y float64) bool {
	return x >= r[0] && y >= r[1] && x < r[0]+r[2] && y < r[1]+r[3]
}

func (r Rect) Center() (float64, float64) { return r[0] + r[2]/2, r[1] + r[3]/2 }
