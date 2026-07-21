package fieldcryptoprocessor

import (
	"sort"
	"unicode"
)

func hasTemplateTokens(pattern string) bool {
	for _, r := range pattern {
		if r == 'A' || r == 'a' || r == 'X' || r == 'x' {
			return true
		}
	}
	return false
}

func maskWithTemplate(value, pattern string) string {
	runes := []rune(value)
	idx := maskableIndexes(runes)
	if len(idx) == 0 {
		return value
	}

	ops := parseTemplateOps(pattern)
	if len(ops) == 0 {
		return value
	}

	visible, ok := solveTemplate(len(idx), ops)
	if !ok {
		visible = fallbackTemplate(len(idx), ops)
	}
	enforceMinMasked(visible, 40)

	for i, rIndex := range idx {
		if !visible[i] {
			runes[rIndex] = 'X'
		}
	}
	return string(runes)
}

func maskableIndexes(r []rune) []int {
	out := make([]int, 0, len(r))
	for i, ch := range r {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			out = append(out, i)
		}
	}
	return out
}

func parseTemplateOps(pattern string) []rune {
	ops := make([]rune, 0, len(pattern))
	prevX := false
	for _, r := range pattern {
		switch r {
		case 'A', 'a':
			ops = append(ops, 'A')
			prevX = false
		case 'X', 'x':
			if !prevX {
				ops = append(ops, 'X')
				prevX = true
			}
		default:
			// Non-template symbols are ignored; punctuation is preserved from the value.
		}
	}
	return ops
}

func solveTemplate(n int, ops []rune) ([]bool, bool) {
	visible := make([]bool, n)
	var rec func(i, j int) bool
	rec = func(i, j int) bool {
		if j == len(ops) {
			return i == n
		}
		if i > n {
			return false
		}

		switch ops[j] {
		case 'A':
			if i >= n {
				return false
			}
			visible[i] = true
			if rec(i+1, j+1) {
				return true
			}
			visible[i] = false
			return false
		case 'X':
			if i >= n {
				return false
			}
			minRem := minNeeded(ops[j+1:])
			maxLen := n - i - minRem
			if maxLen < 1 {
				return false
			}
			for l := maxLen; l >= 1; l-- {
				if rec(i+l, j+1) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	ok := rec(0, 0)
	return visible, ok
}

func minNeeded(ops []rune) int {
	need := 0
	for _, op := range ops {
		if op == 'A' || op == 'X' {
			need++
		}
	}
	return need
}

func fallbackTemplate(n int, ops []rune) []bool {
	visible := make([]bool, n)
	if n == 0 {
		return visible
	}
	prefixA := 0
	for _, op := range ops {
		if op != 'A' {
			break
		}
		prefixA++
	}
	suffixA := 0
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i] != 'A' {
			break
		}
		suffixA++
	}
	for i := 0; i < prefixA && i < n; i++ {
		visible[i] = true
	}
	for i := 0; i < suffixA && i < n-prefixA; i++ {
		visible[n-1-i] = true
	}
	if prefixA == 0 && suffixA == 0 {
		countA := 0
		for _, op := range ops {
			if op == 'A' {
				countA++
			}
		}
		for i := 0; i < countA && i < n; i++ {
			visible[i] = true
		}
	}
	return visible
}

func enforceMinMasked(visible []bool, minPct int) {
	n := len(visible)
	if n == 0 {
		return
	}
	masked := 0
	for _, v := range visible {
		if !v {
			masked++
		}
	}
	minMasked := (n*minPct + 99) / 100
	if masked >= minMasked {
		return
	}
	need := minMasked - masked

	type candidate struct {
		idx  int
		dist int
	}
	center := n / 2
	cands := make([]candidate, 0, n)
	for i, v := range visible {
		if v {
			d := i - center
			if d < 0 {
				d = -d
			}
			cands = append(cands, candidate{idx: i, dist: d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist == cands[j].dist {
			return cands[i].idx < cands[j].idx
		}
		return cands[i].dist < cands[j].dist
	})
	for i := 0; i < len(cands) && need > 0; i++ {
		visible[cands[i].idx] = false
		need--
	}
}
