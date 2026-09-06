// samplerpack.go — 내장 샘플 팩(§14.2 항목 2 "① 내장 팩 먼저").
//
// **팩 표(packTab)는 리드가 못박은 계약**이고, 이 파일의 bakePack은 그 표가 선언한 자리에
// 파형을 써 넣기만 한다. 표를 바꾸면 재생 코드(sampler.go)·UI·해시가 함께 움직이므로
// 슬롯 수·길이·기준음·루프 지점은 여기 리터럴이 정본이다.
//
// **왜 파일이 아니라 합성인가**: 엔진은 순수 Go·외부 의존 0·fmt/os 없음이고 워클릿은
// 파일을 읽지 못한다. 팩을 바이트로 넣으려면 wasm에 3MB를 실어야 하는데 현재 워클릿은
// 49KB다. 그래서 내장 팩은 **결정론적으로 합성해 굽는다**(자체 제작 = CC0). 샘플러의
// 본체는 재생 경로(피치 리샘플·엔벌로프·루프)이고, 팩은 "바이트가 어떻게 버퍼에 들어왔나"일
// 뿐이다. 사용자 업로드 경로(§14.2 ②)는 같은 버퍼에 호스트가 써 넣는 후속 라운드다.
//
// **어디에 사는가**: 팩은 Engine 바깥의 패키지 전역이다. Reset이 `*e = Engine{}`로 구조체를
// 통째로 비우므로 Engine 안에 두면 Reset마다 1.2MB memset + 재굽기가 된다. 팩은 seed와
// 무관한 불변 데이터라(같은 팩이면 같은 바이트) 엔진 인스턴스가 여럿이어도 하나면 된다.
// 굽기는 최초 samplerDev.init에서 1회(ensurePack 가드) — 패키지 init()에 기대지 않는다
// (wasm-unknown 타깃의 _initialize 호출 여부에 계약을 걸지 않기 위해서).
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약(approx.go 주석).
// 엔트로피는 xorshift32만(math/rand 금지). 할당 없음.
package engine

const (
	packSlots  = 8      // 팩 슬롯 수(SmpSelect 0..7)
	packFrames = 290400 // 전 슬롯 길이 합 = 6.05초 @48k. packTab의 마지막 off+n과 정확히 같다
)

// packEntry — 팩 슬롯 하나. off/n은 packBuf 안의 프레임 구간, root는 이 파형이 녹음된 음
// (C1 기준 반음 — voice.go·harmony.go와 같은 도메인), loop는 슬롯 상대 루프 시작 프레임이다.
// loop == n 이면 원샷(끝나면 소리가 멈춘다), loop < n 이면 게이트가 살아 있는 동안 [loop, n)을 돈다.
type packEntry struct {
	off, n uint32
	root   uint8
	loop   uint32
}

// packTab — 리드 계약. 이 리터럴이 팩의 정본이다(길이·기준음·루프).
// off는 앞 슬롯들의 n 누적과 정확히 같아야 한다(TestPackTable이 단언).
var packTab = [packSlots]packEntry{
	{off: 0, n: 48000, root: 24, loop: 48000},      // 0 PLUCK  1.00s C3 뜯은 현(카플러스-스트롱)
	{off: 48000, n: 72000, root: 36, loop: 72000},  // 1 BELL   1.50s C4 비조화 부분음 금속 벨
	{off: 120000, n: 48000, root: 12, loop: 48000}, // 2 VOX    1.00s C2 포먼트 "아"(노동요 목소리)
	{off: 168000, n: 9600, root: 24, loop: 9600},   // 3 WOOD    0.20s C3 우드블록 타격
	{off: 177600, n: 12000, root: 24, loop: 12000}, // 4 SHAKE   0.25s C3 셰이커(노이즈 버스트)
	{off: 189600, n: 38400, root: 24, loop: 0},     // 5 TAPE    0.80s C3 테이프 히스·워블 — 전 구간 루프
	{off: 228000, n: 33600, root: 24, loop: 33600}, // 6 BREATH  0.70s C3 숨 스웰(필터 노이즈)
	{off: 261600, n: 28800, root: 12, loop: 28800}, // 7 SUB     0.60s C2 서브 붐(사인 스윕)
}

// PackNames — UI 라벨의 단일 소유자(앱이 engine.PackNames를 읽는다). 핫 루프에 없다.
var PackNames = [packSlots]string{"PLUCK", "BELL", "VOX", "WOOD", "SHAKE", "TAPE", "BREATH", "SUB"}

// packBuf — 구운 파형(모노 48k). 전역·불변(굽기 이후 쓰기 없음).
var packBuf [packFrames]float32

var packReady bool

// ensurePack — 최초 1회 굽는다. samplerDev.init이 부른다(Reset마다 재굽기 금지).
func ensurePack() {
	if packReady {
		return
	}
	bakePack(&packBuf)
	packReady = true
}

// bakePack — packTab이 선언한 각 구간에 파형을 쓴다. **[임시 자리표 — P5-sampler-pack 라운드가 교체]**
// 지금은 슬롯마다 기준음의 감쇠 사인 하나(재생 경로를 시험할 수 있는 최소 파형).
func bakePack(buf *[packFrames]float32) {
	for i := 0; i < packSlots; i++ {
		e := packTab[i]
		hz := midiHz(e.root)
		inc := hz / SampleRate
		dec := exp2(mul32(-1.0/(0.35*SampleRate), 1.4426950)) // τ 0.35s
		ph, amp := float32(0), float32(0.9)
		for j := uint32(0); j < e.n; j++ {
			x := mul32(ph, twoPiF) - piF
			buf[e.off+j] = mul32(sin5(x), amp)
			ph += inc
			if ph >= 1 {
				ph -= 1
			}
			amp = mul32(amp, dec)
		}
	}
}

// midiHz — C1 기준 반음(0..MaxSemis) → Hz. voice.go baseInc와 같은 기준(note 0 = MIDI 24 = C1).
func midiHz(note uint8) float32 {
	if note > MaxSemis {
		note = MaxSemis
	}
	// 440 · 2^((24+note-69)/12)
	return mul32(440, exp2(mul32(float32(int(note)-45), 1.0/12.0)))
}
