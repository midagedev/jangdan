//go:build !js || !wasm

// 데스크톱 빌드용 브리지 스텁 — 브라우저 계측·오디오가 없다. 이 파일의 존재
// 이유: `go vet ./app/...`(호스트 GOOS)가 syscall/js 없이 app 패키지를
// 타입체크하게 하는 것. 시계는 시작 후 경과 시간(스텝 lit 애니메이션 확인용).
package main

import "time"

type desktopBridge struct{ start time.Time }

func newBridge() Bridge { return desktopBridge{start: time.Now()} }

func (desktopBridge) SetParam(int, float32) {}
func (desktopBridge) Scope([]byte) bool     { return false }
func (d desktopBridge) Clock() float64      { return time.Since(d.start).Seconds() }
func (desktopBridge) Frame(float64)         {}
func (desktopBridge) FirstFrame()           {}
func (desktopBridge) AllocPerFrame(float64) {}
func (desktopBridge) KnobDrag(int, float32) {}
