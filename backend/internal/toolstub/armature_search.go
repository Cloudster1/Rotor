package toolstub

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// ListProjectIssuesTool reads a project's issues, narrowed by an NQL query in q (#271).
const ListProjectIssuesTool = "list_project_issues"

// Armature's pages of issues, and the status each category of the stub's issues is in.
const (
	defaultIssuePage = config.ToolStubIssuePage
	maxIssuePage     = config.ToolStubMaxIssuePage
	categoryDone     = "done"
	day              = 24 * time.Hour
)

var (
	statusNames = map[string]string{"todo": "To Do", "in_progress": "In Progress", categoryDone: "Done"}
	statusOrder = []string{"todo", "in_progress", categoryDone}
	// The stub reads the part of NQL a digest asks for: comparisons joined by AND, then one ORDER BY field.
	nqlOrderBy   = regexp.MustCompile(`(?i)^(.*?)\s*\bORDER\s+BY\s+(\w+)(?:\s+(ASC|DESC))?\s*$`)
	nqlAnd       = regexp.MustCompile(`(?i)\s+AND\s+`)
	nqlCondition = regexp.MustCompile(`^(\w+)\s*(>=|<=|!=|=|>|<)\s*(\S+)$`)
	nqlDuration  = regexp.MustCompile(`^([+-]?\d+)([hdw])$`)
	nqlUnits     = map[string]time.Duration{"h": time.Hour, "d": day, "w": 7 * day}
)

// nqlUnread is the stub's answer to NQL it does not read; Armature reads it, so the test asks too much of the stub.
const nqlUnread = "The test double reads only created or resolved against a duration such as -7d, statusCategory = or !=, " +
	"joined by AND, and one ORDER BY created, resolved or key."

// issueQuery is a parsed q: whether an issue matches, and the field and direction it is ordered by.
type issueQuery struct {
	match []func(Issue) bool
	order string
	desc  bool
}

// parseNQL reads q as Armature would for the conditions the stub knows, relative durations counted from now.
func parseNQL(q string, now time.Time) (*issueQuery, bool) {
	out := &issueQuery{order: "key"}
	q = strings.TrimSpace(q)
	if m := nqlOrderBy.FindStringSubmatch(q); m != nil {
		q, out.order, out.desc = m[1], strings.ToLower(m[2]), strings.EqualFold(m[3], "desc")
		if !slices.Contains([]string{"created", "resolved", "key"}, out.order) {
			return nil, false
		}
	}
	if q == "" {
		return out, true
	}
	for _, part := range nqlAnd.Split(q, -1) {
		m := nqlCondition.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil {
			return nil, false
		}
		field, op, value := strings.ToLower(m[1]), m[2], strings.Trim(m[3], `"`)
		switch field {
		case "created", "resolved":
			d := nqlDuration.FindStringSubmatch(value)
			if d == nil {
				return nil, false
			}
			n, _ := strconv.Atoi(d[1])
			at := now.Add(time.Duration(n) * nqlUnits[d[2]])
			out.match = append(out.match, func(is Issue) bool { return compareTime(issueTime(is, field), op, at) })
		case "statuscategory":
			if (op != "=" && op != "!=") || statusNames[value] == "" {
				return nil, false
			}
			out.match = append(out.match, func(is Issue) bool { return (issueCategory(is) == value) == (op == "=") })
		default:
			return nil, false
		}
	}
	return out, true
}

// compareTime compares as SQL does, so an issue never resolved matches no condition on resolved.
func compareTime(t *time.Time, op string, at time.Time) bool {
	if t == nil {
		return false
	}
	switch op {
	case ">=":
		return !t.Before(at)
	case ">":
		return t.After(at)
	case "<=":
		return !t.After(at)
	case "<":
		return t.Before(at)
	case "=":
		return t.Equal(at)
	}
	return !t.Equal(at)
}

func issueTime(is Issue, field string) *time.Time {
	if field == "resolved" {
		return is.ResolvedAt
	}
	created := stubTime
	if is.CreatedAt != nil {
		created = *is.CreatedAt
	}
	return &created
}

func issueCategory(is Issue) string {
	if is.Category == "" {
		return "todo"
	}
	return is.Category
}

// listProjectIssues answers GET /projects/{projectKey}/issues: the project's issues the caller reads that match q,
// a page at a time, as Armature ANDs the query with the project.
func (s *Stub) listProjectIssues(c *call, who *caller, args map[string]json.RawMessage) any {
	project, _, _ := strings.Cut(strings.TrimSpace(stringArg(args, "projectKey")), "-")
	if project == "" {
		return refusal("Name the project, as projectKey.")
	}
	query, ok := parseNQL(stringArg(args, "q"), s.now())
	if !ok {
		return refusal(nqlUnread)
	}
	limit, offset := intArg(args, "limit"), max(intArg(args, "offset"), 0)
	if limit <= 0 || limit > maxIssuePage {
		limit = defaultIssuePage
	}
	var keys []string
	for key, is := range c.t.opts.Issues {
		if p, _, _ := strings.Cut(key, "-"); p != project || !slices.Contains(is.Readers, who.username) {
			continue
		}
		if !slices.ContainsFunc(query.match, func(m func(Issue) bool) bool { return !m(is) }) {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		n := query.compare(c.t.opts.Issues, a, b)
		if query.desc {
			return -n
		}
		return n
	})
	issues := []any{}
	for _, key := range keys[min(offset, len(keys)):min(offset+limit, len(keys))] {
		issues = append(issues, armatureIssue(key, c.t.opts.Issues[key], len(c.t.written.comments[key]), labelRefs(c.t, key)))
	}
	return answered(map[string]any{"issues": issues, "total": len(keys), "limit": limit, "offset": offset})
}

// compare orders two issues by the query's field, then by key, with an issue never resolved last.
func (q *issueQuery) compare(issues map[string]Issue, a, b string) int {
	if q.order != "key" {
		ta, tb := issueTime(issues[a], q.order), issueTime(issues[b], q.order)
		switch {
		case ta == nil && tb != nil:
			return 1
		case ta != nil && tb == nil:
			return -1
		case ta != nil && !ta.Equal(*tb):
			return ta.Compare(*tb)
		}
	}
	return issueNumberCompare(a, b)
}

// issueNumberCompare orders keys as Armature does, by project then by number, so CP-9 comes before CP-10.
func issueNumberCompare(a, b string) int {
	pa, na, _ := strings.Cut(a, "-")
	pb, nb, _ := strings.Cut(b, "-")
	if pa != pb {
		return strings.Compare(pa, pb)
	}
	ia, _ := strconv.Atoi(na)
	ib, _ := strconv.Atoi(nb)
	return ia - ib
}
