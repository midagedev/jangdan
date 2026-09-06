// generator.go — 패턴 생성기: 도수 베이스라인 A/B·드럼.
//
// 재생성 시점: 바 경계에서 페이즈 진입 또는 마지막 재생성에서 8바(resident.go onBar).
// 모든 확률 추첨은 (사이클 시드, 바, 페이즈)로 시드를 파생한 xorshift32로만 이뤄진다
// — 같은 바에 다시 생성하면 같은 패턴(결정론).
//
// Phase 2부터 노트는 절대음이 아니라 **도수**(octave*7 + degree, engine/harmony.go)다.
// 조성·스케일은 엔진이 알므로 생성기는 가중(루트 0.5·코드톤 0.3·경과음 0.2)만 소유한다
// — 세션 시드의 스케일 추첨은 rng 소비 순서 보존을 위해 sessionRandom에 남기고 결과는
// 쓰지 않는다(폐기 흔적). 베이스라인 A가 주선율(옥타브 1, 기준 note 7), B는 코드 보이스
// — B 모드(BASS/ARP/CHORD)는 페이즈가 정한다(harmony.go bassModeFor):
// BASS = A가 쉬는 스텝을 절반 밀도로 채우는 옥타브 0 보조선, ARP = 밀도 0.9·note 14,
// CHORD = 스텝 0·8·12 스탭. 드럼은 킥 골격(0·4·8·12 필수) 위에 시드 변주를 얹는다.
package resident

import "github.com/midagedev/jangdan/engine"

// patStep — 베이스 패턴 한 스텝(도수 note = octave*7+degree, 0..MaxNote; flags = Gate|Slide|Accent).
type patStep struct {
	note  uint8
	flags uint8
}

// 페이즈별 생성 밀도(계약 범위 안: 베이스 게이트 0.4..0.9).
var bassDensity = [numPhases]float32{0.5, 0.75, 0.9, 0.4}

// 드럼 변주 확률(페이즈별). 킥 골격 0·4·8·12는 확률 밖(항상).
var (
	kickExtra = [numPhases]float32{0.1, 0.25, 0.45, 0.05} // 골격 외 스텝 추가 킥
	ghostSD   = [numPhases]float32{0.05, 0.15, 0.2, 0.05} // 4·12 외 고스트 스네어
)

// chVibeMul — 바이브별 CH 밀도 배율(DeepFocus ×0.5, Rush ×1, Lofi ×0.7).
var chVibeMul = [numVibes]float32{0.5, 1.0, 0.7}

// ohGate — OH 오프비트(8분 뒷박 = 스텝 2·6·10·14) 기본 확률. DeepFocus는 0.
const ohGate = 0.25

// 도수 가중·옥타브 점프·B 보조선 확률(계약 고정값).
const (
	degRootP    = 0.5 // 루트(0)
	degChordP   = 0.3 // 코드 톤(2·4)
	octaveJumpP = 0.1 // ±7 도수(한 옥타브)
	bThinP      = 0.5 // BASS 모드에서 B가 A의 빈 스텝을 채울 확률("절반 비움")
	baseA       = 7   // 베이스 A 기준: 옥타브 1·도수 0(코드 루트)
	baseBass    = 0   // BASS 모드 B 기준: 옥타브 0
	baseArp     = 14  // ARP/CHORD 모드 B 기준: 옥타브 2(도수 필드는 엔진이 무시)
	arpDensity  = 0.9 // ARP 모드 B 게이트 밀도
)

// chordStabs — CHORD 모드 B의 스탭 스텝(게이트·액센트 0).
var chordStabs = [3]int{0, 8, 12}

// passingDegs — 경과음 도수(1·3·5·6, 범주 내 균등).
var passingDegs = [4]int{1, 3, 5, 6}

// sessionRandom — 세션 고정 확률들(휴지 0.10..0.30, 슬라이드 0.10..0.30, 액센트
// 0.15..0.35). 순수 유도 함수 — 저장 없이 (seed, session)에서 계산한다. 첫 추첨(scale)은
// P2 도수화로 폐기된 스케일 선택이었다 — 이후 확률값 변동을 막으려 rng 소비
// 순서를 그대로 유지하고 결과만 버린다(호출부에서 `_`).
func sessionRandom(seed uint32, session int) (scale int, restP, slideP, accP float32) {
	x := xs32(seed ^ 0x6D2B79F5 ^ uint32(session)*2654435761)
	if x == 0 {
		x = 1
	}
	scale = int(x % 3)
	x = xs32(x)
	restP = 0.10 + float32(x%21)/100
	x = xs32(x)
	slideP = 0.10 + float32(x%21)/100
	x = xs32(x)
	accP = 0.15 + float32(x%21)/100
	return
}

// genPatterns — (사이클 시드, 바, 페이즈, 세션, Drop 진입 바 여부)로 패턴을 만들어
// r.pat·r.drums에 저장한다. 방출은 emitPatterns(SelectPattern 다음 바)가 한다.
func (r *Resident) genPatterns(cycleSeed uint32, bar uint32, ph Phase, session int, dropEntry bool) {
	_, restP, slideP, accP := sessionRandom(r.seed, session) // 첫 추첨(스케일)은 폐기 — rng 순서 보존
	x := cycleSeed ^ (bar+1)*2654435761 ^ uint32(ph)*0x85EBCA6B ^ 0x165667B1
	if x == 0 {
		x = 1
	}
	g := rngT{x}

	// 베이스라인 A — 기준 도수 note 7(옥타브 1·도수 0). 밀도 게이트 → 휴지 감산 → 도수·플래그.
	for st := 0; st < engine.Steps; st++ {
		var s patStep
		gated := g.float() < bassDensity[ph] && g.float() >= restP
		if gated {
			s.flags |= engine.StepGate
			s.note = clampNote(jumpDeg(&g, baseA+pickDeg(&g)))
			if g.float() < slideP {
				s.flags |= engine.StepSlide
			}
			if g.float() < accP {
				s.flags |= engine.StepAccent
			}
		}
		r.pat[0][st] = s
	}

	// 베이스라인 B — 모드는 페이즈가 정한다(harmony.go bassModeFor, 방출과 같은 표).
	// BASS: A가 게이트되지 않은 스텝만(오프비트 보완), 절반은 비운다(기준 note 0).
	// ARP: 밀도 0.9·슬라이드 없음·액센트 accP·note 14(도수 필드는 엔진이 무시).
	// CHORD: 스텝 0·8·12만 스탭(액센트 없음, rng 소비 없음).
	bMode, _ := bassModeFor(ph)
	for st := 0; st < engine.Steps; st++ {
		var s patStep
		switch bMode {
		case engine.ModeArp:
			if g.float() < arpDensity {
				s.flags |= engine.StepGate
				s.note = baseArp
				if g.float() < accP {
					s.flags |= engine.StepAccent
				}
			}
		case engine.ModeChord:
			if st == chordStabs[0] || st == chordStabs[1] || st == chordStabs[2] {
				s.flags |= engine.StepGate
				s.note = baseArp
			}
		default: // ModeBass
			if r.pat[0][st].flags&engine.StepGate == 0 && g.float() < bThinP {
				s.flags |= engine.StepGate
				s.note = clampNote(jumpDeg(&g, baseBass+pickDeg(&g)))
				if g.float() < slideP {
					s.flags |= engine.StepSlide
				}
				if g.float() < accP {
					s.flags |= engine.StepAccent
				}
			}
		}
		r.pat[1][st] = s
	}

	// 드럼(파트 순서 = engine BD SD CH OH CP CY).
	// BD — 골격 0·4·8·12 필수 + 페이즈별 추가 확률.
	for st := 0; st < engine.Steps; st++ {
		r.drums[0][st] = 0
	}
	for _, st := range [...]int{0, 4, 8, 12} {
		r.drums[0][st] = engine.StepGate
	}
	for st := 0; st < engine.Steps; st++ {
		if st%4 != 0 && g.float() < kickExtra[ph] {
			r.drums[0][st] = engine.StepGate
		}
	}
	// SD — 4·12(12는 액센트) + 고스트.
	for st := 0; st < engine.Steps; st++ {
		r.drums[1][st] = 0
	}
	r.drums[1][4] = engine.StepGate
	r.drums[1][12] = engine.StepGate | engine.StepAccent
	for st := 0; st < engine.Steps; st++ {
		if st != 4 && st != 12 && g.float() < ghostSD[ph] {
			r.drums[1][st] = engine.StepGate
		}
	}
	// CH — 페이즈 밀도 × 바이브 배율.
	ch := chDensity[ph] * chVibeMul[r.vibe]
	if ch > 1 {
		ch = 1
	}
	for st := 0; st < engine.Steps; st++ {
		r.drums[2][st] = 0
		if g.float() < ch {
			r.drums[2][st] = engine.StepGate
		}
	}
	// OH — 8분 뒷박(2·6·10·14) 확률. DeepFocus는 없음.
	oh := float32(ohGate)
	if r.vibe == DeepFocus {
		oh = 0
	}
	for st := 2; st < engine.Steps; st += 4 {
		r.drums[3][st] = 0
		if g.float() < oh {
			r.drums[3][st] = engine.StepGate
		}
	}
	// CP — Build·Drop만, SD 위에 깔기(4·12). DeepFocus는 없음.
	for st := 0; st < engine.Steps; st++ {
		r.drums[4][st] = 0
	}
	if (ph == Build || ph == Drop) && r.vibe != DeepFocus {
		r.drums[4][4] = engine.StepGate
		r.drums[4][12] = engine.StepGate
	}
	// CY — Drop 진입 바의 슬롯 패턴 step 0(엔진 Drop 발동 CY와 겹쳐 울림).
	for st := 0; st < engine.Steps; st++ {
		r.drums[5][st] = 0
	}
	if dropEntry {
		r.drums[5][0] = engine.StepGate
	}
}

// pickDeg — 도수 가중 추첨: 루트(0) 0.5 · 코드 톤(2·4) 0.3 · 경과음(1·3·5·6) 0.2
// (범주 내 균등). 조성은 엔진이 알므로 여기서는 가중만 소유한다.
func pickDeg(g *rngT) int {
	u := g.float()
	if u < degRootP {
		return 0
	}
	if u < degRootP+degChordP {
		return 2 + int(g.next()&1)*2 // 2 또는 4
	}
	return passingDegs[g.next()%uint32(len(passingDegs))]
}

// jumpDeg — 옥타브 점프(0.1, ±7 도수). 음수는 clampNote가 0으로 정규화한다.
func jumpDeg(g *rngT, n int) int {
	if g.float() < octaveJumpP {
		if g.next()&1 == 1 {
			return n + 7
		}
		return n - 7
	}
	return n
}

// clampNote — 0..MaxNote 정규화(클램프; 엔진 Apply도 클램프하지만 계약은 여기서 지킨다).
func clampNote(n int) uint8 {
	if n < 0 {
		n = 0
	}
	if n > engine.MaxNote {
		n = engine.MaxNote
	}
	return uint8(n)
}

// emitPatterns — 저장된 패턴을 Cmd로 내놓는다(SelectPattern 다음 바의 BarStart 틱).
// BassStep 16×2 + DrumStep 16×6 = 128개.
func (r *Resident) emitPatterns() {
	for p := 0; p < 2; p++ {
		for st := 0; st < engine.Steps; st++ {
			s := r.pat[p][st]
			r.emit(engine.Cmd{Kind: engine.BassStep, A: uint8(p), B: uint8(st), C: s.note, D: s.flags})
		}
	}
	for v := 0; v < 6; v++ {
		part := uint8(engine.BD) + uint8(v)
		for st := 0; st < engine.Steps; st++ {
			r.emit(engine.Cmd{Kind: engine.DrumStep, A: part, B: uint8(st), D: r.drums[v][st]})
		}
	}
}
