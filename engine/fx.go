// fx.go — 이펙트 체인(T1c 소유 파일). engine.go가 호출하는 메서드 집합이 계약이다:
//
//	init()                              New/Reset — 버퍼 0, 계수 기본
//	setParam(k int, q float32)          k 0=Delay 1=Drive 2=Comp 3=Master
//	setTempo(samplesPerStep float64)    딜레이 시간(점8분 = 6스텝) 재계산
//	process(bass, drums, bdSide, dropDrive, delayIn float32) (l, r float32)
//	    체인: 베이스 사이드체인 덕킹(bdSide 트리거) → 드라이브(+dropDrive·0.2 상당) →
//	          템포 딜레이 핑퐁(입력 = delayIn — 파트별 센드 합, fx2.go가 계산) →
//	          마스터 → 소프트클립
//
// 스테레오 계약: 드라이 신호는 모노 센터(L=R), 딜레이 웻만 좌우가 다르다.
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴.
package engine

const delayBufLen = SampleRate // 1초 × 채널

// 사이드체인 릴리즈 시정수 120ms(샘플). duckRel = e^(−1/τ) = 2^(−1/(τ·ln2))를
// x=0 근방 테일러 전개로 직접 계산한다: 1 + u + u²/2, u = −1/τ(≈−1.74e-4).
// 전역 exp2를 쓰면 f≈1 구간의 다항 절단오차(~8e-5 상대)가 샘플당 곱 5760회로
// 복리화되어 τ가 120ms→83ms로 어긋난다(2026-09-05 T1c 실측 계산). 3차항은
// |u|³/6 ≈ 9e-13이라 무시 가능하다. 같은 exp2 테일러 급수의 국소 전개다.
const duckTauSamples = 0.12 * SampleRate // 5760
const duckU = float32(-1.0 / duckTauSamples)

// 피드백 1폴 LP(≈4kHz) 계수: c = 1 − e^(−2π·fc/fs) = 1 − 2^(−2π·fc/(fs·ln2)).
// 인자는 컴파일 타임 상수(덧셈이 없어 FMA 대상 아님). exp2의 다항 오차는 여기서
// f≈0.24 중간 대역이라 ~1e-7 — LP 음색용으로 충분하다.
const fbLPArg = -twoPiF * 4000 / (SampleRate * ln2F)

// 최종 소프트클립 스케일. 유리식 최대치는 x=8에서 8·91/603 = 1.20763이므로
// 1.20763·0.82 = 0.99026 ≤ 1.0 (float32 반올림 ±1e-7 포함 안전).
// (스펙이 제시한 0.83은 1.20763·0.83 = 1.0021로 1.0을 넘는다 — 실측으로 0.82를 택했다.)
const outClipScale float32 = 0.82

type fxChain struct {
	bufL, bufR [delayBufLen]float32
	wpos       int
	delaySamp  int
	delayMix   float32
	delayFB    float32
	drive      float32
	driveCor   float32 // 1/(1+0.5·Drive) — 드라이브 출력 레벨 보정
	comp       float32
	master     float32
	duck       float32 // 사이드체인 엔벨로프 0..1(1 = 최대 덕킹)
	duckRel    float32 // 릴리즈 샘플당 계수 e^(−1/τ), τ = 120ms
	fbLPzL     float32 // 피드백 LP 상태(L 출력 → R 버퍼 경로)
	fbLPzR     float32 // 피드백 LP 상태(R 출력 → L 버퍼 경로)
	fbLPc      float32 // LP 계수 1 − e^(−2π·4000/fs)
}

func (f *fxChain) init() {
	*f = fxChain{
		master:    0.8,
		delaySamp: 24000,
		driveCor:  1, // Drive 0 기본 — 없으면 setParam(1,·) 전에 드라이브 출력이 0이 된다(m10 FAIL-first에서 발견)
		duckRel:   mul32(duckU, 1+mul32(duckU, 0.5)) + 1, // e^(−1/τ)
		fbLPc:     1 - exp2(fbLPArg),
	}
}

func (f *fxChain) setParam(k int, q float32) {
	if q != q || q < 0 { // NaN 방어 포함 — 엔진은 양자화된 0..1만 주지만 직접 구동 방어
		q = 0
	}
	if q > 1 {
		q = 1
	}
	switch k {
	case 0:
		f.delayMix = mul32(q, 0.5)
		f.delayFB = mul32(q, 0.7)
	case 1:
		f.drive = q
		f.driveCor = 1 / (mul32(0.5, q) + 1)
	case 2:
		f.comp = q
	case 3:
		f.master = q
	}
}

func (f *fxChain) setTempo(samplesPerStep float64) {
	v := samplesPerStep * 6 // 점8분
	if v != v || v < 1 {    // NaN·0·음수는 최소 길이로 정규화(클램프 계약)
		v = 1
	}
	if v > delayBufLen-1 {
		v = delayBufLen - 1
	}
	f.delaySamp = int(v)
}

func (f *fxChain) process(bass, drums, bdSide, dropDrive, delayIn float32) (float32, float32) {
	// 1) 사이드체인 덕킹(베이스만) — 엔벨로프는 먼저 릴리즈 감쇠하고, 이번 샘플에
	//    BD 트리거(|bdSide| > 0.05)가 있으면 1로 세운다(어택 0ms ≤ 1ms).
	f.duck = mul32(f.duck, f.duckRel)
	if bdSide > 0.05 || bdSide < -0.05 {
		f.duck = 1
	}
	bassG := 1 - mul32(f.comp, mul32(0.8, f.duck)) // Comp 1 → 게인 0.2(−14dB)
	// 2) 드라이브 — 프리게인(1 + 7·Drive + 1.4·dropDrive; dropDrive 1 = Drive +0.2 상당) →
	//    유리식 소프트클립 x(27+x²)/(27+9x²), |x| ≤ 8 클램프 → 레벨 보정.
	s := mul32(bass, bassG) + drums
	pg := mul32(7, f.drive) + mul32(1.4, dropDrive) + 1
	x := mul32(s, pg)
	if x > 8 {
		x = 8
	}
	if x < -8 {
		x = -8
	}
	x2 := mul32(x, x)
	x = mul32(x, 27+x2) / (mul32(9, x2) + 27)
	s = mul32(x, f.driveCor)
	// 3) 템포 딜레이 핑퐁 — 센드 합 입력(delayIn, §13.2)은 L 버퍼로, L 출력은 R 버퍼로 피드백,
	//    R 출력은 L 버퍼로. 피드백 경로에 1폴 LP(4kHz)를 스트리밍으로 걸어
	//    반복마다 어두워진다. q=0(mix=fb=0)이면 완전 바이패스 — 읽기도 건너뛴다.
	var wl, wr float32
	if f.delayMix != 0 || f.delayFB != 0 {
		r := f.wpos - f.delaySamp
		if r < 0 {
			r += delayBufLen
		}
		lOut := f.bufL[r]
		rOut := f.bufR[r]
		f.fbLPzL = mul32(f.fbLPc, lOut-f.fbLPzL) + f.fbLPzL
		f.fbLPzR = mul32(f.fbLPc, rOut-f.fbLPzR) + f.fbLPzR
		f.bufL[f.wpos] = delayIn + mul32(f.delayFB, f.fbLPzR)
		f.bufR[f.wpos] = mul32(f.delayFB, f.fbLPzL)
		f.wpos++
		if f.wpos >= delayBufLen {
			f.wpos = 0
		}
		wl = mul32(lOut, f.delayMix)
		wr = mul32(rOut, f.delayMix)
	}
	// 4) 마스터(선형) → 5) 최종 소프트클립(|out| ≤ 0.9903 보장).
	l := f.outClip(mul32(s+wl, f.master))
	r := f.outClip(mul32(s+wr, f.master))
	return l, r
}

// outClip — 최종 유리식 소프트클립 + 0.82 스케일. 입력 ±8 클램프 뒤 최대
// 1.20763·0.82 = 0.99026 ≤ 1.0(상수로 보장, 위 outClipScale 주석).
func (f *fxChain) outClip(x float32) float32 {
	if x > 8 {
		x = 8
	}
	if x < -8 {
		x = -8
	}
	x2 := mul32(x, x)
	y := mul32(x, 27+x2) / (mul32(9, x2) + 27)
	return mul32(y, outClipScale)
}
