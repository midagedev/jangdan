// rack_test.go — 장치 그래프(rack.go) 수치 게이트. 리드 소유.
//
// | 계약                                   | 단언                                              | FAIL-first |
// |----------------------------------------|---------------------------------------------------|------------|
// | 기본 랙 = 옛 고정 체인(바이트)           | TestFx2DefaultHash(fx2_test.go) 상수 불변 cacd0efe… | 그래프 도입 커밋에서 상수를 바꾸지 않고 통과 — 바이트 동일 |
// | 기본 랙 구성·위상 순서                  | TestRackDefault: 슬롯 8(폴리 포함)·케이블 32·order 0 1 2 7 3 4 5 6·전부 live | order 비교를 뒤집으면 실패 |
// | AddDevice 정규화                       | TestRackAddRemove: 점유 슬롯·종류 범위 밖·인스턴스 고갈(베이스 3번째) 무동작, 제거 시 닿는 케이블 소멸, Main 제거 불가 | 인스턴스 고갈 검사 제거 시 used 배열 밖 인스턴스로 실패 |
// | Connect 정규화·순환 거부                | TestRackConnectRules: 자기 자신·포트 범위 밖·빈 슬롯 거부, 중복은 갱신, Fx→Reverb→Fx 순환 거부(표 불변) | 되돌리기 제거 시 nCables가 늘어 실패 |
// | 케이블 표 가득                          | TestRackConnectRules: 64개째까지 성공·65번째 거부   | 상한 검사 제거 시 인덱스 패닉 |
// | 결속 게인 유도(derive-don't-store)       | TestRackBoundGain: RevMix 0.5 → 리턴 케이블 게인 0.4, SetParam 뒤 갱신 | boundGain ×0.8 제거 시 실패 |
// | 재배선이 소리를 바꾼다                  | TestRackRewireAudible: 베이스 A → Fx 케이블을 끊으면 출력 변화, 다시 이으면 원래 바이트 | disconnect 무동작이면 "차이 0" 실패 |
// | 상태 v5 왕복                            | TestRackStateRoundTrip: 사용자 장치·케이블·비결속 게인·로컬 파라미터·장치 패턴 왕복, 오염(순환·범위 밖·Main 없음) 재정규화 | placeDevice 중복 검사 제거 시 인스턴스 중복 통과 |
// | 무할당                                  | TestRackNoAllocs: Apply(Connect/Disconnect/AddDevice/RemoveDevice)·재배선 랙 Render 0 | 슬라이스 append로 바꾸면 실패 |
// | 파트 장치 제거 → 레벨 0                 | TestRackLevelsAfterRemove: BassA 제거 뒤 Level(BassA)==0, 드럼 레벨은 유지 | partSlot 갱신 누락 시 옛 포트 값 잔존으로 실패 |
package engine

import "testing"

func TestRackDefault(t *testing.T) {
	e := New(1)
	r := &e.rack
	want := [...]DeviceKind{KindBass, KindBass, KindDrums, KindFx, KindReverb, KindChorus, KindMain, KindPoly}
	for s := range want {
		if e.Kind(s) != want[s] {
			t.Fatalf("슬롯 %d 종류 %d, want %d", s, e.Kind(s), want[s])
		}
	}
	if e.Kind(8) != KindNone || e.Kind(-1) != KindNone || e.Kind(RackSlots) != KindNone {
		t.Fatal("빈 슬롯·범위 밖은 KindNone")
	}
	// 드라이 4 + 딜레이 센드 8 + 리버브 센드 8 + 코러스 2 + 리턴 6 = 28, 폴리(드라이·딜레이·리버브·코러스) 4 = 32
	if e.NumCables() != 32 {
		t.Fatalf("케이블 %d, want 32", e.NumCables())
	}
	if r.nOrder != 8 {
		t.Fatalf("order 길이 %d, want 8", r.nOrder)
	}
	// Kahn 최소 슬롯 우선: 0 1 2 → Fx(3)는 폴리(7)를 기다린다 → 7이 3 앞에 온다
	wantOrder := [...]uint8{0, 1, 2, 7, 3, 4, 5, 6}
	for i := range wantOrder {
		if r.order[i] != wantOrder[i] {
			t.Fatalf("order[%d] = %d, want %d", i, r.order[i], wantOrder[i])
		}
	}
	for s := 0; s < 8; s++ {
		if !r.live[s] {
			t.Fatalf("기본 랙 슬롯 %d live=false", s)
		}
	}
	// 파트 표
	if r.partSlot[BassB] != SlotBassB || r.partPort[BassB] != 0 || r.partSlot[CY] != SlotDrums || r.partPort[CY] != 7 {
		t.Fatalf("파트 표 %v %v", r.partSlot, r.partPort)
	}
	// 케이블은 dst 순
	for i := 1; i < r.nCables; i++ {
		if r.cables[i-1].dst > r.cables[i].dst {
			t.Fatalf("케이블 %d dst 역순", i)
		}
	}
}

func TestRackAddRemove(t *testing.T) {
	e := New(1)
	r := &e.rack
	e.Apply(Cmd{Kind: AddDevice, A: 0, B: uint8(KindReverb)}) // 점유
	if e.Kind(0) != KindBass {
		t.Fatal("점유 슬롯에 장치가 놓임")
	}
	e.Apply(Cmd{Kind: AddDevice, A: 9, B: uint8(NumDeviceKinds)}) // 종류 범위 밖
	e.Apply(Cmd{Kind: AddDevice, A: 9, B: uint8(KindBass)})       // 인스턴스 고갈(2개뿐)
	e.Apply(Cmd{Kind: AddDevice, A: 9, B: uint8(KindReverb)})     // 인스턴스 고갈(1개뿐)
	e.Apply(Cmd{Kind: AddDevice, A: 40, B: uint8(KindBass)})      // 슬롯 범위 밖
	if e.Kind(9) != KindNone {
		t.Fatalf("슬롯 9에 %d — 고갈·범위 밖이 통과", e.Kind(9))
	}
	// 베이스 A를 빼면 인스턴스 0이 풀려 슬롯 9에 놓인다(inst 0)
	n := e.NumCables()
	e.Apply(Cmd{Kind: RemoveDevice, A: SlotBassA})
	if e.Kind(SlotBassA) != KindNone {
		t.Fatal("RemoveDevice 무동작")
	}
	// 베이스 A에 닿던 케이블: Fx 드라이 1 + 딜레이 1 + 리버브 1 + 코러스 1 = 4
	if e.NumCables() != n-4 {
		t.Fatalf("제거 뒤 케이블 %d, want %d", e.NumCables(), n-4)
	}
	if r.partSlot[BassA] != 0xFF {
		t.Fatal("파트 표에 제거된 장치 잔존")
	}
	e.Apply(Cmd{Kind: AddDevice, A: 9, B: uint8(KindBass)})
	if e.Kind(9) != KindBass || r.inst[9] != 0 {
		t.Fatalf("슬롯 9: 종류 %d inst %d, want Bass/0", e.Kind(9), r.inst[9])
	}
	if r.partSlot[BassA] != 9 {
		t.Fatalf("파트 BassA 슬롯 %d, want 9", r.partSlot[BassA])
	}
	// Main·빈 슬롯 제거 불가
	e.Apply(Cmd{Kind: RemoveDevice, A: SlotMain})
	e.Apply(Cmd{Kind: RemoveDevice, A: 12})
	if e.Kind(SlotMain) != KindMain {
		t.Fatal("Main이 제거됨")
	}
}

func TestRackConnectRules(t *testing.T) {
	e := New(1)
	r := &e.rack
	n := e.NumCables()
	// 자기 자신·빈 슬롯·포트 범위 밖
	if r.connect(SlotFx, 0, SlotFx, 1, Unbound, ParamSteps) || r.connect(9, 0, SlotFx, 0, Unbound, 1) ||
		r.connect(SlotBassA, 1, SlotFx, 0, Unbound, 1) || r.connect(SlotBassA, 0, SlotFx, 4, Unbound, 1) ||
		r.connect(SlotBassA, 0, SlotBassB, 0, Unbound, 1) { // 베이스는 입력 포트 없음
		t.Fatal("거부돼야 할 connect가 통과")
	}
	if e.NumCables() != n {
		t.Fatalf("거부됐는데 표가 변함 %d", e.NumCables())
	}
	// 중복은 갱신(개수 불변) — 게인 비결속으로 바꿔 본다
	e.Apply(Cmd{Kind: Connect, A: SlotBassA, B: SlotFx, C: 0 | 0<<4, D: 0xFF, V: 0.5})
	if e.NumCables() != n {
		t.Fatalf("중복 connect가 케이블을 늘림 %d", e.NumCables())
	}
	found := false
	for i := 0; i < r.nCables; i++ {
		c := r.cables[i]
		if c.src == SlotBassA && c.dst == SlotFx && c.dp == 0 {
			found = true
			if c.bind != Unbound || c.gainQ != 2048 {
				t.Fatalf("갱신된 케이블 bind %d gainQ %d", c.bind, c.gainQ)
			}
		}
	}
	if !found {
		t.Fatal("갱신 대상 케이블 없음")
	}
	// 순환: Fx L → Reverb in(이미 Reverb → Main, Fx → Main; 리버브 → Fx 딜레이 입력을 잇고 Fx → 리버브를 이으면 순환)
	if !r.connect(SlotReverb, 0, SlotFx, 3, Unbound, ParamSteps) {
		t.Fatal("Reverb → Fx(딜레이 입력) 연결이 거부됨(순환 아님)")
	}
	m := e.NumCables()
	if r.connect(SlotFx, 0, SlotReverb, 0, Unbound, ParamSteps) {
		t.Fatal("순환 케이블이 통과")
	}
	if e.NumCables() != m || r.nOrder != 8 {
		t.Fatalf("순환 거부 뒤 표 %d(want %d)·order %d(want 8) — 되돌리기 실패", e.NumCables(), m, r.nOrder)
	}
	if !r.disconnect(SlotReverb, 0, SlotFx, 3) || r.disconnect(SlotReverb, 0, SlotFx, 3) {
		t.Fatal("disconnect 1회 참·2회 거짓이어야")
	}
	// 표 가득: 남은 자리를 (베이스 A/B·드럼 8포트) → Fx/Reverb/Chorus 입력 조합으로 채운다(중복 없이)
	e2 := New(2)
	r2 := &e2.rack
	added := 0
	for _, src := range [...]uint8{SlotDrums, SlotBassA, SlotBassB} {
		for sp := uint8(0); sp < kindPorts[r2.kind[src]][1] && r2.nCables < RackCables; sp++ {
			for dp := uint8(0); dp < 4 && r2.nCables < RackCables; dp++ {
				for _, dst := range [...]uint8{SlotFx, SlotReverb, SlotChorus} {
					if r2.nCables >= RackCables || dp >= kindPorts[r2.kind[dst]][0] {
						continue
					}
					before := r2.nCables
					r2.connect(src, sp, dst, dp, Unbound, 100)
					if r2.nCables > before {
						added++
					}
				}
			}
		}
	}
	if r2.nCables != RackCables {
		t.Fatalf("표를 채우지 못함 %d(추가 %d)", r2.nCables, added)
	}
	if r2.connect(SlotBassA, 0, SlotReverb, 0, Unbound, 100) == false { // 이미 있는 케이블(갱신) — 가득이어도 갱신은 됨
		t.Fatal("가득 상태에서 기존 케이블 갱신이 거부됨")
	}
	if r2.connect(SlotBassB, 0, SlotFx, 1, Unbound, 100) { // 새 케이블은 거부
		t.Fatal("65번째 케이블이 통과")
	}
}

func TestRackBoundGain(t *testing.T) {
	e := New(1)
	r := &e.rack
	e.SetParam(RevMix, 0.5)
	for i := 0; i < r.nCables; i++ {
		c := r.cables[i]
		if c.bind == RevMix && c.gain != mul32(e.Param(RevMix), 0.8) {
			t.Fatalf("RevMix 결속 게인 %v, want %v", c.gain, mul32(e.Param(RevMix), 0.8))
		}
		if c.bind == RevSend(SD) && c.gain != e.Param(RevSend(SD)) {
			t.Fatalf("RevSend(SD) 결속 게인 %v, want %v", c.gain, e.Param(RevSend(SD)))
		}
	}
	// 사용자 결속: 새 케이블을 CutoffA에 결속 → 지금 값으로 유도, SetParam 뒤 갱신
	e.Apply(Cmd{Kind: Connect, A: SlotDrums, B: SlotChorus, C: 0 | 0<<4, D: uint8(CutoffA), V: 0})
	e.SetParam(CutoffA, 0.25)
	for i := 0; i < r.nCables; i++ {
		c := r.cables[i]
		if c.src == SlotDrums && c.dst == SlotChorus {
			if c.bind != CutoffA || c.gain != e.Param(CutoffA) {
				t.Fatalf("사용자 결속 케이블 bind %d gain %v, want %d/%v", c.bind, c.gain, CutoffA, e.Param(CutoffA))
			}
			return
		}
	}
	t.Fatal("사용자 결속 케이블 없음")
}

func render(e *Engine, blocks int) []float32 {
	buf := make([]float32, 2*Block)
	out := make([]float32, 0, blocks*2*Block)
	for i := 0; i < blocks; i++ {
		e.Render(buf)
		out = append(out, buf...)
	}
	return out
}

func TestRackRewireAudible(t *testing.T) {
	a := render(New(3), 200)
	e := New(3)
	e.Apply(Cmd{Kind: Disconnect, A: SlotBassA, B: SlotFx, C: 0 | 0<<4})
	b := render(e, 200)
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("베이스 A 드라이 케이블을 끊었는데 출력 불변")
	}
	// 다시 잇는다(비결속 게인 1) — 합산 순서가 BassB, BassA로 바뀌므로 바이트가 아니라 수치로 비교
	e2 := New(3)
	e2.Apply(Cmd{Kind: Disconnect, A: SlotBassA, B: SlotFx, C: 0})
	e2.Apply(Cmd{Kind: Connect, A: SlotBassA, B: SlotFx, C: 0, D: 0xFF, V: 1})
	c := render(e2, 200)
	maxd := float32(0)
	for i := range a {
		if d := abs32(a[i] - c[i]); d > maxd {
			maxd = d
		}
	}
	if maxd > 1e-6 {
		t.Fatalf("재연결 뒤 최대 차 %v > 1e-6", maxd)
	}
}

func TestRackStateRoundTrip(t *testing.T) {
	a := New(5)
	a.Apply(Cmd{Kind: RemoveDevice, A: SlotChorus})
	a.Apply(Cmd{Kind: AddDevice, A: 11, B: uint8(KindChorus)})
	a.Apply(Cmd{Kind: Connect, A: SlotDrums, B: 11, C: 3 | 0<<4, D: 0xFF, V: 0.3}) // OH → 코러스, 비결속 0.3
	a.Apply(Cmd{Kind: Connect, A: 11, B: SlotMain, C: 0 | 0<<4, D: uint8(ChoMix)})
	a.Apply(Cmd{Kind: Connect, A: 11, B: SlotMain, C: 1 | 1<<4, D: uint8(ChoMix)})
	a.SetParam(ChoMix, 0.75)
	a.Apply(Cmd{Kind: DeviceParam, A: 11, B: 2, V: 0.3})
	a.Apply(Cmd{Kind: DeviceParam, A: 11, B: 9, V: 0.3})          // k 범위 밖 무동작
	a.Apply(Cmd{Kind: DeviceStep, A: 11, B: 19, C: 200, D: 0xFF}) // step&15=3, note→36, flags 마스킹
	if a.DevParamQ(11, 2) != 1229 || a.DevParamQ(11, 7) != 0 {
		t.Fatalf("DeviceParam 저장 %d %d", a.DevParamQ(11, 2), a.DevParamQ(11, 7))
	}
	if n, f := a.DevStepAt(11, 3); n != MaxNote || f != StepGate|StepSlide|StepAccent {
		t.Fatalf("DeviceStep 정규화 note %d flags %d", n, f)
	}
	var buf [StateSize]byte
	if a.WriteState(buf[:]) != StateSize {
		t.Fatal("WriteState 길이")
	}
	b := New(5) // 상태는 노이즈 시드를 담지 않는다(키프레임 계약) — 같은 seed로 복원
	if !b.ReadState(buf[:]) {
		t.Fatal("ReadState false")
	}
	ra, rb := &a.rack, &b.rack
	if rb.kind != ra.kind || rb.inst != ra.inst || rb.nCables != ra.nCables {
		t.Fatalf("랙 불일치\n%v %v %d\n%v %v %d", ra.kind, ra.inst, ra.nCables, rb.kind, rb.inst, rb.nCables)
	}
	for i := 0; i < ra.nCables; i++ {
		if ra.cables[i] != rb.cables[i] {
			t.Fatalf("케이블 %d 불일치 %+v vs %+v", i, ra.cables[i], rb.cables[i])
		}
	}
	if rb.order != ra.order || rb.live != ra.live {
		t.Fatal("유도 상태(order/live) 불일치")
	}
	if rb.devParQ != ra.devParQ || rb.devPat != ra.devPat {
		t.Fatal("로컬 파라미터·장치 패턴 왕복 불일치")
	}
	x, y := render(a, 100), render(b, 100)
	for i := range x {
		if x[i] != y[i] {
			t.Fatalf("샘플 %d 불일치", i)
		}
	}
	// 오염: 종류 범위 밖·인스턴스 중복(슬롯 12에 Bass inst 0 — 슬롯 0과 중복)·Main 소멸·
	// 순환 케이블·포트 범위 밖 케이블 → 전부 재정규화, Main은 다시 놓인다.
	buf[offRack+12], buf[offInst+12] = byte(KindBass), 0
	buf[offRack+13] = 200
	buf[offRack+SlotMain] = byte(KindNone)
	n := int(buf[offNCables])
	o := offCables + cableBytes*n
	buf[o], buf[o+1], buf[o+2], buf[o+3] = SlotFx, SlotReverb, 0|0<<4, 0xFF // Fx → Reverb (Reverb → Main, Fx → Main 은 있으나 순환은 아님 — 아래 Reverb → Fx가 순환)
	buf[o+4], buf[o+5] = 0xFF, 0x0F
	buf[o+6], buf[o+7], buf[o+8], buf[o+9] = SlotReverb, SlotFx, 0|3<<4, 0xFF
	buf[o+12], buf[o+13], buf[o+14], buf[o+15] = SlotBassA, SlotFx, 5|0<<4, 0xFF // 포트 범위 밖
	buf[offNCables] = byte(n + 3)
	c := New(1)
	if !c.ReadState(buf[:]) {
		t.Fatal("오염 상태 ReadState false — 재정규화 계약")
	}
	rc := &c.rack
	if rc.kind[12] != KindNone || rc.kind[13] != KindNone {
		t.Fatalf("오염 슬롯 통과: %d %d", rc.kind[12], rc.kind[13])
	}
	if !rc.hasKind(KindMain) {
		t.Fatal("Main 없이 복원됨")
	}
	if rc.nOrder == 0 || !rc.recompute() {
		t.Fatal("순환이 남아 있다")
	}
	// Fx → Reverb는 살고(순환 아님, 가득 아님), Reverb → Fx(3)는 순환이라 버려지고, 포트 5는 범위 밖
	// Main이 슬롯 6에 다시 놓였는데 옛 리턴 케이블(dst 6)은 Main이 없던 시점에 읽혀 거부됐을 수 있다 —
	// 계약은 "정규화된 랙이 나온다"이지 케이블 보존이 아니다. 렌더가 유한한지만 본다.
	z := render(c, 50)
	for i, v := range z {
		if v != v || v > 1 || v < -1 {
			t.Fatalf("오염 복원 랙 렌더 샘플 %d = %v", i, v)
		}
	}
}

func TestRackNoAllocs(t *testing.T) {
	e := New(1)
	e.Apply(Cmd{Kind: AddDevice, A: 9, B: uint8(KindChorus)}) // 인스턴스 고갈 → 무동작 경로
	e.Apply(Cmd{Kind: RemoveDevice, A: SlotChorus})
	e.Apply(Cmd{Kind: AddDevice, A: 9, B: uint8(KindChorus)})
	e.Apply(Cmd{Kind: Connect, A: SlotBassB, B: 9, C: 0, D: 0xFF, V: 0.8})
	e.Apply(Cmd{Kind: Connect, A: 9, B: SlotMain, C: 0, D: 0xFF, V: 0.5})
	e.Apply(Cmd{Kind: Connect, A: 9, B: SlotMain, C: 1 | 1<<4, D: 0xFF, V: 0.5})
	buf := make([]float32, 2*Block)
	e.Render(buf)
	if n := testing.AllocsPerRun(500, func() { e.Render(buf) }); n != 0 {
		t.Fatalf("재배선 랙 Render 할당 %v", n)
	}
	cmds := [...]Cmd{
		{Kind: Connect, A: SlotDrums, B: 9, C: 2, D: 0xFF, V: 0.2},
		{Kind: Disconnect, A: SlotDrums, B: 9, C: 2},
		{Kind: RemoveDevice, A: 9},
		{Kind: AddDevice, A: 9, B: uint8(KindChorus)},
		{Kind: Connect, A: SlotFx, B: 9, C: 0, D: 0xFF, V: 1},        // Fx → Chorus → (Main 미연결) 순환 없음
		{Kind: Connect, A: 9, B: SlotFx, C: 0 | 3<<4, D: 0xFF, V: 1}, // 순환 → 거부·되돌리기 경로
	}
	for _, c := range cmds {
		c := c
		if n := testing.AllocsPerRun(200, func() { e.Apply(c) }); n != 0 {
			t.Fatalf("Apply(%d) 할당 %v", c.Kind, n)
		}
	}
	var st [StateSize]byte
	if n := testing.AllocsPerRun(100, func() { e.WriteState(st[:]); e.ReadState(st[:]) }); n != 0 {
		t.Fatalf("상태 왕복 할당 %v", n)
	}
}

func TestRackLevelsAfterRemove(t *testing.T) {
	e := New(1)
	buf := make([]float32, 2*Block)
	for i := 0; i < 40; i++ {
		e.Render(buf)
	}
	if e.Level(BassA) == 0 || e.Level(BD) == 0 {
		t.Fatalf("기본 랙 레벨 A %v BD %v — 0이면 이 테스트의 전제가 깨짐", e.Level(BassA), e.Level(BD))
	}
	e.Apply(Cmd{Kind: RemoveDevice, A: SlotBassA})
	for i := 0; i < 40; i++ {
		e.Render(buf)
	}
	if e.Level(BassA) != 0 {
		t.Fatalf("제거된 파트 레벨 %v, want 0", e.Level(BassA))
	}
	if e.Level(BD) == 0 {
		t.Fatal("드럼 레벨이 사라짐")
	}
}
