//go:build !(js && wasm)

package assets

import "embed"

//go:embed device/*.png device/sprites/*.png room/*.png
var files embed.FS

// Read — 큰 PNG 바이트. 데스크톱: embed FS.
func Read(name string) ([]byte, error) { return files.ReadFile(name) }

// WaitReady — 데스크톱은 즉시 준비됨.
func WaitReady() bool { return true }
