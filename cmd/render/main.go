// cmd/render — 네이티브 렌더 하네스. 브라우저 오프라인 렌더와 SHA-256로
// 바이트 단위 비교할 원본(리샘플·클립 없는 엔진 출력 그대로)을 만든다.
// fmt·os·crypto 등 표준 라이브러리는 이 파일에서 자유(engine/ 계약과 무관).
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/midagedev/jangdan/engine"
)

type result struct {
	Seed           uint32  `json:"seed"`
	Seconds        float64 `json:"seconds"`
	Blocks         int     `json:"blocks"`
	SHA256         string  `json:"sha256"`
	NsPerBlockMean float64 `json:"ns_per_block_mean"`
	NsPerBlockMax  float64 `json:"ns_per_block_max"`
	PeakAbs        float32 `json:"peak_abs"`
}

func main() {
	seed := flag.Uint("seed", 1, "seed")
	seconds := flag.Float64("seconds", 300, "렌더 초")
	outPath := flag.String("out", "", "WAV 출력 경로(16bit PCM 스테레오 48k, 선택)")
	jsonMode := flag.Bool("json", false, "결과를 JSON 한 줄로")
	flag.Parse()

	e := engine.New(uint32(*seed))
	buf := make([]float32, 2*engine.Block)
	scratch := make([]byte, 4*len(buf))
	h := sha256.New()
	blocks := int(*seconds * 48000.0 / 128.0)

	var wav *os.File
	if *outPath != "" {
		var err error
		wav, err = os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wav create:", err)
			os.Exit(1)
		}
		defer wav.Close()
		writeWavHeader(wav, blocks) // 크기는 미리 계산 가능(고정 블록 수)
	}
	pcm := make([]byte, 2*2*len(buf)) // 16bit 스테레오

	var mean, maxN float64
	var peak float32
	for i := 0; i < blocks; i++ {
		t0 := time.Now()
		e.Render(buf)
		d := float64(time.Since(t0).Nanoseconds())
		if i > 0 { // 첫 블록은 워밍업에서 제외
			mean += d
			if d > maxN {
				maxN = d
			}
		}
		for j, s := range buf {
			a := s
			if a < 0 {
				a = -a
			}
			if a > peak {
				peak = a
			}
			binary.LittleEndian.PutUint32(scratch[4*j:], math.Float32bits(s))
			// WAV용 16bit 변환(출력 해시와 무관)
			c := s
			if c > 1 {
				c = 1
			}
			if c < -1 {
				c = -1
			}
			q := int16(c * 32767)
			binary.LittleEndian.PutUint16(pcm[2*j:], uint16(q))
		}
		h.Write(scratch)
		if wav != nil {
			wav.Write(pcm)
		}
	}
	r := result{
		Seed:    uint32(*seed),
		Seconds: *seconds,
		Blocks:  blocks,
		SHA256:  hex.EncodeToString(h.Sum(nil)),
		// 첫 블록 제외 (blocks-1)개로 평균
		NsPerBlockMean: mean / float64(blocks-1),
		NsPerBlockMax:  maxN,
		PeakAbs:        peak,
	}
	if *jsonMode {
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
	} else {
		fmt.Printf("seed=%d seconds=%g blocks=%d\nsha256=%s\nns_per_block_mean=%.1f ns_per_block_max=%.1f peak_abs=%g\n",
			r.Seed, r.Seconds, r.Blocks, r.SHA256, r.NsPerBlockMean, r.NsPerBlockMax, r.PeakAbs)
	}
}

// writeWavHeader — 44바이트 RIFF 헤더(고정 길이: 블록 수를 미리 안다).
func writeWavHeader(f *os.File, blocks int) {
	dataBytes := uint32(blocks * 256 * 2) // 128프레임 × 스테레오 × 2바이트
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], 36+dataBytes)
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(hdr[22:], 2)  // 스테레오
	binary.LittleEndian.PutUint32(hdr[24:], 48000)
	binary.LittleEndian.PutUint32(hdr[28:], 48000*4)
	binary.LittleEndian.PutUint16(hdr[32:], 4)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], dataBytes)
	f.Write(hdr)
}
