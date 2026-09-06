// dump-native — 엔진 출력의 float32 비트열을 그대로 stdout에 쏟는 디버그 도구.
// wasm 측 dump-wasm.mjs와 1:1 바이트 비교로 첫 불일치 샘플을 찾는다(해시 디버깅용).
// 출력 = 인터리브 스테레오 샘플의 Float32bits(uint32 LE) 나열 — cmd/render의
// 해시 대상 바이트열과 동일 순서.
package main

import (
	"encoding/binary"
	"flag"
	"math"
	"os"

	"github.com/midagedev/jangdan/engine"
)

func main() {
	seed := flag.Uint("seed", 1, "")
	seconds := flag.Float64("seconds", 1, "")
	muteBD := flag.Bool("mute-bd", false, "BDLevel=0")
	muteCH := flag.Bool("mute-ch", false, "CHLevel=0")
	flag.Parse()
	e := engine.New(uint32(*seed))
	if *muteBD {
		e.SetParam(engine.BDLevel, 0)
	}
	if *muteCH {
		e.SetParam(engine.CHLevel, 0)
	}
	buf := make([]float32, 2*engine.Block)
	scratch := make([]byte, 4*len(buf))
	blocks := int(*seconds * 48000.0 / 128.0)
	for i := 0; i < blocks; i++ {
		e.Render(buf)
		for j, s := range buf {
			binary.LittleEndian.PutUint32(scratch[4*j:], math.Float32bits(s))
		}
		os.Stdout.Write(scratch)
	}
}
