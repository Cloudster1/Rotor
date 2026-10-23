package toolstub

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1" //nolint:gosec // git names its objects by SHA-1; nothing here is a security check.
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// git's objects and packs, as much as the git proxy's stand-in needs to serve a clone and take a push from the real
// git client: whole objects out, whole or delta objects in (gitformat-pack(5)).

// Object kinds and their numbers in a pack.
const (
	kindCommit   = "commit"
	kindTree     = "tree"
	kindBlob     = "blob"
	kindTag      = "tag"
	packOfsDelta = 6
	packRefDelta = 7
	// zeroID is the id git sends for a ref that does not exist yet, or no longer will.
	zeroID     = "0000000000000000000000000000000000000000"
	idBytes    = sha1.Size
	packHeader = 12
	packV2     = 2
	// deltaMaxCopy is the size a copy instruction of size zero stands for.
	deltaMaxCopy = 0x10000
)

var packKinds = map[int]string{1: kindCommit, 2: kindTree, 3: kindBlob, 4: kindTag}

// gitObject is one object: its kind and its content.
type gitObject struct {
	kind string
	data []byte
}

// objectID is the object's name, the SHA-1 of its header and content.
func objectID(kind string, data []byte) string {
	h := sha1.New() //nolint:gosec // see the import
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// mustHex is an object id as the 20 bytes a tree holds; ids here are always made by objectID.
func mustHex(id string) []byte {
	raw, _ := hex.DecodeString(id)
	return raw
}

// commitLinks are a commit's tree and parents.
func commitLinks(data []byte) (tree string, parents []string) {
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			break
		}
		if rest, ok := strings.CutPrefix(line, "tree "); ok {
			tree = rest
		} else if rest, ok := strings.CutPrefix(line, "parent "); ok {
			parents = append(parents, rest)
		}
	}
	return tree, parents
}

// treeEntries are the ids a tree names, of blobs, trees and, for submodules, commits it does not hold.
func treeEntries(data []byte) (ids []string, kinds []string) {
	for len(data) > 0 {
		space := bytes.IndexByte(data, ' ')
		nul := bytes.IndexByte(data, 0)
		if space < 0 || nul < space || len(data) < nul+1+idBytes {
			return ids, kinds
		}
		mode := string(data[:space])
		ids = append(ids, hex.EncodeToString(data[nul+1:nul+1+idBytes]))
		switch {
		case mode == "40000":
			kinds = append(kinds, kindTree)
		case mode == "160000":
			kinds = append(kinds, kindCommit)
		default:
			kinds = append(kinds, kindBlob)
		}
		data = data[nul+1+idBytes:]
	}
	return ids, kinds
}

// writePack is a pack of whole objects, in the order given.
func writePack(objects map[string]gitObject, ids []string) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("PACK")
	_ = binary.Write(&out, binary.BigEndian, uint32(packV2))
	_ = binary.Write(&out, binary.BigEndian, uint32(len(ids)))
	for _, id := range ids {
		o := objects[id]
		number := 0
		for n, kind := range packKinds {
			if kind == o.kind {
				number = n
			}
		}
		size := len(o.data)
		b := byte(number<<4) | byte(size&0x0f)
		size >>= 4
		for size > 0 {
			out.WriteByte(b | 0x80)
			b = byte(size & 0x7f)
			size >>= 7
		}
		out.WriteByte(b)
		z := zlib.NewWriter(&out)
		if _, err := z.Write(o.data); err != nil {
			return nil, err
		}
		if err := z.Close(); err != nil {
			return nil, err
		}
	}
	sum := sha1.Sum(out.Bytes()) //nolint:gosec // see the import
	out.Write(sum[:])
	return out.Bytes(), nil
}

// counting reads a pack, knowing where it is in it, a byte at a time when inflating so zlib stops at its end.
type counting struct {
	r *bufio.Reader
	n int64
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *counting) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

// packEntry is an object of a pack as it was sent: whole, or a delta against a base named by id or by offset.
type packEntry struct {
	offset int64
	kind   string
	data   []byte
	delta  []byte
	baseID string
	baseAt int64
}

var errBadPack = errors.New("the pack cannot be read")

// readPack reads a pushed pack into whole objects, resolving deltas against the pack and against known, which
// holds what the repository has already, as a thin pack expects.
func readPack(raw []byte, known map[string]gitObject) (map[string]gitObject, error) {
	if len(raw) < packHeader+idBytes || string(raw[:4]) != "PACK" || binary.BigEndian.Uint32(raw[4:8]) != packV2 {
		return nil, errBadPack
	}
	count := binary.BigEndian.Uint32(raw[8:12])
	in := &counting{r: bufio.NewReader(bytes.NewReader(raw[packHeader:])), n: packHeader}
	entries := make([]*packEntry, 0, min(count, uint32(len(raw))))
	for range count {
		e, err := readEntry(in)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return resolve(entries, known)
}

func readEntry(in *counting) (*packEntry, error) {
	e := &packEntry{offset: in.n}
	c, err := in.ReadByte()
	if err != nil {
		return nil, errBadPack
	}
	kind, size, shift := int(c>>4)&7, int64(c&0x0f), 4
	for c&0x80 != 0 {
		if c, err = in.ReadByte(); err != nil || shift > 56 {
			return nil, errBadPack
		}
		size |= int64(c&0x7f) << shift
		shift += 7
	}
	switch kind {
	case packOfsDelta:
		c, err = in.ReadByte()
		back := int64(c & 0x7f)
		for err == nil && c&0x80 != 0 {
			c, err = in.ReadByte()
			back = (back+1)<<7 | int64(c&0x7f)
		}
		if err != nil || back <= 0 || back > e.offset {
			return nil, errBadPack
		}
		e.baseAt = e.offset - back
	case packRefDelta:
		id := make([]byte, idBytes)
		if _, err := io.ReadFull(in, id); err != nil {
			return nil, errBadPack
		}
		e.baseID = hex.EncodeToString(id)
	default:
		if e.kind = packKinds[kind]; e.kind == "" {
			return nil, errBadPack
		}
	}
	if size > config.ToolStubMaxObjectBytes {
		return nil, errBadPack
	}
	z, err := zlib.NewReader(in)
	if err != nil {
		return nil, errBadPack
	}
	data, err := io.ReadAll(io.LimitReader(z, size+1))
	if err != nil || int64(len(data)) != size || z.Close() != nil {
		return nil, errBadPack
	}
	if e.kind == "" {
		e.delta = data
	} else {
		e.data = data
	}
	return e, nil
}

// resolve turns every entry into a whole object, deltas once their base is whole, in as many passes as it takes.
func resolve(entries []*packEntry, known map[string]gitObject) (map[string]gitObject, error) {
	out := map[string]gitObject{}
	at := map[int64]*packEntry{}
	for _, e := range entries {
		at[e.offset] = e
		if e.kind != "" {
			out[objectID(e.kind, e.data)] = gitObject{kind: e.kind, data: e.data}
		}
	}
	for pending := len(entries); pending > 0; {
		progress, left := false, 0
		for _, e := range entries {
			if e.kind != "" {
				continue
			}
			base, ok := gitObject{}, false
			if e.baseID != "" {
				if base, ok = out[e.baseID]; !ok {
					base, ok = known[e.baseID]
				}
			} else if b := at[e.baseAt]; b != nil && b.kind != "" {
				base, ok = gitObject{kind: b.kind, data: b.data}, true
			}
			if !ok {
				left++
				continue
			}
			data, err := applyDelta(base.data, e.delta)
			if err != nil {
				return nil, err
			}
			e.kind, e.data, progress = base.kind, data, true
			out[objectID(e.kind, e.data)] = gitObject{kind: e.kind, data: e.data}
		}
		if left > 0 && !progress {
			return nil, errBadPack
		}
		pending = left
	}
	return out, nil
}

// applyDelta rebuilds an object from its base and a delta's copy and insert instructions.
func applyDelta(base, delta []byte) ([]byte, error) {
	varint := func() (int, bool) {
		n, shift := 0, 0
		for len(delta) > 0 && shift < 64 {
			c := delta[0]
			delta = delta[1:]
			n |= int(c&0x7f) << shift
			shift += 7
			if c&0x80 == 0 {
				return n, true
			}
		}
		return 0, false
	}
	source, ok1 := varint()
	target, ok2 := varint()
	if !ok1 || !ok2 || source != len(base) || target > config.ToolStubMaxObjectBytes {
		return nil, errBadPack
	}
	out := make([]byte, 0, target)
	for len(delta) > 0 {
		op := delta[0]
		delta = delta[1:]
		switch {
		case op&0x80 != 0:
			offset, size := 0, 0
			for i := range 4 {
				if op&(1<<i) != 0 && len(delta) > 0 {
					offset |= int(delta[0]) << (8 * i)
					delta = delta[1:]
				}
			}
			for i := range 3 {
				if op&(0x10<<i) != 0 && len(delta) > 0 {
					size |= int(delta[0]) << (8 * i)
					delta = delta[1:]
				}
			}
			if size == 0 {
				size = deltaMaxCopy
			}
			if offset+size > len(base) {
				return nil, errBadPack
			}
			out = append(out, base[offset:offset+size]...)
		case op != 0 && int(op) <= len(delta):
			out = append(out, delta[:op]...)
			delta = delta[op:]
		default:
			return nil, errBadPack
		}
	}
	if len(out) != target {
		return nil, errBadPack
	}
	return out, nil
}
