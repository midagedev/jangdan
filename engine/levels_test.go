// levels_test.go — 파트별 블록 피크(Level) 계약 ↔ 단언. 원본: P3-levels 라운드
// (사용자 요청 2026-09-06 "라인별로 소리 날 때 LED가 들어오거나 VU 미터").
// 레벨은 프리 FX 출력(베이스 = bass[p].process 반환값, 드럼 = drumKit.last의 mix 기여)의
// 블록 abs 최대 — 마스터 peak와 같은 자리, 같은 주기(Render가 블록마다 리셋).
//
// | 계약                                    | 단언 | FAIL-first |
// |---|---|---|
// | 트리거한 드럼만 피크, 무음 파트 0          | TestLevelBDOnly: BD 스텝 0 블록에서 Level(BD)>0·나머지 7파트 0 | sample의 partOut 대입을 지우면 베이스 0 단언은 그대로, TestLevelBassA가 "BassA=0"으로 실패(반대 방향 결함) |
// | 베이스 노트온 블록 Level(BassA)>0         | TestLevelBassA | 위와 같은 partOut 결함으로 실패 확인 |
// | 블록마다 리셋(꼬리 감쇠가 레벨에 반영)     | TestLevelMuteBD: 뮤트+꼬리 소진 뒤 정확히 0 | Render의 levels 리셋 루프를 지우자 "구 피크 잔존"으로 실패 확인(본 라운드 실측) |
// | 레벨 ≤ 마스터 피크의 느슨한 상수배          | TestLevelBounds: 5초 전 블록 lv ≤ 4·peak·비음수·비NaN | 상한 4는 스펙 명시 값(프리 FX·덕킹·마스터 게인이라 정확 등식이 없다 — 기본값 최악 게인 ≈0.406의 역수 2.46보다 넉넉) |
// | 범위 밖 파트 인덱스 → 0                   | TestLevelBounds: Level(NumParts)·Level(200) == 0 | 가드를 지우면 배열 인덱스 패닉([8]float32) |
// | 무음 상태 배열 길이·초깃값                 | TestLevelBDOnly 전제: 첫 Render 전 전 파트 0 | (New 직후 0은 구조체 리터럴 — Reset이 보장) |
//
// 이 파일의 곱셈-덧셈은 없다(비교·대입만).
package engine

import "testing"

// silenceAll — 초기 패턴(genInitialPatterns)을 전부 지운 흰 판. 레벨 단언을 결정적으로
// 만든다(초기 패턴의 seed 의존 게이트를 배제). New 뒤 직접 호출(테스트는 패키지 내부).
func silenceAll(e *Engine) {
	for p := 0; p < 2; p++ {
		for s := 0; s < PatternSlots; s++ {
			for st := 0; st < Steps; st++ {
				e.bassPat[p][s][st] = bassStep{}
			}
		}
	}
	for v := 0; v < NumDrums; v++ {
		for st := 0; st < Steps; st++ {
			e.drumPat[v][st] = 0
		}
	}
}

// 1. BD만 트리거 — 첫 Render 블록(started → onStep(0))에서 BD가 치고 나머지 파트는
//    잠잠하다. 드럼 레벨은 × acc × level까지 반영된 mix 기여(last)라 무음 보이스는
//    정확히 0이다(보이스 off → process 0 → 항 0).
func TestLevelBDOnly(t *testing.T) {
	e := New(1)
	silenceAll(e)
	e.drumPat[0][0] = StepGate // BD 스텝 0
	buf := make([]float32, 2*Block)
	e.Render(buf)
	if e.Level(BD) <= 0 {
		t.Fatalf("Level(BD)=%v, want >0 (트리거한 블록)", e.Level(BD))
	}
	for _, p := range []Part{SD, CH, OH, CP, CY} {
		if e.Level(p) != 0 {
			t.Fatalf("Level(%d)=%v, want 0 (트리거하지 않은 드럼)", p, e.Level(p))
		}
	}
	if e.Level(BassA) != 0 || e.Level(BassB) != 0 {
		t.Fatalf("베이스 레벨 %v %v, want 0 0 (게이트 없음)", e.Level(BassA), e.Level(BassB))
	}
}

// 2. 베이스 A 노트온 — 첫 블록에서 Level(BassA) > 0(첫 2.7ms에 필터가 울리는 진폭
//    ≈0.06 실측이라 0.001 문턱), B는 게이트가 없어 0.
func TestLevelBassA(t *testing.T) {
	e := New(2)
	silenceAll(e)
	e.bassPat[0][e.slot[0]][0] = bassStep{note: 7, flags: StepGate} // 옥타브 1 루트 도수
	buf := make([]float32, 2*Block)
	e.Render(buf)
	if e.Level(BassA) <= 0.001 {
		t.Fatalf("Level(BassA)=%v, want >0.001 (노트온 첫 블록)", e.Level(BassA))
	}
	if e.Level(BassB) != 0 {
		t.Fatalf("Level(BassB)=%v, want 0 (게이트 없음)", e.Level(BassB))
	}
}

// 3. 블록마다 리셋 + 뮤트 — BD를 치게 한 뒤 뮤트하고 꼬리(앰프 시정수 300ms)가 소진되면
//    Level(BD)는 정확히 0으로 돌아간다. 리셋이 없으면 구 블록 피크가 잔존한다.
func TestLevelMuteBD(t *testing.T) {
	e := New(3)
	silenceAll(e)
	e.drumPat[0][0] = StepGate
	buf := make([]float32, 2*Block)
	e.Render(buf) // 스텝 0에서 BD 트리거
	if e.Level(BD) <= 0 {
		t.Fatal("전제 실패: BD 레벨이 안 잡혔다")
	}
	e.Apply(Cmd{Kind: Mute, A: uint8(BD), B: 1})
	for i := 0; i < 1700; i++ { // ≈4.36s — 앰프가 1e-6 아래로 감쇠(τ300ms → ≈4.14s)해 보이스 off
		e.Render(buf)
	}
	if e.Level(BD) != 0 {
		t.Fatalf("Level(BD)=%v, want 0 (뮤트 후 꼬리 소진 — 블록 리셋 계약)", e.Level(BD))
	}
}

// 4. 상한·범위 — 기본 파라미터 5초 렌더에서 모든 파트 레벨이 비음수·비NaN이고 마스터
//    피크의 4배 이내(프리 FX·덕킹·마스터 게인 때문에 정확 등식은 없다 — 스펙의 느슨한
//    상한). 범위 밖 파트 인덱스는 0.
func TestLevelBounds(t *testing.T) {
	e := New(5)
	buf := make([]float32, 2*Block)
	for i := 0; i < SampleRate*5/Block; i++ {
		e.Render(buf)
		pk := e.Peak()
		for p := Part(0); p < NumParts; p++ {
			lv := e.Level(p)
			if lv != lv || lv < 0 {
				t.Fatalf("블록 %d Level(%d)=%v (NaN/음수)", i, p, lv)
			}
			if lv > 4*pk {
				t.Fatalf("블록 %d Level(%d)=%v > 4·peak=%v", i, p, lv, 4*pk)
			}
		}
	}
	if e.Level(NumParts) != 0 || e.Level(200) != 0 {
		t.Fatalf("범위 밖 Level = %v %v, want 0 0", e.Level(NumParts), e.Level(200))
	}
}
