// poly_test.go — 폴리 리드 연주법(poly.go) 게이트.
//
//	계약                                  | 단언                         | FAIL-first
//	--------------------------------------+------------------------------+------------------------------------------
//	페이즈 진입 바에만 DeviceStep 16·DeviceParam 5 | TestPolyEmission           | emitPoly를 regen 조건으로 옮기면 8바마다 방출돼 "비진입 바 방출"로 실패
//	패턴 표(Drop 게이트 11·액센트 4, 패드 타이 15) | TestPolyPatterns            | Drop 표에서 pG 하나를 0으로 바꾸면 게이트 수 불일치
//	옥타브·k·값 범위                        | TestPolyEmission             | 레벨을 1.2로 두면 클램프 전 방출 시 V>1 실패
package resident

import (
	"testing"

	"github.com/midagedev/jangdan/engine"
)

func TestPolyPatterns(t *testing.T) {
	count := func(ph Phase, mask uint8) int {
		n := 0
		for _, f := range polyPatterns[ph] {
			if f&mask == mask {
				n++
			}
		}
		return n
	}
	if g := count(Drop, engine.StepGate); g != 11 {
		t.Fatalf("Drop 게이트 %d, want 11", g)
	}
	if a := count(Drop, engine.StepAccent); a != 4 {
		t.Fatalf("Drop 액센트 %d, want 4", a)
	}
	if g := count(Build, engine.StepGate); g != 4 {
		t.Fatalf("Build 게이트 %d, want 4", g)
	}
	for _, ph := range [...]Phase{Intro, Breakdown} {
		if ties := count(ph, engine.StepGate|engine.StepSlide); ties != 15 || polyPatterns[ph][0] != pG {
			t.Fatalf("페이즈 %d 패드: 타이 %d(want 15), 스텝0 %d", ph, ties, polyPatterns[ph][0])
		}
	}
	for ph := Phase(0); ph < numPhases; ph++ {
		for st, f := range polyPatterns[ph] {
			if f&engine.StepSlide != 0 && f&engine.StepGate == 0 {
				t.Fatalf("페이즈 %d 스텝 %d: 게이트 없는 타이", ph, st)
			}
		}
	}
}

func TestPolyEmission(t *testing.T) {
	r := New(11, Rush, Config{})
	recs := drive(r, 120, barDurOf(Rush))
	entries := 0
	for _, tr := range recs {
		steps, params := 0, 0
		for _, c := range tr.cmds {
			switch c.Kind {
			case engine.DeviceStep:
				if c.A != engine.SlotPoly || c.C%engine.NumDegrees != 0 || c.C > engine.MaxNote || c.D&^(pT|pGA) != 0 {
					t.Fatalf("DeviceStep 범위 밖 %+v", c)
				}
				steps++
			case engine.DeviceParam:
				if c.A != engine.SlotPoly || c.B >= engine.DevParams || c.V < 0 || c.V > 1 {
					t.Fatalf("DeviceParam 범위 밖 %+v", c)
				}
				params++
			}
		}
		if !tr.barStart && (steps != 0 || params != 0) {
			t.Fatalf("바 %d 스텝 %d: 바 경계가 아닌 틱에 폴리 방출", tr.bar, tr.step)
		}
		if steps == 0 && params == 0 {
			continue
		}
		if steps != engine.Steps || params != 5 {
			t.Fatalf("바 %d: DeviceStep %d·DeviceParam %d, want 16·5", tr.bar, steps, params)
		}
		entries++
	}
	// 120바(Rush 135BPM ≈ 3.6분) 안에 첫 바 + 페이즈 전이 몇 번 — 첫 바는 반드시, 그리고 매 바는 아니다.
	if entries < 2 || entries > 20 {
		t.Fatalf("폴리 방출 바 %d — 페이즈 진입 바에만 나와야 한다(첫 바 포함, 2..20 기대)", entries)
	}
	if len(recs[0].cmds) == 0 {
		t.Fatal("첫 바 방출 없음")
	}
	first := 0
	for _, c := range recs[0].cmds {
		if c.Kind == engine.DeviceStep {
			first++
		}
	}
	if first != engine.Steps {
		t.Fatalf("첫 바 DeviceStep %d, want 16", first)
	}
}
