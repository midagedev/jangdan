//go:build !(js && wasm)

package main

import "github.com/midagedev/revirth/session"

func installShare(g *game) {}

func sharedLog() *session.Log { return nil }

// sharedLogState — 데스크톱(호스트 없음)은 항상 "없음". -1이므로 폴링도 걸리지 않는다.
func sharedLogState() int { return -1 }
