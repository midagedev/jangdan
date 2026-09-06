// fx2.go — 리버브·코러스 장치(P4-fx2 소유 파일; 믹서 버스는 rack.go 케이블로 승격).
// 계약 원본: docs/impl-plan-2026-09-05.md §13.1(파라미터 표)·§13.2(신호 흐름).
//
// reverb·chorus는 각각 process(in) (l, r) 형태의 독립 장치다(KindReverb·KindChorus).
// 센드 합(입력 포트)과 리턴(출력 포트)은 이제 랙의 케이블이 잇는다(rack.go — 옛 mixBus는
// 기본 랙의 케이블 표로 승격됐다, §14.1). 장치 안에서 다른 파트를 모른다.
//
// 해시 불변 근거(§13.1): 레벨 기본 1.0 곱은 항등(mul32(x,1)==x), 게인>0 입력 케이블이
// 없는 리버브·코러스는 live=false로 처리되지 않고 그 출력 케이블은 합산에서 건너뛴다 —
// 리턴을 "더하지 않는다"(x+0도 −0에서 비트가 달라질 수 있어 "0을 더한다"가 아니다).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 참조).
// 버퍼는 전부 고정 배열(New/Reset에서 0) — 핫 루프 무할당 계약.
package engine

// revCombBase — 콤 기준 길이(§13.2: 1557·1617·1491·1422). 정적 데이터(할당 아님).
var revCombBase = [4]int{1557, 1617, 1491, 1422}

const (
	revPreDelay = SampleRate * 20 / 1000 // 프리딜레이 20 ms = 960 샘플
	revNumCombs = 4
	revNumAPs   = 2
	apCap       = 556       // 올패스 최대 길이 = 556(2단)
	revCombCap  = 2287      // 콤 상한: 기준 최대 1617×1.4 → 2263.8, R +23 → 2287
	choBufLen   = 1024      // 코러스 버퍼(기준 576 + 깊이 288 + 보간 1 < 1024)
	choBase     = 576       // 코러스 기준 지연 12 ms
	choMaxDep   = 288       // 코러스 최대 깊이 6 ms
	apG         = 0.5       // 올패스 게인 — DC 이득 정확히 1(아래 유도)
	log2_30     = 4.9068906 // log2(30) — ChoRate 지수 매핑
	revInGain   = 0.25      // 콤 뱅크 입력 정규화 — 병렬 콤 4의 DC 이득 4/(1−g)
	// (g 0.70..0.86 → 13.3..28.6×)를 3.3..7.1×로 낮춘다. BD 같은 느린 꼬리가 저역
	// 이득으로 붙들려 2초 뒤에도 −60dB 위로 남던 실측(0.0016)을 닫는다. 슈뢰더
	// 원형의 입력 스케일과 같은 자리(§13.2는 콤 위상·피드백만 규정).

)

// reverb — Schroeder(§13.2): 프리딜레이 20 ms → 병렬 콤 4(피드백 경로 1폴 LP) →
// 직렬 올패스 2. R 채널은 콤 길이 +23(스테레오 탈상관). 올패스는 참 슈뢰더 형태로
// 구현한다: y = g·x + b(지연 버퍼 읽기), 쓰기 x − g·y. 전달함수 (g+z^−D)/(1+g·z^−D)로
// |H(e^jω)| ≡ 1 — DC 이득도 정확히 1(자기 리뷰 항: 클립 없이 DC를 증폭하지 않는다).
type reverb struct {
	pre   [revPreDelay]float32 // 프리딜레이(모노 — 채널 분리는 콤 길이가 담당)
	preW  int
	comb  [2][revNumCombs][revCombCap]float32 // [L/R][콤][샘플]
	combW [2][revNumCombs]int
	combZ [2][revNumCombs]float32      // 콤 피드백 LP 상태
	ap    [2][revNumAPs][apCap]float32 // [L/R][단계][샘플]
	apW   [2][revNumAPs]int
	lenL  [revNumCombs]int // 콤 길이(샘플) — setSize에서 유도
	lenR  [revNumCombs]int // R = L + 23
	apLen [revNumAPs]int   // {225, 556} 고정(setSize에서 세팅)
	fb    float32          // 콤 피드백 0.70+0.16·size
	lpC   float32          // 피드백 LP 계수(setDamp)
}

// setSize — RevSize q: 콤 길이 = 기준×(0.6+0.8q), 피드백 g = 0.70+0.16q.
// 길이가 바뀌면 읽기 위치가 점프해 클릭이 날 수 있다 — 노브 조작 중의 일시적
// 아티팩트라 계약 밖으로 둔다(연주 정지 상태의 음질은 길이 불변).
func (r *reverb) setSize(q float32) {
	if q != q || q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	r.fb = mul32(0.16, q) + 0.70
	m := mul32(0.8, q) + 0.6
	for i := 0; i < revNumCombs; i++ {
		n := int(mul32(float32(revCombBase[i]), m))
		if n < 1 {
			n = 1
		}
		if n > revCombCap-23 {
			n = revCombCap - 23
		}
		r.lenL[i] = n
		r.lenR[i] = n + 23
	}
	r.apLen[0], r.apLen[1] = 225, 556
}

// setDamp — RevDamp q: 피드백 LP 차단 12000→2000 Hz(선형). 계수
// c = 1 − e^(−2π·fc/fs) = 1 − exp2(−2π·fc/(fs·ln2)) — fx.go fbLPc와 같은 식.
func (r *reverb) setDamp(q float32) {
	if q != q || q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	fc := 12000 - mul32(10000, q)
	r.lpC = 1 - exp2(-mul32(twoPiF, fc)/(SampleRate*ln2F))
}

// process — 모노 입력 → 스테레오 리턴. 읽기 → 쓰기 → 포인터 진행(fx.go 딜레이와
// 같은 순서). 콤: buf[wr] = in + fb·LP(buf[rd]), 출력 = rd 값들의 합.
func (r *reverb) process(in float32) (float32, float32) {
	in = mul32(in, revInGain) // 콤 뱅크 DC 이득 정규화(위 상수 주석)
	d := r.pre[r.preW]
	r.pre[r.preW] = in
	r.preW++
	if r.preW >= revPreDelay {
		r.preW = 0
	}
	var out [2]float32
	for c := 0; c < 2; c++ {
		sum := float32(0)
		for i := 0; i < revNumCombs; i++ {
			w := r.combW[c][i]
			ln := r.lenL[i]
			if c == 1 {
				ln = r.lenR[i]
			}
			rd := w - ln
			if rd < 0 {
				rd += revCombCap
			}
			y := r.comb[c][i][rd]
			z := mul32(r.lpC, y-r.combZ[c][i]) + r.combZ[c][i]
			r.combZ[c][i] = z
			r.comb[c][i][w] = d + mul32(r.fb, z)
			sum += y
			w++
			if w >= revCombCap {
				w = 0
			}
			r.combW[c][i] = w
		}
		for a := 0; a < revNumAPs; a++ {
			w := r.apW[c][a]
			rd := w - r.apLen[a]
			if rd < 0 {
				rd += apCap
			}
			b := r.ap[c][a][rd]
			y := mul32(apG, sum) + b
			r.ap[c][a][w] = sum - mul32(apG, y)
			sum = y
			w++
			if w >= apCap {
				w = 0
			}
			r.apW[c][a] = w
		}
		out[c] = sum
	}
	return out[0], out[1]
}

// chorus — 기준 지연 12 ms, 삼각 LFO(위상 누산기), 깊이 0..6 ms, 선형 보간,
// 피드백 없음(§13.2). R은 LFO 위상 +90°(0.25). 난수 없음 — 결정론 계약.
type chorus struct {
	buf   [choBufLen]float32
	wpos  int
	phase float32 // L LFO 위상(0..1 미만)
	inc   float32 // 샘플당 위상 증가 = hz/SampleRate
	depth float32 // 샘플 단위(0..288)
}

func (c *chorus) setRate(q float32) {
	if q != q || q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	// rate = 0.1·30^q Hz = 0.1·2^(q·log2 30) — ChoRate 표기(§13.1)
	hz := mul32(0.1, exp2(mul32(q, log2_30)))
	c.inc = hz / SampleRate
}

func (c *chorus) setDepth(q float32) {
	if q != q || q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	c.depth = mul32(q, choMaxDep)
}

// process — 모노 입력 → 스테레오 리턴. 이번 샘플의 지연을 먼저 읽고(위상는
// 이번 샘플 값), 입력을 쓰고, 위상을 진행시킨다.
func (c *chorus) process(in float32) (float32, float32) {
	l := c.read(choBase + mul32(c.depth, tri(c.phase)))
	ph := c.phase + 0.25
	if ph >= 1 {
		ph -= 1
	}
	r := c.read(choBase + mul32(c.depth, tri(ph)))
	c.buf[c.wpos] = in
	c.wpos++
	if c.wpos >= choBufLen {
		c.wpos = 0
	}
	c.phase += c.inc
	if c.phase >= 1 {
		c.phase -= 1
	}
	return l, r
}

// read — 지연 d(288..864 샘플)의 선형 보간 읽기. wpos는 이번 샘플을 쓸 자리이므로
// wpos−di가 "di 샘플 전"(쓰기 전 판정 — fx.go 딜레이와 같은 인덱스 산술).
func (c *chorus) read(d float32) float32 {
	di := int(d)
	df := d - float32(di)
	i0 := c.wpos - di
	if i0 < 0 {
		i0 += choBufLen
	}
	i1 := i0 + 1
	if i1 >= choBufLen {
		i1 = 0
	}
	b0 := c.buf[i0]
	return b0 + mul32(df, c.buf[i1]-b0)
}

// tri — [0,1) 삼각파 → [−1,1](곱셈·덧셈만).
func tri(x float32) float32 {
	if x < 0.5 {
		return mul32(4, x) - 1
	}
	return 3 - mul32(4, x)
}

// busClamp — 리턴 합의 상한 클램프(dry의 소프트클립 상한과 같은 값: outClip(8) =
// 0.82·rat(8) ≈ 0.99026 ≤ 0.9903). 곱셈이 없다 — FMA 계약 무관. dry 단독(리턴 0)은
// 항상 범위 안이라 비트 그대로 통과한다.
func busClamp(x float32) float32 {
	if x > 0.99026 {
		return 0.99026
	}
	if x < -0.99026 {
		return -0.99026
	}
	return x
}
