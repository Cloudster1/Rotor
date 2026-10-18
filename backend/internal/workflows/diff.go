package workflows

import (
	"slices"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Diff operations on a line.
const (
	OpEqual  = "equal"
	OpInsert = "insert"
	OpDelete = "delete"
)

// Line is one line of a diff and what happened to it.
type Line struct {
	Op   string
	Text string
}

// Hunk is a run of changes with the unchanged lines around them; FromLine and ToLine count from 1.
type Hunk struct {
	FromLine int
	ToLine   int
	Lines    []Line
}

// step is one line of the whole alignment, with its line in each version (or -1 where it has none).
type step struct {
	op   string
	text string
	a, b int
}

// LineDiff answers what changed from one text to another, line by line, in hunks with context.
func LineDiff(from, to string) []Hunk {
	a, b := splitLines(from), splitLines(to)
	var steps []step
	i, j := 0, 0
	for _, m := range align(len(a), len(b), func(x, y int) bool { return a[x] == b[y] }) {
		for ; i < m[0]; i++ {
			steps = append(steps, step{OpDelete, a[i], i, -1})
		}
		for ; j < m[1]; j++ {
			steps = append(steps, step{OpInsert, b[j], -1, j})
		}
		steps = append(steps, step{OpEqual, a[i], i, j})
		i, j = i+1, j+1
	}
	for ; i < len(a); i++ {
		steps = append(steps, step{OpDelete, a[i], i, -1})
	}
	for ; j < len(b); j++ {
		steps = append(steps, step{OpInsert, b[j], -1, j})
	}
	return hunks(steps)
}

// hunks cuts the alignment into the changed parts, each with up to DiffContextLines unchanged lines around it.
func hunks(steps []step) []Hunk {
	var out []Hunk
	for k := 0; k < len(steps); {
		if steps[k].op == OpEqual {
			k++
			continue
		}
		start := max(k-config.DiffContextLines, 0)
		end := k
		// Changes closer than twice the context belong to one hunk.
		for end < len(steps) {
			if steps[end].op != OpEqual {
				end++
				continue
			}
			run := end
			for run < len(steps) && steps[run].op == OpEqual {
				run++
			}
			if run == len(steps) || run-end > 2*config.DiffContextLines {
				end = min(end+config.DiffContextLines, len(steps))
				break
			}
			end = run
		}
		out = append(out, toHunk(steps[start:end]))
		k = end
	}
	return out
}

func toHunk(steps []step) Hunk {
	h := Hunk{Lines: make([]Line, 0, len(steps))}
	fromSet, toSet := false, false
	for _, s := range steps {
		if s.a >= 0 && !fromSet {
			h.FromLine, fromSet = s.a+1, true
		}
		if s.b >= 0 && !toSet {
			h.ToLine, toSet = s.b+1, true
		}
		h.Lines = append(h.Lines, Line{Op: s.op, Text: s.text})
	}
	return h
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// align finds the longest common subsequence of two sequences by index, after taking off what they start and
// end with alike. Adapted from Exciter's internal/requirement/diff.go (see NOTICE).
func align(n, m int, eq func(i, j int) bool) [][2]int {
	var head, tail [][2]int
	lo := 0
	for lo < n && lo < m && eq(lo, lo) {
		head = append(head, [2]int{lo, lo})
		lo++
	}
	hn, hm := n, m
	for hn > lo && hm > lo && eq(hn-1, hm-1) {
		hn, hm = hn-1, hm-1
		tail = append(tail, [2]int{hn, hm})
	}
	slices.Reverse(tail)
	rows, cols := hn-lo, hm-lo
	if rows == 0 || cols == 0 || (rows+1)*(cols+1) > config.MaxDiffCells {
		return append(head, tail...)
	}
	// lengths[r][c] is the common length of the suffixes from lo+r and lo+c.
	width := cols + 1
	lengths := make([]int32, (rows+1)*width)
	for r := rows - 1; r >= 0; r-- {
		for c := cols - 1; c >= 0; c-- {
			switch {
			case eq(lo+r, lo+c):
				lengths[r*width+c] = lengths[(r+1)*width+c+1] + 1
			case lengths[(r+1)*width+c] >= lengths[r*width+c+1]:
				lengths[r*width+c] = lengths[(r+1)*width+c]
			default:
				lengths[r*width+c] = lengths[r*width+c+1]
			}
		}
	}
	middle := head
	for r, c := 0, 0; r < rows && c < cols; {
		switch {
		case eq(lo+r, lo+c):
			middle = append(middle, [2]int{lo + r, lo + c})
			r, c = r+1, c+1
		case lengths[(r+1)*width+c] >= lengths[r*width+c+1]:
			r++
		default:
			c++
		}
	}
	return append(middle, tail...)
}
