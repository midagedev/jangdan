// sampler.go — 샘플러(engine.SlotSampler) 연주법: 페이즈별 원샷 패턴과 로컬 파라미터.
//
// 레지던트가 샘플러 장치를 다루는 방식(§14.2 "레지던트 연주법"). 폴리(poly.go)와 다른 점 셋이
// 연주법을 결정한다:
//
//  1. 음을 코드에서 직접 고른다 — 엔진 samplerStep이 게이트 스텝의 패턴 도수를 ResolveNote로
//     해석해 원샷을 쏜다(폴리는 옥타브 부분만 썼다). 도수는 루트(0)와 코드 톤(2·4)뿐 — 타격음이
//     화성을 흐르지 않게(경과음 금지). 팩 슬롯(SmpSelect)이 곧 악기다: 페이즈마다 다른 소리.
//
//  2. 원샷이다 — 게이트 없는 스텝의 noteOff를 장치가 무시하므로 게이트를 촘촘히 깔면 소리가
//     겹쳐 쌓인다(보이스 4, 가장 오래된 것부터 뺏김). 리듬은 성기게(폴리 Drop 11게이트에 비해
//     여기 Drop 6게이트). 타이(StepGate|StepSlide)는 연달아 게이트일 때 재트리거를 막는
//     수단이지만 이 패턴은 인접 게이트가 없어 쓰지 않는다.
//
//  3. 상태 없음(derive-don't-store) — 페이즈 고정표. 트랜스 리드처럼 반복이 정체성이다.
//
//     페이즈 | 슬롯 | 리듬                              | 옥타브·도수(게이트 순)
//     Intro      2 VOX    목소리 한 마디 한 번(스텝 0)        1·[0]
//     Build      6 BREATH  숨이 차오르는 4게이트(0·6·10·12)    2·[0 2 4 0]
//     Drop       0 PLUCK   뜯은 현 리프 6게이트 2액센트        2·[0 4 2 0 4 2]
//     Breakdown  1 BELL    벨 하나가 길게 남는다(스텝 0)       3·[0]
//
// 페이즈 진입 바에 한 번만 방출한다(DeviceStep 16 + DeviceParam 5 — 폴리와 같은 규칙).
// 화성 잠금(LockHarmony)과 무관하다 — 음은 코드 트랙 위의 도수라 사람이 코드를 바꾸면 따라간다.
package resident

import "github.com/midagedev/jangdan/engine"

const (
	sG  = engine.StepGate
	sGA = engine.StepGate | engine.StepAccent
)

// smpSlot — 페이즈별 팩 슬롯(samplerpack.go packTab: 0 PLUCK · 1 BELL · 2 VOX · 3 WOOD ·
// 4 SHAKE · 5 TAPE · 6 BREATH · 7 SUB). SmpSelect의 q는 엔진 식 idx = int(q·7 + 0.5)의
// 역변환인 q = 슬롯/7로 방출한다(왕복은 테스트가 단언).
var smpSlot = [numPhases]uint8{2, 6, 0, 1}

// smpPatterns — 페이즈별 16스텝 게이트(flags = StepGate|StepAccent — 원샷이라 타이 없음).
var smpPatterns = [numPhases][engine.Steps]uint8{
	Intro:     {sG, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	Build:     {sG, 0, 0, 0, 0, 0, sG, 0, 0, 0, sG, 0, sG, 0, 0, 0},
	Drop:      {sGA, 0, 0, sG, 0, 0, sG, 0, sGA, 0, 0, sG, 0, 0, sG, 0},
	Breakdown: {sG, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
}

// smpNotes — 페이즈별 스텝 노트(도수 표기 octave*7+degree, 도수는 루트·코드 톤 0·2·4만).
// 게이트 스텝이 리프 윤곽을 만들고 나머지 스텝은 페이즈 기저음(엔진이 게이트 없는 스텝의
// 노트를 읽지 않는다 — 폴리와 같은 방출 형태). 옥타브는 각 슬롯 기준음(root)에 맞췄다:
// VOX 12·BREATH 24·PLUCK 24·BELL 36 → 재생비가 키 루트 오프셋만큼만 움직인다.
var smpNotes = [numPhases][engine.Steps]uint8{
	Intro:     {7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7},
	Build:     {14, 14, 14, 14, 14, 14, 16, 14, 14, 14, 18, 14, 14, 14, 14, 14},
	Drop:      {14, 14, 14, 18, 14, 14, 16, 14, 14, 14, 14, 18, 14, 14, 16, 14},
	Breakdown: {21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21},
}

// smpTone — 페이즈별 (SmpSelect q, SmpTune, SmpAttack, SmpRelease, SmpLevel).
// 튠은 전 페이즈 0.5(원음). 레벨은 계약 범위 안에서 바이브 시프트(레벨 −0.1)에도
// 범위 안에 남는 값: Intro 0.42·Build 0.58·Drop 0.78·Breakdown 0.52.
var smpTone = [numPhases][5]float32{
	Intro:     {float32(smpSlot[Intro]) / 7, 0.5, 0.35, 0.45, 0.42},
	Build:     {float32(smpSlot[Build]) / 7, 0.5, 0.45, 0.50, 0.58},
	Drop:      {float32(smpSlot[Drop]) / 7, 0.5, 0.02, 0.35, 0.78},
	Breakdown: {float32(smpSlot[Breakdown]) / 7, 0.5, 0.10, 0.60, 0.52},
}

// emitSampler — 페이즈 진입 바에 패턴 16 + 음색 5를 방출한다. 바이브 시프트: DeepFocus 레벨
// −0.1, Lofi 릴리즈 +0.15(폴리와 같은 자리, 클램프 포함).
func (r *Resident) emitSampler(ph Phase) {
	pat := &smpPatterns[ph]
	notes := &smpNotes[ph]
	for st := 0; st < engine.Steps; st++ {
		r.emit(engine.Cmd{Kind: engine.DeviceStep, A: engine.SlotSampler, B: uint8(st), C: notes[st], D: pat[st]})
	}
	tone := smpTone[ph]
	switch r.vibe {
	case DeepFocus:
		tone[4] -= 0.1
	case Lofi:
		tone[3] += 0.15
	}
	ks := [5]uint8{engine.SmpSelect, engine.SmpTune, engine.SmpAttack, engine.SmpRelease, engine.SmpLevel}
	for i, k := range ks {
		r.emit(engine.Cmd{Kind: engine.DeviceParam, A: engine.SlotSampler, B: k, V: clamp01(tone[i])})
	}
}
