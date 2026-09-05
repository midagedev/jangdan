//go:build !(js && wasm)

package main

import "github.com/midagedev/revirth/session"

func installShare(g *game) {}

func sharedLog() *session.Log { return nil }
