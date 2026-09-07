// back.go — 뒷면 케이블 뷰(§14.3, P5-back-view). 상태·입력·그리기 전부 이 파일이 소유한다
// (View 필드 선언만 device.go — scroll.go 관례: 한 기능의 논리는 한 파일에).
//
// 좌표 계약(scroll.go 헤더와 동일): 히트 판정은 레이아웃 좌표(화면 y + scrollY — 변환의
// 단일 소유자는 press 경로 첫머리), 드래그 끝점 같은 제스처 값은 화면 좌표. 잭·행·이름판
// 픽셀 좌표는 전부 rear.json(core.RearLayout)에서 온다 — Go에 픽셀 상수를 새로 만들지
// 않는다. 아래 그리기 기하 수치(베지어 제어·늘어짐·굵기·알파)는 스펙 F절의 그리기
// 계약이라 상수로 둔다.
//
// 뷰는 엔진의 판정을 흉내 내지 않는다(게인 유도·포트 규칙·순환 판정의 정본은
// engine/rack.go): Connect/Disconnect를 보내고 pendConn을 남긴 뒤, 다음 표 재독에서 그
// 케이블의 존재로 성공·거부를 읽는다. 케이블 표는 Bridge.RackRev()가 변할 때만 다시
// 읽는다(진입 첫 회 포함 — 프레임마다 64케이블을 되읽지 않는다).
//
// 게인 팝업(P5-gain, §14.3 남은 항목 "케이블 탭 → 게인 노브"): 입력 잭 탭(이동 < tapSlop·
// 눌림 < tapDur)은 자리 옮기기가 아니라 그 잭의 마지막 케이블 게인 팝업을 연다. 같은 잭에
// 놓는 것은 **랙 무동작**이다 — 같은 자리 재연결(Disconnect+Connect Unbound 1.0)이 결속
// 케이블의 파라미터 결속을 끊던 결함 봉쇄(리드 실측 탭 전 bind=36 gain=0.400 → 탭 후
// bind=59 gain=1.000). 팝업 상태는 jackDrag 안(pop 필드)에 둔다 — View 필드 선언은
// device.go 소유라 이 파일에서 상태를 늘릴 수 있는 자리가 jackDrag뿐이고, device.go가
// jackDrag를 비우는 지점(다른 잭 다시 잡기·놓친 릴리즈·앞뒷면 전환)이 전부 "팝업도 닫혀야
// 하는" 순간과 정확히 겹친다. 게인 송신: 비결속은 같은 끝점의 Connect(connect가 그 자리를
// 갱신 — 케이블 수 불변), 결속은 SetParam(게인의 정본이 결속 파라미터 — Connect로 덮으면
// 결속이 끊긴다). 대상 케이블이 표에서 사라지면(다른 경로로 뽑혔으면) 팝업도 닫는다 —
// 판정은 rearSync의 표 재독 안에서(rev 불변 프레임에 표가 바뀔 수는 없다).
package device

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/midagedev/jangdan/app/assets"
	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// 그리기 계약 수치(스펙 F절 — 좌표가 아닌 기하·알파 값).
const (
	hitJackPad   = 4.0  // 잭 히트 여유 — r12+4 = 지름 32 ≥ 28(§14.3)
	jackLabelDy  = 20.0 // 포트 라벨 y 오프셋(잭 중심 아래)
	rearGuideY   = 60.0 // 상단 안내 문구 y(빈 띠 y 0..120의 중앙 — 첫 행은 133부터)
	cableW       = 5.0  // 케이블 선 굵기
	cableCtrlOut = 60.0 // 베지어 제어점 수평 오프셋(출력 잭 → 바깥, 입력 잭 → 바깥)
	cableSagK    = 0.28 // 늘어짐 = 두 잭 거리 × 계수
	cableSagMin  = 24.0
	cableSagMax  = 130.0
	cableA0      = 0.35 // 케이블 알파 = 0.35 + 0.55·게인(하한·상한이 이 값들)
	cableAK      = 0.55
	cableAMax    = 0.9
	cableASteps  = 8 // 알파 양자화 단계 — 색 그룹 키(그룹당 스트로크 1회)
	// 색 그룹 상한 = 케이블 수 상한. 그룹 묶기는 스트로크 호출을 줄이는 최적화일 뿐이므로,
	// 상한이 모자라면 색이 조용히 틀어진다(초과분이 남의 색으로 그려진다). 기본 랙 32케이블이
	// 이미 16그룹을 정확히 채우는 것을 실측해(TestCableGroupCount) 상한을 케이블 수로 올렸다 —
	// 최악이 케이블당 스트로크 1회, 즉 묶기가 없던 것과 같다.
	maxCableGroups = engine.RackCables
	dragCableA     = 0.9 // 드래그 중 케이블 알파(양자화 밖 — 단독 스트로크)
	rejDur         = 0.5 // 거부 피드백 표시 시간(초)
	rejDecay       = 6.0 // 거부 링 감쇠율(/초) — draw.go polyTrigA와 같은 e^(−k·t) 방식
)

// 게인 팝업(P5-gain). 잭 탭 판정과 판 기하·감도는 이 파일 소유 상수 — 좌표(잭 위치·라벨)는
// rear.json이 소유하고, 좌표가 아닌 크기·여백·감도만 뷰 상수다(§14.3 좌표 계약).
const (
	tapSlop = 6.0  // 잭 탭 최대 이동(논리 px) — device.go tapMoveMax와 같은 값: 앞면 탭과 같은 지오메트리로 뒷면에서 다른 손맛이 나지 않게
	tapDur  = 0.35 // 잭 탭 최대 눌림(초) — 이름판 탭 tapDurMax 0.25보다 느긋하게(케이블 잡기는 노브보다 정확한 손이 필요하다), 패드 길게 누르기 padHoldMute 0.5보다 짧게(뮤트 오조작 직전에 끊는다)

	// 판 기하 — 폭은 실측에서: 기본 랙 최장 출처 라벨 "SAMPLER OUT -> REVERB IN"이
	// 아틀라스 폭 160.8px(scale 0.5)이므로 pad 2배를 더해 177, 자리 잭 열(빈 칸 포함
	// x 76 ± 반폭)이 좌우 클램프 여유를 갖게 184.
	popW = 184.0
	// 높이는 세로줄 3개의 합: 라벨줄 14 + 간격 6 + 막대 12 + 간격 6 + 숫자줄 14 = 52에
	// 안쪽 여백 popPad 2배 = 68. 줄높이 14 = 폰트 lineHeight 28 × labelKnobScale 0.5.
	popH = 68.0
	// 감도: 세로 136px(= 판 높이의 2배) 이동 = Δ1.0. 앞면 노브 dragRange 200이 노브
	// 스프라이트 지름 84의 약 2.4배인 것과 같은 관례(제어물 한 몫의 약 2배 페치) — 판
	// 하나 높이 제스처로 ±0.5, 1px ≈ 0.007로 잔손질 정밀도는 앞면과 같다.
	popDragRange = 136.0
	popPad       = 8.0  // 판 안쪽 여백(사방) — 라벨·막대가 판 테두리에 붙지 않게
	popGap       = 10.0 // 잭 가장자리~판 간격 — 잭 히트 원(지름 32)과 판이 겹치지 않는 최소 여유
	popBarH      = 12.0 // 게인 막대 높이 — 숫자줄(14)보다 작아 손가락이 막대를 가려도 값은 보인다
	popRad       = 10.0 // 둥근 모서리 반지름 — 잭 r12의 아래급(판이 잭 계열 크기로 읽히게)
	popMargin    = 8.0  // 화면 가장자리 최소 여백(좌우 클램프) — 판 밖 여백도 안쪽 여백(popPad)과 같게
)

// popArrow — 출처 표시 구분자. 스펙 문구의 "→"(U+2192)는 폰트 아틀라스가 ASCII 32..126이라
// '?'로 렌더되므로 ASCII 화살표로 대체했다(device.go asciiSep " - "와 같은 충돌·같은 해법).
const popArrow = " -> "

// rearGuide — 상단 안내 문구(빈 띠에 1줄). 폰트 아틀라스가 ASCII 32..126이라 ASCII만.
const rearGuide = "REAR - DRAG OUT TO IN - TAP A NAME PLATE TO RETURN"

// colReject — 순환 거부 잭 링(이 라운드가 새로 만드는 유일한 색 — 알파는 그릴 때 곱한다).
var colReject = color.NRGBA{0xE0, 0x50, 0x40, 0xFF}

// colCable — 케이블 색 = 그 케이블이 나오는 슬롯의 **뒷면 섹션 색 띠**. 값은 채택 패널
// (rear.png, 시드 1234)의 띠 내부 중앙값 실측이다(2026-09-06) — 새 색 발명이 아니라 그림
// 복원이다. 처음 스펙은 colPlateBand(이름판 밴드)를 지정했는데 그 표는 베이스·드럼·Fx가
// 전부 크림색이라 네 종류의 케이블이 한 색으로 뭉쳤다(대표컷 실측 — 리드 스펙 오류).
// 색은 "이 줄이 어디서 나오는가"의 유일한 단서라 구분이 곧 기능이다.
// 인덱스는 슬롯 번호 그대로. 기본 랙 밖(8..15)은 Fx 색.
var colCable = [engine.RackSlots]color.NRGBA{
	{170, 60, 47, 0xFF},   // 0 bassA
	{77, 99, 129, 0xFF},   // 1 bassB
	{193, 127, 64, 0xFF},  // 2 drums
	{112, 143, 116, 0xFF}, // 3 fx
	{97, 86, 129, 0xFF},   // 4 reverb
	{99, 88, 131, 0xFF},   // 5 chorus
	{131, 91, 141, 0xFF},  // 6 main
	{72, 121, 138, 0xFF},  // 7 poly
	{156, 92, 108, 0xFF},  // 8 sampler — 채택 rear.png 띠 실측(리드 재핀 2026-09-07)
	{112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF},
	{112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF},
}

// jackDrag — 진행 중인 케이블 드래그. fromIn=false: 출력 잭에서 새 케이블 늘리기,
// fromIn=true: 입력 잭에 꽂힌 기존 케이블(src는 그 케이블의 출력 쪽, dst는 옛 도착지)을
// 잡아 옮기는 중. x, y는 화면 좌표(제스처 계약 — 그릴 때 scrollY를 더한다). x0, y0, t0는
// 눌림 시작점·시각(탭 판정 — release는 Pointer만 받으므로 시작값을 여기 보관), pop은 그
// 잭에 열린 게인 팝업(파일 주석 참조 — 팝업이 열려 있는 동안엔 케이블 드래그가 병행할 수
// 없다: 판 밖 누름이 팝업을 닫은 뒤에만 다른 잭을 잡을 수 있으므로 두 상태는 배타적이다).
type jackDrag struct {
	on               bool
	ptr              int
	srcSlot, srcPort int
	dstSlot, dstPort int // fromIn일 때만 유효(옛 도착지 — 자리 옮기기·뽑기의 Disconnect 대상)
	fromIn           bool
	x, y             float64
	x0, y0           float64 // 눌림 시작(화면 좌표) — 탭 이동량 판정
	t0               float64 // 눌림 시작 시각 — 탭 눌림 시간 판정
	pop              gainPop
}

// gainPop — 입력 잭 탭으로 연 게인 팝업. 대상은 (dst, dp) 입력 포트의 마지막 케이블
// (openGainPop이 정한다). label은 열릴 때 1회 구성하는 출처 표시(문자열 캐시 관례 —
// Update는 무할당), val은 숫자 표시(값 변화 시에만 재구성, Draw에서).
type gainPop struct {
	on        bool
	src, sp   int // 대상 케이블의 출력 쪽(출처 표시·Connect 재송신용)
	dst, dp   int // 대상 입력 잭(팝업 닻·채널 식별)
	drag      bool
	ptr       int
	grabVal   float64 // 드래그 시작값(비결속 = 게인, 결속 = 파라미터 값)
	grabY     float64 // 드래그 시작 화면 y
	sent      float32 // 마지막 송신값(변화 시에만 보낸다 — 노브 lastSent 관례)
	label     string  // "<SRC 장치> <출력 라벨> -> <DST 장치> <입력 라벨>"
	valKey    int32   // 숫자 문자열 캐시 키(값·결속 비트)
	boundName string  // 결속 케이블이면 그 게인을 도는 앞면 노브 라벨(없으면 "")
	val       string
}

// pendConn — 보낸 Connect의 판정 대기. 다음 표 재독에서 그 케이블이 없으면 거부 피드백.
type pendConn struct {
	on      bool
	src, sp int
	dst, dp int
	t       float64
}

// cableGroup — 같은 (밴드, 알파 단계) 케이블의 스트로크 묶음. vector.Path를 재사용해
// 그리기 비용을 색 그룹 수로 묶는다(Reset은 용량을 남긴다 — 프레임 간 재활용).
type cableGroup struct {
	band  uint8 // src 슬롯 번호(colCable 인덱스)
	aStep uint8
	path  vector.Path
}

// loadRear — 뒷면 레이아웃 로드(newView에서 1회). 이름판 라벨(대문자)도 여기서 1회
// 변환해 둔다 — Draw에서 strings.ToUpper를 부르면 프레임마다 문자열이 새로 할당된다.
func (v *View) loadRear() error {
	l, err := core.LoadRearLayout(assets.DeviceRearJSON)
	if err != nil {
		return fmt.Errorf("device: 뒷면 레이아웃 파싱: %w", err)
	}
	for i := range l.Devices {
		l.Devices[i].Name = strings.ToUpper(l.Devices[i].Name)
	}
	v.rearL = l
	return nil
}

// rearFrame — Update 꼬리의 뒷면 프레임 정리: 관측 카운터 리셋 + 케이블 표 동기화 +
// 팝업 게인 드래그 송신. 표 동기화가 먼저다 — 대상 케이블이 이 프레임에 사라졌으면(다른
// 경로로 뽑혔으면) 송신 단계가 popCable에서 걸러져 죽은 케이블을 되살리는 Connect가
// 나가지 않는다. 같은 프레임에 보낸 Connect(release가 Bridge.Cmd로 미러를 바꾼 뒤)도
// rearSync에서 판정한다.
func (v *View) rearFrame(ctx *core.Ctx) {
	v.rearDraws = 0
	v.rearSync(ctx)
	if v.jackDrag.pop.on && v.jackDrag.pop.drag {
		v.popGainSend(ctx, v.jackDrag.y)
	}
}

// rearSync — 케이블 표 동기화(읽기 계약의 단일 소유자). 뒷면이 아니면 읽지 않고,
// 위상 리비전(RackRev)이 변했을 때만(진입 첫 회 포함) Bridge.Cables로 다시 읽는다.
// pendConn이 걸려 있으면 재독한 표에서 그 케이블을 찾아 성공(침묵)·거부(rejT 점화)를
// 가린다 — 뷰는 순환 판정을 재현하지 않고 결과만 읽는다. 재독한 표에 팝업 대상 케이블이
// 없으면 팝업을 닫는다(스펙 닫힘 조건 "대상 케이블이 표에서 사라지면" — 판정은 이 자리,
// 표를 다시 읽는 곳 안에서만).
func (v *View) rearSync(ctx *core.Ctx) {
	if !v.rear {
		return
	}
	rev := ctx.Bridge.RackRev()
	if v.cableRevOK && rev == v.cableRev {
		return
	}
	v.cableRev, v.cableRevOK = rev, true
	v.nCables = ctx.Bridge.Cables(v.cables[:])
	if v.jackDrag.pop.on && v.popCable() == nil {
		v.jackDrag.pop = gainPop{}
	}
	if !v.pendConn.on {
		return
	}
	p := v.pendConn
	v.pendConn.on = false
	for i := 0; i < v.nCables; i++ {
		c := &v.cables[i]
		if int(c.Src) == p.src && int(c.SP) == p.sp && int(c.Dst) == p.dst && int(c.DP) == p.dp {
			return // 표에 있다 — 연결 성공
		}
	}
	v.rejSlot, v.rejPort, v.rejIn, v.rejT = p.dst, p.dp, true, ctx.Now
}

// pressRear — 뒷면 누름 우선순위(§14.3): 게인 팝업(열려 있으면 판이 잭·이름판보다 위) >
// 잭(입·출력) > 장치 이름판 > 빈 판 = 스크롤. 판 밖 누름은 팝업을 닫고 정상 우선순위로
// 계속 간다(삼키지 않는다 — 이름판 탭으로 앞면 복귀 같은 다른 제스처가 팝업 때문에 죽지
// 않게). 출력 잭은 새 케이블 드래그 시작. 입력 잭은 그 포트로 들어오는 케이블 중 표의 마지막
// (가장 최근 삽입 — §14.1 합산 순서 = 삽입 순서)을 잡는다; 들어오는 케이블이 없으면
// 이름판·스크롤 우선순위로 내려간다.
func (v *View) pressRear(ctx *core.Ctx, p *core.Pointer, si int, y float64) {
	st := &v.ptrs[si]
	st.kind, st.idx = pkNone, -1
	if v.rearL != nil {
		if v.jackDrag.pop.on {
			c := v.popCable()
			r := v.gainPopRect(ctx.Bridge)
			if c == nil || r[2] <= 0 {
				v.jackDrag.pop = gainPop{} // 대상 소실(재독 전이라도) — 닫는다
			} else if r.Contains(p.X, y) {
				po := &v.jackDrag.pop
				po.drag, po.ptr, po.grabY = true, p.ID, p.Y
				g := v.popGrabValue(ctx, c)
				po.grabVal, po.sent = g, float32(g)
				v.jackDrag.x, v.jackDrag.y = p.X, p.Y // 제스처 시작값 — 이동이 없으면 첫 송신도 없다
				st.kind = pkJack                      // 이동·놓기는 잭 드래그와 같은 경로로 back.go에 온다
				return
			} else {
				v.jackDrag.pop = gainPop{} // 판 밖 누름 = 닫기. 누름 자체는 아래 우선순위로 계속
			}
		}
		if slot, port, in, ok := v.hitJack(ctx.Bridge, p.X, y); ok {
			if !in {
				v.jackDrag = jackDrag{on: true, ptr: p.ID, srcSlot: slot, srcPort: port,
					x: p.X, y: p.Y, x0: p.X, y0: p.Y, t0: ctx.Now}
				st.kind = pkJack
				return
			}
			for i := v.nCables - 1; i >= 0; i-- {
				c := &v.cables[i]
				if int(c.Dst) == slot && int(c.DP) == port {
					v.jackDrag = jackDrag{on: true, ptr: p.ID, srcSlot: int(c.Src), srcPort: int(c.SP),
						dstSlot: slot, dstPort: port, fromIn: true,
						x: p.X, y: p.Y, x0: p.X, y0: p.Y, t0: ctx.Now}
					st.kind = pkJack
					return
				}
			}
		}
		for i := range v.rearL.Devices {
			if v.rearL.Devices[i].Plate.Contains(p.X, y) {
				st.kind, st.idx = pkRearPlate, i
				return
			}
		}
	}
	// 빈 판 — 앞면 press 꼬리와 같은 스크롤 잡기(상태·규칙은 scroll.go 소유 그대로).
	if v.scrollMax > 0 && !v.scrollHeld() {
		st.kind, st.lastY = pkScroll, p.Y
		v.scrollV = 0
		v.scrollShowUntil = ctx.Now + scrollIndDur
	}
}

// releaseJack — 잭 드래그 놓기. 팝업 게인 드래그 중이면 마지막 이동분까지 반영해 값만
// 확정하고 판은 유지(노브 놓기와 같은 마감). 케이블 드래그: **출발한 입력 잭과 같은 곳에
// 놓으면 랙을 전혀 건드리지 않는다**(P5-gain 결함 봉쇄 — 예전엔 같은 자리 놓기도
// Disconnect+Connect(Unbound, 1.0) 재연결로 결속을 끊고 게인을 튀게 했다) — 그중 탭(짧고
// 제자리)이면 게인 팝업을 연다. 다른 입력 잭에 놓으면 Connect(fromIn이면 먼저 옛 자리
// Disconnect — 자리 옮기기). 입력 잭이 아닌 곳에 놓으면 fromIn일 때만 뽑기(Disconnect).
// hitJack이 실제 포트 수 밖의 잭은 돌려주지 않으므로 "죽은 잭에 놓기"도 뽑기로 흘러간다.
func (v *View) releaseJack(ctx *core.Ctx, p *core.Pointer) {
	if po := &v.jackDrag.pop; po.on && po.drag {
		if po.ptr == p.ID {
			v.popGainSend(ctx, p.Y)
			po.drag = false
		}
		return
	}
	d := v.jackDrag
	v.jackDrag = jackDrag{}
	if !d.on || d.ptr != p.ID {
		return
	}
	y := p.Y + v.scrollY
	if slot, port, in, ok := v.hitJack(ctx.Bridge, p.X, y); ok && in {
		if d.fromIn && slot == d.dstSlot && port == d.dstPort {
			// 같은 잭 — 자리 옮기기가 아니다: 랙 무동작. 탭이면 게인 팝업(§14.3 남은 항목).
			if ctx.Now-d.t0 < tapDur && math.Hypot(p.X-d.x0, p.Y-d.y0) < tapSlop {
				v.openGainPop(ctx, slot, port)
			}
			return
		}
		if d.fromIn {
			ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Disconnect,
				A: uint8(d.srcSlot), B: uint8(d.dstSlot), C: uint8(d.srcPort | d.dstPort<<4)}, core.Human)
		}
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Connect,
			A: uint8(d.srcSlot), B: uint8(slot), C: uint8(d.srcPort | port<<4),
			D: uint8(engine.Unbound), V: 1}, core.Human)
		v.pendConn = pendConn{on: true, src: d.srcSlot, sp: d.srcPort, dst: slot, dp: port, t: ctx.Now}
		return
	}
	if d.fromIn {
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Disconnect,
			A: uint8(d.srcSlot), B: uint8(d.dstSlot), C: uint8(d.srcPort | d.dstPort<<4)}, core.Human)
	}
}

// openGainPop — (dst, dp) 입력 잭의 게인 팝업. 대상은 그 포트에 꽂힌 표의 마지막 케이블
// (pressRear가 잡는 것과 같은 규칙 — §14.1 합산 순서 = 삽입 순서). 없으면 그냥 열지
// 않는다(빈 입력 잭 탭 무동작). 출처 라벨은 여기서 1회 구성한다(문자열 캐시 관례 — 제스처
// 프레임의 할당은 허용, Update 상시 경로 무할당은 그대로).
func (v *View) openGainPop(ctx *core.Ctx, dst, dp int) {
	for i := v.nCables - 1; i >= 0; i-- {
		c := &v.cables[i]
		if int(c.Dst) != dst || int(c.DP) != dp {
			continue
		}
		v.jackDrag.pop = gainPop{on: true, src: int(c.Src), sp: int(c.SP),
			dst: dst, dp: dp, valKey: -1}
		v.jackDrag.pop.label = v.popLabel(ctx.Bridge, c)
		// 결속 케이블이면 어느 노브가 이 게인을 도는지 — 팝업 열 때 한 번만 유도한다
		// (core.ParamKnobName은 KnobParam의 역인덱스라 앞면 노브 라벨과 어긋날 수 없다).
		// 패널에 자리가 없는 파라미터(딜레이 센드 등)는 이름이 없어 "BOUND"로 갈음한다.
		v.jackDrag.pop.boundName = ""
		if c.Bind < uint8(engine.NumParams) {
			if n, ok := v.layout.ParamKnobName(engine.ParamID(c.Bind)); ok {
				v.jackDrag.pop.boundName = n
			}
		}
		return
	}
}

// popLabel — 출처 표시 "<SRC 장치> <출력 라벨> -> <DST 장치> <입력 라벨>". 이름·라벨의
// 단일 소유자는 rear.json이다(뷰가 문자열을 새로 만들지 않는다 — 구분자만 붙인다).
func (v *View) popLabel(b core.Bridge, c *core.RackCable) string {
	if v.rearL == nil {
		return ""
	}
	sd, dd := v.rearL.RearDeviceAt(int(c.Src)), v.rearL.RearDeviceAt(int(c.Dst))
	if sd == nil || dd == nil {
		return ""
	}
	sj := v.rearJackAtPort(b, int(c.Src), int(c.SP), false)
	dj := v.rearJackAtPort(b, int(c.Dst), int(c.DP), true)
	if sj == nil || dj == nil {
		return ""
	}
	return sd.Name + " " + sj.Name + popArrow + dd.Name + " " + dj.Name
}

// popCable — 재동기화한 표에서 팝업 대상 케이블. 없으면 nil(드래그 송신·그리기가 조용히
// 취소되고, rearSync의 재독 자리에서 팝업이 닫힌다).
func (v *View) popCable() *core.RackCable {
	po := &v.jackDrag.pop
	for i := 0; i < v.nCables; i++ {
		c := &v.cables[i]
		if int(c.Src) == po.src && int(c.SP) == po.sp && int(c.Dst) == po.dst && int(c.DP) == po.dp {
			return c
		}
	}
	return nil
}

// popGrabValue — 게인 드래그가 제어하는 값의 현재값. 비결속 = 케이블 게인(gainQ가 정본),
// 결속 = 결속 파라미터 값(유도 게인의 정본 — 드래그는 파라미터를 움직인다).
func (v *View) popGrabValue(ctx *core.Ctx, c *core.RackCable) float64 {
	if c.Bind < uint8(engine.NumParams) {
		return float64(ctx.Bridge.Param(engine.ParamID(c.Bind)))
	}
	return float64(c.Gain)
}

// popGainSend — 팝업 게인 드래그 송신(값 변화 시에만 — 노브 lastSent 관례). 위로 끌수록
// 값이 오른다. 비결속은 같은 끝점의 Connect(engine의 connect가 그 자리를 갱신 — 케이블
// 수·삽입 순서 불변), 결속은 SetParam(Connect의 D로 결속을 다시 쓰면 안 된다 — 드래그로
// 결속이 풀린다). pendConn을 남기지 않는다: 케이블은 이미 있고 게인 갱신은 거부될 수 없다.
func (v *View) popGainSend(ctx *core.Ctx, curY float64) {
	po := &v.jackDrag.pop
	c := v.popCable()
	if c == nil {
		return
	}
	val := po.grabVal + (po.grabY-curY)/popDragRange
	if val < 0 {
		val = 0
	} else if val > 1 {
		val = 1
	}
	f := float32(val)
	if f == po.sent {
		return
	}
	po.sent = f
	if c.Bind < uint8(engine.NumParams) {
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SetParam, A: c.Bind, V: f}, core.Human)
		return
	}
	ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Connect,
		A: uint8(po.src), B: uint8(po.dst), C: uint8(po.sp | po.dp<<4),
		D: uint8(engine.Unbound), V: f}, core.Human)
}

// gainPopRect — 팝업판 rect(레이아웃 좌표 — 그리기·히트·화면 클램프의 단일 소스). 닻은
// 대상 입력 잭, 잭 아래가 기본이고 판이 보이는 창([scrollY, scrollY+LogicalH]) 바닥을
// 넘으면 위로 열며(스펙: "잭이 판 아래 절반이면 위로 연다"), 좌우·상하를 창 안으로
// 클램프한다(화면 밖 팝업 봉쇄). 잭이 없으면 폭 0 rect(그리지도 잡지도 않는다).
func (v *View) gainPopRect(b core.Bridge) core.Rect {
	po := &v.jackDrag.pop
	j := v.rearJackAtPort(b, po.dst, po.dp, true)
	if j == nil {
		return core.Rect{}
	}
	x := j.CX - popW/2
	bot := v.scrollY + core.LogicalH
	y := j.CY + j.R + popGap
	if y+popH > bot {
		y = j.CY - j.R - popGap - popH // 아래가 잘리면 위로
	}
	if x < popMargin {
		x = popMargin
	}
	if x+popW > core.LogicalW-popMargin {
		x = core.LogicalW - popMargin - popW
	}
	if y < v.scrollY {
		y = v.scrollY
	}
	if y+popH > bot {
		y = bot - popH
	}
	return core.Rect{x, y, popW, popH}
}

// hitJack — (x, y)에서 가장 가까운 잭 하나(입·출력 통틀어). 히트 반지름 = 잭 r +
// hitJackPad(전 잭 r12 — 지름 32 ≥ 28, §14.3). 슬롯의 실제 포트 수(engine.KindPorts)
// 밖의 잭은 대상이 아니다(그리지 않는 잭은 잡지도 않는다 — layout.go 상한 계약).
func (v *View) hitJack(b core.Bridge, x, y float64) (slot, port int, in bool, ok bool) {
	if v.rearL == nil {
		return 0, 0, false, false
	}
	best := math.MaxFloat64
	for di := range v.rearL.Devices {
		d := &v.rearL.Devices[di]
		nin, nout := engine.KindPorts(b.RackKind(d.Slot))
		for i := range d.In {
			j := &d.In[i]
			if j.Port >= int(nin) {
				continue
			}
			if dd := math.Hypot(x-j.CX, y-j.CY); dd <= j.R+hitJackPad && dd < best {
				best, slot, port, in, ok = dd, d.Slot, j.Port, true, true
			}
		}
		for i := range d.Out {
			j := &d.Out[i]
			if j.Port >= int(nout) {
				continue
			}
			if dd := math.Hypot(x-j.CX, y-j.CY); dd <= j.R+hitJackPad && dd < best {
				best, slot, port, in, ok = dd, d.Slot, j.Port, false, true
			}
		}
	}
	return
}

// rearJackAtPort — 슬롯·포트의 잭(방향별). 레이아웃이 상한: 실제 포트 수 밖은 nil.
// 포트 번호는 배열 순서와 무관하게 Jack.Port로 찾는다.
func (v *View) rearJackAtPort(b core.Bridge, slot, port int, in bool) *core.Jack {
	if v.rearL == nil || slot < 0 || slot >= engine.RackSlots {
		return nil
	}
	d := v.rearL.RearDeviceAt(slot)
	if d == nil {
		return nil
	}
	nin, nout := engine.KindPorts(b.RackKind(slot))
	js, lim := d.Out, int(nout)
	if in {
		js, lim = d.In, int(nin)
	}
	if port < 0 || port >= lim || port >= len(js) {
		return nil
	}
	for i := range js {
		if js[i].Port == port {
			return &js[i]
		}
	}
	return nil
}

// drawRearRack — 뒷면 본문. draw.go의 Draw가 부르고(blit·인디케이터는 앞면과 공유),
// 순서: 패널 → 장치 이름 라벨 → 포트 라벨 → 케이블 → 드래그 중 곡선 → 거부 링 → 안내.
// 라벨은 매 프레임 폰트로 올린다(diffusion이 아닌 앱 글자 — §14.3) — 문자열 자체는
// loadRear에서 만들어 둔 것이라 할당이 없다.
func (v *View) drawRearRack(dst *ebiten.Image, ctx *core.Ctx) {
	if v.rearImg != nil {
		v.op.GeoM.Reset()
		v.op.ColorScale.Reset()
		dst.DrawImage(v.rearImg, &v.op)
	}
	f := ctx.Font
	if v.rearL != nil && f != nil {
		for di := range v.rearL.Devices {
			d := &v.rearL.Devices[di]
			// 장치 이름 — 앞면 섹션 이름판 관례(왼쪽 정렬 + plateInset, 세로 중앙, colInk).
			_, lh := f.Measure(d.Name, labelSectionScale)
			f.Draw(dst, d.Name, d.Plate[0]+plateInset, d.Plate[1]+d.Plate[3]/2-lh/2, labelSectionScale, colInk, core.AlignLeft)
			// 포트 라벨 — 잭 아래(jackLabelDy), 실제 포트 수까지만.
			nin, nout := engine.KindPorts(ctx.Bridge.RackKind(d.Slot))
			for i := range d.In {
				j := &d.In[i]
				if j.Port >= int(nin) {
					continue
				}
				f.Draw(dst, j.Name, j.CX, j.CY+jackLabelDy, labelKnobScale, colLabel, core.AlignCenter)
			}
			for i := range d.Out {
				j := &d.Out[i]
				if j.Port >= int(nout) {
					continue
				}
				f.Draw(dst, j.Name, j.CX, j.CY+jackLabelDy, labelKnobScale, colLabel, core.AlignCenter)
			}
		}
	}
	v.drawCables(dst, ctx)
	v.drawDragCable(dst, ctx.Bridge)
	v.drawReject(dst, ctx)
	if f != nil {
		f.Draw(dst, rearGuide, titleShiftX, rearGuideY, labelBtnScale, colLabel, core.AlignLeft)
	}
	v.drawGainPop(dst, ctx)
}

// appendCable — 케이블 하나의 3차 베지어(스펙 F 수치 그대로): p1=(x1+ctrlOut, y1+sag),
// p2=(x2−ctrlOut, y2+sag), sag = clamp(0.28·거리, 24, 130). x1=출력 잭, x2=입력 잭.
func appendCable(p *vector.Path, x1, y1, x2, y2 float64) {
	sag := cableSagK * math.Hypot(x2-x1, y2-y1)
	if sag < cableSagMin {
		sag = cableSagMin
	} else if sag > cableSagMax {
		sag = cableSagMax
	}
	p.MoveTo(float32(x1), float32(y1))
	p.CubicTo(float32(x1+cableCtrlOut), float32(y1+sag),
		float32(x2-cableCtrlOut), float32(y2+sag),
		float32(x2), float32(y2))
}

// drawCables — 케이블 표 → 베지어(§14.3 게이트: 그려진 수 == 표의 수). 색 = src 슬롯
// 색(colCable — src 슬롯), 알파 = 0.35+0.55·게인을 8단계 양자화 — (슬롯, 단계)를 그룹 키로
// 묶어 그룹당 StrokePath 1회(경로 재사용). 어느 한쪽 잭이 레이아웃·실제 포트 수에 없으면
// 그리지도 rearDraws에 세지도 않는다. 그룹 상한 초과분은 마지막 그룹에 합친다.
func (v *View) drawCables(dst *ebiten.Image, ctx *core.Ctx) {
	if v.rearL == nil {
		return
	}
	for gi := range v.cableGroups {
		v.cableGroups[gi].path.Reset()
	}
	n := 0
	for i := 0; i < v.nCables; i++ {
		c := &v.cables[i]
		if int(c.Src) >= engine.RackSlots {
			continue
		}
		j1 := v.rearJackAtPort(ctx.Bridge, int(c.Src), int(c.SP), false)
		j2 := v.rearJackAtPort(ctx.Bridge, int(c.Dst), int(c.DP), true)
		if j1 == nil || j2 == nil {
			continue
		}
		band := c.Src
		a := cableA0 + cableAK*float64(c.Gain)
		if a < cableA0 {
			a = cableA0
		} else if a > cableAMax {
			a = cableAMax
		}
		step := uint8(a * cableASteps)
		if step >= cableASteps {
			step = cableASteps - 1
		}
		gi := 0
		for ; gi < n; gi++ {
			if v.cableGroups[gi].band == band && v.cableGroups[gi].aStep == step {
				break
			}
		}
		if gi == n {
			if n >= maxCableGroups {
				gi = n - 1 // 상한 — 초과분은 마지막 그룹에 합친다
			} else {
				v.cableGroups[gi].band, v.cableGroups[gi].aStep = band, step
				n++
			}
		}
		appendCable(&v.cableGroups[gi].path, j1.CX, j1.CY, j2.CX, j2.CY)
		v.rearDraws++
	}
	for gi := 0; gi < n; gi++ {
		g := &v.cableGroups[gi]
		q := (float64(g.aStep) + 0.5) / cableASteps // 단계 중심값
		c := colCable[g.band]
		v.cableDrawOpts.ColorScale.Reset()
		v.cableDrawOpts.ColorScale.ScaleWithColor(color.NRGBA{c.R, c.G, c.B, uint8(q*255 + 0.5)})
		vector.StrokePath(dst, &g.path, &v.cableStrokeOpts, &v.cableDrawOpts)
	}
}

// drawDragCable — 드래그 중 케이블: 시작 잭(fromIn이면 잡은 케이블의 src 출력 잭)에서
// 포인터까지 같은 규칙의 곡선 하나. 색 = src 행 밴드, 알파 dragCableA(양자화 없이 단독).
// 끝점은 화면 좌표라 그릴 때 scrollY를 더한다(그리기 좌표계는 레이아웃).
func (v *View) drawDragCable(dst *ebiten.Image, b core.Bridge) {
	if !v.jackDrag.on || v.rearL == nil || v.jackDrag.srcSlot >= engine.RackSlots {
		return
	}
	j := v.rearJackAtPort(b, v.jackDrag.srcSlot, v.jackDrag.srcPort, false)
	if j == nil {
		return
	}
	v.dragPath.Reset()
	appendCable(&v.dragPath, j.CX, j.CY, v.jackDrag.x, v.jackDrag.y+v.scrollY)
	c := colCable[v.jackDrag.srcSlot]
	v.cableDrawOpts.ColorScale.Reset()
	v.cableDrawOpts.ColorScale.ScaleWithColor(color.NRGBA{c.R, c.G, c.B, uint8(dragCableA*255 + 0.5)})
	vector.StrokePath(dst, &v.dragPath, &v.cableStrokeOpts, &v.cableDrawOpts)
}

// rejA — 거부 링 알파(순수 함수 — 단언 대상). 미점화(−1) 0, 점화 프레임 1, 이후
// e^(−6·경과초), rejDur(0.5s) 지나면 0. draw.go polyTrigA와 같은 방식(새 수학 없음).
func rejA(t, now float64) float32 {
	if t < 0 {
		return 0
	}
	e := now - t
	if e <= 0 {
		return 1
	}
	if e >= rejDur {
		return 0
	}
	return float32(math.Exp(-rejDecay * e))
}

// drawReject — 순환 거부 피드백(§14.3): 거부당한 도착 잭에 붉은 링 1회(반지름 r+4).
// 점화 시각(rejT)은 rearSync가 남기고 여기선 감쇠만 계산한다.
func (v *View) drawReject(dst *ebiten.Image, ctx *core.Ctx) {
	a := rejA(v.rejT, ctx.Now)
	if a <= 0 || v.rearL == nil || v.rejSlot >= engine.RackSlots {
		return
	}
	j := v.rearJackAtPort(ctx.Bridge, v.rejSlot, v.rejPort, v.rejIn)
	if j == nil {
		return
	}
	v.rejPath.Reset()
	v.rejPath.Arc(float32(j.CX), float32(j.CY), float32(j.R+hitJackPad), 0, 2*math.Pi, vector.Clockwise)
	v.cableDrawOpts.ColorScale.Reset()
	v.cableDrawOpts.ColorScale.ScaleWithColor(color.NRGBA{colReject.R, colReject.G, colReject.B, uint8(float64(a)*255 + 0.5)})
	vector.StrokePath(dst, &v.rejPath, &v.cableStrokeOpts, &v.cableDrawOpts)
}

// appendRoundRect — 둥근 사각 경로(모서리 호 4개, 시계 방향). 무할당(호출자의 Path 재사용).
func appendRoundRect(p *vector.Path, r core.Rect, rad float64) {
	x0, y0, x1, y1 := r[0], r[1], r[0]+r[2], r[1]+r[3]
	p.MoveTo(float32(x0+rad), float32(y0))
	p.LineTo(float32(x1-rad), float32(y0))
	p.Arc(float32(x1-rad), float32(y0+rad), float32(rad), -math.Pi/2, 0, vector.Clockwise)
	p.LineTo(float32(x1), float32(y1-rad))
	p.Arc(float32(x1-rad), float32(y1-rad), float32(rad), 0, math.Pi/2, vector.Clockwise)
	p.LineTo(float32(x0+rad), float32(y1))
	p.Arc(float32(x0+rad), float32(y1-rad), float32(rad), math.Pi/2, math.Pi, vector.Clockwise)
	p.LineTo(float32(x0), float32(y0+rad))
	p.Arc(float32(x0+rad), float32(y0+rad), float32(rad), math.Pi, 1.5*math.Pi, vector.Clockwise)
	p.Close()
}

// drawGainPop — 게인 팝업판(§14.3 남은 항목). ① 출처(rear.json 이름·라벨) ② 게인 막대
// ③ 숫자(소수 둘째 자리). 결속 케이블이면 숫자 뒤에 그 게인을 도는 **앞면 노브 이름**을 붙인다
// (core.ParamKnobName — KnobParam의 역인덱스). 이름 없는 파라미터만 "BOUND". 막대·숫자의
// 값은 표에서 매 프레임 읽는다(결속은 SetParam → 다음 재독 표가 따라온다 — 뷰는 랙을
// 흉내 내지 않는다). 색은 전부 기존 토큰: 판 = 표시창 밑판(colDispWin), 막대 채움 =
// colLCD, 막대 트랙 = colChordDim, 글자 = colLabel. 경로 버퍼는 dragPath를 쓴다 — 이
// 시점에 드래그 곡선은 이미 스트로크됐고(팝업 드래그 중엔 케이블 드래그가 병행 불가),
// Reset이 앞선다(무할당 관례). 숫자 문자열은 값 변화 시에만 재구성(문자열 캐시 관례).
func (v *View) drawGainPop(dst *ebiten.Image, ctx *core.Ctx) {
	po := &v.jackDrag.pop
	if !po.on {
		return
	}
	f := ctx.Font
	if f == nil || v.rearL == nil {
		return
	}
	c := v.popCable()
	if c == nil {
		return
	}
	r := v.gainPopRect(ctx.Bridge)
	if r[2] <= 0 {
		return
	}
	v.dragPath.Reset()
	appendRoundRect(&v.dragPath, r, popRad)
	v.cableDrawOpts.ColorScale.Reset()
	v.cableDrawOpts.ColorScale.ScaleWithColor(colDispWin[0])
	vector.FillPath(dst, &v.dragPath, nil, &v.cableDrawOpts)
	// ① 출처 — 라벨판 폭에 맞춰 축소 하한까지 들어간다(labelScale 관례, draw.go).
	cx := r[0] + r[2]/2
	f.Draw(dst, po.label, cx, r[1]+popPad+7, labelScale(f, po.label, labelKnobScale, r[2]-2*popPad), colLabel, core.AlignCenter)
	// ② 게인 막대 — 값은 표의 유도 게인(결속 케이블은 파라미터를 따라간다).
	g := float64(c.Gain)
	if g < 0 {
		g = 0
	} else if g > 1 {
		g = 1
	}
	by := r[1] + popPad + 14 + 6
	bw := r[2] - 2*popPad
	v.fillRectA(dst, core.Rect{r[0] + popPad, by, bw, popBarH}, colChordDim)
	v.fillRect(dst, core.Rect{r[0] + popPad, by, bw * g, popBarH}, colLCD)
	// ③ 숫자(+BOUND) — 소수 둘째. 키 = 값 100단계 | 결속 비트(비트는 자릿수 계산과 분리).
	n := int32(g*100 + 0.5)
	if n > 100 {
		n = 100
	}
	key := n
	bound := c.Bind < uint8(engine.NumParams)
	if bound {
		key |= 1 << 16
	}
	if key != po.valKey || po.val == "" {
		po.valKey = key
		b := v.scratch[:0]
		if n >= 100 {
			b = append(b, '1', '.', '0', '0')
		} else {
			b = append(b, '0', '.', byte('0'+n/10), byte('0'+n%10))
		}
		if bound {
			// 어느 노브가 이 케이블을 도는가 — 사람이 "여기 값이 왜 저 노브를 따라가지"로
			// 헷갈리지 않게 이름을 보여준다. 이름이 없는 파라미터만 "BOUND"로 갈음.
			if po.boundName != "" {
				b = append(b, ' ')
				b = append(b, "<- "...) // 폰트에 화살표 글리프가 없다(브라우저 실측 ??? — 라벨의 "->"와 같은 ASCII로)
				b = append(b, po.boundName...)
			} else {
				b = append(b, " BOUND"...)
			}
		}
		po.val = string(b)
	}
	f.Draw(dst, po.val, cx, r[1]+popH-popPad-7, labelKnobScale, colLabel, core.AlignCenter)
}
