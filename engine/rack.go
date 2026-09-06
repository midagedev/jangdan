// rack.go — 장치 그래프(§14.1). 리드 소유 파일.
//
// 랙 = 장치 슬롯 고정 배열(RackSlots) + 케이블 표(RackCables). 장치는 인터페이스가 아니라
// **종류(DeviceKind) + 인스턴스 번호**로 가리키고, 실제 상태(bassVoice·drumKit·fxChain·
// reverb·chorus)는 Engine이 종류별 고정 배열로 소유한다 — 인터페이스 박싱은 핫 루프
// 무할당 규칙에 걸린다. 케이블은 (src 슬롯·출력 포트) → (dst 슬롯·입력 포트)에 게인
// 하나. 렌더는 케이블 표에서 유도한 위상 순서(Connect/Disconnect/ReadState 시점에 계산 —
// 핫 루프 밖, 순환은 거부)로 샘플 단위 처리한다(engine.go sample).
//
// 포트 합산 규칙(바이트 계약): 한 입력 포트에 꽂힌 첫 케이블은 **대입**, 이후 케이블은
// 덧셈이다 — "0에 더하기"가 아니다(x+0은 −0의 부호를 지워 dry 바이패스의 비트 동일성을
// 깬다). 살아 있지 않은 장치(live=false: 입력 포트가 있는데 게인>0인 입력 케이블이 없는
// 장치 — 옛 revOn/choOn)에서 나오는 케이블은 합산에서 건너뛴다(더하지 않는다). 이 두 규칙으로
// 기본 랙의 출력은 고정 체인이던 때와 같은 연산 순서·같은 바이트다(fx2_test 기본값 해시).
//
// 게인의 단일 소유자: bind == NumParams(비결속)이면 gainQ(4095 양자화)가 정본이고,
// bind < NumParams(결속)이면 그 파라미터가 정본이며 gain은 boundGain(id, q)로 유도된다
// (derive-don't-store — 상태 v4는 결속 케이블의 게인을 저장하지 않는다). 기본 랙의 센드·
// 리턴 케이블은 전부 §13.1 파라미터에 결속돼 있어 기존 노브가 그대로 케이블을 돌린다.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴(FMA 융합 차단 계약). 할당 없음 — 전부 고정 배열.
package engine

// DeviceKind — 장치 종류. 순서는 직렬화 값이라 바꾸지 않고 뒤에만 덧붙인다.
type DeviceKind uint8

const (
	KindNone   DeviceKind = iota
	KindBass              // 출력 0: 보이스 × 채널 레벨(BassALevel/BassBLevel). 인스턴스 0/1 = 파트 BassA/BassB
	KindDrums             // 출력 0 mix, 1 BD(사이드체인), 2..7 보이스별(BD SD CH OH CP CY — 레벨 반영)
	KindFx                // 입력 0 덕킹 대상(베이스), 1 직결(드럼), 2 사이드체인 트리거, 3 딜레이 센드 합 · 출력 0 L, 1 R
	KindReverb            // 입력 0 모노 · 출력 0 L, 1 R
	KindChorus            // 입력 0 모노 · 출력 0 L, 1 R
	KindMain              // 입력 0 L, 1 R → 엔진 출력(busClamp). 제거 불가
	NumDeviceKinds
)

const (
	RackSlots   = 16
	RackCables  = 64
	MaxOutPorts = 8
	MaxInPorts  = 4
	Unbound     = NumParams // cable.bind 값: 파라미터 결속 없음(gainQ가 정본)
)

// 기본 랙 슬롯(Reset이 만든다 — §14.1 "기본 랙(현재 구성)").
const (
	SlotBassA  = 0
	SlotBassB  = 1
	SlotDrums  = 2
	SlotFx     = 3
	SlotReverb = 4
	SlotChorus = 5
	SlotMain   = 6
)

// kindPorts — 종류별 (입력 수, 출력 수). 정적 데이터(할당 아님).
var kindPorts = [NumDeviceKinds][2]uint8{
	KindNone:   {0, 0},
	KindBass:   {0, 1},
	KindDrums:  {0, 8},
	KindFx:     {4, 2},
	KindReverb: {1, 2},
	KindChorus: {1, 2},
	KindMain:   {2, 0},
}

// kindCap — 종류별 인스턴스 수(Engine이 소유하는 고정 배열 길이).
var kindCap = [NumDeviceKinds]uint8{
	KindNone: 0, KindBass: 2, KindDrums: 1, KindFx: 1, KindReverb: 1, KindChorus: 1, KindMain: 1,
}

// cable — 케이블 하나. 표 안에서 dst 슬롯 순으로 정렬돼 있다(같은 dst 안은 삽입 순 — 합산 순서).
type cable struct {
	src, dst uint8
	sp, dp   uint8
	bind     ParamID // Unbound 또는 결속 파라미터
	gainQ    uint16  // 비결속 게인(0..ParamSteps)
	gain     float32 // 유효 게인(유도값)
}

type rack struct {
	kind [RackSlots]DeviceKind
	inst [RackSlots]uint8 // 종류 안 인스턴스 번호

	cables  [RackCables]cable
	nCables int
	cStart  [RackSlots]int // 슬롯 s로 들어오는 케이블 구간 [cStart, cEnd)
	cEnd    [RackSlots]int

	order  [RackSlots]uint8 // 위상 순서(활성 슬롯만)
	nOrder int
	live   [RackSlots]bool // 이번 샘플에 처리되는 장치(파일 주석)

	// 활성 케이블 목록(recompute가 만든다 — 핫 루프는 이것만 돈다): 게인 > 0이고 src가 live인
	// 케이블의 인덱스를 dst 순으로. 게인 0 케이블은 합산에 ±0만 더하므로 건너뛰어도 첫 케이블
	// 대입·후속 덧셈 규칙 아래 기본 랙의 바이트가 같다(fx2_test 기본값 해시가 게이트).
	act    [RackCables]uint8
	aStart [RackSlots]int
	aEnd   [RackSlots]int

	port [RackSlots][MaxOutPorts]float32 // 이번 샘플의 출력 포트 값

	// 파트 → (슬롯, 포트) — 레벨 미터·센드 원본. slot 0xFF = 그 파트의 장치가 없다.
	partSlot [NumParts]uint8
	partPort [NumParts]uint8

	// 슬롯별 로컬 파라미터(양자화 정본)와 스텝 패턴 — 종류가 해석한다(KindPoly부터). 장치가
	// 없는 슬롯에도 저장은 된다(장치를 놓으면 그 값으로 시작 — 순서 무관 재생).
	devParQ [RackSlots][DevParams]uint16
	devPat  [RackSlots][Steps]bassStep
}

// reset — 빈 랙.
func (r *rack) reset() {
	*r = rack{}
	for p := range r.partSlot {
		r.partSlot[p] = 0xFF
	}
}

// buildDefault — 기본 랙: 고정 체인 시절의 배선을 케이블로. 결속 파라미터는 §13.1의
// 센드·리턴 노브. 순서가 합산 순서다(첫 케이블 대입 규칙 — Fx 입력 0은 BassA + BassB).
func (r *rack) buildDefault() {
	r.reset()
	r.addDevice(SlotBassA, KindBass)
	r.addDevice(SlotBassB, KindBass)
	r.addDevice(SlotDrums, KindDrums)
	r.addDevice(SlotFx, KindFx)
	r.addDevice(SlotReverb, KindReverb)
	r.addDevice(SlotChorus, KindChorus)
	r.addDevice(SlotMain, KindMain)
	// 드라이 경로
	r.connect(SlotBassA, 0, SlotFx, 0, Unbound, ParamSteps)
	r.connect(SlotBassB, 0, SlotFx, 0, Unbound, ParamSteps)
	r.connect(SlotDrums, 0, SlotFx, 1, Unbound, ParamSteps)
	r.connect(SlotDrums, 1, SlotFx, 2, Unbound, ParamSteps)
	// 센드: 파트 8 → 딜레이 입력(Fx 포트 3)·리버브, 베이스 2 → 코러스
	for p := Part(0); p < NumParts; p++ {
		s, o := r.partSlot[p], r.partPort[p]
		r.connect(s, o, SlotFx, 3, DelaySend(p), 0)
	}
	for p := Part(0); p < NumParts; p++ {
		s, o := r.partSlot[p], r.partPort[p]
		r.connect(s, o, SlotReverb, 0, RevSend(p), 0)
	}
	r.connect(SlotBassA, 0, SlotChorus, 0, ChoSendA, 0)
	r.connect(SlotBassB, 0, SlotChorus, 0, ChoSendB, 0)
	// 리턴: dry 먼저(대입), 리버브·코러스 리턴은 덧셈(옛 mixBus.process 순서)
	r.connect(SlotFx, 0, SlotMain, 0, Unbound, ParamSteps)
	r.connect(SlotFx, 1, SlotMain, 1, Unbound, ParamSteps)
	r.connect(SlotReverb, 0, SlotMain, 0, RevMix, 0)
	r.connect(SlotReverb, 1, SlotMain, 1, RevMix, 0)
	r.connect(SlotChorus, 0, SlotMain, 0, ChoMix, 0)
	r.connect(SlotChorus, 1, SlotMain, 1, ChoMix, 0)
}

// addDevice — 슬롯에 종류를 놓는다. 점유 슬롯·범위 밖·인스턴스 고갈이면 false(무동작).
// 인스턴스 번호는 비어 있는 가장 작은 것. 파트 표를 갱신한다.
func (r *rack) addDevice(slot int, k DeviceKind) bool {
	if slot < 0 || slot >= RackSlots || k == KindNone || k >= NumDeviceKinds || r.kind[slot] != KindNone {
		return false
	}
	var used [8]bool // kindCap ≤ 8
	for s := 0; s < RackSlots; s++ {
		if r.kind[s] == k {
			used[r.inst[s]] = true
		}
	}
	inst := -1
	for i := 0; i < int(kindCap[k]); i++ {
		if !used[i] {
			inst = i
			break
		}
	}
	if inst < 0 {
		return false
	}
	r.kind[slot] = k
	r.inst[slot] = uint8(inst)
	r.refreshParts()
	r.recompute()
	return true
}

// removeDevice — 슬롯을 비우고 닿는 케이블을 전부 뺀다. Main·빈 슬롯·범위 밖은 false.
func (r *rack) removeDevice(slot int) bool {
	if slot < 0 || slot >= RackSlots || r.kind[slot] == KindNone || r.kind[slot] == KindMain {
		return false
	}
	w := 0
	for i := 0; i < r.nCables; i++ {
		c := r.cables[i]
		if int(c.src) == slot || int(c.dst) == slot {
			continue
		}
		r.cables[w] = c
		w++
	}
	for i := w; i < r.nCables; i++ {
		r.cables[i] = cable{}
	}
	r.nCables = w
	r.kind[slot] = KindNone
	r.inst[slot] = 0
	r.refreshParts()
	r.recompute()
	return true
}

// connect — 케이블 추가(같은 src·sp·dst·dp가 있으면 게인·결속만 갱신). 거부: 범위 밖 슬롯·
// 빈 슬롯·포트 범위 밖·자기 자신·표 가득·순환(추가 뒤 위상 정렬 실패 시 되돌린다).
// 비결속 게인은 gainQ(0..ParamSteps)로 받는다.
func (r *rack) connect(src, sp, dst, dp uint8, bind ParamID, gainQ uint16) bool {
	if int(src) >= RackSlots || int(dst) >= RackSlots || src == dst {
		return false
	}
	ks, kd := r.kind[src], r.kind[dst]
	if ks == KindNone || kd == KindNone || sp >= kindPorts[ks][1] || dp >= kindPorts[kd][0] {
		return false
	}
	if bind > NumParams {
		bind = Unbound
	}
	if gainQ > ParamSteps {
		gainQ = ParamSteps
	}
	for i := 0; i < r.nCables; i++ {
		c := &r.cables[i]
		if c.src == src && c.sp == sp && c.dst == dst && c.dp == dp {
			c.bind, c.gainQ = bind, gainQ
			if bind == Unbound {
				c.gain = float32(gainQ) / ParamSteps
			}
			r.recompute()
			return true
		}
	}
	if r.nCables >= RackCables {
		return false
	}
	// dst 순 정렬 삽입(같은 dst의 끝에 — 합산 순서 = 삽입 순서)
	pos := r.nCables
	for pos > 0 && r.cables[pos-1].dst > dst {
		r.cables[pos] = r.cables[pos-1]
		pos--
	}
	r.cables[pos] = cable{src: src, dst: dst, sp: sp, dp: dp, bind: bind, gainQ: gainQ}
	if bind == Unbound {
		r.cables[pos].gain = float32(gainQ) / ParamSteps
	}
	r.nCables++
	if !r.recompute() { // 순환 — 되돌린다
		r.removeCableAt(pos)
		r.recompute()
		return false
	}
	return true
}

// disconnect — 일치하는 케이블 제거. 없으면 false.
func (r *rack) disconnect(src, sp, dst, dp uint8) bool {
	for i := 0; i < r.nCables; i++ {
		c := &r.cables[i]
		if c.src == src && c.sp == sp && c.dst == dst && c.dp == dp {
			r.removeCableAt(i)
			r.recompute()
			return true
		}
	}
	return false
}

func (r *rack) removeCableAt(i int) {
	for j := i; j < r.nCables-1; j++ {
		r.cables[j] = r.cables[j+1]
	}
	r.nCables--
	r.cables[r.nCables] = cable{}
}

// setBound — 결속 파라미터 id의 새 값 q → 그 케이블들의 유효 게인. 결속이 없으면 무동작.
// 호출 뒤 live 플래그가 바뀔 수 있어 recompute한다(핫 루프 밖).
func (r *rack) setBound(id ParamID, q float32) {
	g := boundGain(id, q)
	hit := false
	for i := 0; i < r.nCables; i++ {
		if r.cables[i].bind == id {
			r.cables[i].gain = g
			hit = true
		}
	}
	if hit {
		r.recompute()
	}
}

// boundGain — 결속 파라미터 → 게인 매핑(§13.1). 리턴 레벨(RevMix·ChoMix)은 ×0.8,
// 센드는 q 그대로. 그 밖의 파라미터가 결속되면 q 그대로(사용자 케이블의 일반 결속).
func boundGain(id ParamID, q float32) float32 {
	if q != q || q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	switch id {
	case RevMix, ChoMix:
		return mul32(q, 0.8)
	}
	return q
}

// refreshParts — 파트 → (슬롯, 포트) 표. 베이스 인스턴스 i = 파트 i(포트 0), 드럼 보이스 v =
// 파트 BD+v(포트 2+v).
func (r *rack) refreshParts() {
	for p := range r.partSlot {
		r.partSlot[p] = 0xFF
		r.partPort[p] = 0
	}
	for s := 0; s < RackSlots; s++ {
		switch r.kind[s] {
		case KindBass:
			r.partSlot[r.inst[s]] = uint8(s)
			r.partPort[r.inst[s]] = 0
		case KindDrums:
			for v := 0; v < NumDrums; v++ {
				r.partSlot[int(BD)+v] = uint8(s)
				r.partPort[int(BD)+v] = uint8(2 + v)
			}
		}
	}
}

// recompute — 케이블 구간·live·위상 순서를 다시 만든다. 순환이면 false(order는 부분).
// Kahn: 준비된 슬롯 중 번호가 가장 작은 것부터(결정론·기본 랙 순서 = 옛 고정 체인 순서).
func (r *rack) recompute() bool {
	// 구간
	for s := 0; s < RackSlots; s++ {
		r.cStart[s], r.cEnd[s] = 0, 0
	}
	for i := 0; i < r.nCables; i++ { // dst 순 정렬이 불변식 — 구간은 연속
		d := int(r.cables[i].dst)
		if r.cEnd[d] == 0 {
			r.cStart[d] = i
		}
		r.cEnd[d] = i + 1
	}
	// live: 입력 포트가 없는 장치·Main은 항상, 나머지는 게인>0 입력 케이블이 하나라도 있을 때
	for s := 0; s < RackSlots; s++ {
		k := r.kind[s]
		if k == KindNone {
			r.live[s] = false
			continue
		}
		if kindPorts[k][0] == 0 || k == KindMain {
			r.live[s] = true
			continue
		}
		on := false
		for i := r.cStart[s]; i < r.cEnd[s]; i++ {
			if r.cables[i].gain > 0 {
				on = true
				break
			}
		}
		r.live[s] = on
	}
	// 활성 케이블 목록
	n := 0
	for s := 0; s < RackSlots; s++ {
		r.aStart[s] = n
		for i := r.cStart[s]; i < r.cEnd[s]; i++ {
			c := &r.cables[i]
			if c.gain > 0 && r.live[c.src] {
				r.act[n] = uint8(i)
				n++
			}
		}
		r.aEnd[s] = n
	}
	// 위상 정렬
	var indeg [RackSlots]int
	for i := 0; i < r.nCables; i++ {
		indeg[r.cables[i].dst]++
	}
	var done [RackSlots]bool
	r.nOrder = 0
	for {
		pick := -1
		for s := 0; s < RackSlots; s++ {
			if !done[s] && r.kind[s] != KindNone && indeg[s] == 0 {
				pick = s
				break
			}
		}
		if pick < 0 {
			break
		}
		done[pick] = true
		r.order[r.nOrder] = uint8(pick)
		r.nOrder++
		for i := 0; i < r.nCables; i++ {
			if int(r.cables[i].src) == pick {
				indeg[r.cables[i].dst]--
			}
		}
	}
	for s := 0; s < RackSlots; s++ {
		if r.kind[s] != KindNone && !done[s] {
			return false // 순환에 갇힌 슬롯
		}
	}
	return true
}

// sumInputs — 슬롯 s의 입력 포트 합(활성 케이블만: 첫 케이블 대입·이후 덧셈, NaN → 0).
func (r *rack) sumInputs(s int, in *[MaxInPorts]float32) {
	var got [MaxInPorts]bool
	for a := r.aStart[s]; a < r.aEnd[s]; a++ {
		c := &r.cables[r.act[a]]
		v := mul32(r.port[c.src][c.sp], c.gain)
		if got[c.dp] {
			in[c.dp] += v
		} else {
			in[c.dp] = v
			got[c.dp] = true
		}
	}
	for p := 0; p < MaxInPorts; p++ {
		if in[p] != in[p] {
			in[p] = 0
		}
	}
}

// ---- 읽기(UI·상태) ----

// Kind — 슬롯의 장치 종류. 범위 밖은 KindNone.
func (e *Engine) Kind(slot int) DeviceKind {
	if slot < 0 || slot >= RackSlots {
		return KindNone
	}
	return e.rack.kind[slot]
}

// NumCables — 케이블 수.
func (e *Engine) NumCables() int { return e.rack.nCables }

// Cable — i번째 케이블(dst 순). 범위 밖은 ok=false.
func (e *Engine) Cable(i int) (src, sp, dst, dp uint8, gain float32, bind ParamID, ok bool) {
	if i < 0 || i >= e.rack.nCables {
		return 0, 0, 0, 0, 0, Unbound, false
	}
	c := &e.rack.cables[i]
	return c.src, c.sp, c.dst, c.dp, c.gain, c.bind, true
}

// KindPorts — 종류의 (입력 수, 출력 수). 범위 밖은 0,0.
func KindPorts(k DeviceKind) (in, out uint8) {
	if k >= NumDeviceKinds {
		return 0, 0
	}
	return kindPorts[k][0], kindPorts[k][1]
}

// setDevParam — 슬롯 로컬 파라미터 양자화 저장. 범위 밖 무동작. 종류별 계수 유도는 Engine이 한다.
func (r *rack) setDevParam(slot, k int, n uint16) bool {
	if slot < 0 || slot >= RackSlots || k < 0 || k >= DevParams {
		return false
	}
	if n > ParamSteps {
		n = ParamSteps
	}
	r.devParQ[slot][k] = n
	return true
}

// setDevStep — 슬롯 스텝 패턴. step&15, note 클램프, flags 마스킹(게이트·액센트만).
func (r *rack) setDevStep(slot int, step, note, flags uint8) bool {
	if slot < 0 || slot >= RackSlots {
		return false
	}
	if note > MaxNote {
		note = MaxNote
	}
	r.devPat[slot][step&(Steps-1)] = bassStep{note: note, flags: flags & (StepGate | StepAccent)}
	return true
}

// placeDevice — ReadState용: 슬롯에 (종류, 인스턴스)를 그대로 놓는다. 범위 밖·점유·인스턴스
// 범위 밖·같은 종류의 인스턴스 중복이면 false(그 슬롯은 비운다 — 재정규화).
func (r *rack) placeDevice(slot int, k DeviceKind, inst uint8) bool {
	if slot < 0 || slot >= RackSlots || k == KindNone || k >= NumDeviceKinds || r.kind[slot] != KindNone || inst >= kindCap[k] {
		return false
	}
	for s := 0; s < RackSlots; s++ {
		if r.kind[s] == k && r.inst[s] == inst {
			return false
		}
	}
	r.kind[slot] = k
	r.inst[slot] = inst
	r.refreshParts()
	r.recompute()
	return true
}

// hasKind — 그 종류의 장치가 랙에 있는가.
func (r *rack) hasKind(k DeviceKind) bool {
	for s := 0; s < RackSlots; s++ {
		if r.kind[s] == k {
			return true
		}
	}
	return false
}

// rebind — 결속 케이블 전부의 게인을 파라미터 표에서 다시 유도한다(ReadState 마지막).
func (r *rack) rebind(params *[NumParams]float32) {
	for i := 0; i < r.nCables; i++ {
		c := &r.cables[i]
		if c.bind < NumParams {
			c.gain = boundGain(c.bind, params[c.bind])
		}
	}
	r.recompute()
}
