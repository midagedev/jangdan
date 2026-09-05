// jdsess — 합성 손 조작 로그로 공유 URL 길이를 재는 게이트 CLI(docs/impl-plan-2026-09-05.md §5·§10).
// 5분(130 BPM 기본) 세션: 노브 200회 이동 × 중간값 20개(연속 블록, 무작위 파라미터),
// 스텝 편집 100회, 뮤트 20회, 드롭 3회. EncodeURL 길이와 EncodeURLBudget(2000) 결과를
// 표로 찍고 예산 결과가 2000자를 넘으면 0이 아닌 코드로 종료한다. 난수는 고정 시드
// xorshift32 — 실행할 때마다 같은 로그(재현 게이트).
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/midagedev/revirth/engine"
	"github.com/midagedev/revirth/session"
)

const (
	totalBlocks = 300 * 48000 / 128 // 5분 = 112500블록
	gestures    = 200
	gestureLen  = 20
	stepEdits   = 100
	muteCount   = 20
	dropCount   = 3
	budgetChars = 2000
)

func main() {
	printMode := false
	for _, arg := range os.Args[1:] {
		if arg == "-print" {
			printMode = true
		}
	}
	l := synthesize()
	raw := session.EncodeURL(l)
	url, level := session.EncodeURLBudget(l, budgetChars)

	if printMode {
		fmt.Println(url)
		fmt.Printf("entries=%d raw=%d\n", countEntries(url), len(l.Entries))
		return
	}
	word := l.Word
	if word == "" {
		word = "(없음)"
	}
	fmt.Printf("시드 단어  %s  seed=0x%08X\n", word, l.Seed)
	fmt.Printf("세션       5분 130BPM  블록=%d\n", totalBlocks)
	fmt.Printf("엔트리     raw=%d (노브 %d×%d + 스텝 %d + 뮤트 %d + 드롭 %d)\n",
		len(l.Entries), gestures, gestureLen, stepEdits, muteCount, dropCount)
	fmt.Printf("%-10s %6d자\n", "EncodeURL", len(raw))
	fmt.Printf("%-10s %6d자  감량=%d (%s)\n", "예산2000", len(url), level, levelName(level))
	if len(url) <= budgetChars {
		fmt.Printf("게이트     PASS (≤ %d자)\n", budgetChars)
		return
	}
	fmt.Printf("게이트     FAIL (%d자 > %d자)\n", len(url), budgetChars)
	os.Exit(1)
}

func levelName(level int) string {
	if level == 0 {
		return "스텝 내 감량만"
	}
	return fmt.Sprintf("파라미터별 %d스텝당 1개", 1<<level)
}

// countEntries — -print 보조: URL을 디코드해 엔트리 수를 센다(테스트가 이 값을 단언).
func countEntries(url string) int {
	l, err := session.DecodeURL(url)
	if err != nil {
		return -1
	}
	return len(l.Entries)
}

// xorshift32 — 재현 합성용(엔진 스타일, math/rand 회피).
type xorshift32 struct{ s uint32 }

func (x *xorshift32) next() uint32 {
	s := x.s
	s ^= s << 13
	s ^= s >> 17
	s ^= s << 5
	x.s = s
	return s
}

func (x *xorshift32) byteN(n int) int { return int(x.next()>>8) % n }
func (x *xorshift32) unit() float32   { return float32(x.next()) / float32(0xFFFFFFFF) }

// synthesize — 고정 시드 합성 로그. 품목별로 (블록, Cmd)를 모은 뒤 블록 순으로 정렬해
// Append한다(Append의 단조 계약과 같은 결과).
func synthesize() *session.Log {
	rng := xorshift32{s: 0x4A414E47} // "JANG"
	type slot struct {
		block uint32
		cmd   engine.Cmd
	}
	var items []slot

	// 노브 200회: 무작위 파라미터를 20연속 블록에 걸쳐 드래그(중간값 포함).
	for g := 0; g < gestures; g++ {
		base := uint32(1 + (g*totalBlocks)/gestures + rng.byteN(40))
		if end := base + gestureLen; end >= totalBlocks {
			base = totalBlocks - gestureLen - 1
		}
		param := uint8(rng.byteN(int(engine.NumParams)))
		v0, v1 := rng.unit(), rng.unit()
		for k := 0; k < gestureLen; k++ {
			v := v0 + (v1-v0)*float32(k)/float32(gestureLen-1)
			items = append(items, slot{base + uint32(k), engine.Cmd{Kind: engine.SetParam, A: param, V: v}})
		}
	}
	// 스텝 편집 100회: 베이스/드럼 반반.
	for i := 0; i < stepEdits; i++ {
		b := uint32(1 + rng.byteN(totalBlocks-2))
		if i%2 == 0 {
			items = append(items, slot{b, engine.Cmd{Kind: engine.BassStep,
				A: uint8(rng.byteN(2)), B: uint8(rng.byteN(engine.Steps)),
				C: uint8(12 + rng.byteN(int(engine.MaxNote)-11)), D: uint8(rng.byteN(8))}})
		} else {
			items = append(items, slot{b, engine.Cmd{Kind: engine.DrumStep,
				A: uint8(engine.BD) + uint8(rng.byteN(int(engine.NumParts)-int(engine.BD))),
				B: uint8(rng.byteN(engine.Steps)), D: uint8(rng.byteN(5))}})
		}
	}
	// 뮤트 20회 토글, 드롭 3회(분산 배치).
	for i := 0; i < muteCount; i++ {
		items = append(items, slot{uint32(1 + rng.byteN(totalBlocks-2)), engine.Cmd{
			Kind: engine.Mute, A: uint8(rng.byteN(int(engine.NumParts))), B: uint8(i % 2)}})
	}
	for i := 0; i < dropCount; i++ {
		items = append(items, slot{uint32(totalBlocks * (i + 1) / (dropCount + 1)), engine.Cmd{Kind: engine.Drop}})
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].block < items[j].block })
	l := &session.Log{Word: "노동요"}
	l.Seed = session.SeedFromWord(l.Word)
	for _, it := range items {
		l.Append(it.block, session.Human, it.cmd)
	}
	return l
}
