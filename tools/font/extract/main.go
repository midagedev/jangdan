// tools/font/extract — golang.org/x/image/font/gofont(BSD-3)의 Go Bold TTF를 파일로 꺼낸다(아틀라스 생성용).
// 사용: go run ./tools/font/extract <out.ttf>
package main

import (
	"os"

	"golang.org/x/image/font/gofont/gobold"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], gobold.TTF, 0o644); err != nil {
		os.Exit(1)
	}
}
