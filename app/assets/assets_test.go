// assets_test.go — 자산 이름 표의 3중화 게이트(2026-09-06 사건: rear.png을 Names·host.js에는
// 넣고 build.sh 복사 목록에 빠뜨려 브라우저 prefetch가 404였다. 콘솔에만 나타나고 화면은
// 멀쩡해서 계측 왕복에서야 보였다).
//
// 이름의 소유자는 Names 하나여야 옳지만 소비자가 Go·JS·셸 셋이라 지금은 전사다. 전사인 동안
// 이 테스트가 세 사본의 일치를 잰다 — 이름을 하나 더할 때 어느 하나를 잊으면 여기서 걸린다.
package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetNamesMirrored(t *testing.T) {
	// host.js — 이름을 그대로 쓴다(부분 문자열이면 충분).
	b, err := os.ReadFile("../web/host.js")
	if err != nil {
		t.Fatalf("host.js 읽기: %v", err)
	}
	host := string(b)
	for _, n := range Names {
		if !strings.Contains(host, n) {
			t.Errorf("host.js ASSET_NAMES에 %q 없음", n)
		}
	}

	// build.sh — cp 원본 경로가 글롭일 수 있으므로 실제로 펼쳐서 잰다(문자열 일치로는
	// room/*.png 같은 글롭을 못 읽어 이 게이트가 오탐을 낸다 — 저작 중 실측).
	b, err = os.ReadFile("../build.sh")
	if err != nil {
		t.Fatalf("build.sh 읽기: %v", err)
	}
	copied := map[string]bool{}
	for _, tok := range strings.Fields(string(b)) {
		tok = strings.Trim(tok, "\"'")
		if !strings.HasPrefix(tok, "app/assets/") {
			continue
		}
		hits, err := filepath.Glob(filepath.Join("../..", tok))
		if err != nil {
			t.Fatalf("글롭 %q: %v", tok, err)
		}
		for _, h := range hits {
			rel, err := filepath.Rel("../..//app/assets", h)
			if err == nil {
				copied[filepath.ToSlash(rel)] = true
			}
		}
	}
	for _, n := range Names {
		if !copied[n] {
			t.Errorf("build.sh 복사 목록이 %q를 담지 않는다(펼친 목록 %d개)", n, len(copied))
		}
	}
}
