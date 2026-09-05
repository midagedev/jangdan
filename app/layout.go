// Package main — 화면 배치 좌표의 단일 소유자.
// 논리 좌표계 540×960(백킹 1080×1920 = DPR 2). 다른 파일에 좌표 상수를 두지
// 않는다(스파이크 계약). 기준 레이아웃: docs/concepts/README.md 기기 블록 —
// 왼쪽 노브 2행×6, 오른쪽 패드 3×4, 아래 스텝 2×8, 위 중앙 스코프.
package main

// 논리 화면과 스코프.
const (
	LogicalW = 540
	LogicalH = 960

	ScopeCX = 270 // 스코프 중심
	ScopeCY = 130
	ScopeW  = 256 // 512×192 스프라이트의 논리 크기
	ScopeH  = 96

	ScopeSamples = 256 // 프레임당 파형 샘플(Float32)
	ScopeAmp     = 40  // 파형 진폭 상한(px) — 샘플 ±1 기준
)

// 노브 — 2행 × 6열, 표시 64×64.
// 행 0 = 베이스라인 A(파라미터 0..5), 행 1 = 베이스라인 B 자리(6·7만 엔진 연결).
const (
	KnobCount       = 12
	KnobCols        = 6
	KnobSize        = 64
	KnobX0          = 60  // 0번 노브 중심 x
	KnobDX          = 84  // 열 간격
	KnobYTop        = 300 // 행 0 중심 y
	KnobYBot        = 420 // 행 1 중심 y
	KnobDragPx      = 200 // 세로 200px 드래그 = 값 1.0
	KnobUnconnDotDX = 22  // 미연결 점 오프셋(노브 중심 기준)
	KnobUnconnDotR  = 3
)

// 패드 — 3열 × 4행, 표시 72×72, 트리거 시 120ms lit.
const (
	PadCount = 12
	PadCols  = 3
	PadRows  = 4
	PadSize  = 72
	PadX0    = 150
	PadDX    = 120
	PadY0    = 540
	PadDY    = 90
	PadLitMs = 120
)

// 스텝 — 2행 × 8열, 표시 40×40, 현재 스텝 lit(BPM 130 16분음표).
const (
	StepCount = 16
	StepCols  = 8
	StepSize  = 40
	StepX0    = 50
	StepDX    = 63
	StepYTop  = 880
	StepYBot  = 930

	StepBPM    = 130
	StepSecond = 60.0 / float64(StepBPM) / 4 // 16분음표 길이(초)
)

// KnobCenter — 노브 i(0..11)의 논리 중심. 행 = i/6, 열 = i%6.
func KnobCenter(i int) (float64, float64) {
	row, col := i/KnobCols, i%KnobCols
	y := KnobYTop
	if row == 1 {
		y = KnobYBot
	}
	return float64(KnobX0 + KnobDX*col), float64(y)
}

// PadCenter — 패드(열 c=0..2, 행 r=0..3)의 논리 중심. 패드 번호 i = r*3+c.
func PadCenter(c, r int) (float64, float64) {
	return float64(PadX0 + PadDX*c), float64(PadY0 + PadDY*r)
}

// StepCenter — 스텝 s(0..15)의 논리 중심. 행 = s/8(0..7 위, 8..15 아래).
func StepCenter(s int) (float64, float64) {
	y := StepYTop
	if s/StepCols == 1 {
		y = StepYBot
	}
	return float64(StepX0 + StepDX*(s%StepCols)), float64(y)
}
