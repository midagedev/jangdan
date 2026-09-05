// url.go — 로그 ↔ 공유 URL 직렬화. 계약 원본 docs/impl-plan-2026-09-05.md §5.
//
// 형식: "v1." + base64url(패딩 없음). 페이로드 = 헤더(버전 u8, seed varint, 단어 길이
// varint + UTF-8 바이트) + 엔트리 스트림(스텝 델타 varint + Kind u8 + Kind별 필드).
// Author는 URL에 없다(디코드 시 전부 ReplayAuthor), 키프레임도 없다(URL은 처음부터 재생).
//
// 스텝 양자화: 각 엔트리의 블록을 스텝 인덱스로 바꾼다 — 리플레이가 스텝 경계에서
// 적용하기 위해서다. 스텝 인덱스는 저장하지 않고 블록·템포 이력에서 유도한다
// (derive-don't-store): 기본 템포는 엔진 기본값(130 BPM)을 따르고, Tempo SetParam을
// 만나면 이후 스텝 간격을 engine.BPMOf(양자화값) 공식으로 갱신한다(엔진과 같은 식).
// 누적은 float64.
//
// 외부 입력(URL)은 읽으며 재정규화한다: 알 수 없는 Kind는 건너뛰고(필드 없음으로
// 간주 — 프레이밍 유지), 잘린 스트림은 읽은 것까지 유효 + error, 버전 불일치는 거부,
// u16 값은 4095 클램프, A/B 범위는 엔진 Apply가 정규하므로 그대로 둔다.
package session

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/midagedev/revirth/engine"
)

const (
	urlPrefix  = "v1."
	urlVersion = 1
	// urlMaxStep — 디코드 방어 상한: 약 1600만 스텝(160 BPM 기준 약 6일).
	// 악의적 거대 varint 델타로 시계 전진 루프를 돌리는 것을 끊는다.
	urlMaxStep = 1 << 24
)

// EncodeURL — 로그를 공유 URL로. 같은 스텝 안에서 같은 파라미터의 SetParam 중간값은
// 마지막 것만 남는다(스텝 양자화의 자연 결과 — 스텝 경계 적용에서는 마지막 값만 들린다).
func EncodeURL(l *Log) string {
	return encodePayload(l, 0)
}

// EncodeURLBudget — maxChars 이하가 되는 첫 감량 단계로 인코딩해 (URL, 단계)를 반환.
// 단계 level의 감량 그룹은 파라미터별 1<<level 스텝: 0=스텝 내 감량만(1), 1=2스텝당,
// 2=4스텝당, 3=8스텝당, 이후 필요하면 16, 32, ... 4096스텝당(8분 분량 — 사실상
// 파라미터별 최종값)까지 두 배로 올린다. 스펙이 열거한 2·4·8만으로는 5분 합성
// 로그(노브 200회×중간값 20)의 2000자 예산을 채울 수 없다(실측: 8스텝당 2293자,
// 제스처당 1개로 줄여도 약 2194자) — 제스처 사이 간격(약 13스텝)이 8보다 커서
// 파라미터별 감량이 서로 다른 제스처를 못 합치기 때문이다. 그래서 사다리를
// 연장하고, 단계 수를 그대로 반환한다(감량이 예산을 넘으면 최대 단계 결과 반환).
func EncodeURLBudget(l *Log, maxChars int) (string, int) {
	for level := 0; level < maxBudgetLevel; level++ {
		s := encodePayload(l, level)
		if len(s) <= maxChars {
			return s, level
		}
	}
	return encodePayload(l, maxBudgetLevel), maxBudgetLevel
}

// maxBudgetLevel — 감량 상한(4096스텝 = 8분 세션에서 파라미터별 값 1개).
const maxBudgetLevel = 12

// encodePayload — 감량 단계 level(0..maxBudgetLevel, 스텝 그룹 크기 1<<level)로 인코딩.
func encodePayload(l *Log, level int) string {
	if l == nil {
		l = &Log{}
	}
	payload := make([]byte, 0, 16+6*len(l.Entries))
	payload = append(payload, urlVersion)
	payload = binary.AppendUvarint(payload, uint64(l.Seed))
	payload = binary.AppendUvarint(payload, uint64(len(l.Word)))
	payload = append(payload, l.Word...)

	// 1패스: 스텝 인덱스 계산 + SetParam 감량 표시. 시계는 전 엔트리에 대해 전진한다
	// (감량된 Tempo 엔트리도 지나가지만 setTempoQ는 "마지막 값 승리"라 결과가 같다).
	clk := newStepClock()
	steps := make([]uint64, len(l.Entries))
	superseded := make([]bool, len(l.Entries))
	type pgroup struct {
		param uint8
		group uint64
	}
	keeper := make(map[pgroup]int, len(l.Entries))
	for i := range l.Entries {
		e := &l.Entries[i]
		steps[i] = clk.stepOf(e.Block)
		if e.Cmd.Kind == engine.SetParam {
			g := steps[i] >> uint(level) // 감량 그룹: level 0=스텝, 1=2스텝, 2=4스텝, 3=8스텝
			k := pgroup{e.Cmd.A, g}
			if j, ok := keeper[k]; ok {
				superseded[j] = true
			}
			keeper[k] = i
		}
		if e.Cmd.Kind == engine.SetParam && e.Cmd.A == uint8(engine.Tempo) {
			clk.setTempoQ(quantizeV(e.Cmd.V)) // 시계도 양자화된 값으로(디코드와 동일 경로)
		}
	}

	// 2패스: 스트림 순서 그대로 출력(감량 표시만 건너뛴다 — 순서는 항상 보존).
	prev := uint64(0)
	first := true
	for i := range l.Entries {
		if superseded[i] {
			continue
		}
		e := &l.Entries[i]
		if e.Cmd.Kind >= engine.NumCmdKinds {
			continue // 계약 밖 Kind는 직렬화하지 않는다
		}
		if first {
			payload = binary.AppendUvarint(payload, steps[i]) // 첫 델타는 절대 스텝 인덱스
			first = false
		} else {
			payload = binary.AppendUvarint(payload, steps[i]-prev)
		}
		prev = steps[i]
		payload = appendCmd(payload, &e.Cmd)
	}
	return urlPrefix + base64.RawURLEncoding.EncodeToString(payload)
}

// appendCmd — Kind별 필드 인코딩. SetParam 값은 u16 양자화(uint16(v*4095+0.5)).
func appendCmd(b []byte, c *engine.Cmd) []byte {
	b = append(b, byte(c.Kind))
	switch c.Kind {
	case engine.SetParam:
		n := quantizeV(c.V)
		b = append(b, c.A, byte(n), byte(n>>8))
	case engine.BassStep:
		b = append(b, c.A, c.B, c.C, c.D)
	case engine.DrumStep:
		b = append(b, c.A, c.B, c.D)
	case engine.SelectPattern:
		b = append(b, c.A, c.B)
	case engine.Mute:
		b = append(b, c.A, c.B)
	case engine.Trigger:
		b = append(b, c.A)
	case engine.Drop, engine.ResetPos:
	}
	return b
}

// quantizeV — 0..1 클램프 후 4095단계 양자화(계약 공식). float64로 계산해 곱·덧셈
// 융합 여부와 무관하게 플랫폼 간 같은 비트가 나오게 한다. 엔진 내부 quantize(float32
// 경로)와 극값 경계에서 최대 1단계(1/4095) 차이가 있을 수 있다.
func quantizeV(v float32) uint16 {
	if v != v || v < 0 { // NaN 방어 포함
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return uint16(float64(v)*float64(engine.ParamSteps) + 0.5)
}

// stepClock — 블록 ↔ 스텝 환산 시계. 스텝 인덱스를 저장하지 않고 블록·템포 이력에서
// 유도한다. 경계는 float64 블록 위치로 누적(스텝당 최소 약 35블록이라 floor는 충돌하지
// 않는다: 100..160 BPM → 스텝당 35.2..56.3블록).
type stepClock struct {
	step      uint64  // 현재 스텝 인덱스
	start     float64 // step이 시작된 블록 위치(소수 포함)
	spsBlocks float64 // 스텝당 블록 수(현재 템포)
}

func newStepClock() stepClock {
	return stepClock{spsBlocks: blocksPerStep(engine.DefaultParams()[engine.Tempo])}
}

// blocksPerStep — 엔진과 같은 공식: SampleRate*60/BPM/4(샘플)을 블록(128프레임)으로.
func blocksPerStep(tempoQ float32) float64 {
	return engine.SampleRate * 60.0 / engine.BPMOf(tempoQ) / 4.0 / engine.Block
}

// setTempoQ — 양자화 정수 템포. 다음 스텝 경계부터 반영된다.
func (c *stepClock) setTempoQ(n uint16) {
	c.spsBlocks = blocksPerStep(float32(n) / engine.ParamSteps)
}

// stepOf — 블록이 속한 스텝(그 블록 직전에 시작된 스텝). 시계를 전진시키며 경계를
// 누적한다. 과거 블록(비단조 로그 방어)이면 현재 스텝으로 클램프해 출력이 항상
// 비감소가 되게 한다.
func (c *stepClock) stepOf(block uint32) uint64 {
	b := float64(block)
	for c.start+c.spsBlocks <= b {
		c.start += c.spsBlocks
		c.step++
	}
	return c.step
}

// blockOf — 스텝의 시작 블록 위치(소수). DecodeURL이 디코드된 엔트리의 Block을 여기서
// 유도한다(올림해서 사용 — floor는 경계 직전 블록이 되어 재인코딩 stepOf가 한 스텝
// 앞으로 되돌아가 고정점을 깬다. 올림이면 stepOf(ceil(start)) == step이 항상 성립:
// 스텝당 간격이 1블록보다 크기 때문). 같은 템포 이력에서 stepOf의 역수 관계다.
func (c *stepClock) blockOf(step uint64) float64 {
	for c.step < step {
		c.start += c.spsBlocks
		c.step++
	}
	return c.start
}

// DecodeURL — 공유 URL을 로그로. 헤더 실패(접두사·base64·버전·varint 잘림)는 (nil, err).
// 엔트리 스트림이 잘렸으면 읽은 것까지 채운 로그와 error를 함께 돌려준다(부분 결과).
func DecodeURL(s string) (*Log, error) {
	if !strings.HasPrefix(s, urlPrefix) {
		return nil, fmt.Errorf("session: URL 접두사 불일치(기대 %q)", urlPrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[len(urlPrefix):])
	if err != nil {
		return nil, fmt.Errorf("session: base64 디코드 실패: %w", err)
	}
	if len(raw) < 1 {
		return nil, errors.New("session: 빈 페이로드")
	}
	if raw[0] != urlVersion {
		return nil, fmt.Errorf("session: 지원하지 않는 버전 %d(현재 %d)", raw[0], urlVersion)
	}
	pos := 1
	seed, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("session: 헤더 seed varint 잘림")
	}
	pos += n
	if seed > math.MaxUint32 {
		return nil, errors.New("session: seed 범위 초과")
	}
	wl, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("session: 헤더 단어 길이 varint 잘림")
	}
	pos += n
	if uint64(len(raw)-pos) < wl {
		return nil, errors.New("session: 헤더 단어 잘림")
	}
	word := string(raw[pos : pos+int(wl)])
	pos += int(wl)

	l := &Log{Seed: uint32(seed), Word: word}
	clk := newStepClock()
	step := uint64(0)
	for pos < len(raw) {
		delta, n := binary.Uvarint(raw[pos:])
		if n <= 0 {
			return l, errors.New("session: 엔트리 step 델타 잘림 — 읽은 것까지 유효")
		}
		pos += n
		if delta > urlMaxStep || step+delta > urlMaxStep {
			return l, errors.New("session: step 델타 비정상(손상 또는 악의적)")
		}
		step += delta
		if pos >= len(raw) {
			return l, errors.New("session: 엔트리 Kind 잘림 — 읽은 것까지 유효")
		}
		kind := raw[pos]
		pos++
		c, qv, npos, ok := readCmd(raw, pos, kind)
		if npos < 0 {
			return l, errors.New("session: 엔트리 필드 잘림 — 읽은 것까지 유효")
		}
		pos = npos
		if ok { // 알 수 없는 Kind는 ok=false — 건너뛰고 프레이밍은 유지한다
			block := clampBlock(math.Ceil(clk.blockOf(step))) // 블록 먼저(인코딩과 같은 순서)
			if c.Kind == engine.SetParam && c.A == uint8(engine.Tempo) {
				clk.setTempoQ(qv) // 템포는 이 엔트리의 블록 계산 뒤(다음 경계부터)
			}
			l.Entries = append(l.Entries, Entry{Block: block, Author: ReplayAuthor, Cmd: c})
		}
	}
	return l, nil
}

// readCmd — Kind별 필드 파싱. 잘렸으면 npos=-1, 알 수 없는 Kind면 ok=false(바이트 소비 없음).
// SetParam이면 qv에 양자화 정수(0..4095)를 돌려준다(float 역산이 아니라 원본 u16).
func readCmd(raw []byte, pos int, kind byte) (c engine.Cmd, qv uint16, npos int, ok bool) {
	c.Kind = engine.CmdKind(kind)
	switch c.Kind {
	case engine.SetParam:
		if pos+3 > len(raw) {
			return c, 0, -1, false
		}
		c.A = raw[pos]
		q := uint16(raw[pos+1]) | uint16(raw[pos+2])<<8
		if q > engine.ParamSteps {
			q = engine.ParamSteps // 재정규화(12비트 값 도메인)
		}
		qv = q
		c.V = float32(q) / engine.ParamSteps
		return c, qv, pos + 3, true
	case engine.BassStep:
		if pos+4 > len(raw) {
			return c, 0, -1, false
		}
		c.A, c.B, c.C, c.D = raw[pos], raw[pos+1], raw[pos+2], raw[pos+3]
		return c, 0, pos + 4, true
	case engine.DrumStep:
		if pos+3 > len(raw) {
			return c, 0, -1, false
		}
		c.A, c.B, c.D = raw[pos], raw[pos+1], raw[pos+2]
		return c, 0, pos + 3, true
	case engine.SelectPattern, engine.Mute:
		if pos+2 > len(raw) {
			return c, 0, -1, false
		}
		c.A, c.B = raw[pos], raw[pos+1]
		return c, 0, pos + 2, true
	case engine.Trigger:
		if pos+1 > len(raw) {
			return c, 0, -1, false
		}
		c.A = raw[pos]
		return c, 0, pos + 1, true
	case engine.Drop, engine.ResetPos:
		return c, 0, pos, true
	default:
		// 알 수 없는 Kind: 필드 길이를 알 수 없으므로 0바이트로 간주해 건너뛴다.
		// 이 규칙 때문에 미래 버전이 필드 있는 새 Kind를 추가하려면 길이 바이트가
		// 필요하다(현재 계약에는 없는 Kind다).
		return c, 0, pos, false
	}
}

// clampBlock — float64 블록 위치 → uint32(범위 밖 변환이 구현 의존이 되지 않게 클램프).
func clampBlock(f float64) uint32 {
	if f >= float64(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(f)
}
