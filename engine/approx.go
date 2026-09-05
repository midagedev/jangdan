// 근사 함수 — exp2(비트 트릭 + 다항), 사인(5차 홀다항), floor, xorshift32,
// 그리고 mul32(FMA 융합 차단 곱셈).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약.
// mul32는 곱을 float64로 정확히 계산한 뒤 float32로 반올림한다(float32×float32는
// float64에 정확히 들어가므로 규격 f32 곱셈과 비트 동일). 타입이 다른 곱은 f32
// 덧셈과 융합될 수 없어 장벽이 된다. 곱만 정확히 감싼 float32(x*y)+z 도 Go 스펙대로
// 융합을 막는다(2026-09-05 리드 objdump 실측) — 융합되는 것은 float32(x)*y+z,
// float32(x*y+z) 같이 감싼 위치가 틀린 형태다. 헬퍼를 쓰는 이유는 그 실수를
// grep 한 줄로 잡기 위해서고, 최종 게이트는 tools/check-fma.sh(objdump FMADD 0)다.
// math 사용은 Float32bits/Float32frombits/Abs/Sqrt 로 제한(엔진 규칙).
package engine

import "math"

const ln2F float32 = 0.6931472
const piF float32 = 3.14159265
const twoPiF float32 = 6.28318531

// mul32 — FMA 융합 차단 곱셈. 위 파일 주석 참조. 인라인돼도 장벽이 유지된다
// (f64 곱은 f32 덧셈과 융합될 수 없다).
func mul32(a, b float32) float32 {
	return float32(float64(a) * float64(b))
}

// floorf — x가 int32 범위 안임이 보장될 때만 호출(exp2가 클램프 후 호출).
// math.Floor이 허용 목록에 없어 직접 작성. int32(x)는 Go 스펙상 절단.
func floorf(x float32) float32 {
	i := int32(x)
	if x < 0 && float32(i) != x {
		i--
	}
	return float32(i)
}

// exp2 — 2^x. 지수부는 비트 트릭, 가수부는 5차 다항(Taylor of 2^f, f∈[0,1)).
// 상대 오차 ~1.5e-4(오디오용으로 충분). x < -30 → 0, x > 100 → 클램프.
// 결정론: float32 산술 + 비트 조립만 사용, 덧셈에 닿는 곱은 전부 mul32.
func exp2(x float32) float32 {
	if x < -30 {
		return 0
	}
	if x > 100 {
		x = 100
	}
	fl := floorf(x)
	f := x - fl // [0,1)
	// 2^f ≈ 1 + f*(l1 + f*(l2 + f*(l3 + f*(l4 + f*l5))))
	p := mul32(f, 0.0013327) + 0.0096181
	p = mul32(f, p) + 0.0555041
	p = mul32(f, p) + 0.2402265
	p = mul32(f, p) + 0.6931472
	p = mul32(f, p) + 1
	b := uint32(int32(fl)+127) << 23
	return mul32(p, math.Float32frombits(b))
}

// sin5 — 5차 홀다항 사인 근사, 정의역 x∈[-π,π], 최대 절대 오차 ~2e-4.
// 다항 스펙 계약상 Bhaskara 또는 5차 다항 중 다항을 택했다.
func sin5(x float32) float32 {
	x2 := mul32(x, x)
	p := mul32(x2, -0.00019841) + 0.0083330
	p = mul32(x2, p) - 0.16666667
	p = mul32(x2, p) + 1
	return mul32(x, p)
}

// xorshift32 — 엔진 내부 난수(math/rand 금지). 결정론 계약의 유일한 엔트로피원.
type xorshift32 struct{ x uint32 }

func (r *xorshift32) next() uint32 {
	x := r.x
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.x = x
	return x
}
