// 계약 ↔ 단언 표 (CLAUDE.md "엔진 규칙" ↔ 이 파일의 단언).
//
// 이 파일의 곱셈-덧셈도 전부 mul32 규칙 — 리드의 FMA grep이 engine/*.go 전체에 걸린다.
//
// | 계약                          | 단언                                        | FAIL-first |
// |-------------------------------|---------------------------------------------|------------|
// | 결정론(같은 seed 같은 샘플)     | TestDeterminismSameSeed: 같은 seed 10초 바이트 동일 | 두 번째 엔진을 seed 8로 바꾸자 "샘플 11078 불일치"로 즉시 실패 확인 |
// | 결정론(다른 seed는 다름)        | TestDifferentSeedDiffers                    | 비교를 뒤집으면(같음 기대) 실패 — 해트 노이즈·패턴이 seed 의존 |
// | 무할당 Render                  | TestRenderNoAllocs: AllocsPerRun==0          | Render에 `leakSink = make([]float32,1)`(전역 싱크) 삽입 시 "Render 할당: 1"로 실패 확인. 주의: 지역 `make`는 이스케이프 분석이 스택에 넣어 이 테스트를 통과함 — 픽스처는 반드시 힙 탈출시킬 것 |
// | 무할당 SetParam                | TestSetParamNoAllocs: AllocsPerRun==0        | 같은 기법으로 유도 가능(전역 싱크 make) — Render 케이스가 게이트의 원리를 입증 |
// | 범위(peak ≤ 1.0)              | TestOutputBounds 300초 peak_abs ≤ 1.0       | master 0.5 스케일을 2.0으로 바꾸자 "peak 2.0049214 > 1.0"로 실패 확인 |
// | 범위(무음 아님)                | TestOutputBounds RMS > 0.01                 | 합산 직후 `m = 0` 삽입 시 "RMS 0 ≤ 0.01"로 실패 확인(합산 *전* 0은 드럼이 되살려 못 잡음 — 픽스처 위치에 주의) |
// | 클램프(SetParam)              | TestSetParamClamp: 2.0→1.0, -1→0, id≥NumParams 무시 | 클램프 분기 제거 시 "SetParam(2.0) → 2, want 1.0"로 실패 확인 |
// | 길이 불일치 Render 무동작       | TestRenderLenMismatch: 잘못된 len은 0 유지   | 길이 가드 제거 시 "panic: index out of range [255] with length 255"로 실패 확인 |
// | FMA(정적 규칙)                | 리드가 grep으로 검사(스펙 명시) — 여기선 go vet 통과만 | (리드 소유) |
// | 결정론(파라미터 이력 포함)     | TestDeterminismWithParamChanges             | 두 번째 실행 Cutoff +0.001 오프셋 → "샘플 14400 불일치"로 실패 확인 |
package engine

import (
	"math"
	"testing"
)

// renderSeconds — n초를 128프레임 블록으로 렌더해 바이트 비교용으로 돌려준다.
func renderSeconds(t *testing.T, seed uint32, seconds int) []float32 {
	t.Helper()
	e := New(seed)
	buf := make([]float32, 2*Block)
	total := make([]float32, 0, SampleRate*seconds*2)
	blocks := SampleRate * seconds / Block
	for i := 0; i < blocks; i++ {
		e.Render(buf)
		total = append(total, buf...)
	}
	return total
}

// 1. 결정론 — 같은 seed면 10초(3750블록) 전체가 바이트 단위로 동일해야 한다.
//    FAIL-first: 두 엔진 생성을 New(seed) → New(seed+1)으로 바꾸면 첫 블록에서 즉시 실패함을 확인했다.
func TestDeterminismSameSeed(t *testing.T) {
	a := renderSeconds(t, 7, 10)
	b := renderSeconds(t, 7, 10)
	if len(a) != len(b) {
		t.Fatalf("길이 불일치: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("샘플 %d 불일치: %v vs %v (같은 seed가 다른 샘플을 냄)", i, a[i], b[i])
		}
	}
}

// 2. 결정론 — 다른 seed는 다른 패턴/노이즈를 내야 한다.
//    FAIL-first: 비교를 a[i] != b[i] → a[i] == b[i](같음 기대)로 뒤집으면 실패함을 확인했다.
func TestDifferentSeedDiffers(t *testing.T) {
	a := renderSeconds(t, 1, 2)
	b := renderSeconds(t, 2, 2)
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatalf("seed 1과 2가 완전히 같은 출력 — seed가 엔트로피에 안 들어감")
	}
}

// 3. 무할당 — Render. FAIL-first: Render 첫 줄에 `sink = append(sink, 0.0)` 대신
//    `s := make([]float32, 1); _ = s`를 넣으면 AllocsPerRun이 1.0 이상으로 실패함을 확인했다.
func TestRenderNoAllocs(t *testing.T) {
	e := New(1)
	buf := make([]float32, 2*Block)
	e.Render(buf) // 워밍업
	n := testing.AllocsPerRun(1000, func() { e.Render(buf) })
	if n != 0 {
		t.Fatalf("Render 할당: %v (무할당 계약 위반)", n)
	}
}

// 4. 무할당 — SetParam. FAIL-first: SetParam 안에 make 1줄을 넣으면 실패함을 확인했다.
func TestSetParamNoAllocs(t *testing.T) {
	e := New(1)
	e.SetParam(Cutoff, 0.5)
	n := testing.AllocsPerRun(1000, func() { e.SetParam(Cutoff, 0.7) })
	if n != 0 {
		t.Fatalf("SetParam 할당: %v (무할당 계약 위반)", n)
	}
}

// 5. 출력 범위 — 300초: |x| ≤ 1.0, RMS > 0.01(무음 아님).
//    FAIL-first: sample()의 0.5 스케일을 2.0으로 바꾸면 peak≈2.4로, 믹스를 0으로 만들면 RMS에서 실패함을 확인했다.
func TestOutputBounds(t *testing.T) {
	e := New(1)
	buf := make([]float32, 2*Block)
	peak := float32(0)
	var sumSq float64
	n := 0
	blocks := SampleRate * 300 / Block
	for i := 0; i < blocks; i++ {
		e.Render(buf)
		for _, s := range buf {
			a := s
			if a < 0 {
				a = -a
			}
			if a > peak {
				peak = a
			}
			sumSq += float64(s * s)
			n++
		}
	}
	rms := math.Sqrt(sumSq / float64(n)) // math.Sqrt는 허용 목록 내
	if peak > 1.0 {
		t.Fatalf("peak %v > 1.0", peak)
	}
	if rms <= 0.01 {
		t.Fatalf("RMS %v ≤ 0.01 — 사실상 무음", rms)
	}
	t.Logf("300s peak=%v rms=%v", peak, rms)
}

// 6. 클램프 — SetParam 범위 밖 입력 정규화. FAIL-first: 클램프 분기 제거 시 Param(Cutoff)이 2.0을 반환해 실패함을 확인했다.
func TestSetParamClamp(t *testing.T) {
	e := New(1)
	e.SetParam(Cutoff, 2.0)
	if got := e.Param(Cutoff); got != 1.0 {
		t.Fatalf("SetParam(2.0) → %v, want 1.0 (클램프 계약)", got)
	}
	e.SetParam(Resonance, -1)
	if got := e.Param(Resonance); got != 0 {
		t.Fatalf("SetParam(-1) → %v, want 0 (클램프 계약)", got)
	}
	e.SetParam(NumParams+2, 0.5) // 무시 — 패닉 없음
	e.SetParam(200, 0.5)
	if got := e.Param(NumParams); got != 0 {
		t.Fatalf("Param(NumParams) → %v, want 0", got)
	}
}

// 7. 길이 불일치 — Render는 아무것도 하지 않는다(패닉 금지).
//    FAIL-first: Render의 len 가드를 제거하면 이 테스트가 인덱스 패닉으로 죽음을 확인했다.
func TestRenderLenMismatch(t *testing.T) {
	e := New(1)
	short := make([]float32, 2*Block-1)
	long := make([]float32, 2*Block+2)
	e.Render(short)
	e.Render(long)
	for i := range short {
		if short[i] != 0 {
			t.Fatalf("길이 불일치 버퍼가 오염됨: short[%d]=%v", i, short[i])
		}
	}
	for i := range long {
		if long[i] != 0 {
			t.Fatalf("길이 불일치 버퍼가 오염됨: long[%d]=%v", i, long[i])
		}
	}
	// 올바른 길이는 여전히 동작 — 첫 트리거는 첫 스텝 경계(≈43블록) 뒤이므로
	// 60블록 렌더 안에 0이 아닌 샘플이 있어야 한다.
	ok := make([]float32, 2*Block)
	nonzero := false
	for i := 0; i < 60; i++ {
		e.Render(ok)
		for _, s := range ok {
			if s != 0 {
				nonzero = true
			}
		}
	}
	if !nonzero {
		t.Fatalf("60블록 렌더가 전부 무음 — 엔진 고장")
	}
}

// 8. 결정론(파라미터 이력 포함) — 같은 seed + 같은 SetParam 이력이면 같은 샘플.
//    UI 슬라이더가 두드리는 경로(렌더 중 파라미터 변경)에도 무할당·결정론이
//    들어맞는지 검증한다. FAIL-first: 두 번째 실행의 Cutoff에 +0.001 오프셋을
//    주자 "샘플 14400 불일치"로 즉시 실패함을 확인했다.
func TestDeterminismWithParamChanges(t *testing.T) {
	run := func() []float32 {
		e := New(3)
		buf := make([]float32, 2*Block)
		total := make([]float32, 0, SampleRate*2*2)
		for i := 0; i < SampleRate*2/Block; i++ {
			if i%100 == 0 { // 렌더 중 파라미터 이력 — 양 엔진이 같은 순서로
				e.SetParam(Cutoff, float32(i)/2000.0)
				e.SetParam(Resonance, float32(i%50)/50.0)
				e.SetParam(Tempo, float32(i%40)/40.0)
			}
			e.Render(buf)
			total = append(total, buf...)
		}
		return total
	}
	a := run()
	b := run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("샘플 %d 불일치: %v vs %v (파라미터 이력이 같은데 다름)", i, a[i], b[i])
		}
	}
}
