// energy.go — 에너지 곡선: 사이클 유도·페이즈 대역·노브 목표·페이즈 결정.
//
// 에너지 사이클은 저장하지 않고 Input.Now에서 유도한다(derive-don't-store):
// 포모도로 세션 시작(Rest 종료 = 새 사이클 Intro)마다 사이클 0이 시작되고, 사이클
// 길이는 4~8분(분 단위 균등, 사이클 시드 파생)이다. 매 사이클 시드는
// seed' = xorshift(seed ^ cycle) 체인으로 파생되고, 세션마다 시드 체인을 한 번
// 밀어 세션끼리 패턴이 겹치지 않게 한다.
//
// 페이즈 점유: Intro 15% → Build 35% → Drop 25% → Breakdown 25%(사이클 길이 분율).
// 포모도로가 우선한다: Rest = Breakdown 고정, Focus 종료 16바 전 = 강제 Build,
// 종료 바 = Drop(에너지 곡선 무시).
package resident

import "github.com/midagedev/revirth/engine"

// phaseBand — 0..1 대역.
type phaseBand struct{ lo, hi float32 }

// 페이즈별 목표 대역(계약 표 그대로: Intro, Build, Drop, Breakdown 순).
var (
	cutoffBand  = [numPhases]phaseBand{{0.25, 0.4}, {0.4, 0.7}, {0.7, 0.9}, {0.3, 0.5}}
	resoBand    = [numPhases]phaseBand{{0.3, 0.5}, {0.5, 0.7}, {0.6, 0.8}, {0.4, 0.6}}
	envmodTgt   = [numPhases]float32{0.3, 0.5, 0.7, 0.4} // ±0.1 지터
	driveTgt    = [numPhases]float32{0.1, 0.3, 0.5, 0.15}
	delayTgt    = [numPhases]float32{0.2, 0.3, 0.4, 0.5}
	chDensity   = [numPhases]float32{0.3, 0.6, 0.9, 0.2}    // CH 게이트 확률(생성기가 사용)
	phaseEnergy = [numPhases]float32{0.325, 0.55, 0.8, 0.4} // 각 페이즈 컷오프 대역 중심
)

// forcedBuildBars — Focus 종료 전 강제 Build 구간(바 수).
const forcedBuildBars = 16

// derive — Now에서 (페이즈, 사이클 시드, 세션 인덱스)를 유도한다. 바 경계 Tick마다 호출.
func (r *Resident) derive(now float64) (Phase, uint32, int) {
	sp := pomoAt(r.cfg, now)
	session, _, frac, cycleSeed := cycleAt(r.seed, r.cfg, now)
	if sp.ph == Rest {
		// Rest 동안 Breakdown 고정(에너지 곡선 무시).
		return Breakdown, cycleSeed, session
	}
	ph := phaseOfFrac(frac)
	rem := sp.end - now
	barDur := 240.0 / vibeBPM(r.vibe) // 16스텝 = 4비트
	if rem <= barDur {
		return Drop, cycleSeed, session // Focus 종료 바: Drop
	}
	if rem <= forcedBuildBars*barDur {
		return Build, cycleSeed, session // 종료 16바 전부터 강제 Build
	}
	return ph, cycleSeed, session
}

// phaseOfFrac — 사이클 분율 → 페이즈.
func phaseOfFrac(f float64) Phase {
	switch {
	case f < 0.15:
		return Intro
	case f < 0.50:
		return Build
	case f < 0.75:
		return Drop
	default:
		return Breakdown
	}
}

// cycleAt — seed·cfg·now → (세션, 사이클 인덱스, 사이클 내 분율, 사이클 시드).
// 사이클은 세션마다 0부터 다시 시작하고(Rest 종료 = Intro), 길이는 사이클 시드에서
// 4 + x%5 분으로 파생된다.
func cycleAt(seed uint32, cfg Config, now float64) (session, cycle int, frac float64, cycleSeed uint32) {
	sp := pomoAt(cfg, now)
	tau := now - sp.start
	if tau < 0 {
		tau = 0
	}
	x := seed
	for k := 1; k <= sp.session; k++ { // 세션 시드 체인(세션끼리 패턴 분리)
		x = xs32(x ^ uint32(k))
	}
	cs := x
	dur := cycleDurSec(cs)
	c := 0
	for tau >= dur && c < 4096 {
		tau -= dur
		c++
		cs = xs32(cs ^ uint32(c)) // seed' = xorshift(seed ^ cycle)
		dur = cycleDurSec(cs)
	}
	if dur <= 0 {
		dur = 240
	}
	return sp.session, c, tau / dur, cs
}

// cycleDurSec — 사이클 시드 → 길이(4~8분, 분 단위 균등).
func cycleDurSec(cs uint32) float64 { return float64(4+xs32(cs)%5) * 60 }

// updateTargets — 바 경계마다 노브 목표 갱신. 대역 내 값은 (사이클 시드, 바)로
// 시드를 파생해 뽑는다(같은 바 → 같은 목표, 결정론). 바이브 시프트를 적용한 뒤
// [0,1] 클램프. 잠금 여부와 무관하게 목표는 계산하고, 방출 단계(onStep)에서 잠금을 걸러낸다.
func (r *Resident) updateTargets(cycleSeed uint32, bar uint32, ph Phase) {
	x := cycleSeed ^ (bar+1)*2654435761 ^ uint32(ph)*0x85EBCA6B ^ 0x27220A95
	if x == 0 {
		x = 1
	}
	g := rngT{x}
	pick := func(b phaseBand) float32 { return b.lo + (b.hi-b.lo)*g.float() }

	cut := pick(cutoffBand[ph])
	switch r.vibe {
	case DeepFocus:
		cut -= 0.15
	case Rush:
		cut += 0.1
	case Lofi:
		cut -= 0.2
	}
	r.tgt[engine.CutoffA] = clamp01(cut)

	reso := pick(resoBand[ph])
	r.tgt[engine.BassAParams+engine.BReso] = clamp01(reso)
	r.tgt[engine.BassBParams+engine.BReso] = clamp01(reso)

	em := envmodTgt[ph] + (g.float()-0.5)*0.2 // ±0.1
	r.tgt[engine.BassAParams+engine.BEnvMod] = clamp01(em)
	r.tgt[engine.BassBParams+engine.BEnvMod] = clamp01(em)

	dr := driveTgt[ph]
	if r.vibe == Rush {
		dr += 0.15
	}
	if r.vibe == Lofi {
		dr -= 0.1 // 클램프 ≥ 0
	}
	r.tgt[engine.Drive] = clamp01(dr)

	dl := delayTgt[ph]
	if r.vibe == Lofi {
		dl += 0.3
	}
	r.tgt[engine.Delay] = clamp01(dl)
}
