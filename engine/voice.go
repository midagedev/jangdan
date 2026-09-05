// 베이스라인 보이스 — 톱니 오실레이터 → 4극 캐스케이드 원폴 LP + 레조넌스
// 피드백 → 엔벌로프. 정확한 303 모델이 아니라 비용이 대표적인 스탠드인.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go
// 파일 주석의 실측 근거 참조: float32 정체성 변환은 gc가 지우고 FMADDS로 융합).
package engine

// bassVoice — 스탠드인 액시드 베이스. 톱니는 나이브(에일리어싱 있음) —
// 폴리BLEP 보정은 후속 라운드 과제로 남긴다(이 파일의 주석이 그 계약).
type bassVoice struct {
	phase      float32 // 톱니 위상 [0,1)
	y1, y2, y3 float32 // 원폴 LP 캐스케이드 상태
	y4         float32
	fenv       float32 // 필터 엔벌로프 [0,1]
	aenv       float32 // 앰프 엔벌로프
	inc        float32 // 위상 증가량 = freq/SampleRate
	active     bool
}

func (v *bassVoice) trigger(freq float32, level float32) {
	v.inc = freq / SampleRate
	v.aenv = level
	v.fenv = 1
	v.active = true
}

// process — 샘플 1개. 컷오프 계수를 매 샘플 exp2 근사로 갱신한다(303류의
// 표준 구조이고 이 스파이크의 측정 부하를 대표하게 하기 위함).
func (v *bassVoice) process(e *Engine) float32 {
	if !v.active {
		return 0
	}
	// 엔벨로프 감쇠(곱셈만 — 덧셈에 직접 닿지 않는다)
	v.aenv = v.aenv * e.ampDecay
	v.fenv = v.fenv * e.fltDecay
	// 컷오프 = 기본(파라미터) + 엔모드 옥타브 성분
	oct := mul32(e.envOct, v.fenv)
	hz := mul32(e.cutBaseHz, exp2(oct))
	if hz > 16000 {
		hz = 16000
	}
	// 원폴 계수 g = 1 − exp(−2π·hz/SR) = 1 − 2^(−w), w = 2π·hz/(SR·ln2)
	w := mul32(1.88875e-4, hz)
	g := 1 - exp2(-w)
	if g > 0.999 {
		g = 0.999
	}
	// 나이브 톱니
	v.phase = v.phase + v.inc
	if v.phase >= 1 {
		v.phase -= 1
	}
	tp := mul32(2, v.phase)
	saw := tp - 1
	// 레조넌스 피드백 + 클램프(피드백 폭주 방지 — 4극 모두 |상태| ≤ 4 유지)
	fb := mul32(e.resoK, v.y4)
	x := saw + fb
	if x > 4 {
		x = 4
	}
	if x < -4 {
		x = -4
	}
	// 4극 캐스케이드 원폴
	d := x - v.y1
	v.y1 = mul32(g, d) + v.y1
	d = v.y1 - v.y2
	v.y2 = mul32(g, d) + v.y2
	d = v.y2 - v.y3
	v.y3 = mul32(g, d) + v.y3
	d = v.y3 - v.y4
	v.y4 = mul32(g, d) + v.y4
	return mul32(v.y4, v.aenv)
}
