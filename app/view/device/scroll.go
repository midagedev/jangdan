// scroll.go — 세로 스크롤 랙(§13.3, P4-scroll). 랙(720×2000, v4)은 화면(720×1280)보다
// 크다: Draw 본문을 전부 rack 오프스크린에 그리고 scrollY만큼 올려 blit한다.
//
// 상태·입력·그리기의 단일 소유자가 이 파일이다(device.go의 View 필드 정의 제외).
// 레이아웃 좌표 변환(화면 y + scrollY)은 press()의 히트 판정 한 곳뿐이다 — 제스처
// (노브 드래그·스크롤 델타)는 화면 좌표계로 잰다: 스크롤이 랙을 옮기는 중에도 손가락의
// 화면 이동만 재야 프레임 되먹임이 없다(사본을 레이아웃 좌표로 만들면 매 프레임
// scrollY가 델타에 끼어 드래그가 절반씩 죽는다 — scroll_test.go 실측). Draw 안에서
// scrollY를 다시 더하거나 빼지 않는다(구조 봉쇄: 이중 변환 부류를 원천 차단).
// 헤더(이름판·스코프·코드 트랙)도 랙과 함께 스크롤한다 — 좌표계가 하나뿐이다.
//
// 우선순위(§13.3): 컨트롤(노브·버튼·패드·표시창·코드 트랙·이름판) 히트가 이기고,
// 빈 판을 잡은 드래그만 스크롤이다. 노브에서 시작한 드래그는 끝까지 노브다.
package device

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/jangdan/app/core"
)

// 스크롤 수치 계약(§13.3 스펙 origin).
const (
	scrollWheelStep = 60.0 // 휠 한 틱(px)
	scrollFriction  = 0.9  // 관성 감쇠(×/프레임)
	scrollStopV     = 2.0  // 이 속도(px/프레임) 미만이면 정지
	scrollIndDur    = 1.0  // 인디케이터 잔류(초, 스크롤 중 + 1초)
	scrollIndW      = 4.0  // 인디케이터 폭(px) — 화면 오른쪽 가장자리 x 716..719
)

// colScrollInd — 인디케이터 색: colText(#E8E2D2, 방 뷰와 같은 잉크 계열) α0.5(§13.3).
var colScrollInd = color.NRGBA{R: 0xE8, G: 0xE2, B: 0xD2, A: 128}

// wheelScroll — 휠·트랙패드 스크롤. main.go 수정 없이 device.Update 안에서 매 프레임
// ebiten.Wheel()을 읽는다. 휠은 관성을 만들지 않는다(즉시 정지 — 브라우저가 이미
// 트랙패드 관성을 이벤트로 흘려 주므로 이중 감쇠가 된다).
func (v *View) wheelScroll(ctx *core.Ctx) {
	if v.scrollMax <= 0 {
		return
	}
	_, wy := ebiten.Wheel()
	if wy == 0 || wy != wy { // NaN 방어(방어 2) — clamp는 NaN을 못 걷는다(비교가 전부 거짓)
		return
	}
	v.scrollY = v.clampScroll(v.scrollY - wy*scrollWheelStep)
	v.scrollV = 0
	v.scrollShowUntil = ctx.Now + scrollIndDur
}

// stepScroll — 관성: 놓인 뒤 속도 감쇠(×0.9/프레임, |v| < 2px/프레임에서 정지).
// 스크롤 포인터를 잡은 중에는 손가락이 직접 움직이므로 쉰다(이중 이동 방지).
// 경계에 닿아 더 못 가면 즉시 정지한다(반발 없음).
func (v *View) stepScroll(ctx *core.Ctx) {
	if v.scrollV == 0 || v.scrollMax <= 0 || v.scrollHeld() {
		return
	}
	ny := v.clampScroll(v.scrollY - v.scrollV)
	if ny == v.scrollY {
		v.scrollV = 0
		return
	}
	v.scrollY = ny
	v.scrollV *= scrollFriction // 부호 보존 — 계속 같은 방향으로 감쇠한다(되튐 없음)
	a := v.scrollV
	if a < 0 {
		a = -a
	}
	if a < scrollStopV {
		v.scrollV = 0
	}
	v.scrollShowUntil = ctx.Now + scrollIndDur
}

// clampScroll — scrollY 경계 클램프(0..scrollMax). NaN·음수 → 0, 상한 초과 → scrollMax.
// scrollMax 0(구 레이아웃, 높이 ≤ 화면)이면 항상 0 — 스크롤 없음(방어 1).
func (v *View) clampScroll(y float64) float64 {
	if y != y || y < 0 {
		return 0
	}
	if y > v.scrollMax {
		return v.scrollMax
	}
	return y
}

// scrollHeld — 이 순간 스크롤을 잡은 포인터가 있는가(첫 것만 산다는 규칙의 판정).
func (v *View) scrollHeld() bool {
	for i := 0; i < v.nptrs; i++ {
		if v.ptrs[i].kind == pkScroll {
			return true
		}
	}
	return false
}

// scrollIndGeom — 인디케이터 기하(§13.3): 길이 = 화면높이/랙높이×화면높이,
// y = scrollY/max×(화면높이 − 길이). 순수 함수로 떼어 테스트 가능하게 했다.
func scrollIndGeom(scrollY, max, layoutH float64) (y, h float64) {
	h = float64(core.LogicalH) * float64(core.LogicalH) / layoutH
	if max > 0 {
		y = scrollY / max * (float64(core.LogicalH) - h)
	}
	return y, h
}

// drawScrollInd — 오른쪽 가장자리 인디케이터. 화면 좌표에 그린다(rack 밖 — 스크롤과
// 함께 움직이지 않는다). 스크롤 중 + 1초 동안만 보인다. 구 레이아웃은 max 0 — 미그림.
func (v *View) drawScrollInd(screen *ebiten.Image, ctx *core.Ctx) {
	if v.scrollMax <= 0 || ctx.Now > v.scrollShowUntil {
		return
	}
	y, h := scrollIndGeom(v.scrollY, v.scrollMax, v.layout.Size[1])
	v.fillRectA(screen, core.Rect{float64(core.LogicalW) - scrollIndW, y, scrollIndW, h}, colScrollInd)
}
