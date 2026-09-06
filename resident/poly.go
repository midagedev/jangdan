// poly.go — 폴리 리드(engine.SlotPoly) 연주법: 페이즈별 게이트 패턴과 로컬 파라미터.
//
// 레지던트가 폴리 장치를 다루는 방식(§14.2 "레지던트 연주법"): 음은 고르지 않는다 —
// 엔진이 코드 트랙의 코드 톤을 보이스에 배정한다(engine.go onStep, KindPoly). 여기서
// 정하는 것은 **리듬**(16스텝 게이트·타이·액센트, 옥타브)과 **음색 자리**(컷오프·어택·
// 릴리즈·디튠·레벨)뿐이고, 둘 다 페이즈 진입 바에 한 번 방출한다(DeviceStep 16 + DeviceParam 5).
// 패턴은 페이즈 고정표다(derive-don't-store — 저장 상태 없음). 사이클 시드 변주는 뒤로 미룬다:
// 트랜스 리드는 반복이 정체성이다.
//
//   - Intro: 패드 — 스텝 0 게이트 뒤 타이(코드를 한 바 유지), 옥타브 2, 어두운 컷오프·느린 어택
//   - Build: 오프비트 8분(스텝 2·6·10·14) 펌핑, 옥타브 3
//   - Drop: 트랜스 게이트(0·1·3·4·6·7·9·10·12·13·15, 액센트 0·4·9·12), 옥타브 3, 밝은 컷오프·넓은 디튠
//   - Breakdown: 패드 — 타이 코드, 옥타브 2, 긴 릴리즈
//
// 화성 잠금(LockHarmony)과 무관하다 — 리듬·음색만 내고 음은 엔진이 코드 트랙에서 읽으므로
// 사람이 코드를 바꾸면 그 코드를 따라간다.
package resident

import "github.com/midagedev/jangdan/engine"

// polyStep — 페이즈 패턴 한 스텝(flags = StepGate|StepSlide(타이)|StepAccent).
type polyStep struct{ flags uint8 }

const (
	pG  = engine.StepGate
	pT  = engine.StepGate | engine.StepSlide // 타이: 게이트 유지·재트리거 없음
	pGA = engine.StepGate | engine.StepAccent
)

// polyPatterns — 페이즈별 16스텝(계약 표 그대로).
var polyPatterns = [numPhases][engine.Steps]uint8{
	Intro:     {pG, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT},
	Build:     {0, 0, pG, 0, 0, 0, pG, 0, 0, 0, pG, 0, 0, 0, pG, 0},
	Drop:      {pGA, pG, 0, pG, pGA, 0, pG, pG, 0, pGA, pG, 0, pGA, pG, 0, pG},
	Breakdown: {pG, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT, pT},
}

// polyOctave — 페이즈별 옥타브(도수 표기 note = octave*7 — 도수 필드는 엔진이 무시).
var polyOctave = [numPhases]uint8{2, 3, 3, 2}

// 폴리 로컬 파라미터 k(engine/poly.go 표).
const (
	polyCutoff  = engine.PolyCutoff
	polyAttack  = engine.PolyAttack
	polyRelease = engine.PolyRelease
	polyDetune  = engine.PolyDetune
	polyLevel   = engine.PolyLevel
)

// polyTone — 페이즈별 (컷오프, 어택, 릴리즈, 디튠, 레벨).
var polyTone = [numPhases][5]float32{
	Intro:     {0.30, 0.70, 0.70, 0.40, 0.50},
	Build:     {0.50, 0.15, 0.35, 0.50, 0.75},
	Drop:      {0.70, 0.05, 0.30, 0.60, 0.85},
	Breakdown: {0.40, 0.60, 0.85, 0.45, 0.60},
}

// emitPoly — 페이즈 진입 바에 패턴 16 + 음색 5를 방출한다. 바이브 시프트: DeepFocus 컷오프 −0.1·
// 레벨 −0.1, Lofi 컷오프 −0.15.
func (r *Resident) emitPoly(ph Phase) {
	pat := &polyPatterns[ph]
	note := polyOctave[ph] * engine.NumDegrees
	for st := 0; st < engine.Steps; st++ {
		r.emit(engine.Cmd{Kind: engine.DeviceStep, A: engine.SlotPoly, B: uint8(st), C: note, D: pat[st]})
	}
	tone := polyTone[ph]
	switch r.vibe {
	case DeepFocus:
		tone[0] -= 0.1
		tone[4] -= 0.1
	case Lofi:
		tone[0] -= 0.15
	}
	ks := [5]uint8{polyCutoff, polyAttack, polyRelease, polyDetune, polyLevel}
	for i, k := range ks {
		r.emit(engine.Cmd{Kind: engine.DeviceParam, A: engine.SlotPoly, B: k, V: clamp01(tone[i])})
	}
}
