//go:build !(js && wasm)

package core

// NewBridge — 데스크톱: 호스트 없음. 전부 no-op(값 0). `go vet ./app/...`용 타입체크 경로.
func NewBridge() Bridge { return nopBridge{} }
