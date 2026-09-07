// sampler_test.go — 샘플러 연주법(sampler.go) 게이트.
//
//	계약                                          | 단언                      | FAIL-first(전부 실측 FAIL)
//	----------------------------------------------+---------------------------+------------------------------------------------------------
//	1 페이즈 진입 바에만 방출(Step 16 + Param 5,    | TestSamplerEmission       | resident.go의 emitSampler 호출을 regen 조건으로 옮기면 8바마다
//	  비진입 바엔 슬롯 8 Cmd 0개)                   |                           | 방출 — "비진입 바 방출"로 FAIL
//	2 표 수치 계약(게이트 수·액센트 수·레벨·        | TestSamplerPhaseContract  | Drop 게이트 둘을 0으로(6→4), 액센트 하나를 sG로(2→1),
//	  Breakdown 릴리즈 ≥ 0.5)                      |                           | Drop 레벨 0.78→0.90, Breakdown 릴리즈 0.60→0.30 — 각각 FAIL
//	3 슬롯 선택 왕복 q → int(q·7+0.5) = 표 슬롯     | TestSamplerPhaseContract  | Build q의 분모 7→8(슬롯 5 TAPE로 해석) — FAIL
//	4 도수 제한(게이트 note%7 ∈ {0,2,4})           | TestSamplerPhaseContract  | Drop 스텝3 note 18→17(도수 3) — FAIL
//	5 바이브 시프트(DeepFocus 레벨 −0.1,            | TestSamplerVibeShift      | 시프트 −0.1→−0.05 — FAIL
//	  Lofi 릴리즈 +0.15, 클램프 경계 포함)          |                           |
//	6 실제로 들린다(엔진 Apply·렌더·FlagSampler)    | TestSamplerSounds         | DeviceStep D: pat[st]&0(게이트 소실) — FlagSampler 미설정 FAIL
package resident

import (
	"testing"

	"github.com/midagedev/jangdan/engine"
)

// samplerEmit — 순수 emitSampler만 불러 방출을 모은다(페이즈 유도 없이 단위 수준에서).
func samplerEmit(v Vibe, ph Phase) []engine.Cmd {
	r := New(11, v, Config{})
	r.emitSampler(ph)
	return append([]engine.Cmd(nil), r.buf...)
}

// smpLevelOf / smpReleaseOf — 방출에서 k=SmpLevel·SmpRelease의 V를 꺼낸다.
func smpLevelOf(t *testing.T, cmds []engine.Cmd) float32 {
	t.Helper()
	for _, c := range cmds {
		if c.Kind == engine.DeviceParam && c.A == engine.SlotSampler && c.B == engine.SmpLevel {
			return c.V
		}
	}
	t.Fatal("SmpLevel 방출 없음")
	return 0
}

func smpReleaseOf(t *testing.T, cmds []engine.Cmd) float32 {
	t.Helper()
	for _, c := range cmds {
		if c.Kind == engine.DeviceParam && c.A == engine.SlotSampler && c.B == engine.SmpRelease {
			return c.V
		}
	}
	t.Fatal("SmpRelease 방출 없음")
	return 0
}

// ---- 계약 2·3·4: 표 수치 계약 + 슬롯 왕복 + 도수 제한 ----

func TestSamplerPhaseContract(t *testing.T) {
	// 표(sampler.go 파일 주석·스펙 계약 표)의 수치. 레벨 하한이 없는 페이즈(Intro·Breakdown)는
	// 방출 도메인 하한 0을 줬다(DeviceParam V는 clamp01 산물).
	for _, tc := range []struct {
		name                       string
		ph                         Phase
		slot                       uint8
		minGates, maxGates, minAcc int
		loLvl, hiLvl, minRel       float32
	}{
		{"Intro", Intro, 2, 1, 2, 0, 0, 0.45, 0},
		{"Build", Build, 6, 3, 5, 0, 0.45, 0.65, 0},
		{"Drop", Drop, 0, 5, 8, 2, 0.65, 0.85, 0},
		{"Breakdown", Breakdown, 1, 1, 2, 0, 0, 0.55, 0.5},
	} {
		cmds := samplerEmit(Rush, tc.ph) // Rush = 샘플러 시프트 없는 바이브(기준 방출)
		gates, accents := 0, 0
		var gateDegs []uint8
		stepSeen := [engine.Steps]bool{}
		for _, c := range cmds {
			if c.Kind != engine.DeviceStep || c.A != engine.SlotSampler {
				continue
			}
			if c.B >= engine.Steps || stepSeen[c.B] {
				t.Fatalf("%s: 스텝 %d 중복/범위 밖", tc.name, c.B)
			}
			stepSeen[c.B] = true
			if c.C > engine.MaxNote {
				t.Fatalf("%s: 스텝 %d 노트 %d > MaxNote", tc.name, c.B, c.C)
			}
			if c.D&^(engine.StepGate|engine.StepSlide|engine.StepAccent) != 0 {
				t.Fatalf("%s: 스텝 %d 플래그 범위 밖 %d", tc.name, c.B, c.D)
			}
			if c.D&engine.StepGate != 0 {
				gates++
				gateDegs = append(gateDegs, c.C%engine.NumDegrees)
				if c.D&engine.StepAccent != 0 {
					accents++
				}
			}
		}
		if gates < tc.minGates || gates > tc.maxGates {
			t.Fatalf("%s 게이트 %d, want %d..%d", tc.name, gates, tc.minGates, tc.maxGates)
		}
		if accents < tc.minAcc {
			t.Fatalf("%s 액센트 %d < %d", tc.name, accents, tc.minAcc)
		}
		for _, d := range gateDegs { // 계약 4: 도수는 루트·코드 톤만
			if d != 0 && d != 2 && d != 4 {
				t.Fatalf("%s 게이트 도수 %d — {0,2,4} 밖(경과음 금지)", tc.name, d)
			}
		}
		lvl := smpLevelOf(t, cmds)
		if lvl < tc.loLvl || lvl > tc.hiLvl {
			t.Fatalf("%s 레벨 %.4f, want %.2f..%.2f", tc.name, lvl, tc.loLvl, tc.hiLvl)
		}
		if rel := smpReleaseOf(t, cmds); rel < tc.minRel {
			t.Fatalf("%s 릴리즈 %.4f < %.2f", tc.name, rel, tc.minRel)
		}
		// 계약 3: 슬롯 선택 왕복 — 방출된 q를 엔진 식(engine/sampler.go SmpSelect)에 넣어
		// 표의 팩 슬롯이 나와야 한다.
		var q float32
		found := false
		for _, c := range cmds {
			if c.Kind == engine.DeviceParam && c.A == engine.SlotSampler && c.B == engine.SmpSelect {
				q, found = c.V, true
			}
		}
		if !found {
			t.Fatalf("%s SmpSelect 방출 없음", tc.name)
		}
		if got := int(float64(q)*7 + 0.5); got != int(tc.slot) {
			t.Fatalf("%s 슬롯 왕복 q=%.6f → %d, want %d(%s)", tc.name, q, got, tc.slot, engine.PackNames[tc.slot])
		}
		// 모양: DeviceStep 16 + DeviceParam 5(k 집합 정확히 Select·Tune·Attack·Release·Level).
		steps, params := 0, 0
		wantKs := map[uint8]bool{engine.SmpSelect: true, engine.SmpTune: true, engine.SmpAttack: true, engine.SmpRelease: true, engine.SmpLevel: true}
		for _, c := range cmds {
			switch c.Kind {
			case engine.DeviceStep:
				if c.A == engine.SlotSampler {
					steps++
				}
			case engine.DeviceParam:
				if c.A == engine.SlotSampler {
					if !wantKs[c.B] || c.V < 0 || c.V > 1 {
						t.Fatalf("%s DeviceParam 범위 밖 k=%d V=%.4f", tc.name, c.B, c.V)
					}
					delete(wantKs, c.B)
					params++
				}
			}
		}
		if steps != engine.Steps || params != 5 || len(wantKs) != 0 {
			t.Fatalf("%s 모양: Step %d·Param %d·남은 k %d, want 16·5·0", tc.name, steps, params, len(wantKs))
		}
		t.Logf("%s: 게이트 %d·액센트 %d·슬롯 %d(%s) q=%.6f·레벨 %.2f·릴리즈 %.2f",
			tc.name, gates, accents, tc.slot, engine.PackNames[tc.slot], q, lvl, smpReleaseOf(t, cmds))
	}
}

// ---- 계약 5: 바이브 시프트 ----

func TestSamplerVibeShift(t *testing.T) {
	for ph := Phase(0); ph < numPhases; ph++ {
		base := samplerEmit(Rush, ph)
		deep := samplerEmit(DeepFocus, ph)
		lofi := samplerEmit(Lofi, ph)
		bl, dl, ll := smpLevelOf(t, base), smpLevelOf(t, deep), smpLevelOf(t, lofi)
		br, dr, lr := smpReleaseOf(t, base), smpReleaseOf(t, deep), smpReleaseOf(t, lofi)
		if dl != clamp01(bl-0.1) { // DeepFocus 레벨 정확히 −0.1(클램프 경계 포함)
			t.Fatalf("페이즈 %d DeepFocus 레벨 %.6f ≠ 기준 %.6f−0.1", ph, dl, bl)
		}
		if lr != clamp01(br+0.15) { // Lofi 릴리즈 정확히 +0.15
			t.Fatalf("페이즈 %d Lofi 릴리즈 %.6f ≠ 기준 %.6f+0.15", ph, lr, br)
		}
		if dr != br || ll != bl { // 시프트는 제 단 하나만 움직인다
			t.Fatalf("페이즈 %d 시프트 누출: 릴리즈 %v→%v·레벨 %v→%v", ph, br, dr, bl, ll)
		}
		// DeviceStep 16은 바이브와 무관하게 동일해야 한다(결정론).
		var bs, ds [engine.Steps]uint8
		for _, c := range base {
			if c.Kind == engine.DeviceStep && c.A == engine.SlotSampler {
				bs[c.B] = c.C
			}
		}
		for _, c := range lofi {
			if c.Kind == engine.DeviceStep && c.A == engine.SlotSampler {
				ds[c.B] = c.C
			}
		}
		if bs != ds {
			t.Fatalf("페이즈 %d: 바이브가 스텝 노트를 바꿨다", ph)
		}
	}
}

// ---- 계약 1: 페이즈 진입 바에만 방출 ----

func TestSamplerEmission(t *testing.T) {
	r := New(11, Rush, Config{})
	barDur := barDurOf(Rush)
	countSmp := func(cmds []engine.Cmd) (steps, params int) {
		for _, c := range cmds {
			switch c.Kind {
			case engine.DeviceStep:
				if c.A == engine.SlotSampler {
					steps++
				}
			case engine.DeviceParam:
				if c.A == engine.SlotSampler {
					params++
				}
			}
		}
		return steps, params
	}
	const bars = 120
	entries := 0
	prev := Phase(0)
	seen := false
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		cs := r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true})
		ph := r.Phase()
		entry := !seen || ph != prev // 첫 바 + 페이즈 전이(레지던트 phaseEntry와 같은 정의)
		prev, seen = ph, true
		steps, params := countSmp(cs)
		for s := 1; s < 16; s++ {
			cs = r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false})
			if st2, p2 := countSmp(cs); st2 != 0 || p2 != 0 {
				t.Fatalf("바 %d 스텝 %d: 바 경계가 아닌 틱에 샘플러 방출", b, s)
			}
		}
		if !entry {
			if steps != 0 || params != 0 {
				t.Fatalf("바 %d(페이즈 %d 유지): 비진입 바에 샘플러 방출 Step %d·Param %d", b, ph, steps, params)
			}
			continue
		}
		if steps != engine.Steps || params != 5 {
			t.Fatalf("진입 바 %d: DeviceStep %d·DeviceParam %d, want 16·5", b, steps, params)
		}
		entries++
	}
	if entries < 2 {
		t.Fatalf("샘플러 방출 바 %d — 페이즈 전이를 한 번도 못 겪었다(구동 부족)", entries)
	}
	if entries > 20 {
		t.Fatalf("샘플러 방출 바 %d — 진입 바에만 나와야 한다(2..20 기대)", entries)
	}
}

// ---- 계약 6: 실제로 들린다 ----

func TestSamplerSounds(t *testing.T) {
	const seed = uint32(1) // keyRoot 1 — Intro VOX(팩 기준음 12) 재생비 ≈ 1.06
	r := New(seed, Rush, Config{})
	e := engine.New(seed)
	buf := make([]float32, 256)
	const bars = 4
	const blocksPerBar = 667 // 240/135 BPM 바 ≈ 1.778s × 48000 / 128
	barDur := barDurOf(Rush)
	var flags uint32
	peak := float32(0)
	for b := 0; b < bars; b++ {
		now := float64(b) * barDur
		for _, c := range r.Tick(Input{Bar: uint32(b), Step: 0, Now: now, BarStart: true}) {
			e.Apply(c)
		}
		for s := 1; s < 16; s++ {
			for _, c := range r.Tick(Input{Bar: uint32(b), Step: s, Now: now + float64(s)*barDur/16, BarStart: false}) {
				e.Apply(c)
			}
		}
		for i := 0; i < blocksPerBar; i++ {
			e.Render(buf)
			flags |= e.Flags() // Flags는 직전 블록의 일 — 누적
			for _, x := range buf {
				if x < 0 {
					x = -x
				}
				if x > peak {
					peak = x
				}
			}
		}
	}
	if flags&engine.FlagSampler == 0 {
		t.Fatal("FlagSampler 미설정 — 패턴은 방출됐지만 샘플러가 실제로 울리지 않았다")
	}
	if peak <= 0 {
		t.Fatalf("렌더 침묵(peak %.4f)", peak)
	}
	t.Logf("샘플러 실연: FlagSampler 설정·peak %.4f·%d블록", peak, bars*blocksPerBar)
}
