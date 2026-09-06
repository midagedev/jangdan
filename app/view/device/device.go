// Package device — 기기 뷰. fal.ai로 칠한 패널 한 장(app/assets/device/panel.png) 위에
// 노브 스프라이트를 −135°..+135° 회전해 올리고, 라벨·숫자·표시창은 앱 폰트로 그린다
// (diffusion은 글자를 망친다). 좌표의 단일 소유자는 레이아웃 JSON이다.
//
// 이 파일은 상태기계(입력 → Cmd)를 담고 그리기는 draw.go에 있다. Update만 Cmd를 보내며
// Draw는 무송신. 프레임당 힙 할당은 문자열 캐시(값 변화 시에만 재구성)를 제외하면 0이다.
package device

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	_ "image/png" // 패널·스프라이트 디코드

	"github.com/midagedev/jangdan/app/assets"
	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// 수치 계약(스펙 origin). 픽셀 좌표는 여기에 없다 — 레이아웃 JSON이 소유한다.
const (
	hitKnobPad       = 6     // 노브 히트 여유(px, 중심 거리 r+6)
	hitRectPad       = 4     // rect 컨트롤 히트 여유(px)
	dragRange        = 200.0 // 노브 세로 드래그: 200px = Δ1.0
	tapMoveMax       = 6.0   // 탭 판정 최대 이동(px)
	tapDurMax        = 0.25  // 탭 판정 최대 눌림(초)
	padHoldMute      = 0.5   // 패드 길게 누르기 뮤트 임계(초)
	padLitDur        = 0.12  // 패드 탭 lit(초)
	sweepSendMin     = 0.05  // 스윕 중 SetParam 최소 송신 간격(초)
	knobLitBoost     = 1.12  // 잡힌 노브 밝기 배수
	padMuteScale     = 0.55  // 뮤트 패드 ColorScale(오버레이 알파 0.45로 환원)
	dropPulseAmp     = 0.35  // Build 중 DROP 버튼 펄스 진폭(+0..35%)
	dropPulseHz      = 1.0   // 펄스 주파수
	overlayLitA      = 0.18  // lit 반투명 흰 사각 알파
	knobDyMain       = 18    // 노브 라벨 오프셋: cy + r + 18(베이스라인·fx — 눈금 겹침에서 +14px)
	knobDyDrums      = 14    // 드럼 노브 라벨 오프셋(라벨판이 좁아 +10px)
	plateInset       = 8     // 섹션 이름판 왼쪽 정렬 들여쓰기(px) — 판 테두리가 rect 안쪽 ≈6px에 있어 6이면 첫 글자가 테두리에 걸린다(2차 비전 처방)
	titleShiftX      = 40    // JANGDAN x 이동(좌상단 DOM 시드 입력 회피)
	labelFitPad      = 4     // 버튼 라벨 폭 예산 = rect 폭 − 4
	labelTransportDy = 13.0  // PLAY/DROP 라벨을 아이콘 아래로(버튼 높이 48의 하단 1/4 중심)
	labelFloor       = 0.3   // 라벨 축소 하한
	plateBandW       = 6     // 이름판 좌측 밴드 폭(px) — 게이트 2의 검사 밴드와 같은 폭. 페인팅 잔글자를 판색으로 덮는다
	stepFaceInset    = 2     // 스텝 버튼 면 들여쓰기(px, rect 안쪽) — 면은 앱이 그린다(16번 자리가 스크럽으로 지워짐)
	botDispPad       = 8     // 하단 표시창 라벨 폭 예산 = rect 폭 − 8(베이스 표시창·버튼은 −4 — §12.3)
)

// 색 계약(스펙 hex 그대로).
var (
	colLabel  = color.NRGBA{0xE8, 0xE2, 0xD2, 191} // #E8E2D2 α0.75 — 라벨
	colInk    = color.NRGBA{0x2A, 0x26, 0x22, 230} // #2A2622 α0.9 — 밝은 판 위 어두운 잉크 라벨(비전 처방; hex는 colLEDOff와 같음)
	colLCD    = color.NRGBA{0x7F, 0xE0, 0x8A, 255} // #7FE08A — 표시창·스코프
	colLEDOn  = color.NRGBA{0xFF, 0x9A, 0x3C, 255} // 켜짐 α1.0
	colLEDMid = color.NRGBA{0xFF, 0x9A, 0x3C, 115} // 중간 α0.45
	colLEDOff = color.NRGBA{0x2A, 0x26, 0x22, 230} // 꺼짐 α0.9

	// 앱이 그리는 채움색 — 전부 패널에서 잰 중앙값(2026-09-06 실측). 새 색 발명이 아니라 패널 복원이다.
	// 스텝 면: 1..15의 중앙값이 전부 (146,94,59)로 동일하다. wire.py의 2계열(4의 배수 밝은 주황
	// (200,110,40)/나머지 (120,80,50))은 페인팅에 남아 있지 않고, 그대로 쓰면 R max/min 게이트(≤1.3)를
	// 깬다(200/120=1.67) — 측정 중앙값 단일색으로 통일(보고서 참조).
	colStepFace = color.NRGBA{0x92, 0x5E, 0x3B, 0xFF} // #925E3B = (146,94,59) — 16스텝 버튼 면 공통
	// 이름판 좌측 밴드 패치색 — bassA·bassB·drums·fx·mixer·fx2·poly 순. 판 내부 중앙값(테두리·잔글자 제외 영역).
	colPlateBand = [7]color.NRGBA{
		{0xDB, 0xD3, 0xBF, 0xFF}, // (219,211,191)
		{0xDB, 0xD3, 0xBF, 0xFF}, // (219,211,191)
		{0xDA, 0xD0, 0xB6, 0xFF}, // (218,208,182)
		{0xE8, 0xDC, 0xC1, 0xFF}, // (232,220,193)
		// P4-scroll: 새 두 모듈은 §13.3 보라 (120,80,150) 계열 띠다 — panel-v3 실측 중앙값.
		{0x84, 0x5B, 0x8B, 0xFF}, // mixer (132,91,139)
		{0x84, 0x5B, 0x95, 0xFF}, // fx2 (132,91,149)
		// P5-poly: 패널은 아직 와이어프레임(그림 라운드가 panel.png를 칠한다) — wire.py 섹션
		// 띠 틴트 (60,130,170)를 임시로 쓰고, 채색 병합 뒤 리드가 패널 실측 중앙값으로 재조정한다.
		{0x2A, 0x85, 0xB2, 0xFF}, // poly (42,133,178) — 채택 패널(seed 7 y1790) 띠 중앙값 실측
	}
	// 베이스라인 표시창 창색 — 창 청정부(하단 20px) 중앙값. 불투명 채움으로 페인팅 잔글자("68."류)를 차단.
	colDispWin = [2]color.NRGBA{
		{0x44, 0x44, 0x48, 0xFF}, // basslineA (68,68,72)
		{0x40, 0x40, 0x46, 0xFF}, // basslineB (64,64,70)
	}
	// 코드 트랙 띠(§12.3) — RGB는 계약색(colLabel/colLEDOn) 그대로, 알파만 스펙 지정. 새 색 발명이 아니다.
	colChordEdge = color.NRGBA{0xE8, 0xE2, 0xD2, 89}  // colLabel α0.35 — 셀 외곽선 1px
	colChordSel  = color.NRGBA{0xFF, 0x9A, 0x3C, 153} // colLEDOn α0.6 — 선택기 현재 값·7th 채움
	// 선택기 7th 토글 셀 채움 — 하단 표시창 필드색(그림 실측 (99,108,110)). 비전 FIX 2026-09-06:
	// 도수 셀과 같은 슬레이트라 "B<n> 7"이 8번째 후보로 읽혔다 — 표시창 계열 색으로 분리.
	colChordTog = color.NRGBA{99, 108, 110, 0xFF}
	colChordDim = color.NRGBA{0, 0, 0, 77} // 선택기 열림 중 띠 아래 감광(검정 α0.30)
)

// 라벨·표시 스케일.
const (
	labelKnobScale    = 0.5
	labelBtnScale     = 0.45
	labelPadScale     = 0.6
	labelTitleScale   = 1.6 // JANGDAN 타이틀(스펙: 1.0 → 1.6)
	labelSectionScale = 0.5
	dispBassScale     = 0.5
	dispBottomScale   = 0.6
)

// 섹션 인덱스. mixer·fx2는 P4-scroll 랙 확장(§13.3), poly는 P5-poly(§14.1) —
// 스크롤 없이는 화면에 없다.
const (
	secBassA = 0
	secBassB = 1
	secDrums = 2
	secFx    = 3
	secMixer = 4
	secFx2   = 5
	secPoly  = 6
)

const numBassSecBtns = 10 // 베이스라인 섹션 버튼(saw..patD) 수

// 표시창 문자열의 ASCII 구분자. 스펙 문구의 "·"(U+00B7)는 폰트 아틀라스가 ASCII
// 32..126이라 '?'로 렌더되므로 대체했다 — 스펙↔폰트 계약 충돌, 보고서 참조.
const asciiSep = " - "

var phaseNames = [4]string{"INTRO", "BUILD", "DROP", "BREAK"}

// ptrKind — 포인터가 잡은 컨트롤 종류.
type ptrKind uint8

const (
	pkNone ptrKind = iota
	pkKnob
	pkPad
	pkScroll    // 빈 판을 잡은 드래그 — 랙 스크롤(§13.3, 상태는 scroll.go가 소유)
	pkTitle     // 기기 이름판 누름(§14.3) — 짧게 놓으면 방, 길게 누르면 뒷면(상태는 back.go)
	pkRearPlate // 뒷면 장치 이름판(§14.3) — 탭하면 앞면 복귀
	pkJack      // 뒷면 잭 드래그(§14.3) — 케이블 늘리기·옮기기·뽑기
)

// ptrState — 포인터 ID별 캡처. 잡힌 노브는 이동 중 다른 컨트롤로 넘어가지 않는다.
type ptrState struct {
	id        int
	kind      ptrKind
	idx       int
	x0, y0    float64 // 누른 지점(탭 판정 이동 거리의 기준)
	lastY     float64 // 직전 프레임 y(스크롤 프레임 델타·관성 속도의 기준)
	t0        float64 // 누른 시각
	grabVal   float32 // 노브 잡은 시점의 값
	longFired bool    // 패드 길게 누르기 이미 발동
	seen      bool
}

// bassDisp — 베이스라인 표시창 캐시. 문자열은 값 변화 시에만 재구성한다.
// B는 노브 접촉 뒤 dispKnobValDur초간 값, 그 뒤 모드 문자열(Bridge.Mode에서 유도).
type bassDisp struct {
	knob    int // 그 섹션에서 마지막으로 만진 노브, -1 = 없음
	knobT   float64
	val99   int32
	modeKey int32 // 캐시된 B 모드 키(mode | dir<<8) — 모드 문자열 재구성 판정
	text    string
	dirty   bool
}

// bottomDisp — 하단 표시창 캐시(키·BPM·마디·페이즈 — "Am 120 B3 BUILD").
type bottomDisp struct {
	key      int32
	bpm, bar int32
	phase    uint8
	manual   bool
	text     string
	dirty    bool
}

// View — 기기 뷰. New(이미지 있는 제품 경로)과 newView(레이아웃만 — 테스트)로 만든다.
type View struct {
	layout *core.DeviceLayout

	knobs   []knob
	buttons []button
	pads    []padCtl
	leds    []core.LED

	secLEDs [2][numBassSecBtns]int // 섹션 버튼 순서(왼→오: saw..patD)의 LED 인덱스, -1 없음
	fxLEDs  [engine.Steps]int
	fxPlay  int // play(PLAY) 버튼 인덱스
	fxRec   int // rec(DROP) 버튼 인덱스

	titlePlate    core.Rect
	hasTitle      bool
	bassPlates    [2]core.Rect
	hasBassPlate  [2]bool
	sectionPlates [5]core.Rect // drums·fx·mixer·fx2·poly 이름판(라벨용)
	hasSection    [5]bool

	dispRects [2]core.Rect
	hasDisp   [2]bool
	scopeRect core.Rect
	botRect   core.Rect

	chordRect     core.Rect // 코드 트랙 띠(§12.3)
	chordCells    [engine.ChordBars]core.Rect
	chordTogRect  core.Rect // 선택기 7th 토글 셀(셀 7 − 왼쪽 chordTogGap)
	chordRingRect core.Rect // 선택기 열림 외곽선 자리(띠 사방 chordOpenInset)
	chord         chordState
	harmonyOK     bool // 화성 API 래치(chord.go 헤더) — 구 호스트 패닉 1회에 끊긴다

	selPart engine.Part // 16스텝 편집 대상(기본 BassA)
	mode    editMode

	ptrs  [8]ptrState
	nptrs int

	// 스크롤 랙(§13.3 — 상태·입력·그리기 전부 scroll.go가 소유한다). rack은 New에서
	// 1회 만드는 레이아웃 크기(720×2000, v4) 오프스크린 — Draw 본문을 전부 여기에 그린 뒤
	// scrollY만큼 올려 화면에 blit한다. sptrs는 포인터의 화면 좌표 사본(ctx.Pointers는
	// 재사용 슬라이스라 수정 금지) — 제스처는 화면 좌표계로 재고, 레이아웃 좌표 변환
	// (y+scrollY)의 단일 소유자는 press()의 히트 판정이다(scroll.go 헤더 참조).
	scrollY         float64
	scrollV         float64
	scrollShowUntil float64
	scrollMax       float64
	rack            *ebiten.Image
	sptrs           [8]core.Pointer
	disp            [2]bassDisp
	bottom          bottomDisp
	meters          meters  // 라인 VU 밸리스틱(P3-meters) — 파트 8 + 마스터
	polyTrigT       float64 // 폴리 트리거 점 최종 점화 시각(ctx.Now, −1 = 미점화 — P5-poly)

	// 뒷면 케이블 뷰(§14.3, P5-back-view — 상태·입력·그리기 전부 back.go가 소유한다;
	// scroll.go 관례: 이곳에는 필드 선언만). rearImg는 New에서만 디코드(newView 경로 nil).
	rear       bool
	rearL      *core.RearLayout
	rearImg    *ebiten.Image
	cables     [engine.RackCables]core.RackCable
	nCables    int
	cableRev   uint32
	cableRevOK bool
	jackDrag   jackDrag
	pendConn   pendConn
	rejSlot    int
	rejPort    int
	rejIn      bool
	rejT       float64 // 순환 거부 피드백 점화 시각(−1 = 미점화 — polyTrigT 관례)
	rearDraws  int     // 이 프레임에 그려진 케이블 수(Update에서 리셋 — meterDraws 관례)

	// 뒷면 벡터 재사용 버퍼 — 스코프의 strokeOpts/drawOpts와는 별개 변수(앞면 스코프 색을
	// 침범하지 않는다). 그룹 경로는 색 그룹 수로 스트로크를 묶는다(back.go).
	cableGroups     [maxCableGroups]cableGroup
	dragPath        vector.Path
	rejPath         vector.Path
	cableStrokeOpts vector.StrokeOptions
	cableDrawOpts   vector.DrawPathOptions

	// 재구성 카운터 — 표시창 캐시 계약의 테스트 근거.
	rebuilds int

	// 미터 그리기 결정 수(VU 세그먼트 + 패드 LED 점) — Update에서 리셋. 구 호스트(Levels 전부 0)
	// 소거 단언의 테스트 근거(room 뷰 draws 관례 준용).
	meterDraws int

	// 한 프레임 유효 플래그
	back, drop bool
	grabID     engine.ParamID
	grabOK     bool

	// 드로잉 자산 — newView(테스트)에서는 nil. 디코드는 New에서만.
	panel      *ebiten.Image
	spriteImg  []*ebiten.Image // 반지름 클래스 오름차순
	spriteCls  []float64
	ledImg     [3]*ebiten.Image // on/mid/off
	padLEDImg  *ebiten.Image    // 패드 라인 LED 점 r4(P3-meters) — newView(테스트)에서는 nil
	polyDotImg *ebiten.Image    // 폴리 트리거 점 r4(P5-poly) — newView(테스트)에서는 nil
	ledR       float64
	white1     *ebiten.Image
	black1     *ebiten.Image
	dispImg    [3]*ebiten.Image // bassA·bassB·하단
	labelLayer *ebiten.Image
	layersOK   bool

	op         ebiten.DrawImageOptions
	wave       vector.Path
	strokeOpts vector.StrokeOptions
	drawOpts   vector.DrawPathOptions
	scopeBytes [4 * scopeSamples]byte
	scopeF32   [scopeSamples]float32
	scopeHPX   float32 // 스코프 하이패스 상태 x[n−1] — 프레임 간 유지(DC 제거가 창 경계에서 리셋되지 않게)
	scopeHPY   float32 // 스코프 하이패스 상태 y[n−1]
	scratch    [40]byte
}

const scopeSamples = 256

// New — 제품 경로: 임베디드 자산에서 레이아웃·패널·노브 스프라이트를 읽는다.
func New(ctx *core.Ctx) (*View, error) {
	l, err := core.LoadDeviceLayout(assets.DeviceLayoutJSON)
	if err != nil {
		return nil, fmt.Errorf("device: 레이아웃 파싱: %w", err)
	}
	v, err := newView(l)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(mustAsset("device/panel.png")))
	if err != nil {
		return nil, fmt.Errorf("device: 패널 디코드: %w", err)
	}
	v.panel = ebiten.NewImageFromImage(img)
	// 랙 오프스크린(레이아웃 크기 = 720×2000, v4) — 스크롤 blit의 원본 한 장. 여기서만 만든다.
	v.rack = ebiten.NewImage(int(l.Size[0]), int(l.Size[1]))
	// 뒷면 패널(§14.3) — 아직 와이어프레임이지만 있는 그대로 그린다(룩 판단은 아트 라운드).
	img, _, err = image.Decode(bytes.NewReader(mustAsset("device/rear.png")))
	if err != nil {
		return nil, fmt.Errorf("device: 뒷면 패널 디코드: %w", err)
	}
	v.rearImg = ebiten.NewImageFromImage(img)

	type cls struct {
		r   float64
		img image.Image
	}
	clss := make([]cls, 0, len(l.Sprites))
	for name, sp := range l.Sprites {
		data, err := assets.Read("device/sprites/" + name)
		if err != nil {
			return nil, fmt.Errorf("device: 스프라이트 %s: %w", name, err)
		}
		im, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("device: 스프라이트 %s 디코드: %w", name, err)
		}
		clss = append(clss, cls{r: float64(sp.R), img: im})
	}
	sort.Slice(clss, func(i, j int) bool { return clss[i].r < clss[j].r })
	for _, c := range clss {
		v.spriteCls = append(v.spriteCls, c.r)
		v.spriteImg = append(v.spriteImg, ebiten.NewImageFromImage(c.img))
	}

	// LED 스프라이트: 반지름은 레이아웃의 최대 LED r 하나로 통일하고 그릴 때 축소.
	maxR := 0.0
	for _, led := range l.LEDs {
		if led.R > maxR {
			maxR = led.R
		}
	}
	if maxR <= 0 {
		maxR = 6
	}
	v.ledR = maxR
	v.ledImg[0] = ebiten.NewImageFromImage(ledCircle(maxR, colLEDOn))
	v.ledImg[1] = ebiten.NewImageFromImage(ledCircle(maxR, colLEDMid))
	v.ledImg[2] = ebiten.NewImageFromImage(ledCircle(maxR, colLEDOff))
	v.padLEDImg = ebiten.NewImageFromImage(ledCircle(padLEDR, colLEDOn))  // 패드 라인 LED 점(P3-meters)
	v.polyDotImg = ebiten.NewImageFromImage(ledCircle(polyTrigR, colLCD)) // 폴리 트리거 점(P5-poly)
	v.white1 = ebiten.NewImageFromImage(solid1x1(color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}))
	v.black1 = ebiten.NewImageFromImage(solid1x1(color.NRGBA{0, 0, 0, 0xFF}))
	v.initStrokeOpts()
	return v, nil
}

// newView — 레이아웃만 파싱해 컨트롤 인덱스를 구축한다(이미지 없음 — 유닛 테스트 경로).
func newView(l *core.DeviceLayout) (*View, error) {
	v := &View{layout: l, selPart: engine.BassA, fxPlay: -1, fxRec: -1, harmonyOK: true, polyTrigT: -1, rejT: -1}
	v.disp[0].knob, v.disp[1].knob = -1, -1
	for s := 0; s < 2; s++ {
		for j := range v.secLEDs[s] {
			v.secLEDs[s][j] = -1
		}
	}
	for i := range v.fxLEDs {
		v.fxLEDs[i] = -1
	}

	secOf := map[string]uint8{"basslineA": secBassA, "basslineB": secBassB, "drums": secDrums, "fx": secFx, "mixer": secMixer, "fx2": secFx2, "poly": secPoly}
	for _, k := range l.Knobs {
		sec, ok := secOf[k.Section]
		if !ok {
			return nil, fmt.Errorf("device: 노브 %q의 알 수 없는 섹션 %q", k.Name, k.Section)
		}
		kn := knob{name: k.Name, label: knobLabel(sec, k.Name), sec: sec, cx: k.CX, cy: k.CY, r: k.R}
		// 매핑 3단계: 전역 ParamID → 장치 로컬(§14.1 DeviceParam — 지금은 poly 8종) → 매핑
		// 없음은 레이아웃 오류(기존 계약 유지). dev 노브의 id는 0으로 남지만 쓰는 경로가
		// 없다(knobValue·sendParam이 dev 분기 — JustGrabbed는 dev 노브를 보고하지 않는다).
		if id, ok := core.KnobParam(k.Section, k.Name); ok {
			kn.id = id
		} else if slot, dk, ok := core.KnobDevParam(k.Section, k.Name); ok {
			kn.dev, kn.slot, kn.k = true, slot, dk
		} else {
			return nil, fmt.Errorf("device: 노브 %q/%q의 파라미터 매핑 없음", k.Section, k.Name)
		}
		v.knobs = append(v.knobs, kn)
	}
	for _, b := range l.Buttons {
		sec, ok := secOf[b.Section]
		if !ok {
			return nil, fmt.Errorf("device: 버튼 %q의 알 수 없는 섹션 %q", b.Name, b.Section)
		}
		bt := button{name: b.Name, sec: sec, rect: b.Rect}
		switch b.Name {
		case "saw":
			bt.kind = bkWaveSaw
		case "sqr":
			bt.kind = bkWaveSqr
		case "slide":
			bt.kind = bkModeSlide
		case "acc":
			bt.kind = bkModeAcc
		case "oct-":
			bt.kind = bkOctDown
		case "oct+":
			bt.kind = bkOctUp
		case "patA", "patB", "patC", "patD":
			bt.kind, bt.arg = bkPat, int(b.Name[3]-'A')
		case "play":
			bt.kind = bkPlay
		case "rec":
			bt.kind = bkRec
		case "rev_on", "cho_on", "rev_pre", "cho_st": // §13.3 fx2 장식 버튼 — 송신·lit 없음(bkDeco)
			bt.kind = bkDeco
		default:
			if n, err := strconv.Atoi(strings.TrimPrefix(b.Name, "step")); err == nil && n >= 1 && n <= engine.Steps {
				bt.kind, bt.arg = bkStep, n-1
			} else {
				return nil, fmt.Errorf("device: 알 수 없는 버튼 %q/%q", b.Section, b.Name)
			}
		}
		bt.label = buttonLabel(bt.name, bt.kind, bt.arg)
		v.buttons = append(v.buttons, bt)
	}
	for i := range v.buttons {
		switch v.buttons[i].kind {
		case bkPlay:
			v.fxPlay = i
		case bkRec:
			v.fxRec = i
		}
	}
	for _, p := range l.Pads {
		part, ok := core.PadPart(p.Name)
		if !ok {
			return nil, fmt.Errorf("device: 패드 %q의 파트 매핑 없음", p.Name)
		}
		v.pads = append(v.pads, padCtl{name: p.Name, part: part, rect: p.Rect})
	}
	v.leds = l.LEDs
	for _, pl := range l.Plates {
		switch pl.For {
		case "title":
			v.titlePlate, v.hasTitle = pl.Rect, true
		case "basslineA":
			v.bassPlates[0], v.hasBassPlate[0] = pl.Rect, true
		case "basslineB":
			v.bassPlates[1], v.hasBassPlate[1] = pl.Rect, true
		case "drums":
			v.sectionPlates[0], v.hasSection[0] = pl.Rect, true
		case "fx":
			v.sectionPlates[1], v.hasSection[1] = pl.Rect, true
		case "mixer":
			v.sectionPlates[2], v.hasSection[2] = pl.Rect, true
		case "fx2":
			v.sectionPlates[3], v.hasSection[3] = pl.Rect, true
		case "poly": // P5-poly — 트리거 점(draw.go)과 라벨이 이 판 rect를 쓴다
			v.sectionPlates[4], v.hasSection[4] = pl.Rect, true
		}
	}
	for _, d := range l.Displays {
		switch d.For {
		case "basslineA":
			v.dispRects[0], v.hasDisp[0] = d.Rect, true
		case "basslineB":
			v.dispRects[1], v.hasDisp[1] = d.Rect, true
		}
	}
	v.scopeRect = l.Scope.Rect
	v.botRect = l.Display.Rect
	// 스크롤 상한 = 레이아웃 높이 − 화면 높이(§13.3). 구 레이아웃(≤1280)이면 0 —
	// 스크롤 비활성·인디케이터 없음(3클래스 방어 1).
	if m := l.Size[1] - core.LogicalH; m > 0 {
		v.scrollMax = m
	}
	v.initChord()
	if err := v.pairLEDs(); err != nil {
		return nil, err
	}
	// 뒷면 케이블 스트로크 옵션(§14.3 수치 — back.go 소유 상수). 스코프 옵션과 별개 변수로
	// 둔다: drawScope가 v.drawOpts.ColorScale == colLCD를 사실상 상정하므로 뒷면이 앞면
	// 스코프 색을 침범하면 안 된다.
	v.cableStrokeOpts = vector.StrokeOptions{Width: cableW, LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound}
	v.cableDrawOpts = vector.DrawPathOptions{AntiAlias: true}
	if err := v.loadRear(); err != nil {
		return nil, err
	}
	return v, nil
}

// pairLEDs — LED를 cy 밴드로 묶어 섹션 버튼(베이스라인 A·B, 왼→오)·스텝 버튼(1..16)과
// 짝짓는다. 밴드가 3개뿐이던 구현을 P4-scroll의 5밴드(믹서 활동 8·fx2 장식 4 추가)로
// 일반화한다: 각 버튼 행은 "아직 안 쓴 밴드 중 개수가 같고 행 y에 가장 가까운" 밴드와
// 짝짓는다. 짝이 남는 밴드는 장식 — 어떤 상태와도 묶지 않는다(패널이 어둡게 칠해 둔 자리).
func (v *View) pairLEDs() error {
	type band struct {
		cy   float64
		ids  []int
		used bool
	}
	var bands []band
	for i, led := range v.leds {
		found := false
		for bi := range bands {
			if math.Abs(bands[bi].cy-led.CY) <= 1 {
				bands[bi].ids = append(bands[bi].ids, i)
				found = true
				break
			}
		}
		if !found {
			bands = append(bands, band{cy: led.CY, ids: []int{i}})
		}
	}
	sort.Slice(bands, func(i, j int) bool { return bands[i].cy < bands[j].cy })
	for bi := range bands {
		ids := bands[bi].ids
		sort.Slice(ids, func(a, b int) bool {
			return v.leds[ids[a]].CX < v.leds[ids[b]].CX
		})
		bands[bi].ids = ids
	}
	// nearest — 아직 안 쓴 밴드 중 개수 n과 같고 y에 가장 가까운 것(없으면 -1).
	nearest := func(y float64, n int) int {
		best, bestD := -1, 0.0
		for bi := range bands {
			if bands[bi].used || len(bands[bi].ids) != n {
				continue
			}
			if d := math.Abs(bands[bi].cy - y); best < 0 || d < bestD {
				best, bestD = bi, d
			}
		}
		return best
	}
	for s := 0; s < 2; s++ {
		var btns []int
		cy := 0.0
		for i := range v.buttons {
			if v.buttons[i].sec == uint8(s) {
				btns = append(btns, i)
				cy += v.buttons[i].rect[1] + v.buttons[i].rect[3]/2
			}
		}
		if len(btns) == 0 || len(btns) > numBassSecBtns {
			return fmt.Errorf("device: 섹션 %d 버튼 %d개(짝짓기 불가)", s, len(btns))
		}
		sort.Slice(btns, func(a, b int) bool {
			return v.buttons[btns[a]].rect[0] < v.buttons[btns[b]].rect[0]
		})
		bi := nearest(cy/float64(len(btns)), len(btns))
		if bi < 0 {
			return fmt.Errorf("device: 섹션 %d 버튼 %d개와 짝인 LED 밴드 없음", s, len(btns))
		}
		bands[bi].used = true
		copy(v.secLEDs[s][:], bands[bi].ids)
	}
	var steps []int
	cy := 0.0
	for i := range v.buttons {
		if v.buttons[i].kind == bkStep {
			steps = append(steps, i)
			cy += v.buttons[i].rect[1] + v.buttons[i].rect[3]/2
		}
	}
	if len(steps) == 0 {
		return nil
	}
	sort.Slice(steps, func(a, b int) bool { return v.buttons[steps[a]].arg < v.buttons[steps[b]].arg })
	bi := nearest(cy/float64(len(steps)), len(steps))
	if bi < 0 {
		return fmt.Errorf("device: 스텝 버튼 %d개와 짝인 LED 밴드 없음", len(steps))
	}
	bands[bi].used = true
	copy(v.fxLEDs[:], bands[bi].ids)
	return nil
}

// Update — 이 프레임의 입력 처리. Cmd 송신은 여기서만 일어난다.
func (v *View) Update(ctx *core.Ctx) {
	v.back, v.drop, v.grabOK = false, false, false
	v.meterDraws = 0
	v.runSweeps(ctx)
	v.wheelScroll(ctx) // 휠·트랙패드(§13.3) — 포인터와 무관하게 매 프레임(main.go 수정 없음)
	v.stepScroll(ctx)  // 관성 감쇠(§13.3) — 스크롤 포인터를 잡은 중에는 쉰다
	// 포인터 사본 — 화면 좌표 그대로(ctx.Pointers는 재사용 슬라이스라 절대 수정하지 않는다).
	// 제스처(노브 드래그·스크롤 델타)는 화면 좌표계로 잰다: 스크롤이 랙을 옮기는 중에도
	// 손가락의 화면 이동만 재야 프레임 되먹임이 없다. 레이아웃 좌표가 필요한 곳은 히트
	// 판정뿐이고 +scrollY 변환의 단일 소유자는 press()다(scroll.go 헤더 참조).
	// 동시 8개 초과 포인터는 초과분 무시(방어 3 — 사본 배열 상한).
	n := len(ctx.Pointers)
	if n > len(v.sptrs) {
		n = len(v.sptrs)
	}
	ptrs := v.sptrs[:n]
	for i := 0; i < n; i++ {
		ptrs[i] = ctx.Pointers[i]
	}
	for i := range ptrs {
		p := &ptrs[i]
		si := v.findPtr(p.ID)
		switch {
		case p.JustPressed:
			if si < 0 {
				si = v.allocPtr(p.ID)
			}
			v.ptrs[si].seen = true
			v.press(ctx, p, si)
			// 한 프레임 안에서 누르고 놓은 입력(빠른 탭·합성 입력)은 두 플래그가 함께 온다.
			// 누름만 처리하고 넘기면 다음 프레임에 그 포인터가 사라져 "놓친 릴리즈" 정리로
			// 흘러가고, 놓기에 달린 판정(패드 탭·코드 셀·이름판·뒷면 잭 놓기)이 통째로
			// 사라진다 — 화면에는 아무 일도 안 일어난 것으로 보인다(2026-09-06 브라우저 실측:
			// 뒷면 잭 드래그의 연결과 이름판 탭이 이 경로로 조용히 죽었다).
			if p.JustReleased {
				v.release(ctx, p, si)
				v.freePtr(si)
			}
		case p.JustReleased:
			if si >= 0 {
				v.release(ctx, p, si)
				v.freePtr(si)
			}
		default:
			if si >= 0 {
				v.ptrs[si].seen = true
				v.movePtr(ctx, p, si)
			}
		}
	}
	// 이 프레임에 관측되지 않은 캡처는 놓친 릴리즈 — 탭 판정 없이 컨트롤을 놓고 정리한다.
	// 잡은 노브를 그냥 두면 useLocal이 남아 표시값이 얼어붙는다(놓친 릴리즈 결함 클래스 봉쇄).
	for i := v.nptrs - 1; i >= 0; i-- {
		if !v.ptrs[i].seen {
			v.dropPtr(i)
		} else {
			v.ptrs[i].seen = false
		}
	}
	v.rearFrame(ctx) // 뒷면(§14.3) — 관측 카운터 리셋 + 케이블 표 동기화(이 프레임 송신의 판정 포함)
	v.chordIdleClose(ctx.Now)
	v.meters.update(ctx.Tick, float32(ctx.DT))
	// 폴리 트리거 점(P5-poly): FlagPoly 프레임에 점화 시각을 래치 — 감쇠 계산은 그리기 쪽
	// (polyTrigA). 비교·대입뿐이라 무할당 계약 안.
	if ctx.Tick.Flags&engine.FlagPoly != 0 {
		v.polyTrigT = ctx.Now
	}
	v.cacheChord(ctx)
	v.cacheDisplays(ctx)
}

func (v *View) findPtr(id int) int {
	for i := 0; i < v.nptrs; i++ {
		if v.ptrs[i].id == id {
			return i
		}
	}
	return -1
}

func (v *View) allocPtr(id int) int {
	if v.nptrs < len(v.ptrs) {
		i := v.nptrs
		v.nptrs++
		v.ptrs[i] = ptrState{id: id}
		return i
	}
	// 8개 초과 동시 포인터: 가장 오래된 슬롯을 놓고(노브 얼어붙음 방지) 끝에 재할당.
	v.dropPtr(0)
	i := v.nptrs
	v.nptrs++
	v.ptrs[i] = ptrState{id: id}
	return i
}

func (v *View) freePtr(i int) {
	last := v.nptrs - 1
	if i != last {
		v.ptrs[i] = v.ptrs[last]
	}
	v.nptrs = last
}

// dropPtr — 놓친 릴리즈 정리: 컨트롤을 놓는다(탭 아님). 스윕 중이 아니면 노브는
// 브리지 값 추적으로 돌아간다. 잭 드래그도 접는다(§14.3 — 판정·송신 없이).
func (v *View) dropPtr(i int) {
	if v.ptrs[i].kind == pkJack {
		v.jackDrag = jackDrag{}
	}
	if v.ptrs[i].kind == pkKnob {
		kn := &v.knobs[v.ptrs[i].idx]
		kn.held = false
		if !kn.swActive {
			kn.useLocal = false
		}
	}
	v.freePtr(i)
}

// press — 포인터 누름. 히트 우선순위(§13.3): 노브 > 패드 > 버튼 > 코드 트랙 띠 > B 표시창 >
// 이름판 > 베이스 이름판(섹션 선택) > 빈 판 = 스크롤 제스처. 코드 선택기가 열려 있으면 띠 밖
// 눌림으로 닫는다(송신 없음) — 눌림의 원래 동작은 계속된다. 히트 판정은 레이아웃 좌표로:
// 화면 y + scrollY 변환의 단일 소유자가 이 함수 첫머리다(제스처는 화면 좌표 — scroll.go 헤더).
func (v *View) press(ctx *core.Ctx, p *core.Pointer, si int) {
	st := &v.ptrs[si]
	st.x0, st.y0, st.t0, st.longFired = p.X, p.Y, ctx.Now, false
	y := p.Y + v.scrollY
	if v.chord.open && v.chordCellAt(p.X, y) < 0 {
		v.chord.open = false
	}
	if v.rear { // §14.3 — 뒷면 히트 우선순위(잭 > 장치 이름판 > 빈 판 스크롤)는 back.go가 소유.
		v.pressRear(ctx, p, si, y)
		return
	}
	if k := v.hitKnob(p.X, y); k >= 0 {
		st.kind, st.idx = pkKnob, k
		st.grabVal = v.knobValue(ctx, &v.knobs[k])
		v.grabKnob(ctx, k)
		return
	}
	if pd := v.hitPad(p.X, y); pd >= 0 {
		st.kind, st.idx = pkPad, pd
		return
	}
	st.kind, st.idx = pkNone, -1
	if b := v.hitButton(p.X, y); b >= 0 {
		v.pressButton(ctx, b)
		return
	}
	if c := v.chordCellAt(p.X, y); c >= 0 {
		v.tapChordBand(ctx, c)
		return
	}
	if v.hasDisp[1] && rectHit(v.dispRects[1], p.X, y) {
		v.tapBassModeDisplay(ctx)
		return
	}
	if v.hasTitle && rectHit(v.titlePlate, p.X, y) {
		// §14.3 — 누르는 즉시 방이 아니라 놓을 때 판정: 짧게 놓으면 탭(방), 길게
		// 누르면 뒷면 전환(movePtr). 즉시 방이면 뒷면 진입 제스처가 불가능해진다.
		st.kind = pkTitle
		return
	}
	for s := 0; s < 2; s++ {
		if v.hasBassPlate[s] && rectHit(v.bassPlates[s], p.X, y) {
			v.selPart = engine.Part(s)
			return
		}
	}
	// 어느 컨트롤에도 맞지 않은 눌림 = 빈 판 잡기 → 스크롤(§13.3 우선순위의 맨 아래).
	// lastY는 화면 y — 스크롤 델타는 화면 좌표계로 잰다. 스크롤이 없는 구 레이아웃
	// (scrollMax 0)은 잡지 않는다. 이미 스크롤을 잡은 포인터가 있으면 첫 것만 산다
	// (두 손가락 동시 스크롤 방지 — 두 번째는 pkNone 그대로).
	if v.scrollMax > 0 && !v.scrollHeld() {
		st.kind, st.lastY = pkScroll, p.Y
		v.scrollV = 0 // 새 잡기는 직전 관성을 끊는다
		v.scrollShowUntil = ctx.Now + scrollIndDur
	}
}

// movePtr — 눌린 채 이동. 잡은 컨트롤만 갱신한다(다른 컨트롤로 넘어가지 않는다).
func (v *View) movePtr(ctx *core.Ctx, p *core.Pointer, si int) {
	st := &v.ptrs[si]
	switch st.kind {
	case pkKnob:
		kn := &v.knobs[st.idx]
		val := clamp01(st.grabVal + float32((st.y0-p.Y)/dragRange))
		kn.local = val
		if val != kn.lastSent {
			v.sendParam(ctx, kn, val)
		}
	case pkPad:
		if !st.longFired && ctx.Now-st.t0 >= padHoldMute {
			st.longFired = true
			v.holdPad(ctx, st.idx)
		}
	case pkTitle: // §14.3 — 길게 누르기로 앞↔뒷면 전환(잡힌 케이블은 접는다).
		if !st.longFired && ctx.Now-st.t0 >= padHoldMute {
			st.longFired = true
			v.rear = !v.rear
			v.jackDrag = jackDrag{}
		}
	case pkJack: // §14.3 — 드래그 끝점은 화면 좌표(그릴 때 scrollY를 더한다).
		v.jackDrag.x, v.jackDrag.y = p.X, p.Y
	case pkScroll:
		dy := p.Y - st.lastY
		st.lastY = p.Y
		v.scrollY = v.clampScroll(v.scrollY - dy)
		v.scrollV = dy // 놓는 순간의 최근 프레임 델타가 관성 초기 속도(§13.3)
		v.scrollShowUntil = ctx.Now + scrollIndDur
	}
}

// release — 놓음. 노브는 탭 판정(스윕), 패드는 탭(Trigger)·길게(뮤트 이미 발동) 판정.
func (v *View) release(ctx *core.Ctx, p *core.Pointer, si int) {
	st := &v.ptrs[si]
	moved := math.Hypot(p.X-st.x0, p.Y-st.y0)
	dur := ctx.Now - st.t0
	switch st.kind {
	case pkKnob:
		v.releaseKnob(ctx, st.idx, moved, dur)
	case pkPad:
		if !st.longFired && dur < padHoldMute && moved < tapMoveMax {
			v.tapPad(ctx, st.idx)
		}
	case pkTitle: // §14.3 — 길게 누르기 없이 짧게·제자리에서 놓았을 때만 방.
		if !st.longFired && dur <= tapDurMax && moved <= tapMoveMax {
			v.back = true
		}
	case pkRearPlate: // §14.3 — 뒷면 장치 이름판 탭 = 앞면 복귀.
		if dur <= tapDurMax && moved <= tapMoveMax {
			v.rear = false
			v.jackDrag = jackDrag{}
		}
	case pkJack:
		v.releaseJack(ctx, p)
	}
}

// cacheDisplays — 표시창 문자열 캐시. 값이 변화할 때만 재구성한다(rebuilds 카운터).
// B 표시창: 노브 접촉 뒤 dispKnobValDur초는 파라미터 값, 그 밖에는 모드 문자열(Bridge.Mode).
func (v *View) cacheDisplays(ctx *core.Ctx) {
	for s := 0; s < 2; s++ {
		d := &v.disp[s]
		if d.knob >= 0 && (s == 0 || ctx.Now-d.knobT < dispKnobValDur) {
			kn := &v.knobs[d.knob]
			val := v.knobValue(ctx, kn)
			v99 := int32(val*99 + 0.5)
			if v99 != d.val99 {
				d.val99 = v99
				d.knobT = ctx.Now // 값이 계속 움직이는 동안 값 표시 창을 이어 붙인다
				nm := kn.name
				if len(nm) > 3 {
					nm = nm[:3]
				}
				b := append(v.scratch[:0], nm...)
				b = append(b, ' ')
				b = strconv.AppendInt(b, int64(v99), 10)
				d.text = string(b) // 재구성 = 할당 — 값 변화 시에만
				d.dirty = true
				d.modeKey = -1 // 값 표시 중임을 표시 — 2초 뒤 모드 경로가 재구성을 놓치지 않게
				v.rebuilds++
			}
			continue
		}
		if s == 0 { // A는 모드가 BASS 고정 — 빈 창은 고장으로 읽힌다(비전 FIX 2026-09-06)
			if d.text == "" {
				d.text = bassModeName(engine.ModeBass, engine.DirUp)
				d.dirty = true
				v.rebuilds++
			}
			continue
		}
		if s == 1 { // 모드 문자열 — 뷰 상태 없이 브리지에서 유도
			mode, dir := v.bridgeMode(ctx.Bridge, engine.BassB)
			mk := int32(mode) | int32(dir)<<8
			if mk != d.modeKey || d.text == "" {
				d.modeKey = mk
				d.text = bassModeName(mode, dir)
				d.dirty = true
				v.rebuilds++
			}
		}
	}
	key := int32(v.bridgeKey(ctx.Bridge))
	bpm := int32(engine.BPMOf(ctx.Bridge.Param(engine.Tempo)) + 0.5)
	bar := int32(ctx.Tick.Bar)
	ph := ctx.Phase & 3
	man := ctx.ManualLocked
	if key == v.bottom.key && bpm == v.bottom.bpm && bar == v.bottom.bar && ph == v.bottom.phase && man == v.bottom.manual {
		return
	}
	v.bottom.key, v.bottom.bpm, v.bottom.bar, v.bottom.phase, v.bottom.manual = key, bpm, bar, ph, man
	b := appendKey(v.scratch[:0], int(key))
	b = append(b, ' ')
	b = strconv.AppendInt(b, int64(bpm), 10)
	b = append(b, ' ', 'B')
	b = strconv.AppendInt(b, int64(bar), 10)
	b = append(b, ' ')
	if man {
		b = append(b, "MANUAL"...)
	} else {
		b = append(b, phaseNames[ph]...)
	}
	v.bottom.text = string(b)
	v.bottom.dirty = true
	v.rebuilds++
}

// BackTapped — 이 프레임에 이름판(title)이 탭됐는가(main.go가 방 뷰로 복귀).
func (v *View) BackTapped() bool { return v.back }

// JustGrabbed — 이 프레임에 사용자가 새로 잡은 노브(MANUAL 잠금용).
func (v *View) JustGrabbed() (engine.ParamID, bool) { return v.grabID, v.grabOK }

// ResumeTapped — 폐지(§12.3: RESUME은 30초 무접촉 자동). main.go 호환을 남겨 두고
// 항상 false를 돌려준다(정리는 호스트 라운드).
func (v *View) ResumeTapped() bool { return false }

// DropTapped — rec 자리(라벨 DROP) 탭. Drop Cmd는 뷰가 직접 보낸다.
func (v *View) DropTapped() bool { return v.drop }

// mustAsset — 자산 바이트(없으면 빈 슬라이스 → image.Decode가 오류를 내고 New가 그 오류를 돌려준다).
func mustAsset(name string) []byte {
	b, err := assets.Read(name)
	if err != nil {
		return nil
	}
	return b
}
