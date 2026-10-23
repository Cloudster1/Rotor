package toolstub

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// git's smart HTTP protocol, version 0 as a stateless exchange (gitprotocol-http(5), gitprotocol-pack(5)): the
// advertisements, a clone or fetch, shallow ones included, and a push with its status report.

const (
	flushPkt  = "0000"
	pktHexLen = 4
	stubAgent = "agent=armature-stub"
)

func pktLine(s string) []byte { return []byte(fmt.Sprintf("%04x%s", len(s)+pktHexLen, s)) }

// readPkt reads one pkt-line; flush is true for a flush or delimiter packet, and io.EOF ends the request.
func readPkt(r *bufio.Reader) (line string, flush bool, err error) {
	head := make([]byte, pktHexLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return "", false, err
	}
	n, err := strconv.ParseUint(string(head), 16, 16)
	if err != nil {
		return "", false, errBadRequest
	}
	if n < pktHexLen {
		return "", true, nil
	}
	data := make([]byte, n-pktHexLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", false, errBadRequest
	}
	return strings.TrimSuffix(string(data), "\n"), false, nil
}

var errBadRequest = errors.New("the request is not git's smart protocol")

// advertise is a service's reference advertisement: HEAD and every ref, the first carrying the capabilities.
func (g *gitRepo) advertise(service string) []byte {
	var out bytes.Buffer
	out.Write(pktLine("# service=" + service + "\n"))
	out.WriteString(flushPkt)
	caps := "report-status delete-refs ofs-delta " + stubAgent
	if service == serviceUploadPack {
		caps = "shallow no-progress symref=HEAD:refs/heads/" + g.DefaultBranch + " " + stubAgent
	}
	names := make([]string, 0, len(g.refs))
	for name := range g.refs {
		names = append(names, name)
	}
	slices.Sort(names)
	first := true
	line := func(id, name string) {
		if first {
			out.Write(pktLine(id + " " + name + "\x00" + caps + "\n"))
			first = false
			return
		}
		out.Write(pktLine(id + " " + name + "\n"))
	}
	if head, ok := g.refs["refs/heads/"+g.DefaultBranch]; ok && service == serviceUploadPack {
		line(head, "HEAD")
	}
	for _, name := range names {
		line(g.refs[name], name)
	}
	if first {
		line(zeroID, "capabilities^{}")
	}
	out.WriteString(flushPkt)
	return out.Bytes()
}

// uploadPack answers one stateless round of a clone or fetch: the shallow boundary when deepening, then an ACK or
// NAK, and after done the pack of what the wants reach that the haves do not.
func (g *gitRepo) uploadPack(body io.Reader) ([]byte, error) {
	r := bufio.NewReader(body)
	var wants []string
	depth := 0
	for {
		line, flush, err := readPkt(r)
		if errors.Is(err, io.EOF) && len(wants) == 0 {
			return nil, nil
		}
		if err != nil {
			return nil, errBadRequest
		}
		if flush {
			break
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 2 && fields[0] == "want":
			if _, ok := g.objects[fields[1]]; !ok {
				return nil, fmt.Errorf("%w: no object %s", errBadRequest, fields[1])
			}
			wants = append(wants, fields[1])
		case len(fields) == 2 && fields[0] == "deepen":
			if depth, err = strconv.Atoi(fields[1]); err != nil || depth < 1 {
				return nil, errBadRequest
			}
		case len(fields) == 2 && fields[0] == "shallow":
		default:
			return nil, fmt.Errorf("%w: %q is not offered", errBadRequest, line)
		}
	}
	var out bytes.Buffer
	commits, shallow := g.commitsFrom(wants, depth)
	if depth > 0 {
		for _, id := range shallow {
			out.Write(pktLine("shallow " + id + "\n"))
		}
		out.WriteString(flushPkt)
	}
	common := ""
	for {
		line, flush, err := readPkt(r)
		if errors.Is(err, io.EOF) {
			// The first round of a deepening fetch ends with the boundary; the next one asks again, with done.
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, errBadRequest
		}
		if have, ok := strings.CutPrefix(line, "have "); ok && common == "" {
			if _, known := g.objects[have]; known {
				common = have
			}
		}
		if flush || line == "done" {
			if common != "" {
				out.Write(pktLine("ACK " + common + "\n"))
			} else {
				out.Write(pktLine("NAK\n"))
			}
		}
		if line == "done" {
			break
		}
		if flush {
			return out.Bytes(), nil
		}
	}
	if common != "" {
		// What the client has stays out of the pack: every commit it reaches.
		seen, _ := g.commitsFrom([]string{common}, 0)
		commits = slices.DeleteFunc(commits, func(id string) bool { return slices.Contains(seen, id) })
	}
	pack, err := writePack(g.objects, g.closure(commits))
	if err != nil {
		return nil, err
	}
	out.Write(pack)
	return out.Bytes(), nil
}

// commitsFrom is every commit the wants reach, at most depth deep when depth is set, and the boundary at the depth.
func (g *gitRepo) commitsFrom(wants []string, depth int) (commits, shallow []string) {
	level := map[string]int{}
	queue := slices.Clone(wants)
	for _, w := range wants {
		level[w] = 1
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		o, ok := g.objects[id]
		if !ok || o.kind != kindCommit {
			continue
		}
		commits = append(commits, id)
		_, parents := commitLinks(o.data)
		if depth > 0 && level[id] >= depth {
			// As git's upload-pack, a commit at the depth is the boundary whether or not it has parents.
			shallow = append(shallow, id)
			continue
		}
		for _, p := range parents {
			if _, seen := level[p]; !seen {
				level[p] = level[id] + 1
				queue = append(queue, p)
			}
		}
	}
	return commits, shallow
}

// closure is the commits and every tree and blob they hold, each once.
func (g *gitRepo) closure(commits []string) []string {
	out := slices.Clone(commits)
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		o, ok := g.objects[id]
		if !ok || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
		if o.kind == kindTree {
			ids, kinds := treeEntries(o.data)
			for i, child := range ids {
				if kinds[i] != kindCommit {
					walk(child)
				}
			}
		}
	}
	for _, c := range commits {
		tree, _ := commitLinks(g.objects[c].data)
		walk(tree)
	}
	return out
}

// refUpdate is one command of a push.
type refUpdate struct{ old, new, ref string }

// pushRequest is a push as git sends it: its commands, the capabilities it asked for, and its pack.
type pushRequest struct {
	updates []refUpdate
	asked   []string
	pack    []byte
	signed  bool
}

func readPush(body []byte) (*pushRequest, error) {
	r := bufio.NewReader(bytes.NewReader(body))
	push := &pushRequest{}
	for {
		line, flush, err := readPkt(r)
		if err != nil {
			return nil, errBadRequest
		}
		if flush {
			break
		}
		if strings.HasPrefix(line, "shallow ") {
			continue
		}
		if strings.HasPrefix(line, "push-cert") {
			push.signed = true
			continue
		}
		command, caps, _ := strings.Cut(line, "\x00")
		if caps != "" {
			push.asked = strings.Fields(caps)
		}
		fields := strings.Fields(command)
		if len(fields) != 3 || len(fields[0]) != len(zeroID) || len(fields[1]) != len(zeroID) {
			return nil, errBadRequest
		}
		push.updates = append(push.updates, refUpdate{old: fields[0], new: fields[1], ref: fields[2]})
	}
	push.pack, _ = io.ReadAll(r)
	return push, nil
}

// report is receive-pack's status report: each ref ok, or refused with its reason on one line.
func report(push *pushRequest, reasons map[string]string) []byte {
	var out bytes.Buffer
	out.Write(pktLine("unpack ok\n"))
	for _, u := range push.updates {
		if reason := reasons[u.ref]; reason != "" {
			out.Write(pktLine("ng " + u.ref + " " + strings.ReplaceAll(reason, "\n", " ") + "\n"))
		} else {
			out.Write(pktLine("ok " + u.ref + "\n"))
		}
	}
	out.WriteString(flushPkt)
	return out.Bytes()
}
