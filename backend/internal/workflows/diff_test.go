package workflows

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func render(hunks []Hunk) string {
	var b strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&b, "@@ %d %d\n", h.FromLine, h.ToLine)
		for _, l := range h.Lines {
			b.WriteString(map[string]string{OpEqual: " ", OpInsert: "+", OpDelete: "-"}[l.Op] + l.Text + "\n")
		}
	}
	return b.String()
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i+1)
	}
	return out
}

func TestADiffShowsAChangeWithItsContext(t *testing.T) {
	a := numbered(10)
	b := append([]string{}, a...)
	b[4] = "line five"
	got := render(LineDiff(strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n"))
	want := "@@ 2 2\n line 2\n line 3\n line 4\n-line 5\n+line five\n line 6\n line 7\n line 8\n"
	if got != want {
		t.Errorf("got\n%swant\n%s", got, want)
	}
}

func TestChangesFarApartMakeTwoHunksAndCloseOnesOne(t *testing.T) {
	a := numbered(30)
	far := append([]string{}, a...)
	far[2], far[25] = "x", "y"
	if hunks := LineDiff(strings.Join(a, "\n"), strings.Join(far, "\n")); len(hunks) != 2 || hunks[1].FromLine != 23 {
		t.Errorf("far apart = %s", render(hunks))
	}
	close := append([]string{}, a...)
	close[2], close[6] = "x", "y"
	if hunks := LineDiff(strings.Join(a, "\n"), strings.Join(close, "\n")); len(hunks) != 1 {
		t.Errorf("close together = %s", render(hunks))
	}
}

func TestADiffOfInsertionsAndDeletionsAtTheEnds(t *testing.T) {
	got := render(LineDiff("a\nb\n", "b\nc\n"))
	if got != "@@ 1 1\n-a\n b\n+c\n" {
		t.Errorf("got\n%s", got)
	}
	if len(LineDiff("same\n", "same\n")) != 0 {
		t.Error("equal texts have a diff")
	}
	if got := render(LineDiff("", "new\n")); got != "@@ 0 1\n+new\n" {
		t.Errorf("from nothing = %q", got)
	}
}

func TestSharingNamesRealGroupsOnlyWhenSharingWithGroups(t *testing.T) {
	if groups, err := shareTo(SharingGroups, []string{" /developers", "developers", "quality"}); err != nil || strings.Join(groups, ",") != "developers,quality" {
		t.Errorf("groups = %v, %v", groups, err)
	}
	if groups, err := shareTo(SharingEveryone, []string{"developers"}); err != nil || len(groups) != 0 {
		t.Errorf("everyone keeps groups %v, %v", groups, err)
	}
	var invalid *Invalid
	for _, tc := range []struct {
		sharing string
		groups  []string
	}{{SharingGroups, nil}, {SharingGroups, []string{"no;such"}}, {"friends", nil}} {
		if _, err := shareTo(tc.sharing, tc.groups); !errors.As(err, &invalid) || !strings.HasSuffix(invalid.Problems[0].Message, ".") {
			t.Errorf("shareTo(%q, %v) = %v", tc.sharing, tc.groups, err)
		}
	}
}
