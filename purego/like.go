package purego

import "strings"

// LikeCmp LIKE 模式匹配
func LikeCmp(s, pattern string) bool {
	si := 0
	pi := 0
	for pi < len(pattern) {
		if pattern[pi] == '%' {
			pi++
			if pi >= len(pattern) {
				return true
			}
			for si <= len(s) {
				if LikeCmp(s[si:], pattern[pi:]) {
					return true
				}
				si++
			}
			return false
		} else if pattern[pi] == '_' {
			if si >= len(s) {
				return false
			}
			si++
			pi++
		} else if pattern[pi] == '[' {
			// 字符集匹配
			pi++
			negate := false
			if pi < len(pattern) && pattern[pi] == '^' {
				negate = true
				pi++
			}
			matched := false
			for pi < len(pattern) && pattern[pi] != ']' {
				if pi+2 < len(pattern) && pattern[pi+1] == '-' {
					if si < len(s) && s[si] >= pattern[pi] && s[si] <= pattern[pi+2] {
						matched = true
					}
					pi += 3
				} else {
					if si < len(s) && s[si] == pattern[pi] {
						matched = true
					}
					pi++
				}
			}
			if pi < len(pattern) {
				pi++ // skip ']'
			}
			if negate {
				matched = !matched
			}
			if !matched {
				return false
			}
			si++
		} else {
			if si >= len(s) {
				return false
			}
			if s[si] != pattern[pi] {
				return false
			}
			si++
			pi++
		}
	}
	return si == len(s)
}

// ILikeCmp 不区分大小写的 LIKE 模式匹配
func ILikeCmp(s, pattern string) bool {
	return LikeCmp(strings.ToLower(s), strings.ToLower(pattern))
}
