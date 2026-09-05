// lumin.go — 화면 휘도 모델·플래시 게이트(광과민 안전망). 계약 원본: 기획서 05절 광과민 규정.
//
// 방 뷰는 화면녹화되는 머니샷이므로 광과민 규정이 절대 조건이다. 매 프레임 광원 상태
// (스탠드 빛 풀 + 창 점등)에서 화면 휘도 L을 계산해 임계 이상의 상승·하강 전이를
// 링버퍼(1초)에 기록하고, 상승·하강 쌍(방향 반전)이 1초에 3회를 넘으면 그 프레임의
// 광원 변화를 되돌린다(클램프). 전체 화면 휘도 반전(블랙아웃→화이트아웃)을 만드는
// 코드는 이 뷰 어디에도 없다.
package room

import "math"

// timeOfDay — 방의 시간(기획서 05절 "방의 시간"). night 기본 / 휴식=저녁 / 긴 세션=오후.
type timeOfDay uint8

const (
	todNight     timeOfDay = iota
	todEvening             // 포모도로 휴식 — 스탠드 꺼지고 창이 밝음
	todAfternoon           // 세션 45분 초과 — 비 오는 오후
)

// tint — 시간대 전체 ColorScale(스펙 §8 수치).
func (t timeOfDay) tint() (r, g, b float32) {
	switch t {
	case todEvening:
		return 1.0, 0.96, 0.9
	case todAfternoon:
		return 1.08, 1.08, 1.08
	}
	return 1, 1, 1
}

// lampAlpha — 스탠드 빛 알파(evening = 0, 꺼짐).
func (t timeOfDay) lampAlpha() float32 {
	if t == todEvening {
		return 0
	}
	return 1
}

// windowsLit — 창 강제 점등 계수(evening = 전부 켜짐).
func (t timeOfDay) windowsLit() float32 {
	if t == todEvening {
		return 1
	}
	return 0
}

// windowAlpha — 창 켜짐 기본 알파(보통 0.35, evening 0.5).
func (t timeOfDay) windowAlpha() float32 {
	if t == todEvening {
		return 0.5
	}
	return 0.35
}

// rainFloor — 비 밀도 하한(afternoon = 0.6).
func (t timeOfDay) rainFloor() float32 {
	if t == todAfternoon {
		return 0.6
	}
	return 0
}

// 블렌드 헬퍼 — prev→tod 를 k(0..1)로 섞는다(30초 크로스페이드의 보간 원본).
func lerpTint(a, b timeOfDay, k float64) (r, g, b_ float32) {
	ar, ag, ab := a.tint()
	br, bg, bb := b.tint()
	kf := float32(k)
	return ar + (br-ar)*kf, ag + (bg-ag)*kf, ab + (bb-ab)*kf
}

func lerpF32(a, b, k float32) float32 { return a + (b-a)*k }

const (
	// flashThreshold — 전이 판정 상대 임계(10%).
	flashThreshold = 0.10
	// lumFloor — 휘도가 이 값 이하면 전이 검출을 쓰지 않는다(0 나눗셈·노이즈 방어).
	lumFloor = 0.005
	// flashRing — 전이 이벤트 링버퍼 용량(1초치보다 넉넉).
	flashRing = 64
	// flashMaxPairs — 1초 창에서 허용되는 상승·하강 쌍 수(WCAG 2.3.1 초당 3회 초과 금지).
	flashMaxPairs = 3
)

type flashEvent struct {
	t     float64
	rise  bool
	valid bool
}

// flashGate — 밴드 통과 방식의 휘도 전이 검출.
//
// 직전 프레임 Δ가 아니라 국소 극값 기준을 둔다: 200ms 페이드처럼 의도적으로 천천히
// 흘러가는 변화는 프레임 간 Δ가 임계 밑이라 합법이고, 한 프레임에 뛰었다가 되돌아오는
// 변화는 기준점에서 10% 벗어남으로 잡힌다. 인접한 반대 방향 전이(상승·하강 쌍)만 세고
// 같은 방향 연속 전이는 세지 않는다 — 160BPM 박 트리거(초당 2.67회)는 구조상 쌍이
// 3을 넘을 수 없고(기획서가 템포 상한 160과 플래시 상한 3을 짝지은 이유), 그 이상의
// 요동은 잡는다.
type flashGate struct {
	events  [flashRing]flashEvent
	head    int
	n       int
	lastL   float64 // 직전 프레임 휘도(클램프 시 유지할 값)
	refLo   float64 // 상승 판정 기준(국소 최소)
	refHi   float64 // 하강 판정 기준(국소 최대)
	rising  bool    // 직전 전이 방향(true=상승)
	started bool
}

// observe — 이번 프레임 휘도 l(시각 t)을 반영하고, 클램프가 필요하면 true.
// 클램프 시 호출자는 광원 상태를 직전 프레임 값으로 되돌리고 l을 다시 넣지 않는다
// (lastL이 유지된다).
func (g *flashGate) observe(t, l float64) bool {
	if !g.started {
		g.started = true
		g.lastL, g.refLo, g.refHi = l, l, l
		return false
	}
	clamp := false
	if g.refLo > lumFloor {
		if l >= g.refLo*(1+flashThreshold) {
			if g.push(t, true) > flashMaxPairs {
				clamp = true
			}
			if !clamp {
				g.rising, g.refLo, g.refHi = true, l, l
			}
		}
	}
	if !clamp && g.refHi > lumFloor {
		if l <= g.refHi*(1-flashThreshold) {
			if g.push(t, false) > flashMaxPairs {
				clamp = true
			}
			if !clamp {
				g.rising, g.refLo, g.refHi = false, l, l
			}
		}
	}
	if !clamp {
		// 완만한 표류는 기준점을 따라가게 한다(느린 변화가 쌓여 거짓 전이가 되지 않게).
		g.refLo = math.Min(g.refLo, l)
		g.refHi = math.Max(g.refHi, l)
		g.lastL = l
	}
	return clamp
}

// push — 이벤트 기록 후 1초 창의 상승·하강 쌍(방향 반전) 수를 반환.
func (g *flashGate) push(t float64, rise bool) int {
	g.events[g.head] = flashEvent{t: t, rise: rise, valid: true}
	g.head = (g.head + 1) % flashRing
	if g.n < flashRing {
		g.n++
	}
	return g.pairs(t)
}

// pairs — 최근 1초 창에서 인접 반대 방향 전이쌍 수(테스트·계측 노출).
func (g *flashGate) pairs(t float64) int {
	cnt := 0
	var prev flashEvent
	seen := false
	for i := 0; i < g.n; i++ {
		idx := (g.head - 1 - i + 2*flashRing) % flashRing
		e := g.events[idx]
		if !e.valid || t-e.t > 1.0 {
			continue
		}
		if seen && e.rise != prev.rise {
			cnt++
		}
		prev, seen = e, true
	}
	return cnt
}
