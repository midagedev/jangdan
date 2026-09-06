// samplerpack.go — 내장 샘플 팩(§14.2 항목 2 "① 내장 팩 먼저").
//
// **팩 표(packTab)는 리드가 못박은 계약**이고, 이 파일의 bakePack은 그 표가 선언한 자리에
// 파형을 써 넣기만 한다. 표를 바꾸면 재생 코드(sampler.go)·UI·해시가 함께 움직이므로
// 슬롯 수·길이·기준음·루프 지점은 여기 리터럴이 정본이다.
//
// **왜 파일이 아니라 합성인가**: 엔진은 순수 Go·외부 의존 0·fmt/os 없음이고 워클릿은
// 파일을 읽지 못한다. 팩을 바이트로 넣으려면 wasm에 3MB를 실어야 하는데 현재 워클릿은
// 49KB다. 그래서 내장 팩은 **결정론적으로 합성해 굽는다**(자체 제작 = CC0). 샘플러의
// 본체는 재생 경로(피치 리샘플·엔벌로프·루프)이고, 팩은 "바이트가 어떻게 버퍼에 들어왔나"일
// 뿐이다. 사용자 업로드 경로(§14.2 ②)는 같은 버퍼에 호스트가 써 넣는 후속 라운드다.
//
// **어디에 사는가**: 팩은 Engine 바깥의 패키지 전역이다. Reset이 `*e = Engine{}`로 구조체를
// 통째로 비우므로 Engine 안에 두면 Reset마다 1.2MB memset + 재굽기가 된다. 팩은 seed와
// 무관한 불변 데이터라(같은 팩이면 같은 바이트) 엔진 인스턴스가 여럿이어도 하나면 된다.
// 굽기는 최초 samplerDev.init에서 1회(ensurePack 가드) — 패키지 init()에 기대지 않는다
// (wasm-unknown 타깃의 _initialize 호출 여부에 계약을 걸지 않기 위해서).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
// 엔트로피는 xorshift32만(math/rand 금지). 할당 없음.
package engine

const (
	packSlots  = 8      // 팩 슬롯 수(SmpSelect 0..7)
	packFrames = 290400 // 전 슬롯 길이 합 = 6.05초 @48k. packTab의 마지막 off+n과 정확히 같다
)

// packEntry — 팩 슬롯 하나. off/n은 packBuf 안의 프레임 구간, root는 이 파형이 녹음된 음
// (C1 기준 반음 — voice.go·harmony.go와 같은 도메인), loop는 슬롯 상대 루프 시작 프레임이다.
// loop == n 이면 원샷(끝나면 소리가 멈춘다), loop < n 이면 게이트가 살아 있는 동안 [loop, n)을 돈다.
type packEntry struct {
	off, n uint32
	root   uint8
	loop   uint32
}

// packTab — 리드 계약. 이 리터럴이 팩의 정본이다(길이·기준음·루프).
// off는 앞 슬롯들의 n 누적과 정확히 같아야 한다(TestPackTable이 단언).
var packTab = [packSlots]packEntry{
	{off: 0, n: 48000, root: 24, loop: 48000},      // 0 PLUCK  1.00s C3 뜯은 현(카플러스-스트롱)
	{off: 48000, n: 72000, root: 36, loop: 72000},  // 1 BELL   1.50s C4 비조화 부분음 금속 벨
	{off: 120000, n: 48000, root: 12, loop: 48000}, // 2 VOX    1.00s C2 포먼트 "아"(노동요 목소리)
	{off: 168000, n: 9600, root: 24, loop: 9600},   // 3 WOOD    0.20s C3 우드블록 타격
	{off: 177600, n: 12000, root: 24, loop: 12000}, // 4 SHAKE   0.25s C3 셰이커(노이즈 버스트)
	{off: 189600, n: 38400, root: 24, loop: 0},     // 5 TAPE    0.80s C3 테이프 히스·워블 — 전 구간 루프
	{off: 228000, n: 33600, root: 24, loop: 33600}, // 6 BREATH  0.70s C3 숨 스웰(필터 노이즈)
	{off: 261600, n: 28800, root: 12, loop: 28800}, // 7 SUB     0.60s C2 서브 붐(사인 스윕)
}

// PackNames — UI 라벨의 단일 소유자(앱이 engine.PackNames를 읽는다). 핫 루프에 없다.
var PackNames = [packSlots]string{"PLUCK", "BELL", "VOX", "WOOD", "SHAKE", "TAPE", "BREATH", "SUB"}

// packBuf — 구운 파형(모노 48k). 전역·불변(굽기 이후 쓰기 없음).
var packBuf [packFrames]float32

var packReady bool

// ensurePack — 최초 1회 굽는다. samplerDev.init이 부른다(Reset마다 재굽기 금지).
func ensurePack() {
	if packReady {
		return
	}
	bakePack(&packBuf)
	packReady = true
}

// bakePack — packTab이 선언한 8구간에 서로 다른 성격의 파형을 굽는다(P5-sampler-pack).
// 슬롯별 합성법과 수치 근거는 아래 각 baker 주석이 정본. 슬롯마다 공통 후처리 pkFinish
// (DC 제거 → 피크 0.9 정규화)를 마지막에 돌린다 — 계약 "피크 0.85..0.95"·"|평균| ≤ 0.01"은
// 이 한 곳에서 만족된다. 원샷 끝의 declick(닫는 램프)은 각 baker가 pkClose로 — 감쇠형
// 파형이라도 마지막 한 샘플이 0 근처임을 구조적으로 보장한다.
func bakePack(buf *[packFrames]float32) {
	for i := 0; i < packSlots; i++ {
		e := packTab[i]
		s := buf[e.off : e.off+e.n]
		switch i {
		case 0:
			bakePluck(s, e.root)
		case 1:
			bakeBell(s, e.root)
		case 2:
			bakeVox(s, e.root)
		case 3:
			bakeWood(s)
		case 4:
			bakeShake(s)
		case 5:
			bakeTape(s)
		case 6:
			bakeBreath(s)
		default:
			bakeSub(s, e.root)
		}
		pkFinish(s)
	}
}

// bakePluck — 슬롯0 카플러스-스트롱 뜯은 현: 지연선(1주기) + 1폴 평균(인접 2탭) + 감쇠.
//
//	d = round(SR/midiHz(root)) — root 24 → 367. 지연선 길이 l = d+2, 두 탭 지연이
//	(l, l−1)이라 실효 지연 l−0.5 = 368.5프레임 → 130.26Hz(목표 130.81과 −7센트),
//	ZCR = 2f ≈ 260 교차/초(계약 261±60 중앙).
//	감쇠는 **revolution당** 한 번 곱한다 — 샘플당으로 착각하고 τ를 넣으면 실효
//	시정수가 주기(368.5)만큼 늘어난다(실측: 넣은 0.32s가 3.5s로 측정). u = −l/(τ·SR)
//	패스 환산 후 국소 전개. τ 0.32s → 측정 0.32s(계약 0.20..0.60).
//	여재: 1주기 사인 + 고조파(2차 20%·3차 10%) + 어두운 잡음 25%. 잡음 여재는 못
//	쓴다 — 1폴 평균의 조화 손실이 낮은 고조파를 못 죽인다(2차 손실률 ~0.019/초라
//	밝은 질감이 1s 내내 남는다 — 잡음 여재 실측 ZCR 1976, 예산 321의 6배). 사인
//	위주 여재로 ZCR을 260에 고정하고 잡음은 어택 질감분만. 잡음 기울기(~180/s)가
//	기본파 영교차 기울기(~818/s)보다 작아 추가 교차를 만들지 않는다.
func bakePluck(s []float32, root uint8) {
	d := int(48000.0/float64(midiHz(root)) + 0.5) // round(SR/f) — 367 (root 24)
	if d < 8 {                                    // 계약 밖 root 방어(클램프 — 패닉 금지 규칙)
		d = 8
	}
	if d > 700 {
		d = 700
	}
	l := d + 2 // 실효 지연 l−0.5 = 368.5 → 130.26Hz(위 주석)
	var dl [702]float32
	r := xorshift32{0x5EED01} // 고정 시드 — 팩은 seed와 무관한 상수 데이터
	g := lpCoef(500)          // 여재 잡음 LP
	var lp float32
	p4 := 4.0 / float32(l) // 1주기 위상 증분(oscSine 도메인 1주기 = 4)
	for j := 0; j < l; j++ {
		ph := mul32(float32(j), p4)
		x := oscSine(ph) + mul32(0.20, oscSine(mul32(ph, 2))) + mul32(0.10, oscSine(mul32(ph, 3)))
		lp = lp + mul32(g, noiseBipolar(r.next())-lp)
		dl[j] = x + mul32(0.25, lp)
	}
	u := float32(-float64(l) / (0.32 * SampleRate)) // 감쇠 패스 환산(위 주석)
	dec := 1 + u + mul32(mul32(u, u), 0.5)
	p := 0 // 다음 쓰기 위치 = 가장 오래된 값
	for j := 0; j < len(s); j++ {
		a := dl[p]
		q := p + 1
		if q == l {
			q = 0
		}
		b := dl[q]
		s[j] = a
		dl[p] = mul32(dec, mul32(0.5, a+b)) // 1폴 평균 + 감쇠(revolution당)
		p++
		if p == l {
			p = 0
		}
	}
	pkClose(s, 320)
}

// bakeBell — 슬롯1 금속 벨: 비조화 부분음 7개 + 어택 클릭. 배음비
// [0.50 1.19 1.75 2.13 2.66 3.31 4.17]×root — 실벨의 hum/prime/tierce/quint/nominal
// 계열(정수배 아님 — 기저 0.5×의 정수배와 최소 34Hz 떨어져 스펙트럼 게이트가 잡는다).
// 부분음별 감쇠 τ [2.6 1.5 1.1 0.85 0.7 0.58 0.48]s — 저성분이 오래 남는다("각 부분음
// 감쇠가 다름"). 합성 포락 τ ≈ 1.05s(계약 ≥ 0.80, RMS 창 기울기로 측정). 끝 declick
// 16800(0.35s) — τ 계약(느린 감쇠)과 원샷 조용한 끝(≤피크 5%)을 함께 만족시키는 릴리스.
func bakeBell(s []float32, root uint8) {
	base := midiHz(root)
	ratio := [7]float32{0.50, 1.19, 1.75, 2.13, 2.66, 3.31, 4.17}
	amp := [7]float32{0.60, 1.00, 0.85, 0.70, 0.60, 0.45, 0.35}
	tau := [7]float64{2.6, 1.5, 1.1, 0.85, 0.7, 0.58, 0.48}
	var ph, inc, dec [7]float32
	for k := 0; k < 7; k++ {
		inc[k] = mul32(mul32(base, ratio[k]), 4.0/48000.0) // oscSine 위상 도메인(1주기 = 4)
		dec[k] = pkDecay(tau[k])
		ph[k] = mul32(float32(k), 0.61) // 출발 위상 분산(전 부분음 동시 피크 방지)
	}
	r := xorshift32{0xBE11}
	clickDec := pkDecay(0.003)
	clickLP := lpCoef(6000)
	click := float32(1)
	var clp float32
	for j := 0; j < len(s); j++ {
		var x float32
		for k := 0; k < 7; k++ {
			x += mul32(amp[k], oscSine(ph[k]))
			amp[k] = mul32(amp[k], dec[k])
			ph[k] += inc[k]
			if ph[k] >= 4 {
				ph[k] -= 4
			}
		}
		clp = clp + mul32(clickLP, noiseBipolar(r.next())-clp)
		s[j] = x + mul32(0.35, mul32(click, clp))
		click = mul32(click, clickDec)
	}
	pkClose(s, 16800)
}

// bakeVox — 슬롯2 사람 목소리 "아": 밴드리미티 톱니(가산 합성) → 포먼트 공진 3병렬.
// 톱니 = Σ_{k=1..144} sin(kθ)/k(상한 9.4kHz — 나 이상은 숨소리 성분이라 여기선 생략).
// 포먼트 F1 730 F2 1090 F3 2440Hz([ɑ] 모음의 정준값), 2폴 BPF Q 9/11/14(대역폭
// 81/99/174Hz). 5.2Hz 비브라토(깊이 0.4%) — 사람 성대의 미세 떨림. 유지형 엔벨로프:
// 어택 30ms + 완만 감쇠 τ 4s → RMS(0.55~0.65s)/RMS(0.05~0.15s) ≈ 0.88(계약 ≥ 0.50).
func bakeVox(s []float32, root uint8) {
	const K = 144                            // 고조파 수 — 65.4Hz × 144 ≈ 9.4kHz
	const tabN = 2048                        // 한 주기 파형표 길이(리드 2026-09-07 — 아래 주석)
	base := mul32(midiHz(root), 1.0/48000.0) // 주기당 1로 정규화한 위상 증분

	// 성대음(1/k 톱니)은 **한 주기를 표로 굽고 읽는다**. 샘플마다 144개 사인을 더하면
	// 6.9M oscSine이 되어 이 슬롯 하나가 팩 굽기 50ms 중 43ms를 먹었다(리드 실측
	// 2026-09-07). 표는 2048×144 = 295k회로 23배 싸고, 읽기는 선형 보간이다. 최고
	// 고조파(144)가 표에서 14.2표본/주기라 보간 오차 ~2%인데, 뒤따르는 포먼트 BPF
	// (Q 9..14, 730/1090/2440Hz)가 그 대역을 이미 −30dB 넘게 눌러 들리지 않는다.
	// 표를 쓰면 대역제한이 유지된다는 성질(에일리어싱 없음)도 그대로다.
	var tab [tabN + 1]float32 // +1 = 보간 센티널(마지막 칸 = 첫 칸과 같은 위상)
	for i := 0; i <= tabN; i++ {
		th := mul32(float32(i), 4.0/tabN) // oscSine 도메인(1주기 = 4)
		var v float32
		for k := 1; k <= K; k++ {
			v += mul32(1/float32(k), oscSine(mul32(th, float32(k)))) // 톱니 진폭 1/k
		}
		tab[i] = v
	}

	var f1, f2, f3 pkBiq // 포먼트 공진기(2폴 BPF)
	f1.setBPF(730, 9)
	f2.setBPF(1090, 11)
	f3.setBPF(2440, 14)
	ph, vibPh := float32(0), float32(0)
	vibInc := mul32(5.2, 4.0/48000.0)
	dec := pkDecay(4.0)
	sd := float32(1) // 완만 감쇠 성분
	for j := 0; j < len(s); j++ {
		inc := mul32(base, 1+mul32(0.004, oscSine(vibPh))) // 비브라토
		x := mul32(ph, tabN)
		i0 := int32(x)
		f := x - float32(i0)
		saw := tab[i0] + mul32(tab[i0+1]-tab[i0], f)
		v := f1.run(saw) + mul32(0.55, f2.run(saw)) + mul32(0.30, f3.run(saw))
		var atk float32
		if j < 1440 { // 어택 30ms
			atk = mul32(float32(j), 1.0/1440.0)
		} else {
			atk = 1
		}
		s[j] = mul32(mul32(atk, sd), v)
		sd = mul32(sd, dec)
		ph += inc
		if ph >= 1 {
			ph -= 1
		}
		vibPh += vibInc
		if vibPh >= 4 {
			vibPh -= 4
		}
	}
	pkClose(s, 9600) // 릴리즈 0.2s
}

// bakeWood — 슬롯3 우드블록: 클릭 트랜지언트(τ 2ms 잡음, LP 3.5kHz) + 공진 3개
// (827Hz τ 20ms · 1342Hz τ 14ms · 415Hz τ 25ms). 음높이는 고정 — 타격음의 음색
// 식별은 공진 배치가 하고, root(24)는 재생 피치 매핑이 쓴다. 피크 이후 0.05s 안에
// RMS가 피크의 20% 밑으로 떨어진다(가장 느린 성분 τ 25ms·진폭 0.3 → 0.05s에 ~10%).
func bakeWood(s []float32) {
	inc0 := mul32(827, 4.0/48000.0)
	inc1 := mul32(1342, 4.0/48000.0)
	inc2 := mul32(415, 4.0/48000.0)
	a0, a1, a2 := float32(1), float32(0.6), float32(0.3)
	dec0, dec1, dec2 := pkDecay(0.020), pkDecay(0.014), pkDecay(0.025)
	var p0, p1, p2 float32
	r := xorshift32{0x600D}
	clickDec := pkDecay(0.002)
	clickLP := lpCoef(3500)
	click := float32(1)
	var clp float32
	for j := 0; j < len(s); j++ {
		x := mul32(a0, oscSine(p0)) + mul32(a1, oscSine(p1)) + mul32(a2, oscSine(p2))
		clp = clp + mul32(clickLP, noiseBipolar(r.next())-clp)
		s[j] = x + mul32(0.5, mul32(click, clp))
		a0 = mul32(a0, dec0)
		a1 = mul32(a1, dec1)
		a2 = mul32(a2, dec2)
		click = mul32(click, clickDec)
		p0 += inc0
		if p0 >= 4 {
			p0 -= 4
		}
		p1 += inc1
		if p1 >= 4 {
			p1 -= 4
		}
		p2 += inc2
		if p2 >= 4 {
			p2 -= 4
		}
	}
	pkClose(s, 256)
}

// bakeShake — 슬롯4 셰이커: 백색잡음 → 밴드통과(5800Hz 중심 — bandLP2, drums.go의
// 안정성 입증된 LP2−LP2 구성 재사용) → 어택 6ms + 감쇠 τ 55ms. 대역 중심이 높아
// ZCR ≥ 3000 교차/초가 자연히 성립한다.
func bakeShake(s []float32) {
	var band bandLP2
	band.set(7400, 4600) // 피크 ≈ √(hi·lo) ≈ 5837Hz
	r := xorshift32{0x5AAE}
	dec := pkDecay(0.055)
	var amp float32
	for j := 0; j < len(s); j++ {
		x := band.process(noiseBipolar(r.next()))
		if j < 288 { // 어택 6ms — 셰이커는 막대로 때리는 소리가 아니라 흔들어 여는 소리
			amp = mul32(float32(j), 1.0/288.0)
		} else {
			amp = mul32(amp, dec)
		}
		s[j] = mul32(amp, x)
	}
	pkClose(s, 384)
}

// bakeTape — 슬롯5 테이프 히스 + 느린 워블, **전 구간 루프(loop=0)**. 잡음 → 2폴
// 하이패스(1500Hz — 원폴 2단, bandLP2와 같은 안정 구성) → 진폭 워블. 워블 주파수는
// 0.8s 슬롯에 정확히 1주·3주(1.25·3.75Hz) — 루프에서 변조 궤적까지 이어진다.
// 양쪽 끝 96프레임 u² 엣지 페이드: 양끝이 정확히 0에 수렴해 루프 이음 계약
// |buf[n−1] − buf[0]| ≤ 0.05 를 구조적으로 만족시킨다(페이드 폭 2ms — 히스에서
// 들리지 않는다). 창 RMS 통계는 대칭 페이드로 양쪽이 같이 내려가 비윌 유지된다.
func bakeTape(s []float32) {
	g := lpCoef(1500) // HP = x − LP2(x): 원폴 2단(bandLP2.process와 같은 구성)
	var l1, l2 float32
	r := xorshift32{0x7A9E}
	w1inc := mul32(1.25, 4.0/48000.0)
	w3inc := mul32(3.75, 4.0/48000.0)
	w1, w3 := float32(0), float32(0.8276) // 3차 워블 시작 위상(1.3라디안 환산)
	for j := 0; j < len(s); j++ {
		x := noiseBipolar(r.next())
		l1 = l1 + mul32(g, x-l1)
		l2 = l2 + mul32(g, l1-l2)
		hp := x - l2
		m := 1 + mul32(0.18, oscSine(w1)) + mul32(0.07, oscSine(w3))
		s[j] = mul32(m, hp)
		w1 += w1inc
		if w1 >= 4 {
			w1 -= 4
		}
		w3 += w3inc
		if w3 >= 4 {
			w3 -= 4
		}
	}
	const F = 96
	for k := 0; k < F; k++ {
		u := mul32(float32(k+1), 1.0/F)
		s[k] = mul32(s[k], mul32(u, u)) // 열림: 0 → 1
		w := 1 - u
		s[len(s)-F+k] = mul32(s[len(s)-F+k], mul32(w, w)) // 닫힘: 1 → 0
	}
}

// bakeBreath — 슬롯6 숨 스웰: 잡음원을 어두운 원폴 LP(700Hz)와 밝은 대역통과
// (1900Hz, bandLP2)로 나눠 **필터가 열리며** 밝아지는 크로스페이드 + 볼륨 스웰
// (0.1 → 1.0, 0.42s에 정점 — sin² 곡선). RMS(앞 20%) / RMS(40~60%) ≈ 0.25
// (계약 < 0.70). 릴리즈 0.15s.
func bakeBreath(s []float32) {
	var band bandLP2
	band.set(2400, 1450) // 피크 ≈ 1865Hz
	darkG := lpCoef(700)
	r := xorshift32{0xB8A7}
	var dark float32
	for j := 0; j < len(s); j++ {
		nz := noiseBipolar(r.next())
		dark = dark + mul32(darkG, nz-dark)
		bright := band.process(nz)
		u := mul32(float32(j), 1.0/20160.0) // 스웰 0.42s
		if u > 1 {
			u = 1
		}
		sv := sin5(mul32(u, 1.57079637))
		vol := 0.10 + mul32(0.90, mul32(sv, sv))
		qb := mul32(float32(j), 1.0/21600.0) // 여림 0.45s
		if qb > 1 {
			qb = 1
		}
		x := dark + mul32(mul32(0.15, 1-qb)+qb, bright-dark) // 밝기 0.15 → 1
		s[j] = mul32(vol, x)
	}
	pkClose(s, 7200)
}

// bakeSub — 슬롯7 서브 붔: 아래로 떨어지는 사인 스윕. 시작 2×root(130.8Hz)에서
// 지수적으로 하강(τ 0.365s — 0.45s에 38Hz 도달 후 유지), 진폭은 어택 6ms + 완만
// 감쇠 τ 0.9s. 평균 순간주파수 ≈ 75Hz → ZCR ≈ 150 교차/초(계약 ≤ 200).
func bakeSub(s []float32, root uint8) {
	f := mul32(midiHz(root), 2)
	rf := pkDecay(0.365)
	dec := pkDecay(0.9)
	ph := float32(0)
	var amp float32
	for j := 0; j < len(s); j++ {
		if j < 288 { // 어택 6ms — 감쇠는 램프 정점(1)에서 이어받는다
			amp = mul32(float32(j), 1.0/288.0)
		} else {
			amp = mul32(amp, dec)
		}
		s[j] = mul32(amp, oscSine(ph))
		ph += mul32(f, 4.0/48000.0)
		if ph >= 4 {
			ph -= 4
		}
		f = mul32(f, rf)
		if f < 38 {
			f = 38 // 하한 클램프 — 붔의 바닥
		}
	}
	pkClose(s, 7200)
}

// pkFinish — 슬롯 공통 후처리: 평균 차감(DC 제거) → 피크 0.9 정규화. 계약 1·2를
// 이곳 하나로 만족시킨다(모든 baker의 원시 출력은 어느 쪽도 보장하지 않는다).
func pkFinish(s []float32) {
	var sum float32
	for j := 0; j < len(s); j++ {
		sum += s[j]
	}
	mean := mul32(sum, 1/float32(len(s)))
	var peak float32
	for j := 0; j < len(s); j++ {
		s[j] -= mean
		v := s[j]
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak < 1e-9 {
		return // 0 나눗기 방어(클램프). 죽은 슬롯은 RMS 게이트(TestPackBakeCommon)가 잡는다
	}
	g := mul32(0.9, 1/peak)
	for j := 0; j < len(s); j++ {
		s[j] = mul32(s[j], g)
	}
}

// pkDecay — 시정수 tau(초)의 샘플당 감쇠 계수. 국소 전개로 유도한다(엔진 규칙):
// 매 샘플 곱하는 계수를 exp2로 유도하면 근사 오차(~1.5e-4)가 누적돼 시정수가
// 어긋난다(voice.go 파일 주석의 사고 — τ 1000ms가 469ms). u = −1/(τ·SR),
// coef = 1 + u + u²/2 — 오차 O(u³) ≈ 1e-13/샘플, 시정수 보정 불필요.
// drums.go의 decayCoef(exp2+decayCal)는 필드 보정된 기존 헬퍼지만 이 파일은
// 스펙이 국소 전개를 지정했다 — 두 방식 모두 dec^N = e^(−N/(τ·SR))를 ±2e-6으로
// 맞춘다(등가, 유도 경로만 다르다).
func pkDecay(tau float64) float32 {
	u := float32(-1.0 / (tau * SampleRate))
	return 1 + u + mul32(mul32(u, u), 0.5)
}

// pkClose — 원샷 끝 declick: 마지막 f 프레임에 (1−u)² 램프(u = 1/f..1)를 곱한다.
// 마지막 샘플이 정확히 0에 수렴 — "|buf[off+n−1]| ≤ 0.01"과 "마지막 256프레임
// max|x| ≤ 피크×0.05" 계약의 구조적 보장. (1−u)²는 u=0에서 기울기 0 — 램프 시작
// 지점이 불연속 없이 이어진다.
func pkClose(s []float32, f int) {
	n := len(s)
	if f > n {
		f = n
	}
	inv := 1 / float32(f)
	for k := 0; k < f; k++ {
		u := mul32(float32(k+1), inv)
		w := 1 - u
		s[n-f+k] = mul32(s[n-f+k], mul32(w, w))
	}
}

// pkBiq — 2폴 공진 바이쿼드 BPF(RBJ 정수 피크 이득형, DF1). 포먼트 공진에만 쓴다.
// 기존 2폴 대역통과인 drums.go의 bandLP2(LP2−LP2)는 비공진(피크 이득 < 1)이라
// 성도 포먼트의 공진 특성을 낼 수 없다 — 검색 후 신규 작성(보고서 참조).
// 계수는 setBPF에서 1회 유도(코사인은 sin5(π/2−ω) 동등식 — math 금지 규칙).
// 안정성: 사용 범위 fc ≤ 2440Hz·Q ≤ 14 — 나이퀴스트 근처 고역 고Q(bandLP2 주석의
// 불안정 영역)와는 거리가 멀다.
type pkBiq struct {
	b0, b1, b2, a1, a2 float32 // a0으로 정규화된 전달 계수
	x1, x2, y1, y2     float32 // DF1 상태
}

func (f *pkBiq) setBPF(hz, q float32) {
	w := mul32(hz, 1.30899694e-4) // 2π/48000
	sw := sin5(w)                 // w ∈ (0, π) — 정의역 내
	cw := sin5(1.57079637 - w)    // cos(w) = sin(π/2 − w)
	al := mul32(sw, 0.5/q)        // α = sinω/(2Q)
	inv := 1 / (1 + al)           // 1/a0
	f.b0 = mul32(al, inv)
	f.b1 = 0
	f.b2 = -f.b0
	f.a1 = -mul32(mul32(2, cw), inv)
	f.a2 = mul32(1-al, inv)
	f.x1, f.x2, f.y1, f.y2 = 0, 0, 0, 0
}

func (f *pkBiq) run(x float32) float32 {
	y := mul32(f.b0, x) + mul32(f.b1, f.x1) + mul32(f.b2, f.x2) - mul32(f.a1, f.y1) - mul32(f.a2, f.y2)
	f.x2 = f.x1
	f.x1 = x
	f.y2 = f.y1
	f.y1 = y
	return y
}

// midiHz — C1 기준 반음(0..MaxSemis) → Hz. voice.go baseInc와 같은 기준(note 0 = MIDI 24 = C1).
func midiHz(note uint8) float32 {
	if note > MaxSemis {
		note = MaxSemis
	}
	// 440 · 2^((24+note-69)/12)
	return mul32(440, exp2(mul32(float32(int(note)-45), 1.0/12.0)))
}
