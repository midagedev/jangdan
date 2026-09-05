// props.go — 사물 반응(스탠드·비·창·머그·라디에이터·레코드)의 프레임 로직.
// 계약 원본: 기획서 05절 "사물 ↔ 엔진 신호" 표와 스펙 §1~§6 수치. Draw는 room.go가
// state 값을 읽어 그릴 뿐 — 모든 계산은 여기 step에서 끝난다(이미지 없이 단위 테스트).
package room

import (
	"math"

	"github.com/midagedev/revirth/engine"
)

// 스펙 수치(기획서 05절·스펙 §1~§6). 픽셀 좌표는 layout.json 소유 — 여기는 비율·시간·진폭만.
const (
	lampBreathAmp  = 0.06  // BD 트리거 빛 풀 밝기 상승(상한)
	lampDecayTau   = 0.150 // 지수 감쇠 시정수(150ms)
	lampShakeDur   = 0.120 // 액센트 떨림 지속
	lampShakeAmpPx = 2.0   // 떨림 진폭(px)
	lampDimBright  = 0.82  // 컷오프 q=0 밝기(노랗고 어둡게)
	lampHotBright  = 1.0   // 컷오프 q=1 밝기

	rainDensityMin = 0.15 // 햇 게이트 0 → 잦아든다
	rainThickMin   = 1.0  // OH 게이트 0 → 굵기(px)
	rainThickMax   = 2.5  // OH 게이트 16
	windowFadeSec  = 0.2  // 창 페이드
	dropLightMax   = 3    // 드롭에 동시에 켜지는 창 상한
	screenAreaFrac = 0.05 // 창 동시 변화 면적 상한(화면의 5%)

	steamCount     = 6    // 김 파티클
	steamSpeedPx   = 18.0 // 130BPM 기준 상승 속도(px/s)
	steamRiseRatio = 3.0  // 김이 오르는 높이(머그 rect 높이 배수)
	steamSwayPx    = 1.0  // 좌우 흔들림
	steamSizePx    = 2.0  // 파티클 크기
	steamAlpha     = 0.25 // 김 알파(먼지와 같은 무게)

	radGlowSec  = 0.060 // 라디에이터 하이라이트
	radLiftPx   = 1.0   // 바 경계 리프트
	radHiAlpha  = 0.12  // 하이라이트 알파
	recFadeSec  = 0.300 // 레코드 순서 교체 크로스페이드
	rngSeedInit = 0x9E3779B9
)

// stepLamp — 스탠드. BD 트리거 → +6%(상한) 후 150ms 지수 감쇠("호흡").
// 액센트 → 광원 중심 ±2px 120ms 떨림(RM에서는 정지). 컷오프(BassA 미러 q) → 밝기·색온도.
func (s *state) stepLamp(sig Signals, dt float64) {
	q := sig.Cutoff
	if q < 0 {
		q = 0
	} else if q > 1 {
		q = 1
	}
	s.lampQ = q
	s.lampBase = lampDimBright + (lampHotBright-lampDimBright)*q
	// 감쇠를 먼저 적용하고 트리거를 얹는다 — 피크 프레임이 정확히 상한에 걸린다.
	s.lampPulse *= float32(math.Exp(-dt / lampDecayTau))
	if sig.Flags&(1<<engine.BD) != 0 {
		s.lampPulse = s.lampAmp
	}
	if sig.Flags&engine.FlagAccent != 0 && !s.reduced {
		s.lampShakeT = lampShakeDur
	}
	if s.lampShakeT > 0 && !s.reduced {
		s.lampShakeT -= dt
		p := 1 - s.lampShakeT/lampShakeDur // 0..1 경과
		s.lampShakeOff = [2]float64{lampShakeAmpPx * math.Sin(p*4*math.Pi), 0}
	} else {
		s.lampShakeT = 0
		s.lampShakeOff = [2]float64{}
	}
}

// lampBrightness — 빛 풀 밝기(기본 0.82..1.0 + 호흡 ≤ +6%).
func (s *state) lampBrightness() float32 { return s.lampBase + s.lampPulse }

// stepRain — 천창의 비. 밀도 = CH 게이트 수/16 → 0.15..1, 굵기 = OH 게이트 수/16 → 1..2.5px.
// 오후 시간대는 밀도 하항 0.6. 뮤트는 공식에 없다(게이트 수만 본다 — 기획서 "햇 뮤트면
// 잦아든다"는 게이트 0으로 실현된다).
func (s *state) stepRain(sig Signals) {
	g := float32(sig.CHGates) / engine.Steps
	d := rainDensityMin + g*(1-rainDensityMin)
	floor := lerpF32(s.prevTod.rainFloor(), s.tod.rainFloor(), float32(s.todBlend))
	if d < floor {
		d = floor
	}
	s.rainDensity = d
	if !s.reduced { // RM: 밀도 변화만 남긴다(굵기 고정)
		oh := float32(sig.OHGates) / engine.Steps
		s.rainThick = rainThickMin + oh*(rainThickMax-rainThickMin)
	}
}

// toggleWindow — 바 경계 토글. 면적 상한을 넘는 후보는 건너뛴다(켜짐·꺼짐 모두 "변화").
func (s *state) toggleWindow(i int) {
	if s.winChangedArea+s.winArea[i] > s.capArea {
		return
	}
	s.winChangedArea += s.winArea[i]
	s.winLit[i] = !s.winLit[i]
}

// lightWindow — 드롭 점등. 면적 상한 초과 후보는 건너뛴다.
func (s *state) lightWindow(i int) bool {
	if s.winLit[i] || s.winChangedArea+s.winArea[i] > s.capArea {
		return false
	}
	s.winChangedArea += s.winArea[i]
	s.winLit[i] = true
	return true
}

// stepWindows — 창밖 마을 불빛. FlagBar마다 하나 토글, FlagDrop에 최대 3개 점등(xorshift,
// 결정론). 켜짐은 200ms 페이드. evening은 전부 켜짐(알파 0.5, 30초 크로스페이드에 따라).
func (s *state) stepWindows(sig Signals, dt float64) {
	n := len(s.winLit)
	s.winChangedArea = 0
	if n == 0 {
		return
	}
	if !s.reduced {
		if sig.Flags&engine.FlagBar != 0 {
			s.toggleWindow(int(s.xorshift()) % n)
		}
		if sig.Flags&engine.FlagDrop != 0 {
			lit := 0
			for tries := 0; tries < n && lit < dropLightMax; tries++ {
				if s.lightWindow(int(s.xorshift()) % n) {
					lit++
				}
			}
		}
	}
	forced := lerpF32(s.prevTod.windowsLit(), s.tod.windowsLit(), float32(s.todBlend))
	baseA := lerpF32(s.prevTod.windowAlpha(), s.tod.windowAlpha(), float32(s.todBlend))
	if s.reduced { // RM: 창은 정지 상태로 그린다(페이드도 멈춘다)
		for i := range s.winAlpha {
			s.winAlpha[i] = baseA * s.winFade[i]
		}
		return
	}
	adv := float32(dt / windowFadeSec)
	if adv > 1 {
		adv = 1
	}
	for i := range s.winFade {
		target := float32(0)
		if s.winLit[i] {
			target = 1
		}
		if forced > target {
			target = forced // evening 강제 점등
		}
		switch {
		case s.winFade[i] < target:
			s.winFade[i] = minF32(s.winFade[i]+adv, target)
		case s.winFade[i] > target:
			s.winFade[i] = maxF32(s.winFade[i]-adv, target)
		}
		s.winAlpha[i] = baseA * s.winFade[i]
	}
}

// stepMug — 머그의 김. 상승 속도 = BPM/130 × 18px/s, BD에 표면(상단 2px)이 1프레임 떨린다.
func (s *state) stepMug(sig Signals, dt float64, risePx float64) {
	if !s.reduced {
		s.steamT += dt * (s.bpm / 130) * steamSpeedPx
	}
	s.mugJitter = !s.reduced && sig.Flags&(1<<engine.BD) != 0
	for i := 0; i < steamCount; i++ {
		phase := s.steamT + float64(i)*risePx/steamCount
		prog := math.Mod(phase, risePx) / risePx // 0(표면)..1(끝)
		s.steamP[i] = [3]float32{
			float32(math.Sin(phase*0.7+float64(i)) * steamSwayPx),
			float32(prog),
			float32(math.Sin(math.Pi*prog) * steamAlpha),
		}
	}
}

// stepFurnishings — 라디에이터(바마다 1px 리프트 + 60ms 하이라이트)와 레코드
// (Phase 변화 → 두 개 위치 교환, 300ms 크로스페이드).
func (s *state) stepFurnishings(sig Signals, dt float64, nRec int) {
	s.radLift = !s.reduced && sig.Flags&engine.FlagBar != 0
	if s.radLift && s.radGlow <= 0 {
		s.radGlow = radGlowSec
	}
	if s.radGlow > 0 {
		s.radGlow -= dt
		if s.radGlow < 0 {
			s.radGlow = 0
		}
	}
	if nRec >= 2 && sig.Phase != s.prevPhase {
		s.recOrder[0], s.recOrder[1] = s.recOrder[1], s.recOrder[0]
		if s.reduced {
			s.recFadeK = 1 // RM: 교환은 즉시(모션 없음)
		}
	}
	s.prevPhase = sig.Phase
	if !s.reduced && s.recFadeK < 1 {
		s.recFadeK += dt / recFadeSec
		if s.recFadeK > 1 {
			s.recFadeK = 1
		}
	}
}

// xorshift32 — 창 선택용 결정론 난수(엔진 규칙 준수: 외부 난수 없음).
func (s *state) xorshift() uint32 {
	x := s.rng
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	s.rng = x
	return x
}

func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
