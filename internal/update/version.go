package update

import (
	"git-task/internal/diagnostic"
	"regexp"
	"strconv"
	"strings"
)

type version struct {
	core [3]uint64
	pre  []string
}

func parseVersion(text string) (version, error) {
	var result version
	pattern := regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	m := pattern.FindStringSubmatch(text)
	if m == nil {
		return result, diagnostic.Errorf("неподдерживаемая версия %q; сначала установите официальный выпуск", text)
	}
	for i := range result.core {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return result, diagnostic.Errorf("неверная версия %q: %w", text, err)
		}
		result.core[i] = n
	}
	if m[4] != "" {
		result.pre = strings.Split(m[4], ".")
		for _, p := range result.pre {
			if numeric(p) && len(p) > 1 && p[0] == '0' {
				return version{}, diagnostic.Errorf("неверная версия %q", text)
			}
		}
	}
	return result, nil
}

func numeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func compare(a, b version) int {
	for i := range a.core {
		if a.core[i] < b.core[i] {
			return -1
		}
		if a.core[i] > b.core[i] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) != 0 {
		return 1
	}
	if len(b.pre) == 0 && len(a.pre) != 0 {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		xn, yn := numeric(x), numeric(y)
		if xn != yn {
			if xn {
				return -1
			}
			return 1
		}
		if xn && len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		if x < y {
			return -1
		}
		return 1
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}
