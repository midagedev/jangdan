// meter.go — 라인별 활동 LED·VU 미터(라운드 P3-meters). 계약 원본: docs/impl-plan-2026-09-05.md §12.3 라인 미터.
//
// 레벨의 원본은 core.Tick.Levels(파트 순 블록 피크, 프리 FX)과 Tick.Peak(마스터)다.
// 경로: vuOf 매핑(−36..0dB → 0..1, 입력 방어 포함) → 밸리스틱(어택 즉시·릴리스 vuRelease/s)
// → 표시. 표시는 베이스 A·B 표시창 하단 12세그먼트 띠, 하단 표시창 하단 20세그먼트 띠(마스터),
// 드럼 패드 lit 합성 + 좌상단 LED 점. 띠는 표시창 문자열 캐시(dispImg) 밖에서 화면에 직접
// 매 프레임 그린다 — 캐시 안에 넣으면 문자열 변화 시에만 갱신돼 밸리스틱이 멎는다.
// 색은 계약색 재사용(colLCD α0.9·colLED) — 새 hex를 만들지 않는다.
package device

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/jangdan/app/core"
)

// 수치 계약(스펙 P3-meters origin). 픽셀 절대좌표는 없다 — 표시창·패드 rect(레이아웃 JSON)에서 유도한다.
const (
	vuRelease    = 4.0      // 릴리스 감쇠(/s) — 풀스케일 → 0에 250ms. 어택은 즉시(어택 계수 없음).
	vuFloorDB    = -36.0    // 이 dB 밑은 vu 0
	vuHot        = 5.0 / 6. // 상단 1/6(≥ −6dB) 세그먼트은 colLED — 12개에서 10·11과 같은 규칙을 세그먼트 수에 일반화
	vuSegPad     = 2.0      // 띠 좌우 여백(px)
	vuSegGap     = 1.0      // 세그먼트 간 간격(px)
	vuBandLift   = 2.0      // 띠를 창 바닥에서 올리는 여백(px)
	vuBandH      = 5        // 베이스 표시창 띠 높이(px) — 비전 FIX 2026-09-06: 4→5(꺼진 트랙과 함께 미터로 읽히게)
	vuBandHBot   = 3        // 하단 표시창 띠 높이(px)
	vuSegsBass   = 12       // 베이스 표시창 세그먼트 수
	vuSegsBottom = 20       // 하단 표시창(마스터) 세그먼트 수
	vuTextDy     = 2.0      // 베이스 표시창 텍스트 리프트(px, 중앙에서 위로 — 띠 4px 회피)
	vuTextDyBot  = 1.0      // 하단 표시창 텍스트 리프트(띠가 3px라 2px 올리면 과하다 — slot별 오프셋)
	padLEDInset  = 6.0      // 패드 LED 점 들여쓰기(좌상단 +6,+6, px)
	padLEDR      = 6.0      // 패드 LED 점 반지름(px) — 비전 FIX 2026-09-06: 4→6(약해진 워시 보상)
	padLitMinA   = 0.12     // 패드 레벨 lit 기본 알파: α = 0.12 + 0.5·vu
	padLitVuA    = 0.5
	padLitMaxA   = 0.20 // 탭 lit과의 합성 상한 — 비전 FIX 2026-09-06: 0.62→0.20(점등 패드 라벨 대비 1.93:1 → 판정자 시뮬 3.13:1; 0.22 실측 2.78:1)
	vuOffA       = 41   // 꺼진 세그먼트 알파(0.16×255) — 비전 FIX 2026-09-06: 트랙이 없으면 켜진 1~2칸이 얼룩으로 읽힌다
)

// colVUSeg — VU 세그먼트 기본색 = colLCD α0.9(RGB는 계약색 그대로 — 새 색이 아니다).
var colVUSeg = color.NRGBA{colLCD.R, colLCD.G, colLCD.B, 230}

// colVUOff — 꺼진 세그먼트(트랙) 색 = colLCD α0.16. 창 배경(68,68,72) 위 ≈ (79,92,82).
var colVUOff = color.NRGBA{colLCD.R, colLCD.G, colLCD.B, vuOffA}

// meters — 라인 VU 밸리스틱 상태. 값 타입 배열 — 할당 0.
type meters struct {
	disp   [core.NumLevels]float32 // 파트 순 표시값(Tick.Levels와 같은 순서 — engine.Part 값이 곧 인덱스)
	master float32                 // 하단 표시창(Tick.Peak)
}

// vuOf — 레벨 → VU 0..1. dB = 20·log10(level), −36dB→0 · 0dB→1 선형 보간.
// 입력 방어: NaN·0·음수 → 0, 1 초과(클리핑 전 프리 FX) → 1 상한. 순수 함수(단언 대상).
func vuOf(level float32) float32 {
	if !(level > 0) { // NaN 포함 — NaN과의 비교는 거짓이라 이 가드가 함께 잡는다
		return 0
	}
	if level > 1 {
		level = 1
	}
	v := (20*math.Log10(float64(level)) - vuFloorDB) / -vuFloorDB
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return float32(v)
}

// vuStep — 밸리스틱 1프레임: 표시값 = max(vu, 표시값 − dt·vuRelease), 하한 0.
// 어택은 즉시(새 vu가 크면 그 프레임에 바로). 순수 함수(단언 대상).
func vuStep(vu, disp, dt float32) float32 {
	d := disp - dt*vuRelease
	if d < 0 {
		d = 0
	}
	if vu > d {
		return vu
	}
	return d
}

// update — 파트 8개 + 마스터 밸리스틱 1프레임(Update에서). 구 호스트(Levels 전부 0)는
// vu가 전부 0이라 표시값은 시작값 0에서 움직이지 않는다(밸리스틱 잔상도 없다).
func (m *meters) update(t core.Tick, dt float32) {
	for i := range m.disp {
		m.disp[i] = vuStep(vuOf(t.Levels[i]), m.disp[i], dt)
	}
	m.master = vuStep(vuOf(t.Peak), m.master, dt)
}

// vuSegsOn — 띠에 켜질 세그먼트 수 = round(vu×segs)(상한 segs). 0이면 그리지 않는다. 순수 함수.
func vuSegsOn(vu float32, segs int) int {
	if vu <= 0 {
		return 0
	}
	n := int(math.Round(float64(vu) * float64(segs)))
	if n > segs {
		return segs
	}
	return n
}

// padLitAlpha — 패드 lit 합성 알파: max(탭 lit, 0.12+0.5·vu) 상한 padLitMaxA. 레벨 0이면 탭 lit만
// (레벨 꺼짐이 탭 피드백을 지우지 않는다 — 소거 계약은 그림 픽셀 쪽에만). 순수 함수(단언 대상).
func padLitAlpha(tapA, vu float32) float32 {
	a := tapA
	if vu > 0 {
		lv := padLitMinA + padLitVuA*vu
		if lv > a {
			a = lv
		}
	}
	if a > padLitMaxA {
		return padLitMaxA
	}
	return a
}

// drawMeters — 표시창 3개 하단의 VU 띠(베이스 A·B 12세그먼트, 하단 마스터 20세그먼트).
// Draw에서 표시창 blit 뒤에 — 띠는 화면에 직접(dispImg 캐시와 무관하게 매 프레임).
func (v *View) drawMeters(screen *ebiten.Image, ctx *core.Ctx) {
	for s := 0; s < 2; s++ {
		if v.hasDisp[s] {
			v.drawVuBand(screen, v.dispRects[s], v.meters.disp[s], vuSegsBass, vuBandH)
		}
	}
	v.drawVuBand(screen, v.botRect, v.meters.master, vuSegsBottom, vuBandHBot)
}

// drawVuBand — 창 r 하단의 VU 세그먼트 띠. 세그먼트 폭 = (창 폭 − 2×여백 − (segs−1)×간격)/segs,
// 높이 bandH, 창 바닥에서 vuBandLift 위. 꺼진 세그먼트는 항상 colVUOff 트랙으로 그린다(비전 FIX
// 2026-09-06). 켜진 세그먼트 0..hot−1은 colVUSeg(colLCD α0.9), hot..은 colLED.
// meterDraws는 켜진 세그먼트만 센다(트랙은 레벨과 무관한 상시 요소 — 소거 계약 밖).
func (v *View) drawVuBand(screen *ebiten.Image, r core.Rect, vu float32, segs, bandH int) {
	if r[2] <= 0 {
		return
	}
	lit := vuSegsOn(vu, segs)
	v.meterDraws += lit // 그리기 결정 카운터 — 헤드리스 테스트 근거(room 뷰 draws 관례)
	hot := int(math.Round(float64(segs) * vuHot))
	segW := (r[2] - 2*vuSegPad - vuSegGap*float64(segs-1)) / float64(segs)
	y := r[1] + r[3] - vuBandLift - float64(bandH)
	for i := 0; i < segs; i++ {
		c := colVUOff
		if i < lit {
			c = colVUSeg
			if i >= hot {
				c = colLEDOn
			}
		}
		v.fillRectA(screen, core.Rect{r[0] + vuSegPad + float64(i)*(segW+vuSegGap), y, segW, float64(bandH)}, c)
	}
}

// drawPadLED — 패드 좌상단 안쪽(+6,+6) r4 LED 점(colLED, α = vu). 레벨 0이면 호출자가 그리지
// 않는다. 스프라이트는 New에서 1회 — newView(테스트) 경로는 nil이라 결정 카운터만 남는다.
func (v *View) drawPadLED(screen *ebiten.Image, r core.Rect, vu float32) {
	v.meterDraws++
	if v.padLEDImg == nil {
		return
	}
	v.op.GeoM.Reset()
	v.op.GeoM.Translate(r[0]+padLEDInset, r[1]+padLEDInset)
	v.op.ColorScale.Reset()
	// 프리멀티플라이드 스프라이트를 (vu,vu,vu,vu)로 축소 = 점 전체를 알파 vu로(overlayRect와 같은 계약).
	v.op.ColorScale.Scale(vu, vu, vu, vu)
	screen.DrawImage(v.padLEDImg, &v.op)
}
