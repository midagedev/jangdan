// params.go — 파라미터 ID 표와 기본값. 리드 소유(docs/impl-plan-2026-09-05.md §2.1이 원본).
//
// 전 파라미터는 0..1이고 엔진은 SetParam 시점에 4095단계로 양자화해 저장한다 —
// 로그·키프레임의 uint16이 곧 엔진 값이라 직렬화 왕복 오차가 0이다.
package engine

type ParamID uint8

// 베이스라인 파라미터의 파트 내 오프셋(BassA = 0+k, BassB = 8+k).
const (
	BTune ParamID = iota
	BCutoff
	BReso
	BEnvMod
	BDecay
	BAccent
	BWave // <0.5 톱니, ≥0.5 사각
	BOct  // 3단: <1/3 → −1옥타브, <2/3 → 0, 그 외 +1
	BassParams
)

const (
	BassAParams ParamID = 0
	BassBParams ParamID = BassParams // 8

	// 드럼: 보이스 v(0..5 = BD SD CH OH CP CY)의 Level = DrumParams+2v, Tune = DrumParams+2v+1.
	DrumParams ParamID = 2 * BassParams // 16
	NumDrums           = 6

	Delay  ParamID = DrumParams + 2*NumDrums // 28
	Drive  ParamID = Delay + 1              // 29
	Comp   ParamID = Delay + 2              // 30
	Master ParamID = Delay + 3              // 31
	Tempo  ParamID = Delay + 4              // 32
)

// 믹서·리버브·코러스 버스(§13.1 — 기존 ID 0..32 불변, 뒤에 덧붙인다).
// RevSend/DelaySend는 파트별 8개(RevSendBase+p, p = Part 0..7), 코러스 센드는
// 베이스 A/B 두 개뿐이다(§13.2: choIn = bassA'·choA + bassB'·choB).
const (
	BassALevel ParamID = Tempo + 1 // 33 — 베이스 A 채널 레벨(프리 FX). 기본 1.0 = 항등
	BassBLevel ParamID = Tempo + 2 // 34 — 베이스 B 채널 레벨

	RevSendBase ParamID = Tempo + 3               // 35 — RevSend(p), p = Part 0..7
	RevSize     ParamID = RevSendBase + ParamID(NumParts) // 43 — 콤 길이 배율·피드백
	RevDamp     ParamID = RevSize + 1             // 44 — 콤 피드백 LP 차단(12k→2k Hz)
	RevMix      ParamID = RevSize + 2             // 45 — 리버브 리턴 레벨(×0..0.8)
	ChoSendA    ParamID = RevSize + 3             // 46 — 베이스 A 코러스 센드
	ChoSendB    ParamID = RevSize + 4             // 47 — 베이스 B 코러스 센드
	ChoRate     ParamID = RevSize + 5             // 48 — LFO 0.1..3 Hz(지수 0.1·30^q)
	ChoDepth    ParamID = RevSize + 6             // 49 — 변조 깊이 0..6 ms(기준 12 ms)
	ChoMix      ParamID = RevSize + 7             // 50 — 코러스 리턴 레벨(×0..0.8)

	DelaySendBase ParamID = ChoMix + 1 // 51 — DelaySend(p), p = Part 0..7
	NumParams     ParamID = DelaySendBase + ParamID(NumParts) // 59
)

// RevSend — 파트 p의 리버브 센드 ID(§13.1: 35..42). 범위 밖 파트는 NumParams
// (setParam·Param이 무시하는 값)로 정규화한다.
func RevSend(p Part) ParamID {
	if p >= NumParts {
		return NumParams
	}
	return RevSendBase + ParamID(p)
}

// DelaySend — 파트 p의 딜레이 센드 ID(§13.1: 51..58). 범위 밖은 RevSend와 같다.
func DelaySend(p Part) ParamID {
	if p >= NumParts {
		return NumParams
	}
	return DelaySendBase + ParamID(p)
}

// 이름 있는 별칭(자주 쓰는 것만). 나머지는 BassAParams+BCutoff 꼴로 조합한다.
const (
	CutoffA = BassAParams + BCutoff
	CutoffB = BassBParams + BCutoff
	BDLevel = DrumParams + 0
	BDTune  = DrumParams + 1
	SDLevel = DrumParams + 2
	CHLevel = DrumParams + 4
	OHLevel = DrumParams + 6
	CPLevel = DrumParams + 8
	CYLevel = DrumParams + 10
)

// ParamSteps — 양자화 단계 수(값 = n/ParamSteps, n = 0..ParamSteps).
const ParamSteps = 4095

// DefaultParams — 기본값. New/Reset이 이 표를 적용한다.
func DefaultParams() [NumParams]float32 {
	var p [NumParams]float32
	for i := range p {
		p[i] = 0.5
	}
	for _, base := range [...]ParamID{BassAParams, BassBParams} {
		p[base+BTune] = 0.5   // 0반음
		p[base+BCutoff] = 0.45
		p[base+BReso] = 0.55
		p[base+BEnvMod] = 0.5
		p[base+BDecay] = 0.4
		p[base+BAccent] = 0.6
		p[base+BWave] = 0.0 // 톱니
		p[base+BOct] = 0.5  // 0옥타브
	}
	// B 파트는 리드(사용자 2026-09-06 "기본 재생에 그럴싸한 리드 신스가 없다 — 리드에 딜레이, 전체에
	// 리버브 살짝, 웻한 트랜스"): 한 옥타브 위·열린 컷오프·긴 디케이. 레지던트의 B 모드(ARP/CHORD)가
	// 코드 트랙을 연주하는 파트라 리드 자리에 맞다. A는 베이스로 남는다.
	p[BassBParams+BCutoff] = 0.6
	p[BassBParams+BOct] = 0.85 // B는 한 옥타브 위(≥ 2/3 → +1)
	p[BassBParams+BDecay] = 0.65
	p[BassBParams+BAccent] = 0.45
	for v := 0; v < NumDrums; v++ {
		p[DrumParams+ParamID(2*v)] = 0.8   // Level
		p[DrumParams+ParamID(2*v)+1] = 0.5 // Tune
	}
	p[Delay] = 0.4 // 리드 딜레이 리턴(레지던트가 페이즈 대역으로 갱신 — resident/energy.go delayTgt)
	p[Drive] = 0.2
	p[Comp] = 0.4
	p[Master] = 0.8
	p[Tempo] = 0.5 // 130 BPM
	// §13.1 — 믹서·버스 기본값 = "살짝 웻한 트랜스"(사용자 2026-09-06). 리버브는 전체에 조금(BD 제외),
	// 리드(B)는 딜레이 100%·코러스, 베이스(A)는 딜레이 조금. 센드가 하나라도 >0이면 버스가 켜지므로
	// 기본 출력 바이트가 바뀐다 — 기본값 해시는 fx2_test.go 상수로 재기준(변경마다 1회).
	p[BassALevel] = 1
	p[BassBLevel] = 0.85
	for pt := Part(0); pt < NumParts; pt++ {
		p[RevSend(pt)] = 0
		p[DelaySend(pt)] = 0
	}
	p[RevSend(BassA)] = 0.1
	p[RevSend(BassB)] = 0.4
	p[RevSend(SD)] = 0.3
	p[RevSend(CH)] = 0.15
	p[RevSend(OH)] = 0.25
	p[RevSend(CP)] = 0.35
	p[RevSend(CY)] = 0.3
	p[RevSize] = 0.6
	p[RevDamp] = 0.55
	p[RevMix] = 0.45
	p[ChoSendA] = 0
	p[ChoSendB] = 0.45
	p[ChoRate] = 0.35
	p[ChoDepth] = 0.4
	p[ChoMix] = 0.5
	p[DelaySend(BassA)] = 0.35
	p[DelaySend(BassB)] = 1
	return p
}

// quantize — 0..1 클램프 후 ParamSteps 단계로 양자화. (n, n/ParamSteps).
// 정수 변환은 플랫폼 무관 결정론이다. 나눗셈은 FMA 대상이 아니다.
// (이 파일에도 곱셈-덧셈이 있다: 전부 mul32로 감싼다.)
func quantize(v float32) (uint16, float32) {
	if v != v || v < 0 { // NaN 방어 포함
		v = 0
	}
	if v > 1 {
		v = 1
	}
	t := mul32(v, ParamSteps) // float32 곱을 정확히 감싸 FMA를 막는다(float64 곱은 FMADDD로 융합됐다 — objdump 실측)
	n := uint16(t + 0.5)
	return n, float32(n) / ParamSteps
}

// BPMOf — Tempo 파라미터(양자화된 0..1) → BPM 100..160.
func BPMOf(q float32) float64 {
	return 100 + float64(mul32(q, 60))
}
