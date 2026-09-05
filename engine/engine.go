// 패키지 engine — 장단(Jangdan) 스파이크 스탠드인 DSP.
// 순수 Go, 외부 의존 0, math는 Float32bits/Float32frombits/Abs/Sqrt만 허용.
//
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴 — FMA 융합 차단 계약. 근거와
// mul32의 의미, 곱을 잘못 감싸면 융합되는 형태는 approx.go 파일 주석에 있다.
// 게이트: tools/check-fma.sh (네이티브 arm64 objdump에 FMADD/FMSUB 0개).
// 핫 루프(Render/SetParam) 무할당: 전 할당은 New에서만. 결정론: 같은 seed·
// 같은 파라미터 이력 → 어떤 플랫폼에서든 같은 샘플.
package engine

const SampleRate = 48000
const Block = 128

type ParamID uint8

const (
	Cutoff ParamID = iota // 0..1 → 200Hz..8kHz 지수 매핑
	Resonance             // 0..1
	EnvMod                // 0..1
	Decay                 // 0..1 → 30ms..2s
	Accent                // 0..1
	Tempo                 // 0..1 → 100..160 BPM (기본 130 → 0.5)
	BDLevel               // 0..1
	CHLevel               // 0..1
	NumParams
)

// Engine — 모든 상태. 맵·슬라이스·인터페이스 없음(무할당·결정론 계약).
type Engine struct {
	params [NumParams]float32

	// 시퀀서
	rng            xorshift32 // 패턴 생성(New에서만 소진)
	noise          xorshift32 // 런타임 노이즈(해트)
	note           [16]uint8
	gate           [16]uint8
	accent         [16]uint8
	bdStep         [16]uint8
	chStep         [16]uint8
	stepIdx        int
	stepPos        float64 // 현재 스텝 내 샘플 위치
	samplesPerStep float64

	bass bassVoice
	bd   bdVoice
	ch   chVoice

	// SetParam에서 유도되는 계수(핫 루프에서는 읽기만)
	cutBaseHz float32
	resoK     float32
	envOct    float32
	ampDecay  float32
	fltDecay  float32
	bdLevel   float32
	chLevel   float32
}

// New — seed에서 패턴을 생성하고 기본 파라미터(전부 0.5, Tempo 0.5=130BPM)를 적용.
// 이 함수가 유일한 할당 지점이다.
func New(seed uint32) *Engine {
	e := &Engine{}
	s := seed
	if s == 0 {
		s = 0x9E3779B9
	}
	e.rng = xorshift32{s}
	e.noise = xorshift32{s ^ 0x5BF03635}
	// 베이스 패턴: 노트 12개 중, 게이트 확률 0.75, 액센트 확률 0.25(슬라이드는 이 라운드 생략)
	for i := 0; i < 16; i++ {
		v := e.rng.next()
		e.note[i] = uint8((v >> 8) % 12)
		if v&0xFF < 192 {
			e.gate[i] = 1
		}
		if v&0xFF < 64 {
			e.accent[i] = 1
		}
	}
	// BD: 1·5·9·13 + seed로 1개 추가 / CH: 매 짝수 스텝 + seed로 2개 추가
	for _, st := range [...]int{1, 5, 9, 13} {
		e.bdStep[st] = 1
	}
	e.bdStep[int((e.rng.next()>>4)%16)] = 1
	for i := 0; i < 16; i += 2 {
		e.chStep[i] = 1
	}
	e.chStep[int((e.rng.next()>>4)%16)] = 1
	e.chStep[int((e.rng.next()>>4)%16)] = 1
	// 기본 파라미터 → 계수 유도
	for i := 0; i < int(NumParams); i++ {
		e.SetParam(ParamID(i), 0.5)
	}
	return e
}

// SetParam — 클램프 [0,1], id ≥ NumParams 무시. 계수 재계산도 여기서(핫 루프 밖).
// 유도 계산 역시 결정론 규칙(곱셈-덧셈 명시 변환, 허용 math만)을 따른다.
func (e *Engine) SetParam(id ParamID, v float32) {
	if id >= NumParams {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	e.params[id] = v
	switch id {
	case Cutoff: // 200Hz..8kHz 지수: 200·40^v, log2(40)=5.3219281
		o := mul32(v, 5.3219281)
		e.cutBaseHz = mul32(200, exp2(o))
	case Resonance:
		e.resoK = mul32(v, 3.2)
	case EnvMod: // 필터 엔벨로프 깊이 0..4옥타브
		e.envOct = mul32(v, 4)
	case Decay: // 30ms..2s 지수: 0.03·(66.67)^v, log2=6.0588937
		o := mul32(v, 6.0588937)
		sec := mul32(0.03, exp2(o))
		// 샘플당 감쇠 = exp(−1/(τ·SR)) = 2^(−1/(τ·SR·ln2)). 필터 엔벨로프는 3배 느리게.
		e.ampDecay = exp2(float32(-1.0 / (float64(sec) * 48000.0 * 0.6931471805599453)))
		e.fltDecay = exp2(float32(-1.0 / (float64(sec) * 3.0 * 48000.0 * 0.6931471805599453)))
	case Tempo: // 100..160 BPM, 스텝=16분음표
		b := mul32(60, v)
		bpm := float64(b) + 100
		e.samplesPerStep = 48000.0 * 60.0 / bpm / 4.0
	case BDLevel:
		e.bdLevel = v
	case CHLevel:
		e.chLevel = v
	}
}

// Param — 현재 값. id ≥ NumParams이면 0.
func (e *Engine) Param(id ParamID) float32 {
	if id >= NumParams {
		return 0
	}
	return e.params[id]
}

// Render — len(out) == 2*Block 스테레오 인터리브를 채운다. 길이가 다르면
// 아무것도 하지 않는다(패닉 금지 계약). 힙 할당 0.
func (e *Engine) Render(out []float32) {
	if len(out) != 2*Block {
		return
	}
	for i := 0; i < Block; i++ {
		if e.stepPos >= e.samplesPerStep {
			e.stepPos -= e.samplesPerStep
			e.stepIdx = (e.stepIdx + 1) & 15
			st := e.stepIdx
			if e.gate[st] != 0 {
				af := float32(e.accent[st])
				av := float32(e.params[Accent] * af)
				lv := mul32(av, 0.6)
				e.bass.trigger(e.noteFreq(e.note[st]), 0.4+lv)
			}
			if e.bdStep[st] != 0 {
				e.bd.trigger()
			}
			if e.chStep[st] != 0 {
				e.ch.trigger()
			}
		}
		e.stepPos++
		l, r := e.sample()
		out[2*i] = l
		out[2*i+1] = r
	}
}

// noteFreq — 노트 0..11을 MIDI 36..47(C2..B2)로 보고 440Hz 기준 주파수.
func (e *Engine) noteFreq(n uint8) float32 {
	m := int32(n) + 36
	o := float32(m-69) / 12
	return mul32(exp2(o), 440)
}

// sample — 베이스+BD+CH 합 → ±8 클램프 → 유리식 소프트클립 x(27+x²)/(27+9x²)
// → 0.5 스케일. L=R(모노). |출력| ≤ clip(8)·0.5 ≈ 0.604 < 1.
func (e *Engine) sample() (float32, float32) {
	b := e.bass.process(e)
	d := e.bd.process()
	h := e.ch.process(&e.noise)
	m := b
	t := mul32(d, e.bdLevel)
	m += t
	t = mul32(h, e.chLevel)
	m += t
	if m > 8 {
		m = 8
	}
	if m < -8 {
		m = -8
	}
	x2 := mul32(m, m)
	num := mul32(m, float32(27+x2))
	den := mul32(9, x2) + 27
	y := num / den
	o := mul32(y, 0.5)
	return o, o
}
