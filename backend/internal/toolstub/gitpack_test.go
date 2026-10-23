package toolstub

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// A pack the real git client makes, its deltas by offset and by id among them, reads into the objects git names.
func TestAPackOfGitsReadsIntoItsObjects(t *testing.T) {
	git := newGitClient(t)
	dir := t.TempDir()
	if out, ok := git.run(dir, "init", "-q"); !ok {
		t.Fatal(out)
	}
	lines := strings.Repeat("A line long enough that the next version is stored as a delta of this one.\n", 300)
	for _, text := range []string{lines, lines + "One more.\n", "A first line.\n" + lines + "One more.\n"} {
		git.commit(dir, "file.txt", text)
	}
	listed, ok := git.run(dir, "rev-list", "--objects", "--all")
	if !ok {
		t.Fatal(listed)
	}
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(listed), "\n") {
		want = append(want, strings.Fields(line)[0])
	}
	for _, flags := range [][]string{{"--delta-base-offset"}, {}} {
		cmd := exec.Command("git", append([]string{"pack-objects", "--stdout", "-q"}, flags...)...)
		cmd.Dir, cmd.Env = dir, []string{"HOME=" + git.home, "GIT_CONFIG_NOSYSTEM=1"}
		cmd.Stdin = strings.NewReader(listed)
		pack, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		objects, err := readPack(pack, nil)
		if err != nil {
			t.Fatalf("a pack of %v does not read: %v", flags, err)
		}
		if deltas := countDeltas(t, pack); deltas == 0 {
			t.Errorf("git made a pack of %v without deltas, so the test proves nothing of them", flags)
		}
		got := make([]string, 0, len(objects))
		for id := range objects {
			got = append(got, id)
		}
		slices.Sort(got)
		sorted := slices.Sorted(slices.Values(want))
		if !slices.Equal(got, sorted) {
			t.Errorf("a pack of %v reads into %v, want %v", flags, got, sorted)
		}
	}
	if _, err := readPack([]byte("PACK not really"), nil); err == nil {
		t.Error("a broken pack reads")
	}
}

// countDeltas is how many of a pack's entries are deltas.
func countDeltas(t *testing.T, pack []byte) int {
	t.Helper()
	in := &counting{r: bufio.NewReader(bytes.NewReader(pack[packHeader:])), n: packHeader}
	deltas := 0
	for range binary.BigEndian.Uint32(pack[8:packHeader]) {
		e, err := readEntry(in)
		if err != nil {
			t.Fatal(err)
		}
		if e.kind == "" {
			deltas++
		}
	}
	return deltas
}
