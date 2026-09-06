// fx2_test.go — 믹서·리버브·코러스 버스(fx2.go) 계약 ↔ 단언. 원본: P4-fx2 라운드
// (docs/impl-plan-2026-09-05.md §13.1 파라미터 표·§13.2 신호 흐름).
//
// | 계약                              | 단언 | FAIL-first(본 라운드 실측) |
// |---|---|---|
// | 기본값 해시 재기준(§13.1 — 딜레이 센드만 바이트를 바꾼다) | TestFx2DefaultHash: 30초 seed 1 SHA-256 == 상수(H2) | "센드 0인데 리턴을 무조건 더하는" 결함(활성 분기 true)을 심었더니 이 테스트는 통과 — 기본 30초 스트림에 −0 비트가 0개라는 증명(하나라도 있으면 +0로 반전해 해시 변동). 이 결함 계열의 상비 경비는 아래 비트 동일성 테스트다 |
// | 센드 0 버스는 비트 단위 바이패스 | TestFx2BusBypassBitIdentity: dry(±0·상수)·parts(±0·NaN 포함)에 대해 Float32bits 동일 | 같은 결함으로 "바이패스 비트 불일치: dry -0/0 → 0/0" 실패 확인(−0 사례 포함) |
// | 리버브가 울리고 감쇠한다 | TestFx2ReverbTail: RevSend(BD)=1·RevMix=1 — 2382샘플(프리딜레이+최단 콤) 전 수치 차 0, [50ms..300ms] 차 > 0, [200ms..700ms] 꼬리 > 0.01, [2s..2.2s] ≤ 1e-3(−60dB) | setParam의 RevSend 저장을 지우면 "[50ms..300ms] 최대 차 0 ≤ 0 — 리버브가 울지 않음" 실패 확인 |
// | 코러스 리턴·상한 | TestFx2Chorus: ChoSendA=1·ChoDepth=1 — dry와 다른 출력·\|out\| ≤ 0.9903 | ChoSendA 저장을 지우면 "코러스 리턴이 dry와 동일" 실패 확인 |
// | 무할당(센드 켠 상태) | TestFx2NoAllocs: Render·SetParam(bus 전 경로) AllocsPerRun==0 | 활성 분기에 탈출하는 make(패키지 변수 대입)를 넣으면 "Render(센드 켠 상태) 할당: 128" 실패 확인 — 폐기만 하는 make는 스택 배정돼 안 잡힌다(탈출이어야 힙) |
// | 상태 v3 왕복·v2 거부 | TestFx2StateV3: Write→Read 파라미터 59개 ParamQ 동일, 'J','2'·짧은 입력 false | ReadState 파라미터 루프를 BassALevel(33) 미만으로 자르면 "Param 33 불일치 1229 vs 4095" 실패 확인 |
//
// 벤치마크: BenchmarkRender(기본값)·BenchmarkRenderSends(센드 켠 상태) — §13.2
// 비용 게이트(Phase 2 대비 +60% 이내)의 측정축.
//
// 테스트 파일은 math·crypto 패키지 자유(fx_test.go 공통 계약).
package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

// p4fx2HashV1 — 기본 파라미터 30초 seed 1 해시(재기준 상수).
// 이력: cde82a5b…c4d12(Phase 2) → 58d944c2…d27b3(P4-fx2, 딜레이 센드 A 1.0·B 0.6만 바이트 변경 —
// 리버브·코러스·레벨은 센드 0·레벨 1.0에서 바이트에 닿지 않음을 중간 상태 해시로 실측) →
// cacd0efe…abc2a(2026-09-06 사용자 "웻한 트랜스" 기본값: B 리드 옥타브·컷오프·디케이·액센트·레벨 0.85,
// 리버브 센드 7파트·RevSize/Damp/Mix, 코러스 B, 딜레이 센드 A 0.35·B 1.0, Delay 0.4. FAIL-first: 구 상수로
// 실패 확인 후 재기준 — 이 커밋에서 바이트에 걸리는 변경은 DefaultParams 하나).
const p4fx2HashV1 = "cacd0efe01da05cba22e6457900ed70fb74a83bac3d4e61c02c91370f39abc2a"

// 1. 기본값 해시 불변(재기준) — §13.1: 이 해시가 바뀌면 기본값에서 바이트에 닿는
//    변경이 섞인 것이다(딜레이 센드 외的一切). native 렌더(cmd/render)·워클릿 wasm
//    (hash-node)·브라우저(hash-browser) 셋 다 이 값이어야 한다(계약).
func TestFx2DefaultHash(t *testing.T) {
	s := renderSeconds(t, 1, 30)
	b := make([]byte, 4*len(s))
	for i, v := range s {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(v))
	}
	got := sha256.Sum256(b)
	if s := hex.EncodeToString(got[:]); s != p4fx2HashV1 {
		t.Fatalf("기본값 30초 seed 1 해시 %s, want %s (딜레이 센드 외에 바이트를 바꾼 것이 있음)", s, p4fx2HashV1)
	}
}

// 2. 센드 0 리버브·코러스는 비트 단위 바이패스 — dry를 그대로(±0 부호 포함) 돌려준다.
//    "0을 더한다"가 아니라 "더하지 않는다"가 계약(§13.1 해시 근거). 랙(rack.go)에서는
//    게인>0 입력 케이블이 없는 장치가 live=false가 되고 그 출력 케이블은 Main 합산에서
//    건너뛰어진다 — 포트 값을 직접 심어 sumInputs로 단언한다.
func TestFx2BusBypassBitIdentity(t *testing.T) {
	e := New(1)
	for p := Part(0); p < NumParts; p++ {
		e.SetParam(RevSend(p), 0)
	}
	e.SetParam(ChoSendA, 0)
	e.SetParam(ChoSendB, 0)
	r := &e.rack
	if r.live[SlotReverb] || r.live[SlotChorus] {
		t.Fatal("센드 전부 0인데 리버브/코러스가 live")
	}
	negz := float32(math.Copysign(0, -1))
	r.port[SlotReverb] = [MaxOutPorts]float32{0.5, 0.5}    // 죽은 장치의 잔존 포트 값 — 닿으면 안 된다
	r.port[SlotChorus] = [MaxOutPorts]float32{negz, 0.25}
	for _, dry := range [...]float32{negz, 0, 0.4922174, -0.7} {
		r.port[SlotFx][0], r.port[SlotFx][1] = dry, -dry
		var in [MaxInPorts]float32
		r.sumInputs(SlotMain, &in)
		if math.Float32bits(in[0]) != math.Float32bits(dry) || math.Float32bits(in[1]) != math.Float32bits(-dry) {
			t.Fatalf("바이패스 비트 불일치: dry %v/%v → %v/%v (센드 0인데 리턴이 닿음)", dry, -dry, in[0], in[1])
		}
	}
	// 센드를 켜고 다시 0으로 — live 플래그 갱신(recompute)이 올바르게 꺼지는지.
	e.SetParam(RevSend(BD), 1)
	e.SetParam(ChoSendA, 1)
	if !r.live[SlotReverb] || !r.live[SlotChorus] {
		t.Fatal("센드 켠 뒤 live 미설정")
	}
	e.SetParam(RevSend(BD), 0)
	e.SetParam(ChoSendA, 0)
	if r.live[SlotReverb] || r.live[SlotChorus] {
		t.Fatal("센드 0으로 돌아갔는데 live 잔존")
	}
	// NaN 입력 방어: 파트 포트가 NaN이면 리버브 입력은 0.
	e.SetParam(RevSend(CY), 1)
	r.port[SlotDrums][2+int(CY-BD)] = float32(math.NaN())
	var in [MaxInPorts]float32
	r.sumInputs(SlotReverb, &in)
	if in[0] != 0 {
		t.Fatalf("NaN 파트 → 리버브 입력 %v, want 0", in[0])
	}
}

// 3. 리버브 꼬리 — RevSend(BD)=1·RevMix=1에서 BD 단발 트리거(첫 블록 뒤 스텝 0
//    게이트를 지워 다음 바 2.0s 재트리거를 막는다) 뒤: (a) 리버브 최단 경로
//    (프리딜레이 960 + 최단 콤 1422)가 열리기 전인 처음 2382샘플은 대조 엔진(센드 0)과
//    수치 차 0(dry 보존 — busClamp가 dry를 깎지 않는 단언), (b) [50ms..300ms]에서
//    차 > 0(리버브가 운다), (c) [200ms..700ms] 꼬리 > 0.01, (d) [2s..2.2s] ≤ 1e-3
//    (−60dB — 실측 0.00067, 이 중 리버브 기여 0.00015).
func TestFx2ReverbTail(t *testing.T) {
	run := func(revOn bool) []float32 {
		e := New(1)
		silenceAll(e)
		e.drumPat[0][0] = StepGate // BD 스텝 0(첫 렌더 블록에서 트리거)
		if revOn {
			e.SetParam(RevSend(BD), 1)
			e.SetParam(RevMix, 1)
		}
		buf := make([]float32, 2*Block)
		blocks := SampleRate * 22 / 10 / Block // 2.2 s = 825 블록
		total := make([]float32, 0, 2*Block*blocks)
		e.Render(buf) // 스텝 0 발동(BD 단발 트리거)
		total = append(total, buf...)
		e.drumPat[0][0] = 0 // 다음 바(2.0s) 재트리거 차단 — 꼬리만 잰다
		for i := 1; i < blocks; i++ {
			e.Render(buf)
			total = append(total, buf...)
		}
		return total
	}
	a := run(true)
	b := run(false)
	if len(a) != len(b) {
		t.Fatalf("길이 불일치 %d vs %d", len(a), len(b))
	}
	const open = 960 + 1422 // 프리딜레이 + 최단 콤(L) — 이 전엔 리버브 리턴이 정확히 0
	pre := float32(0)
	for i := 0; i < 2*open && i < len(a); i++ {
		if d := abs32(a[i] - b[i]); d > pre {
			pre = d
		}
	}
	if pre != 0 {
		t.Fatalf("최단 경로 열리기 전 차 %v > 0 — 리버브가 프리딜레이를 우회함", pre)
	}
	mid := float32(0)
	for i := 2 * (SampleRate*50/1000); i < 2*(SampleRate*300/1000); i++ {
		if d := abs32(a[i] - b[i]); d > mid {
			mid = d
		}
	}
	if mid <= 0 {
		t.Fatalf("[50ms..300ms] 최대 차 %v ≤ 0 — 리버브가 울지 않음", mid)
	}
	tail := float32(0)
	for i := 2 * (SampleRate * 200 / 1000); i < 2*(SampleRate*700/1000); i++ {
		if v := abs32(a[i]); v > tail {
			tail = v
		}
	}
	if tail <= 0.01 {
		t.Fatalf("[200ms..700ms] 꼬리 %v ≤ 0.01 — 사실상 무음", tail)
	}
	late := float32(0)
	for i := 2 * SampleRate * 2; i < len(a); i++ {
		if v := abs32(a[i]); v > late {
			late = v
		}
	}
	if late > 1e-3 {
		t.Fatalf("[2s..2.2s] %v > 1e-3(−60dB) — 감쇠 안 함", late)
	}
	t.Logf("꼬리: mid diff %v, [200..700ms] %v, [2s..] %v", mid, tail, late)
}

// 4. 코러스 — ChoSendA=1·ChoDepth=1에서 출력이 dry와 다르고 |out| ≤ 0.9903
//    (리턴이 클립 뒤에 더해져도 마지막 busClip이 상한을 지킨다, §13.2).
func TestFx2Chorus(t *testing.T) {
	// 코러스 장치 + Main 합산(케이블 게인 = ChoMix 1 → boundGain 0.8) + busClamp — 랙이 하는
	// 연산을 그대로 펼친 형태(rack.go sumInputs 규칙: dry 대입 뒤 리턴 덧셈).
	var c chorus
	c.setRate(0.5)
	c.setDepth(1)
	mix := boundGain(ChoMix, 1)
	diff, peak := false, float32(0)
	for i := 0; i < 48000; i++ {
		in := float32(0.7)
		if i%2 == 1 { // 변조가 있는 입력 — 상수면 지연 읽기가 dry와 같아진다
			in = -0.7
		}
		dry := float32(0.9)
		if i%3 == 0 {
			dry = -0.9
		}
		cl, cr := c.process(mul32(in, 1))
		l, r := busClamp(dry+mul32(cl, mix)), busClamp(dry+mul32(cr, mix))
		if l != dry || r != dry {
			diff = true
		}
		if a := abs32(l); a > peak {
			peak = a
		}
		if a := abs32(r); a > peak {
			peak = a
		}
		if l > 0.9903 || l < -0.9903 || r > 0.9903 || r < -0.9903 {
			t.Fatalf("상한 위반 |%v,%v| > 0.9903 (i=%d)", l, r, i)
		}
	}
	if !diff {
		t.Fatal("코러스 리턴이 dry와 동일 — 딜레이 변조가 출력에 안 닿음")
	}
	t.Logf("코러스 peak %v", peak)
}

// 5. 무할당 — 센드 켠 상태(리버브+코러스+딜레이 센드 전부 활성)의 Render와 버스
//    SetParam 전 경과(계수 유도 exp2 포함)가 0할당.
func TestFx2NoAllocs(t *testing.T) {
	e := New(1)
	e.SetParam(RevSend(BD), 0.8)
	e.SetParam(ChoSendA, 0.7)
	e.SetParam(ChoSendB, 0.5)
	e.SetParam(DelaySend(CY), 0.4)
	buf := make([]float32, 2*Block)
	e.Render(buf) // 워밍업
	if n := testing.AllocsPerRun(1000, func() { e.Render(buf) }); n != 0 {
		t.Fatalf("Render(센드 켠 상태) 할당: %v", n)
	}
	for _, id := range [...]ParamID{BassALevel, RevSend(CH), RevSize, RevDamp, RevMix, ChoSendB, ChoRate, ChoDepth, ChoMix, DelaySend(OH)} {
		p := e.Param(id)
		if n := testing.AllocsPerRun(1000, func() { e.SetParam(id, 1-p) }); n != 0 {
			t.Fatalf("SetParam(%d) 할당: %v", id, n)
		}
	}
}

// 6. 상태 — Write→Read로 파라미터 59개 전부 ParamQ 동일, 구버전 매직·짧은 입력 거부.
func TestFx2StateV3(t *testing.T) {
	a := New(5)
	a.SetParam(BassALevel, 0.3)
	a.SetParam(BassBLevel, 0.9)
	a.SetParam(RevSend(BD), 0.7)
	a.SetParam(RevSize, 0.1)
	a.SetParam(RevDamp, 0.9)
	a.SetParam(RevMix, 0.25)
	a.SetParam(ChoSendA, 0.6)
	a.SetParam(ChoRate, 0.05)
	a.SetParam(ChoDepth, 1)
	a.SetParam(ChoMix, 0.85)
	a.SetParam(DelaySend(BassB), 0.55)
	a.SetParam(DelaySend(CP), 0.15)
	var buf [StateSize]byte
	if n := a.WriteState(buf[:]); n != StateSize {
		t.Fatalf("WriteState %d, want %d", n, StateSize)
	}
	if buf[0] != 'J' || buf[1] != '5' {
		t.Fatalf("매직 %q%q, want J5", buf[0], buf[1])
	}
	f := New(9)
	if !f.ReadState(buf[:]) {
		t.Fatal("ReadState false")
	}
	for i := 0; i < int(NumParams); i++ {
		if a.ParamQ(ParamID(i)) != f.ParamQ(ParamID(i)) {
			t.Fatalf("Param %d 불일치 %d vs %d", i, a.ParamQ(ParamID(i)), f.ParamQ(ParamID(i)))
		}
	}
	// v4·v3·v2는 거부 — 장치 패턴·랙 표·새 파라미터 해석이 없다(state.go 주석).
	for _, old := range [...][2]int{{'4', 1424}, {'3', 748}, {'2', 696}} {
		v := make([]byte, StateSize) // 길이를 채워도 매직으로 거부
		v[0], v[1] = 'J', byte(old[0])
		if f.ReadState(v) {
			t.Fatalf("v%c 매직을 받아들임 — 거부 계약", old[0])
		}
	}
	if f.ReadState(buf[:StateSize-1]) {
		t.Fatal("짧은 입력을 받아들임")
	}
}

// BenchmarkRender — 기본 파라미터 ns/블록(비용 게이트의 기준축. §13.2: 센드 켠
// 상태가 Phase 2(이 파일 도입 전) 대비 +60% 이내).
func BenchmarkRender(b *testing.B) {
	e := New(1)
	buf := make([]float32, 2*Block)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e.Render(buf)
	}
}

// BenchmarkRenderSends — 센드 켠 상태(리버브+코러스+딜레이 센드 활성) ns/블록.
func BenchmarkRenderSends(b *testing.B) {
	e := New(1)
	e.SetParam(RevSend(BD), 0.8)
	e.SetParam(ChoSendA, 0.7)
	e.SetParam(ChoSendB, 0.5)
	e.SetParam(DelaySend(CY), 0.4)
	buf := make([]float32, 2*Block)
	e.Render(buf)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Render(buf)
	}
}
