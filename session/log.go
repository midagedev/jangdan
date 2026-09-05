// log.go — 이벤트 로그·저자·키프레임. 계약 원본 docs/impl-plan-2026-09-05.md §1·§5.
//
// "로그가 정본": 사람 손이든 레지던트든 엔진에 닿는 모든 변경은 (블록, 저자, Cmd)로
// 기록되고, 리플레이·URL 열기·서버 렌더는 전부 "새 엔진 + 같은 로그"로 재생된다.
// 이 패키지는 메인 스레드(표준 Go wasm)에서 돌므로 표준 라이브러리가 자유롭다 —
// engine/의 TinyGo 제약(math 금지·무할당)은 여기 적용되지 않는다.
//
// 방어 계약(오용은 무시하지 않고 정규화한다):
//   - Append에 과거 블록이 오면 마지막 블록으로 클램프하고 Reordered를 센다.
//   - AddKeyframe에 StateSize 미만 state가 오면 키프레임을 만들지 않는다(부분 키프레임 금지).
//   - AddKeyframe도 블록 단조를 지킨다(마지막 키프레임 블록으로 클램프).
package session

import "github.com/midagedev/revirth/engine"

// Author — 로그 엔트리의 저자. 값 자체가 직렬화 의미 계약이다(0..3).
type Author uint8

const (
	Human    Author = 0
	Resident Author = 1
	// ReplayAuthor — URL 디코드 결과 엔트리의 저자. 스펙이 정한 값은 2로 그대로 두고,
	// 아래 Replay 함수(재생 헬퍼)와 패키지 스코프 이름이 충돌해 식별자만 ReplayAuthor다.
	ReplayAuthor Author = 2
	System       Author = 3
)

// Entry — 한 명령 기록. Block은 엔진 블록 인덱스(Append가 단조를 보장).
type Entry struct {
	Block  uint32
	Author Author
	Cmd    engine.Cmd
}

// Keyframe — 바 경계 제어 상태 스냅숏(engine.WriteState 출력). 보이스 내부 상태는 없다.
type Keyframe struct {
	Block uint32
	State [engine.StateSize]byte
}

// Log — 한 세션. Seed·Word는 시드 단어와 그 해시(독립 저장 — 어긋나도 그대로 보존).
type Log struct {
	Seed      uint32
	Word      string
	Entries   []Entry
	Keyframes []Keyframe

	// Reordered — Append가 과거 블록을 클램프한 횟수(진단 카운터, 직렬화되지 않는다).
	Reordered int
}

// Append — 엔트리 기록. 블록은 단조 비감소: 과거 블록이 오면 마지막 블록으로 클램프하고
// Reordered를 증가시킨다(드랍이 아니라 정규화 — 재생 순서가 깨지지 않게).
func (l *Log) Append(block uint32, a Author, c engine.Cmd) {
	if n := len(l.Entries); n > 0 && block < l.Entries[n-1].Block {
		block = l.Entries[n-1].Block
		l.Reordered++
	}
	l.Entries = append(l.Entries, Entry{Block: block, Author: a, Cmd: c})
}

// AddKeyframe — 키프레임 추가. state가 engine.StateSize 미만이면 무시한다(엔진 WriteState가
// 짧은 dst에 0을 돌려주는 것과 같은 방어). 블록은 키프레임 사이 단조로 클램프된다.
func (l *Log) AddKeyframe(block uint32, state []byte) {
	if len(state) < engine.StateSize {
		return
	}
	if n := len(l.Keyframes); n > 0 && block < l.Keyframes[n-1].Block {
		block = l.Keyframes[n-1].Block
	}
	var kf Keyframe
	kf.Block = block
	copy(kf.State[:], state[:engine.StateSize])
	l.Keyframes = append(l.Keyframes, kf)
}

// Since — 리플레이 시작점. block 이하 마지막 키프레임과 그 키프레임 이후 엔트리 전부.
// 키프레임이 없으면(nil) 엔트리 전부(처음부터 재생). 반환 슬라이스/포인터는 로그 내부를
// 공유한다(복제하지 않음 — 호출자는 읽기만).
func (l *Log) Since(block uint32) (*Keyframe, []Entry) {
	kfi := -1
	for i := range l.Keyframes {
		if l.Keyframes[i].Block <= block {
			kfi = i
		} else {
			break
		}
	}
	if kfi < 0 {
		return nil, l.Entries
	}
	kf := &l.Keyframes[kfi]
	i := 0
	for i < len(l.Entries) && l.Entries[i].Block <= kf.Block {
		i++
	}
	return kf, l.Entries[i:]
}
