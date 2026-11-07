package toolstub

import (
	"encoding/json"
	"hash/fnv"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Armature's label tools (#270): list_labels reads the installation's labels, which every project shares, and
// bulk_edit_issues is how an MCP client labels an issue, since Armature has no tool for one.
const (
	ListLabelsTool     = "list_labels"
	BulkEditIssuesTool = "bulk_edit_issues"
)

// Armature's bounds and words for labels and bulk changes, from its label and bulk packages.
const (
	defaultLabelPage  = config.ToolStubLabelPage
	maxLabelPage      = config.ToolStubMaxLabelPage
	maxBulkSelection  = config.ToolStubMaxBulkSelection
	maxLabelNameRunes = config.ToolStubMaxLabelName
)

var badLabelName = "A label is one word of up to " + strconv.Itoa(maxLabelNameRunes) + " characters, with no spaces"

// labelColors is Armature's palette, from which a label it coins takes a colour by its name.
var (
	labelColors      = []string{"gray", "red", "orange", "amber", "green", "teal", "blue", "purple", "pink"}
	labelNamePattern = regexp.MustCompile(`^\S+$`)
)

// coinedLabel is a label a bulk change made because no label of its name existed, as Armature makes one.
type coinedLabel struct {
	name string
	at   string
}

// stubLabel is one of a tenant's labels, in the spelling it was first given.
type stubLabel struct {
	name, createdAt string
}

// tenantLabels are the tenant's labels, names unique regardless of case and the first spelling kept as Armature's
// lower(name) index keeps it: the options' labels, those on its issues, then those writes coined.
func tenantLabels(t *tenant) []stubLabel {
	seen := map[string]bool{}
	var out []stubLabel
	add := func(name, at string) {
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			out = append(out, stubLabel{name: name, createdAt: at})
		}
	}
	at := stubTime.Format(time.RFC3339)
	for _, name := range t.opts.Labels {
		add(name, at)
	}
	keys := make([]string, 0, len(t.opts.Issues))
	for key := range t.opts.Issues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, name := range t.opts.Issues[key].Labels {
			add(name, at)
		}
	}
	for _, c := range t.written.coined {
		add(c.name, c.at)
	}
	return out
}

// issueLabels are the label names on an issue: what a write left there, or else what the options gave it.
func issueLabels(t *tenant, key string) []string {
	if names, ok := t.written.issueLabels[key]; ok {
		return names
	}
	return t.opts.Issues[key].Labels
}

// labelRefs are an issue's labels in Armature's LabelRef shape, ordered by name regardless of case, as it sorts them.
func labelRefs(t *tenant, key string) []any {
	names := slices.Clone(issueLabels(t, key))
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	out := []any{}
	for _, name := range names {
		out = append(out, map[string]any{"id": labelID(t, name), "name": name, "color": labelColor(name)})
	}
	return out
}

func labelID(t *tenant, name string) string { return stubID(t.name, "label", strings.ToLower(name)) }

// labelColor is Armature's ColorFor: an FNV-1a hash of the name, case aside, into the palette.
func labelColor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return labelColors[int(h.Sum32()%uint32(len(labelColors)))]
}

// listLabels answers GET /labels: names holding q, case aside, those starting with it first, each alphabetically,
// with how many of the tenant's issues carry each.
func listLabels(t *tenant, args map[string]json.RawMessage) map[string]any {
	q := strings.ToLower(strings.TrimSpace(stringArg(args, "q")))
	limit, offset := intArg(args, "limit"), max(intArg(args, "offset"), 0)
	if limit <= 0 || limit > maxLabelPage {
		limit = defaultLabelPage
	}
	used := map[string]int{}
	for key := range t.opts.Issues {
		for _, name := range issueLabels(t, key) {
			used[strings.ToLower(name)]++
		}
	}
	var matched []stubLabel
	for _, l := range tenantLabels(t) {
		if strings.Contains(strings.ToLower(l.name), q) {
			matched = append(matched, l)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := strings.ToLower(matched[i].name), strings.ToLower(matched[j].name)
		if pa, pb := strings.HasPrefix(a, q), strings.HasPrefix(b, q); pa != pb {
			return pa
		}
		return a < b
	})
	labels := []any{}
	for _, l := range matched[min(offset, len(matched)):min(offset+limit, len(matched))] {
		labels = append(labels, map[string]any{"id": labelID(t, l.name), "name": l.name, "color": labelColor(l.name),
			"issueCount": used[strings.ToLower(l.name)], "createdAt": l.createdAt, "updatedAt": l.createdAt})
	}
	return map[string]any{"labels": labels, "total": len(matched), "limit": limit, "offset": offset}
}

// bulkEdit answers POST /issues/bulk for the one change the stub makes, adding labels: each key on its own, an issue
// the caller cannot read refused as not found, and a label nobody made yet coined, as Armature coins it.
func (s *Stub) bulkEdit(c *call, who *caller, args map[string]json.RawMessage) any {
	var keys []string
	_ = json.Unmarshal(args["keys"], &keys)
	var change map[string]json.RawMessage
	_ = json.Unmarshal(args["change"], &change)
	var add []string
	_ = json.Unmarshal(change["addLabels"], &add)
	switch {
	case len(keys) == 0:
		return refusal("Choose at least one issue.")
	case len(keys) > maxBulkSelection:
		return refusal("Change at most " + strconv.Itoa(maxBulkSelection) + " issues at once; this is " + strconv.Itoa(len(keys)) + ".")
	case len(change) == 0:
		return refusal("Say what to change.")
	}
	for field := range change {
		if field != "addLabels" {
			return refusal("The test double changes only addLabels in a bulk edit; it does not answer " + field + ".")
		}
	}
	applied, refused := []string{}, []any{}
	for _, key := range keys {
		issue, ok := c.t.opts.Issues[key]
		if !ok || !slices.Contains(issue.Readers, who.username) {
			refused = append(refused, map[string]any{"key": key, "reason": issueNotFound})
			continue
		}
		if reason := s.addLabels(c.t, key, add); reason != "" {
			refused = append(refused, map[string]any{"key": key, "reason": reason})
			continue
		}
		applied = append(applied, key)
	}
	return answered(map[string]any{"applied": applied, "refused": refused, "moved": []any{}})
}

// addLabels puts the names on the issue in the spelling the tenant knows them by, coining those it does not know;
// a name that is not one word refuses the whole change to that issue.
func (s *Stub) addLabels(t *tenant, key string, add []string) string {
	known := map[string]string{}
	for _, l := range tenantLabels(t) {
		known[strings.ToLower(l.name)] = l.name
	}
	names := slices.Clone(issueLabels(t, key))
	var coin []string
	for _, raw := range add {
		name := strings.TrimSpace(raw)
		if name == "" || !labelNamePattern.MatchString(name) || len([]rune(name)) > maxLabelNameRunes {
			return badLabelName + ": " + strconv.Quote(raw) + "."
		}
		spelled, ok := known[strings.ToLower(name)]
		if !ok {
			spelled, known[strings.ToLower(name)] = name, name
			coin = append(coin, name)
		}
		if !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, spelled) }) {
			names = append(names, spelled)
		}
	}
	for _, name := range coin {
		t.written.coined = append(t.written.coined, coinedLabel{name: name, at: s.stubNow()})
	}
	if names == nil {
		names = []string{}
	}
	t.written.issueLabels[key] = names
	return ""
}

// intArg is an integer argument, or 0 when it is missing or not a whole number.
func intArg(args map[string]json.RawMessage, name string) int {
	var v int
	_ = json.Unmarshal(args[name], &v)
	return v
}
