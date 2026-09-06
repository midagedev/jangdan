// replay.go — 로그를 엔진에 재생하는 헬퍼(네이티브 검증·서버 렌더용).
// "새 엔진 + 같은 로그 = 같은 소리" 계약(docs/impl-plan-2026-09-05.md §1)의 실행측.
package session

import "github.com/midagedev/jangdan/engine"

// Replay — 블록 로그 재생. ResetPos 후 블록 0..upToBlock-1을 렌더하며, 각 블록 렌더
// 시작 전에 그 블록에 도달한(Block <= b인 아직 적용 안 된) Cmd를 Apply한다. 정렬된
// 로그에서는 "Block == 엔트리 블록"과 같고, 비정렬 로그(직접 구성)에서는 기록 블록
// 이후 첫 블록에 적용되는 정규화로 떨어진다. out은 블록마다 렌더 직후 호출(nil이면
// 건너뛴다). 같은 seed·같은 로그면 어떤 엔진 인스턴스에서든 같은 샘플이 나온다.
func Replay(e *engine.Engine, l *Log, upToBlock uint32, out func(block uint32, buf []float32)) {
	e.Apply(engine.Cmd{Kind: engine.ResetPos})
	var buf [2 * engine.Block]float32
	entries := entriesOf(l)
	i := 0
	for b := uint32(0); b < upToBlock; b++ {
		for i < len(entries) && entries[i].Block <= b {
			e.Apply(entries[i].Cmd)
			i++
		}
		e.Render(buf[:])
		if out != nil {
			out(b, buf[:])
		}
	}
}

// ReplaySteps — 스텝 양자화 로그(URL 디코드 결과) 재생. DecodeURL이 각 엔트리의
// Block을 스텝 시작 블록(올림)으로 유도해 두므로, 그 블록 렌더 전에 Apply하는 것이 곧
// 스텝 경계 적용이다. 엔진의 Step()/Bar() 변화(bar*16+step 서수)를 감시해 stepCount개
// 스텝에 도달하면 멈춘다(스텝 0..stepCount-1 재생). 엔진 시계와 유도 블록이 1블록
// 미끄러진 극단 경계에서는 적용이 한 블록 일찍/늦게 날 수 있다(들어도 2.7ms).
//
// 정지 방어(2026-09-06, P2-session 라운드 발견): 로그에 Transport 정지가 있으면 엔진 시계가
// 동결되어 stepsEntered가 영원히 stepCount에 닿지 않는다. 블록 상한(스텝당 최대 56.25블록인
// 100 BPM 기준 64블록 × stepCount + 여유)에서 끊는다 — 정지된 로그는 "그만큼 렌더하고 끝".
func ReplaySteps(e *engine.Engine, entries []Entry, stepCount int, out func(block uint32, buf []float32)) {
	e.Apply(engine.Cmd{Kind: engine.ResetPos})
	var buf [2 * engine.Block]float32
	i := 0
	b := uint32(0)
	maxBlocks := uint32(stepCount)*replayMaxBlocksPerStep + replayMaxBlocksPerStep
	for stepsEntered(e) < stepCount && b < maxBlocks {
		for i < len(entries) && entries[i].Block <= b {
			e.Apply(entries[i].Cmd)
			i++
		}
		e.Render(buf[:])
		if out != nil {
			out(b, buf[:])
		}
		b++
	}
}

// replayMaxBlocksPerStep — ReplaySteps 블록 상한 계수(최저 템포 100 BPM = 스텝당 56.25블록 < 64).
const replayMaxBlocksPerStep = 64

// stepsEntered — 재생 시작 후 진입한 스텝 서수(스텝 0 진입 = 0).
func stepsEntered(e *engine.Engine) int {
	return int(e.Bar())*engine.Steps + e.Step()
}

func entriesOf(l *Log) []Entry {
	if l == nil {
		return nil
	}
	return l.Entries
}
