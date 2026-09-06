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
	hitJackPad     = 4.0  // 잭 히트 여유 — r12+4 = 지름 32 ≥ 28(§14.3)
	jackLabelDy    = 20.0 // 포트 라벨 y 오프셋(잭 중심 아래)
	rearGuideY     = 60.0 // 상단 안내 문구 y(빈 띠 y 0..120의 중앙 — 첫 행은 133부터)
	cableW         = 5.0  // 케이블 선 굵기
	cableCtrlOut   = 60.0 // 베지어 제어점 수평 오프셋(출력 잭 → 바깥, 입력 잭 → 바깥)
	cableSagK      = 0.28 // 늘어짐 = 두 잭 거리 × 계수
	cableSagMin    = 24.0
	cableSagMax    = 130.0
	cableA0        = 0.35 // 케이블 알파 = 0.35 + 0.55·게인(하한·상한이 이 값들)
	cableAK        = 0.55
	cableAMax      = 0.9
	cableASteps    = 8   // 알파 양자화 단계 — 색 그룹 키(그룹당 스트로크 1회)
	// 색 그룹 상한 = 케이블 수 상한. 그룹 묶기는 스트로크 호출을 줄이는 최적화일 뿐이므로,
	// 상한이 모자라면 색이 조용히 틀어진다(초과분이 남의 색으로 그려진다). 기본 랙 32케이블이
	// 이미 16그룹을 정확히 채우는 것을 실측해(TestCableGroupCount) 상한을 케이블 수로 올렸다 —
	// 최악이 케이블당 스트로크 1회, 즉 묶기가 없던 것과 같다.
	maxCableGroups = engine.RackCables
	dragCableA     = 0.9 // 드래그 중 케이블 알파(양자화 밖 — 단독 스트로크)
	rejDur         = 0.5 // 거부 피드백 표시 시간(초)
	rejDecay       = 6.0 // 거부 링 감쇠율(/초) — draw.go polyTrigA와 같은 e^(−k·t) 방식
)

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
	{112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF},
	{112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF}, {112, 143, 116, 0xFF},
}

// jackDrag — 진행 중인 케이블 드래그. fromIn=false: 출력 잭에서 새 케이블 늘리기,
// fromIn=true: 입력 잭에 꽂힌 기존 케이블(src는 그 케이블의 출력 쪽, dst는 옛 도착지)을
// 잡아 옮기는 중. x, y는 화면 좌표(제스처 계약 — 그릴 때 scrollY를 더한다).
type jackDrag struct {
	on               bool
	ptr              int
	srcSlot, srcPort int
	dstSlot, dstPort int // fromIn일 때만 유효(옛 도착지 — 자리 옮기기·뽑기의 Disconnect 대상)
	fromIn           bool
	x, y             float64
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

// rearFrame — Update 꼬리의 뒷면 프레임 정리: 관측 카운터 리셋 + 케이블 표 동기화.
// 같은 프레임에 보낸 Connect(release가 Bridge.Cmd로 미러를 바꾼 뒤)도 여기서 판정한다.
func (v *View) rearFrame(ctx *core.Ctx) {
	v.rearDraws = 0
	v.rearSync(ctx)
}

// rearSync — 케이블 표 동기화(읽기 계약의 단일 소유자). 뒷면이 아니면 읽지 않고,
// 위상 리비전(RackRev)이 변했을 때만(진입 첫 회 포함) Bridge.Cables로 다시 읽는다.
// pendConn이 걸려 있으면 재독한 표에서 그 케이블을 찾아 성공(침묵)·거부(rejT 점화)를
// 가린다 — 뷰는 순환 판정을 재현하지 않고 결과만 읽는다.
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

// pressRear — 뒷면 누름 우선순위(§14.3): 잭(입·출력) > 장치 이름판 > 빈 판 = 스크롤.
// 출력 잭은 새 케이블 드래그 시작. 입력 잭은 그 포트로 들어오는 케이블 중 표의 마지막
// (가장 최근 삽입 — §14.1 합산 순서 = 삽입 순서)을 잡는다; 들어오는 케이블이 없으면
// 이름판·스크롤 우선순위로 내려간다.
func (v *View) pressRear(ctx *core.Ctx, p *core.Pointer, si int, y float64) {
	st := &v.ptrs[si]
	st.kind, st.idx = pkNone, -1
	if v.rearL != nil {
		if slot, port, in, ok := v.hitJack(ctx.Bridge, p.X, y); ok {
			if !in {
				v.jackDrag = jackDrag{on: true, ptr: p.ID, srcSlot: slot, srcPort: port, x: p.X, y: p.Y}
				st.kind = pkJack
				return
			}
			for i := v.nCables - 1; i >= 0; i-- {
				c := &v.cables[i]
				if int(c.Dst) == slot && int(c.DP) == port {
					v.jackDrag = jackDrag{on: true, ptr: p.ID, srcSlot: int(c.Src), srcPort: int(c.SP),
						dstSlot: slot, dstPort: port, fromIn: true, x: p.X, y: p.Y}
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

// releaseJack — 잭 드래그 놓기. 입력 잭 위에 놓으면 Connect(fromIn이면 먼저 옛 자리
// Disconnect — 자리 옮기기; 같은 자리에 돌려놓으면 순서대로 재연결된다). 입력 잭이
// 아닌 곳에 놓으면 fromIn일 때만 뽑기(Disconnect). hitJack이 실제 포트 수 밖의 잭은
// 돌려주지 않으므로 "죽은 잭에 놓기"도 뽑기로 흘러간다. 어떤 경우든 jackDrag는 비운다.
func (v *View) releaseJack(ctx *core.Ctx, p *core.Pointer) {
	d := v.jackDrag
	v.jackDrag = jackDrag{}
	if !d.on || d.ptr != p.ID {
		return
	}
	y := p.Y + v.scrollY
	if slot, port, in, ok := v.hitJack(ctx.Bridge, p.X, y); ok && in {
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
