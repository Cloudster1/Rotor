package skills

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// entry is one thing to put in a crafted zip; a nil mode is a regular file.
type entry struct {
	name    string
	content string
	mode    fs.FileMode
}

func makeZip(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// zipCodes is how ReadZip's refusal reads: code@path of each problem.
func zipCodes(t *testing.T, data []byte) []string {
	t.Helper()
	files, err := ReadZip(data)
	var invalid *Invalid
	if !errors.As(err, &invalid) {
		t.Fatalf("ReadZip = %v, %v, want a refusal", files, err)
	}
	return codes(invalid.Problems)
}

func TestAZipExportedImportsUnchanged(t *testing.T) {
	files := []File{{Path: "references/b.md", Content: "B\n"}, valid(), {Path: "a.md", Content: "A\n"}}
	want, problems := Check(files)
	if problems != nil {
		t.Fatal(problems)
	}
	data, err := WriteZip("requirements-engineering", files)
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadZip(data)
	if err != nil {
		t.Fatal(err)
	}
	got, problems := Check(read)
	if got == nil || got.Hash != want.Hash {
		t.Fatalf("the round trip gave %v, want hash %s (%v)", got, want.Hash, problems)
	}
}

func TestAnExportIsTheSameBytesEveryTimeWhateverTheOrderGiven(t *testing.T) {
	a := []File{valid(), {Path: "x.md", Content: "x"}, {Path: "y/z.md", Content: "z"}}
	b := []File{a[2], a[1], a[0]}
	first, _ := WriteZip("s", a)
	second, _ := WriteZip("s", b)
	if !bytes.Equal(first, second) {
		t.Fatal("the same files exported to different bytes")
	}
	r, _ := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	names := []string{}
	for _, f := range r.File {
		names = append(names, f.Name)
		if f.Modified.Year() != 1980 {
			t.Errorf("%s is dated %v, want a fixed date", f.Name, f.Modified)
		}
	}
	if want := []string{"s/SKILL.md", "s/x.md", "s/y/z.md"}; !slices.Equal(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
}

func TestSkillMDImportsAtTheRootOrInOneTopFolder(t *testing.T) {
	for name, entries := range map[string][]entry{
		"root":   {{name: "SKILL.md", content: valid().Content}, {name: "references/g.md", content: "g"}},
		"folder": {{name: "skill/", mode: fs.ModeDir | 0o755}, {name: "skill/SKILL.md", content: valid().Content}, {name: "skill/references/g.md", content: "g"}},
	} {
		files, err := ReadZip(makeZip(t, entries...))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		paths := []string{}
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		slices.Sort(paths)
		if want := []string{"SKILL.md", "references/g.md"}; !slices.Equal(paths, want) {
			t.Errorf("%s: paths = %v, want %v", name, paths, want)
		}
	}
}

func TestAZipIsRefusedWithTheCodeAndPathOfWhatIsWrong(t *testing.T) {
	md := valid().Content
	many := []entry{{name: "SKILL.md", content: md}}
	for i := 0; i < config.MaxSkillFiles; i++ {
		many = append(many, entry{name: "f/" + strings.Repeat("a", i+1) + ".md", content: "x"})
	}
	cases := map[string]struct {
		data []byte
		want string
	}{
		"not a zip":      {[]byte("this is text"), "skill_not_zip@-"},
		"empty body":     {nil, "skill_not_zip@-"},
		"dot dot":        {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: "../evil.md", content: "x"}), "skill_path@../evil.md"},
		"dot dot inside": {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: "a/../../evil.md", content: "x"}), "skill_path@a/../../evil.md"},
		"absolute":       {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: "/etc/passwd", content: "x"}), "skill_path@/etc/passwd"},
		"backslash":      {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: `a\b.md`, content: "x"}), `skill_path@a\b.md`},
		"symlink":        {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: "link.md", content: "/etc/passwd", mode: fs.ModeSymlink | 0o777}), "skill_path@link.md"},
		"too many files": {makeZip(t, many...), "skill_too_many_files@-"},
		"two folders":    {makeZip(t, entry{name: "a/SKILL.md", content: md}, entry{name: "b/SKILL.md", content: md}), "skill_missing@-"},
		"no SKILL.md":    {makeZip(t, entry{name: "readme.md", content: "x"}), "skill_missing@-"},
		"file too large": {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: "big.md", content: strings.Repeat("a", config.MaxSkillFileBytes+1)}), "skill_too_large@big.md"},
		"declared total": {makeZip(t, entry{name: "SKILL.md", content: md}, entry{name: "a.md", content: strings.Repeat("a", config.MaxSkillFileBytes)},
			entry{name: "b.md", content: strings.Repeat("a", config.MaxSkillFileBytes)}, entry{name: "c.md", content: strings.Repeat("a", config.MaxSkillFileBytes)},
			entry{name: "d.md", content: strings.Repeat("a", config.MaxSkillFileBytes)}, entry{name: "e.md", content: "x"}), "skill_too_large@-"},
	}
	for name, c := range cases {
		if got := zipCodes(t, c.data); !slices.Contains(got, c.want) {
			t.Errorf("%s: refused with %v, want %s", name, got, c.want)
		}
	}
}

func TestAZipBombWhoseHeaderUnderstatesItsSizeIsRefusedNotUnpacked(t *testing.T) {
	// The entry claims 10 bytes and inflates to 8 MiB, which a few KiB of zip can carry.
	body := strings.Repeat("a", 8<<20)
	var packed bytes.Buffer
	fw, _ := flate.NewWriter(&packed, flate.BestCompression)
	_, _ = fw.Write([]byte(body))
	_ = fw.Close()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	raw, err := w.CreateRaw(&zip.FileHeader{Name: "SKILL.md", Method: zip.Deflate, CRC32: crc32.ChecksumIEEE([]byte(body)),
		CompressedSize64: uint64(packed.Len()), UncompressedSize64: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = raw.Write(packed.Bytes())
	_ = w.Close()
	if out.Len() > config.MaxSkillImportBytes {
		t.Fatalf("the bomb is %d bytes, which the import would refuse for its size alone", out.Len())
	}
	if got := zipCodes(t, out.Bytes()); !slices.Contains(got, "skill_not_zip@SKILL.md") {
		t.Fatalf("the bomb was refused with %v, want skill_not_zip@SKILL.md", got)
	}
}

func TestABinaryFileInAZipIsRefusedByTheChecks(t *testing.T) {
	files, err := ReadZip(makeZip(t, entry{name: "SKILL.md", content: valid().Content}, entry{name: "logo.png", content: "\x89PNG\x00\xff"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := Check(files); !slices.Contains(codes(problems), "skill_binary@logo.png") {
		t.Fatalf("Check = %v, want skill_binary@logo.png", codes(problems))
	}
}
