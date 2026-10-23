package toolstub

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// What the story workflow writes at Exciter (#130): requirements made and revised, linked to Armature issues and
// sent for review, kept per tenant and answered as Exciter answers them.

// Exciter's bounds and words for the writes, as its API has them.
const (
	searchLimit    = 20
	searchLimitMax = 100
	revisionClash  = "Somebody saved a newer revision of %s while you were editing. Compare it with your changes, then save again over it."
)

var (
	priorities        = []string{"must", "should", "could", "wont"}
	armatureRelations = []string{"implements", "verifies", "relates"}
)

func (s *Stub) createRequirement(c *call, who *caller, args map[string]json.RawMessage) any {
	project, title := strings.TrimSpace(stringArg(args, "projectKey")), strings.TrimSpace(stringArg(args, "title"))
	switch {
	case project == "":
		return refusal("Name the project to create the requirement in, as projectKey.")
	case title == "":
		return refusal("Give the requirement a title.")
	}
	kind := stringArg(args, "kind")
	if kind == "" {
		kind = "requirement"
	}
	number := 1
	for _, r := range readableRequirements(c.t, who) {
		if r.project == project && r.number >= number {
			number = r.number + 1
		}
	}
	at := s.stubNow()
	req := &storedRequirement{key: project + "-" + strconv.Itoa(number), project: project, kind: kind, title: title, number: number,
		revision: 1, labels: []string{}, createdBy: who.username, updatedBy: who.username, createdAt: at, updatedAt: at, created: true}
	if refused := req.apply(args); refused != "" {
		return refusal(refused)
	}
	if refused := req.place(c.t, who, args["place"]); refused != "" {
		return refusal(refused)
	}
	c.t.written.requirements = append(c.t.written.requirements, req)
	return answered(map[string]any{"requirement": req.answer()})
}

// updateRequirement revises a requirement over the revision the caller read, as Exciter refuses one over another.
func (s *Stub) updateRequirement(c *call, who *caller, args map[string]json.RawMessage) any {
	req := findRequirement(c.t, who, stringArg(args, "reqKey"))
	if req == nil {
		return refusal(requirementNotFound)
	}
	var base *int
	_ = json.Unmarshal(args["baseRevision"], &base)
	switch {
	case base == nil:
		return refusal(baseRevisionMissing)
	case *base != req.revision:
		return refusal(fmt.Sprintf(revisionClash, req.key))
	}
	if !req.created {
		// A requirement of the options becomes the tenant's own once written, so the next read sees the change.
		req.created = true
		c.t.written.requirements = append(c.t.written.requirements, req)
	}
	next := *req
	if title := stringArg(args, "title"); title != "" {
		next.title = title
	}
	if refused := next.apply(args); refused != "" {
		return refusal(refused)
	}
	next.revision++
	next.updatedBy, next.updatedAt = who.username, s.stubNow()
	*req = next
	return answered(map[string]any{"requirement": req.answer()})
}

// apply takes the fields create and update share from the arguments, answering what Exciter would refuse.
func (r *storedRequirement) apply(args map[string]json.RawMessage) string {
	if _, sent := args["statementMarkdown"]; sent {
		r.statement = stringArg(args, "statementMarkdown")
	}
	if _, sent := args["rationaleMarkdown"]; sent {
		r.rationale = stringArg(args, "rationaleMarkdown")
	}
	if raw, sent := args["priority"]; sent {
		var p *string
		if err := json.Unmarshal(raw, &p); err != nil || (p != nil && !slices.Contains(priorities, *p)) {
			return "A priority is one of " + strings.Join(priorities, ", ") + "."
		}
		r.priority = p
	}
	if raw, sent := args["labels"]; sent {
		var labels []string
		if err := json.Unmarshal(raw, &labels); err != nil {
			return "Send the labels as a list of names."
		}
		r.labels = append([]string{}, labels...)
	}
	if !slices.Contains([]string{"requirement", "section"}, r.kind) {
		return "A kind is requirement or section."
	}
	return r.applyType(args)
}

// searchRequirements finds the requirements the caller may read whose title or statement holds every word asked,
// in the order they were made; Exciter's phrases, either-or and ranking are more than a test needs.
func searchRequirements(t *tenant, who *caller, args map[string]json.RawMessage) any {
	var projects, labels []string
	_ = json.Unmarshal(args["project"], &projects)
	_ = json.Unmarshal(args["label"], &labels)
	limit, offset := searchLimit, 0
	_ = json.Unmarshal(args["limit"], &limit)
	_ = json.Unmarshal(args["offset"], &offset)
	if limit < 1 || limit > searchLimitMax || offset < 0 {
		return refusal("Ask for 1 to " + strconv.Itoa(searchLimitMax) + " hits from an offset of 0 or later.")
	}
	words := strings.Fields(strings.ToLower(stringArg(args, "q")))
	hits := []map[string]any{}
	for _, r := range readableRequirements(t, who) {
		text := strings.ToLower(r.title + " " + r.statement)
		if (len(projects) > 0 && !slices.Contains(projects, r.project)) ||
			(len(labels) > 0 && !slices.ContainsFunc(labels, func(l string) bool { return slices.Contains(r.labels, l) })) ||
			slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			continue
		}
		snippet := []map[string]any{}
		if r.statement != "" {
			snippet = append(snippet, map[string]any{"text": r.statement, "match": false})
		}
		hits = append(hits, map[string]any{"requirement": r.ref(), "projectName": r.project,
			"title": []map[string]any{{"text": r.title, "match": false}}, "snippet": snippet, "labels": r.labels,
			"updatedAt": r.updatedAt, "updatedByName": r.updatedBy})
	}
	total := len(hits)
	hits = hits[min(offset, total):min(offset+limit, total)]
	// Exciter's glossary terms that match; the stub keeps no glossary.
	return answered(map[string]any{"hits": hits, "limit": limit, "offset": offset, "total": total, "terms": []any{}})
}

func listArmatureLinks(t *tenant, who *caller, args map[string]json.RawMessage) any {
	req := findRequirement(t, who, stringArg(args, "reqKey"))
	if req == nil {
		return refusal(requirementNotFound)
	}
	links := t.written.armatureLinks[req.key]
	if links == nil {
		links = []map[string]any{}
	}
	return answered(map[string]any{"links": links})
}

// linkArmatureIssue links a requirement to an Armature issue by its key, or changes the relation of a link there.
func (s *Stub) linkArmatureIssue(c *call, who *caller, args map[string]json.RawMessage) any {
	req := findRequirement(c.t, who, stringArg(args, "reqKey"))
	issueKey, relation := strings.TrimSpace(stringArg(args, "issueKey")), stringArg(args, "relation")
	switch {
	case req == nil:
		return refusal(requirementNotFound)
	case issueKey == "":
		return refusal("Name the Armature issue to link, as issueKey.")
	case !slices.Contains(armatureRelations, relation):
		return refusal("A link's relation is one of " + strings.Join(armatureRelations, ", ") + ".")
	}
	links := c.t.written.armatureLinks[req.key]
	for _, link := range links {
		if link["issueKey"] == issueKey {
			link["relation"] = relation
			return answered(map[string]any{"link": link})
		}
	}
	link := map[string]any{"id": stubID(c.t.name, "armature-link", req.key, issueKey), "relation": relation, "issueKey": issueKey,
		"statusCategory": "todo", "statusStale": false, "sync": "synced", "error": "", "brokenAt": nil, "detachedAt": nil,
		"reviewedRevision": req.revision, "suspect": false, "createdByName": who.username, "createdAt": s.stubNow(), "via": via(c.t)}
	c.t.written.armatureLinks[req.key] = append(links, link)
	return answered(map[string]any{"link": link})
}

// requestReviews asks for a review of each revision named, refusing an item alone as Exciter's bulk call does.
func (s *Stub) requestReviews(c *call, who *caller, args map[string]json.RawMessage) any {
	var items []struct {
		Key      string `json:"key"`
		Revision int    `json:"revision"`
	}
	project := stringArg(args, "projectKey")
	if err := json.Unmarshal(args["items"], &items); err != nil || project == "" {
		return refusal("Name the project, as projectKey, and the requirements to review, as items of a key and a revision.")
	}
	results := []map[string]any{}
	for _, item := range items {
		req := findRequirement(c.t, who, item.Key)
		refused := func(code, message string) {
			results = append(results, map[string]any{"key": item.Key, "outcome": "refused", "review": nil,
				"error": map[string]any{"code": code, "message": message}})
		}
		switch {
		case req == nil || req.project != project:
			refused("not_found", requirementNotFound)
			continue
		case item.Revision != req.revision:
			refused("revision_conflict", fmt.Sprintf(revisionClash, req.key))
			continue
		}
		review := map[string]any{"id": stubID(c.t.name, "review", req.key, strconv.Itoa(req.revision)), "requirement": req.ref(),
			"revision": req.revision, "state": "pending", "required": 1, "approvals": 0, "approvers": []any{}, "decisions": []any{},
			"comment": stringArg(args, "comment"), "requestedByName": who.username, "via": via(c.t), "requestedAt": s.stubNow(),
			"finishedAt": nil, "authorName": req.createdBy, "can": map[string]any{"decide": false, "withdraw": true}, "signed": false,
			"meanings": map[string]any{}, "session": nil}
		id, _ := review["id"].(string)
		req.pendingReview = &id
		c.t.written.reviews = append(c.t.written.reviews, review)
		results = append(results, map[string]any{"key": req.key, "outcome": "requested", "review": review, "error": nil})
	}
	return answered(map[string]any{"results": results})
}
