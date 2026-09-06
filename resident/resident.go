// resident.go — 레지던트 DJ: 상태·Tick·MANUAL 잠금·노브 보간.
//
// 레지던트는 메인 스레드(표준 Go wasm)에서 돌며 바 경계마다 []engine.Cmd를 내놓는다.
// 호스트가 그것을 엔진에 보내며 로그(저자=Resident)에 기록한다 — 레지던트 자체는
// 리플레이되지 않으므로 결정론이 계약이다: 같은 seed·vibe·cfg·Input 시퀀스 → 같은
// Cmd 시퀀스. 엔트로피원은 아래 rngT(xorshift32; engine의 것은 비내보출이라 여기에
// 같은 알고리즘을 둔다 — 저장소 탐색: 재사용 가능한 내보출 구현은 없었다)뿐이고
// 시간은 Input.Now로만 들어온다(벽시계 참조 금지). engine/의 TinyGo 제약(math 금지·
// 무할당)은 여기에 적용되지 않는다(표준 라이브러리 자유, math/rand는 제외).
//
// 설계 원칙 — derive-don't-store: 페이즈·포모도로·에너지 사이클·코드 진행은 매 바 Tick에서
// Input.Now·cfg·seed로부터 유도한다(pomodoro.go·energy.go·harmony.go). 저장 상태는 전이
// 감지용 lastPhase, 세션 첫 바 플래그 vibePending·keyPending, 슬롯 순환 regenCount,
// 노브 모델 cur/tgt뿐이다.
//
// Tick 반환 슬라이스는 내부 버퍼 재사용이다 — 호출자는 다음 Tick 전에 즉시 소비
// (엔진 Apply 또는 복사)해야 한다.
package resident

import "github.com/midagedev/jangdan/engine"

// Vibe — 세션 분위기. 바이브 변경(SetVibe)은 다음 바 경계부터 반영된다.
type Vibe uint8

const (
	DeepFocus Vibe = iota // 120BPM, 드럼 얇게(OH·CP 없음·CH 밀도 ×0.5), 컷오프 대역 −0.15
	Rush                  // 135BPM, 풀 드럼, 컷오프 대역 +0.1, Drive +0.15
	Lofi                  // 112BPM, Drive −0.1(≥0), Delay +0.3, 컷오프 대역 −0.2, CH 밀도 ×0.7
	numVibes
)

// Phase — 에너지 곡선의 네 페이즈(점유: Intro 15% → Build 35% → Drop 25% → Breakdown 25%).
type Phase uint8

const (
	Intro Phase = iota
	Build
	Drop
	Breakdown
	numPhases
)

// PomodoroPhase — 포모도로 구간.
type PomodoroPhase uint8

const (
	Focus PomodoroPhase = iota
	Rest
)

// Input — Tick 하나의 입력. Now는 세션 시작 후 초이며 단조를 전제한다
// (역행 방어: 낮은 Now는 직전 최댓값으로 클램프된다).
type Input struct {
	Bar      uint32
	Step     int
	Now      float64 // 세션 시작 후 초
	BarStart bool    // 이 틱이 바 경계
}

// Config — 포모도로 길이(분). 0 이하는 기본값으로 정규화한다(25/5/5).
type Config struct {
	FocusMin     float64
	RestMin      float64
	DemoFocusMin float64 // 첫 세션(기본 5분 데모)
}

// 노브 보간 상수: 바당 SetParam 상한 8개, 파라미터당 방출 간격 ≥ 2스텝
// (스텝 4·12 두 번, 각 회 최대 4개 파라미터 → 4+4 = 8).
const (
	setParamBudgetPerBar = 8
	interpStep1          = 4
	interpStep2          = 12
	moversPerRound       = 4
	minMove              = float32(1.5) / float32(engine.ParamSteps) // 양자화 단계 미만 이동은 무의미
)

// moverParams — 에너지 곡선이 움직이는 노브(고정 순서 = 결정론). 컷오프는 계약상
// A 파트 대역만 지정되므로 CutoffA만 둔다(레조넌스·EnvMod은 대역이 파트 무효격이라
// 양쪽에 적용).
var moverParams = [...]engine.ParamID{
	engine.CutoffA,
	engine.BassAParams + engine.BReso,
	engine.BassBParams + engine.BReso,
	engine.BassAParams + engine.BEnvMod,
	engine.BassBParams + engine.BEnvMod,
	engine.Drive,
	engine.Delay,
}

// Resident — 레지던트 상태 전체. New 한 번 만들고 Tick으로만 진행한다.
type Resident struct {
	seed uint32
	vibe Vibe
	cfg  Config

	vibePending bool // 다음 바 경계에 Tempo SetParam 1개(또는 초기 적용)
	keyPending  bool // 세션 첫 바 경계에 SetKey 1개(harmony.go §12.4)

	now     float64 // Input.Now의 단조 클램프값
	prevBar uint32  // 직전 Tick의 Input.Bar(Hand 유지 판정)

	locked [engine.NumParams]bool // MANUAL 잠금

	harmonyLocked bool // 사람이 코드·모드·키를 편집했다 — 세션 내 영구(Resume()로 풀리지 않는다, §12.2)

	cur [engine.NumParams]float32 // 레지던트가 아는 현재 노브값(엔진 기본값에서 출발)
	tgt [engine.NumParams]float32 // 이 바의 목표(바 경계에 갱신)

	pat   [2][engine.Steps]patStep // 생성된 베이스 패턴(파트 A/B)
	drums [6][engine.Steps]uint8   // 생성된 드럼 패턴(BD SD CH OH CP CY)

	seenPhase    bool
	lastPhase    Phase  // 페이즈 전이 감지(재생성·Drop 발동 트리거)
	lastRegenBar uint32 // 마지막 패턴 재생성 바(8바 주기용)
	regenCount   int    // 슬롯 4..7 순환 카운터
	pendingWrite bool   // SelectPattern을 보낸 바 다음 바에 BassStep/DrumStep 16개씩 발사

	phase  Phase
	energy float32

	handParam engine.ParamID
	handBar   uint32

	spBudget int // 이 바의 SetParam 잔여 예산(바 경계에서 8으로 리셋)

	buf []engine.Cmd // Tick 반환 버퍼(재사용)
}

// New — 레지던트 생성. vibe가 범위 밖이면 DeepFocus로 정규화(오용 방어),
// cfg의 0 이하 필드는 기본값(25/5/5)으로 정규화한다. 초기 Tempo 적용도
// 첫 바 경계에 1개 나간다(vibePending).
func New(seed uint32, vibe Vibe, cfg Config) *Resident {
	if seed == 0 { // xorshift32(0) = 0 영구 고정 방어(engine.Reset과 같은 상수)
		seed = 0x9E3779B9
	}
	if vibe >= numVibes {
		vibe = DeepFocus
	}
	r := &Resident{
		seed:        seed,
		vibe:        vibe,
		cfg:         normConfig(cfg),
		vibePending: true,
		keyPending:  true,
		buf:         make([]engine.Cmd, 0, 160),
	}
	r.cur = engine.DefaultParams()
	r.tgt = r.cur
	r.phase, _, _ = r.derive(0)
	r.energy = phaseEnergy[r.phase]
	return r
}

// Tick — 한 틱. BarStart 틱에서 페이즈·목표 대역·패턴 재생성을 결정하고,
// 그 외 틱에서는 노브 보간 SetParam만 낸다(바당 ≤ 8개).
func (r *Resident) Tick(in Input) []engine.Cmd {
	r.buf = r.buf[:0]
	if in.Now < r.now { // 오용 방어 3: Now 역행 → 단조 클램프(시간 유도 상태는 뒤로 안 감)
		in.Now = r.now
	}
	prevNow := r.now
	r.now = in.Now
	in.Step &= engine.Steps - 1 // 스텝 정규화(클램프가 아니라 마스크 — engine.Apply 관례)
	r.prevBar = in.Bar

	// 포모도로 경계(Focus↔Rest) 통과 → MANUAL 잠금 전체 해제(기획 결정).
	// 경계는 바 중간(스텝 틱 사이)에서도 넘을 수 있어 모든 틱에서 검사한다.
	if pomoCrossed(r.cfg, prevNow, in.Now) {
		r.Resume()
	}

	if in.BarStart {
		r.onBar(in)
	} else {
		r.onStep(in)
	}
	return r.buf
}

// onBar — 바 경계 틱: 바이브 Tempo·페이즈·패턴·목표 대역.
func (r *Resident) onBar(in Input) {
	r.spBudget = setParamBudgetPerBar

	// 바이브 변경(또는 초기 적용): Tempo SetParam 딱 1개. Tempo 잠금 시 생략.
	if r.vibePending {
		r.vibePending = false
		if !r.locked[engine.Tempo] && r.spBudget > 0 {
			r.emitParam(engine.Tempo, tempoQ(vibeBPM(r.vibe)), in.Bar)
			r.spBudget--
		}
	}

	// 세션 첫 바: 조성 선언 1개(엔진 초기값 seed%12와 같은 값 — 로그 자기서술).
	// 화성 잠금 중이면 내지 않는다(플래그는 그대로 소진 — 잠금은 세션 내 영구).
	if r.keyPending {
		r.keyPending = false
		if !r.harmonyLocked {
			r.emit(engine.Cmd{Kind: engine.SetKey, A: uint8(r.seed % engine.NumKeys)})
		}
	}

	ph, cycleSeed, session := r.derive(in.Now)
	dropEntry := r.seenPhase && ph == Drop && r.lastPhase != Drop
	bdEntry := r.seenPhase && ph == Breakdown && r.lastPhase != Breakdown
	introEntry := !r.seenPhase || (ph == Intro && r.lastPhase != Intro)
	phaseEntry := !r.seenPhase || ph != r.lastPhase
	r.phase = ph
	r.energy = phaseEnergy[ph]

	// 화성(§12.4, harmony.go): 잠금 중이면 사람이 소유 — 조성·코드·모드를 내지 않는다.
	// 방출 순서는 고정(결정론): 코드 트래 → B 모드 → (아래) 패턴 재생성 → Drop.
	if !r.harmonyLocked {
		if introEntry {
			r.emitChordTrack(cycleSeed, false) // 사이클 시작: 진행표 추첨·방출(7th 없이)
		}
		if dropEntry {
			r.emitChordTrack(cycleSeed, true) // 같은 진행을 7th(iv·v·VII)로 재방출
		}
		if bdEntry {
			r.emitChordTrack(cycleSeed, false) // 브레이크다운: 7th 없이 재방출
		}
		if phaseEntry { // B 모드는 모든 페이즈 진입 바(첫 바 = 초기 모드 명시 포함)
			m, d := bassModeFor(ph)
			r.emit(engine.Cmd{Kind: engine.BassMode, A: 1, B: m, C: d})
		}
	}
	if phaseEntry { // 폴리 리드 리듬·음색(poly.go) — 화성 잠금과 무관(음은 엔진이 코드 트랙에서 읽는다)
		r.emitPoly(ph)
	}

	// 패턴 재생성: 페이즈 진입 또는 마지막 재생성에서 8바.
	regen := !r.seenPhase || ph != r.lastPhase || in.Bar-r.lastRegenBar >= 8
	if regen {
		r.seenPhase = true
		r.lastPhase = ph
		r.lastRegenBar = in.Bar
		slot := uint8(4 + r.regenCount&3) // 슬롯 4..7 순환
		r.regenCount++
		// BassStep은 현재 슬롯에만 쓴다 — 이 바에 SelectPattern을 보내 다음 바
		// 경계부터 새 슬롯이 현재가 되고, 그때 16스텝씩 쓴다(스펙에 명시된 순서).
		r.emit(engine.Cmd{Kind: engine.SelectPattern, A: uint8(engine.BassA), B: slot})
		r.emit(engine.Cmd{Kind: engine.SelectPattern, A: uint8(engine.BassB), B: slot})
		r.genPatterns(cycleSeed, in.Bar, ph, session, dropEntry)
		r.pendingWrite = true
	} else {
		r.lastPhase = ph
		if r.pendingWrite {
			// 저번 바에 돌려둔 슬롯이 이 바부터 현재 슬롯 — 여기에 패턴을 쓴다.
			r.pendingWrite = false
			r.emitPatterns()
		}
	}

	// Drop 진입 바: 다음 바에 엔진이 발동(전 파트 뮤트 해제·CY·8바 감쇠).
	if dropEntry {
		r.emit(engine.Cmd{Kind: engine.Drop})
	}

	r.updateTargets(cycleSeed, in.Bar, ph)
}

// onStep — 바 경계가 아닌 틱: 노브 보간. 스텝 4·12 두 회, 각 회 델타 상위 최대
// 4개 파라미터(동률은 moverParams 순) → 바당 최대 8 SetParam, 파라미터당 간격 8스텝.
func (r *Resident) onStep(in Input) {
	if in.Step != interpStep1 && in.Step != interpStep2 {
		return
	}
	t := float32(13.0 / 16.0)
	if in.Step == interpStep1 {
		t = float32(5.0 / 16.0)
	}
	var used [len(moverParams)]bool
	for k := 0; k < moversPerRound; k++ {
		best := -1
		var bestDelta float32
		for i, id := range moverParams {
			if used[i] || r.locked[id] {
				continue
			}
			d := r.tgt[id] - r.cur[id]
			if d < 0 {
				d = -d
			}
			if d > bestDelta { // 동률은 배열 순서가 이긴다(결정론)
				bestDelta = d
				best = i
			}
		}
		if best < 0 || bestDelta < minMove || r.spBudget <= 0 {
			break
		}
		used[best] = true
		id := moverParams[best]
		v := r.cur[id] + (r.tgt[id]-r.cur[id])*t
		r.emitParam(id, v, in.Bar)
		r.spBudget--
	}
}

// Lock — MANUAL 잠금. 이후 그 파라미터로는 어떤 경로(보간·페이즈·바이브)에서도
// SetParam을 내지 않는다. 범위 밖 ID는 무시한다(오용 방어 2).
func (r *Resident) Lock(id engine.ParamID) {
	if id < engine.NumParams {
		r.locked[id] = true
	}
}

// Locked — 잠금 여부. 범위 밖 ID는 항상 false.
func (r *Resident) Locked(id engine.ParamID) bool {
	return id < engine.NumParams && r.locked[id]
}

// Resume — 노브 잠금 전체 해제(harmonyLocked는 건드리지 않는다). 포모도로 경계·30초 무접촉에서도 호출된다.
func (r *Resident) Resume() {
	r.locked = [engine.NumParams]bool{}
}

// LockHarmony — 사람이 SetChord/BassMode/SetKey를 보냈다. 이후 레지던트는 화성 명령(SetKey·SetChord·BassMode)을
// 내지 않는다(패턴·노브는 계속). 세션 내 영구 — 코드 선택은 노브와 달리 작곡이라 자동 복귀하지 않는다(§12.2).
func (r *Resident) LockHarmony() { r.harmonyLocked = true }

// HarmonyLocked — LockHarmony 이후 true.
func (r *Resident) HarmonyLocked() bool { return r.harmonyLocked }

// SetVibe — 바이브 변경. Tempo는 다음 바 경계에 SetParam 1개, 대역·드럼 밀도도
// 다음 바 재계산부터 반영. 범위 밖 값은 무시한다(오용 방어).
func (r *Resident) SetVibe(v Vibe) {
	if v >= numVibes || v == r.vibe {
		return
	}
	r.vibe = v
	r.vibePending = true
}

// Hand — 마지막으로 움직인 파라미터와 그 유효성(1바 유지). UI 캐릭터 손 애니메이션용.
func (r *Resident) Hand() (engine.ParamID, bool) {
	if r.prevBar < r.handBar { // 바 역행 관측 시 활성으로 둔다(보수적)
		return r.handParam, true
	}
	return r.handParam, r.prevBar-r.handBar <= 1
}

// Phase — 현재(직전 바 경계에서 유도한) 페이즈.
func (r *Resident) Phase() Phase { return r.phase }

// Energy — 현재 목표 에너지 0..1(페이즈 기준값).
func (r *Resident) Energy() float32 { return r.energy }

// Pomodoro — 현재 포모도로 구간·남은 초·세션 인덱스(r.now에서 매번 유도).
func (r *Resident) Pomodoro() (PomodoroPhase, float64, int) {
	sp := pomoAt(r.cfg, r.now)
	return sp.ph, sp.end - r.now, sp.session
}

// emit / emitParam — 버퍼 추가 헬퍼. emitParam은 [0,1] 클램프·노브 모델 동기·Hand 갱신.
func (r *Resident) emit(c engine.Cmd) { r.buf = append(r.buf, c) }

func (r *Resident) emitParam(id engine.ParamID, v float32, bar uint32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	r.cur[id] = v
	r.emit(engine.Cmd{Kind: engine.SetParam, A: uint8(id), V: v})
	r.handParam = id
	r.handBar = bar
}

// rngT — xorshift32(engine/approx.go의 것과 같은 알고리즘, 비내보출이라 재정의).
type rngT struct{ x uint32 }

func (g *rngT) next() uint32 {
	x := g.x
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	g.x = x
	return x
}

// float — [0,1) 균등. 상위 24비트 사용(부동 소수점 정밀도 한계에 맞춤).
func (g *rngT) float() float32 { return float32(g.next()>>8) / float32(1<<24) }

// xs32 — 상태 없는 xorshift32 한 스텝(시드 파생용).
func xs32(x uint32) uint32 {
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	return x
}

func clamp01(v float32) float32 {
	if v != v || v < 0 { // NaN 방어 포함
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// vibeBPM — 바이브의 템포. barDur = 240/BPM(16스텝 = 4비트).
func vibeBPM(v Vibe) float64 {
	switch v {
	case DeepFocus:
		return 120
	case Rush:
		return 135
	case Lofi:
		return 112
	}
	return 130
}

// tempoQ — BPM → Tempo 파라미터값 q = (BPM−100)/60, [0,1] 클램프.
func tempoQ(bpm float64) float32 {
	q := (bpm - 100) / 60
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	return float32(q)
}

// normConfig — 0 이하 필드 기본값 정규화(25/5/5, 데모 첫 세션 5분).
func normConfig(c Config) Config {
	if c.FocusMin <= 0 {
		c.FocusMin = 25
	}
	if c.RestMin <= 0 {
		c.RestMin = 5
	}
	if c.DemoFocusMin <= 0 {
		c.DemoFocusMin = 5
	}
	return c
}
