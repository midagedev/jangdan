// pomodoro.go — 포모도로 상태기계(저장 없이 Input.Now에서 유도).
//
// 전이표 — 상태는 (session s의 Focus/Rest)이고 전이 조건은 Now뿐이다:
//
//	┌────────────┬─────────────────────┬──────────────────────────────────────┐
//	│ 현재       │ 조건                │ 다음 + 바 경계 동작                  │
//	├────────────┼─────────────────────┼──────────────────────────────────────┤
//	│ Focus(s)   │ Now ≥ Focus 종료    │ Rest(s)   — MANUAL 잠금 전체 해제    │
//	│ Rest(s)    │ Now ≥ Rest 종료     │ Focus(s+1)— 잠금 해제, 에너지 사이클 │
//	│            │                     │ 0 리셋(= Intro 진입)                │
//	│ Focus 내부 │ 종료 16바 전 진입   │ 강제 Build(에너지 곡선 무시)         │
//	│ Focus 내부 │ 종료 바(잔여 ≤ 1바) │ Drop Cmd 1개(페이즈 전이로 1회 보장) │
//	└────────────┴─────────────────────┴──────────────────────────────────────┘
//
// 첫 세션 Focus 길이는 DemoFocusMin(기본 5분), 이후 FocusMin(기본 25분).
// Rest는 RestMin(기본 5분). 경계 통과는 연속한 두 Tick의 Now가 다른 구간에
// 속하는지로 감지한다(pomoCrossed) — 포모도로 경계에서 잠금이 자동 해제되는
// 기획 결정의 구현 지점.
package resident

// pomoSpan — Now가 속한 포모도로 구간.
type pomoSpan struct {
	session int
	ph      PomodoroPhase
	start   float64
	end     float64
}

// focusDurSec — 세션 s의 Focus 길이(초). 첫 세션은 데모(기본 5분).
func focusDurSec(cfg Config, session int) float64 {
	if session == 0 {
		return cfg.DemoFocusMin * 60
	}
	return cfg.FocusMin * 60
}

// pomoAt — cfg·Now → 현재 구간. 구간 누적합을 처음부터 걸어 유도한다
// (derive-don't-store: 카운터를 저장하면 Tick 누락과 어긋날 수 있다).
func pomoAt(cfg Config, now float64) pomoSpan {
	if now < 0 {
		now = 0
	}
	t := 0.0
	for s := 0; s < 100000; s++ {
		fd := focusDurSec(cfg, s)
		if now < t+fd {
			return pomoSpan{session: s, ph: Focus, start: t, end: t + fd}
		}
		t += fd
		rd := cfg.RestMin * 60
		if now < t+rd {
			return pomoSpan{session: s, ph: Rest, start: t, end: t + rd}
		}
		t += rd
	}
	// 사실상 도달 불가(100000 세션 ≈ 3년) — 마지막 Rest로 수렴시킨다.
	rd := cfg.RestMin * 60
	return pomoSpan{session: 99999, ph: Rest, start: t, end: t + rd}
}

// pomoCrossed — 두 시점이 서로 다른 포모도로 구간에 속하는가(경계 통과 감지).
func pomoCrossed(cfg Config, a, b float64) bool {
	pa := pomoAt(cfg, a)
	pb := pomoAt(cfg, b)
	return pa.ph != pb.ph || pa.session != pb.session
}
