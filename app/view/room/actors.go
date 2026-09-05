// actors.go — 고양이 4동작·캐릭터 6동작 상태기계. 계약: 스펙 §5(고양이)·§7(캐릭터).
//
// 포즈 스프라이트가 없으면 플레이트의 rect 서브이미지를 변형(회전·이동·스케일 ≤3%)해
// 동작을 낸다 — 변환 출력(dx,dy,ang,scale)은 state에 저장되고 Draw가 그대로 쓴다.
// ReducedMotion에서는 배우를 정지 상태(변환 0, 슬립 자세의 정적 스케일만)로 그린다.
package room

import (
	"math"

	"github.com/midagedev/revirth/engine"
)

// 스펙 수치(§5·§7). 각도는 도(degree)로 명명 후 라디안으로 환산해 쓴다.
const (
	catTailAmpDeg   = 1.5 // 꼬리 — 템포에 맞춰 항상
	catEarsLiftPx   = 4.0 // 드롭 뒤 2바 — rect 위로
	catEarsBars     = 2
	catSleepScale   = 0.97 // 브레이크다운·휴식 — 잠
	catBreathAmp    = 0.01 // 4초 주기 호흡 ±1%
	catBreathPeriod = 4.0

	charNodAmpDeg    = 1.5 // 끄덕임 — 박마다 1왕복(항상 합성)
	charHeadupDeg    = 3.0 // 드롭 뒤 1바 −3° 유지
	charHeadupBars   = 1
	charReachPx      = 6.0  // 레지던트 손 — 기기 방향 이동
	charReachSec     = 0.5  // 이징
	charManualSec    = 0.3  // MANUAL 잠금 해제 — 손 거둠 복귀
	charStretchScale = 1.03 // 휴식 진입 3초 기지개
	charStretchSec   = 3.0
	charIdleAngDeg   = 2.0 // 30초마다 고양이 방향 1.5초
	charIdleEverySec = 30.0
	charIdleLookSec  = 1.5
)

const (
	deg = math.Pi / 180
)

// catAction — 고양이 기본 동작. layout poses 배열 순서(0..3)와 같다: tail, ears, sleep, sit.
type catAction uint8

const (
	catTail catAction = iota
	catEars
	catSleep
	catSit
)

type catState struct {
	action       catAction
	dx, dy       float64 // ears 리프트
	ang          float64 // tail 회전
	scale        float64 // sleep 호흡 스케일
	earsUntilBar uint32
	breathT      float64
}

// stepCat — 우선순위: sleep(브레이크다운/휴식) > ears(드롭 뒤 2바) > tail(기본) / sit(RM 정지).
// tail 회전은 항상 합성된다.
func (s *state) stepCat(sig Signals, dt float64) {
	c := &s.cat
	if sig.Flags&engine.FlagDrop != 0 {
		c.earsUntilBar = sig.Bar + catEarsBars
	}
	sleep := sig.Phase == 3 || sig.PomodoroRest // 3 = Breakdown
	switch {
	case sleep:
		c.action = catSleep
	case sig.Bar < c.earsUntilBar:
		c.action = catEars
	case s.reduced:
		c.action = catSit
	default:
		c.action = catTail
	}
	if s.reduced { // 정지 상태: 회전·이동 0, 잠자는 자세의 정적 스케일만
		c.dx, c.dy, c.ang = 0, 0, 0
		c.scale = 1
		if sleep {
			c.scale = catSleepScale
		}
		return
	}
	c.breathT += dt
	c.dx = 0
	c.dy = 0
	c.ang = catTailAmpDeg * deg * math.Sin(2*math.Pi*s.beatPhase)
	if c.action == catEars {
		c.dy = -catEarsLiftPx
	}
	if c.action == catSleep {
		c.scale = catSleepScale * (1 + catBreathAmp*math.Sin(2*math.Pi*c.breathT/catBreathPeriod))
	} else {
		c.scale = 1
	}
}

// charAction — 캐릭터 동작. 우선순위(스펙 §7): headup > stretch > manual > reach > idle_look.
type charAction uint8

const (
	charNone charAction = iota
	charHeadup
	charStretch
	charManual
	charReach
	charIdle
)

type charState struct {
	action         charAction
	dx, dy         float64 // reach 변위
	ang, scale     float64 // 회전(nod 합성)·기지개 스케일
	reachT         float64 // 0..1(완복 이징 진행)
	headupUntilBar uint32
	stretchT       float64
	wasRest        bool
	idleTimer      float64
	idleActive     float64
	idleDir        float64 // +1/−1(고양이 방향)
}

// stepChar — 6동작. nod는 다른 동작과 합성 가능(항상 더해진다).
func (s *state) stepChar(sig Signals, dt float64) {
	c := &s.char
	if sig.Flags&engine.FlagDrop != 0 {
		c.headupUntilBar = sig.Bar + charHeadupBars
	}
	if sig.PomodoroRest && !c.wasRest { // 휴식 진입
		c.stretchT = charStretchSec
	}
	c.wasRest = sig.PomodoroRest

	if !s.reduced {
		// reach 진행: 레지던트 손이면 뻗고(0.5s), MANUAL 잠금이면 0.3s에 거둔다.
		target := 0.0
		if sig.ResidentHandOn && !sig.ManualLocked {
			target = 1
		}
		rate := charReachSec
		if target < c.reachT && sig.ManualLocked {
			rate = charManualSec
		}
		c.reachT = approach(c.reachT, target, dt/rate)
		// idle_look: 30초마다 1회
		c.idleTimer += dt
		if c.idleActive > 0 {
			c.idleActive -= dt
			if c.idleActive < 0 {
				c.idleActive = 0
			}
		} else if c.idleTimer >= charIdleEverySec {
			c.idleTimer = 0
			c.idleActive = charIdleLookSec
		}
		if c.stretchT > 0 {
			c.stretchT -= dt
			if c.stretchT < 0 {
				c.stretchT = 0
			}
		}
	}
	switch {
	case sig.Bar < c.headupUntilBar:
		c.action = charHeadup
	case c.stretchT > 0:
		c.action = charStretch
	case sig.ManualLocked:
		c.action = charManual
	case sig.ResidentHandOn && c.reachT > 0:
		c.action = charReach
	case c.idleActive > 0:
		c.action = charIdle
	default:
		c.action = charNone
	}
	e := easeOutCubic(c.reachT)
	c.dx = s.reachDX * e
	c.dy = s.reachDY * e
	c.ang = charNodAmpDeg * deg * math.Sin(2*math.Pi*s.beatPhase) // nod — 항상
	c.scale = 1
	if s.reduced { // 정지 상태
		c.dx, c.dy, c.ang = 0, 0, 0
		c.scale = 1
		return
	}
	switch c.action {
	case charHeadup:
		c.ang += -charHeadupDeg * deg
	case charStretch:
		c.scale = charStretchScale
	case charIdle:
		prog := 1 - c.idleActive/charIdleLookSec
		c.ang += c.idleDir * charIdleAngDeg * deg * math.Sin(math.Pi*prog)
	}
}

func approach(v, target, step float64) float64 {
	if v < target {
		return math.Min(v+step, target)
	}
	return math.Max(v-step, target)
}

func easeOutCubic(x float64) float64 { return 1 - (1-x)*(1-x)*(1-x) }
