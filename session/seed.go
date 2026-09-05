// seed.go — 시드 단어 → uint32. 계약 원본 docs/impl-plan-2026-09-05.md §5.
//
// 정규화는 "소문자화(룬 단위) + 공백·제어문자 제거"뿐이다. NFC 같은 유니코드 정규화는
// 의도적으로 하지 않는다(유니코드 정규화 라이브러리는 외부 의존이라 이 패키지 금지 —
// 조합형/완성형처럼 정규형만 다른 입력은 다른 시드가 될 수 있다는 뜻이고, 그대로 받아들인다).
// 빈 결과는 0x9E3779B9, FNV-1a 결과가 0이면 1(엔진이 seed 0을 0x9E3779B9로 대체하므로,
// 로그의 시드와 엔진의 실제 시드가 어긋나는 것을 원천 차단한다).
package session

import (
	"strings"
	"unicode"
)

// SeedFromWord — 같은 단어는 항상 같은 값. 공백·대소문자는 무시된다.
func SeedFromWord(s string) uint32 {
	w := normalizeWord(s)
	if w == "" {
		return 0x9E3779B9
	}
	return fnvOrOne(fnv1a32(w))
}

// normalizeWord — 룬 단위 소문자화 + 공백·제어 문자 제거. NFC 정규화 없음(파일 상단 참조).
func normalizeWord(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

const (
	fnvOffset32 = 2166136261
	fnvPrime32  = 16777619
)

// fnv1a32 — FNV-1a 32비트(UTF-8 바이트).
func fnv1a32(s string) uint32 {
	h := uint32(fnvOffset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= fnvPrime32
	}
	return h
}

// fnvOrOne — 결과 0 금지(엔진 seed 0 대체 회피).
func fnvOrOne(h uint32) uint32 {
	if h == 0 {
		return 1
	}
	return h
}
