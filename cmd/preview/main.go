// preview — 레지던트 오토파일럿이 엔진을 구동하는 "기본 재생"을 N초 WAV로 렌더한다(앱과 같은 경로:
// engine.New(seed) + resident.New(seed, vibe, cfg), 블록마다 Tick → Apply → Render).
//
//	go run ./cmd/preview --seed 1 --seconds 60 --vibe deepfocus --out /tmp/preview.wav
//
// 용도: 기본 파라미터·레지던트 연주법을 바꿀 때 배포 전에 귀로 확인하는 표본(사용자 청취 루프).
// cmd/render는 레지던트 없는 엔진 단독(해시 게이트)이고, 이 도구는 사람이 듣는 것을 재현한다.
// 결정론: 같은 seed·vibe·seconds → 같은 바이트(레지던트·엔진 둘 다 시드 결정론). 해시를 함께 찍는다.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/midagedev/jangdan/engine"
	"github.com/midagedev/jangdan/resident"
)

func main() {
	seed := flag.Uint("seed", 1, "seed(엔진·레지던트 공용)")
	seconds := flag.Float64("seconds", 60, "렌더 초")
	vibeName := flag.String("vibe", "deepfocus", "deepfocus|rush|lofi")
	outPath := flag.String("out", "", "WAV 출력 경로(16bit PCM 스테레오 48k)")
	flag.Parse()
	vibe := resident.DeepFocus
	switch strings.ToLower(*vibeName) {
	case "rush":
		vibe = resident.Rush
	case "lofi":
		vibe = resident.Lofi
	}
	e := engine.New(uint32(*seed))
	r := resident.New(uint32(*seed), vibe, resident.Config{FocusMin: 25, RestMin: 5, DemoFocusMin: 5})
	blocks := int(*seconds * engine.SampleRate / engine.Block)
	buf := make([]float32, 2*engine.Block)
	pcm := make([]byte, 2*len(buf))
	scratch := make([]byte, 4*len(buf))
	h := sha256.New()
	var wav *os.File
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wav create:", err)
			os.Exit(1)
		}
		defer f.Close()
		wav = f
		writeWavHeader(wav, blocks)
	}
	prevBar, prevStep := e.Bar(), e.Step()
	first := true
	var peak float32
	cmds := 0
	for i := 0; i < blocks; i++ {
		bar, step := e.Bar(), e.Step()
		barStart := first || (step == 0 && (bar != prevBar || step != prevStep))
		if first || bar != prevBar || step != prevStep {
			now := float64(i) * engine.Block / engine.SampleRate
			for _, c := range r.Tick(resident.Input{Bar: bar, Step: step, Now: now, BarStart: barStart}) {
				e.Apply(c)
				cmds++
			}
			first = false
		}
		prevBar, prevStep = bar, step
		e.Render(buf)
		for j, s := range buf {
			if a := float32(math.Abs(float64(s))); a > peak {
				peak = a
			}
			binary.LittleEndian.PutUint32(scratch[4*j:], math.Float32bits(s))
			c := s
			if c > 1 {
				c = 1
			}
			if c < -1 {
				c = -1
			}
			binary.LittleEndian.PutUint16(pcm[2*j:], uint16(int16(c*32767)))
		}
		h.Write(scratch)
		if wav != nil {
			wav.Write(pcm)
		}
	}
	fmt.Printf("preview seed=%d vibe=%s seconds=%g blocks=%d cmds=%d peak=%.4f sha256=%s\n",
		*seed, *vibeName, *seconds, blocks, cmds, peak, hex.EncodeToString(h.Sum(nil)))
}

// writeWavHeader — 44바이트 RIFF 헤더(cmd/render와 같은 형식).
func writeWavHeader(f *os.File, blocks int) {
	dataBytes := uint32(blocks * engine.Block * 2 * 2)
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], 36+dataBytes)
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1)
	binary.LittleEndian.PutUint16(hdr[22:], 2)
	binary.LittleEndian.PutUint32(hdr[24:], engine.SampleRate)
	binary.LittleEndian.PutUint32(hdr[28:], engine.SampleRate*4)
	binary.LittleEndian.PutUint16(hdr[32:], 4)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], dataBytes)
	f.Write(hdr)
}
