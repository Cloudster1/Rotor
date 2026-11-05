package toolstub

import (
	"encoding/json"
	"slices"
	"strings"
)

// Where a feature workflow finds its requirements and what changed (#140): a requirement placed under another in
// the project's tree, and its links read back, suspect where an end moved on since it was linked.

// linkNotFound is said before the id of a link the caller cannot clear.
const linkNotFound = "There is no link you may clear, or you may not see both its ends. Take its id from list_links: "

const placeNotOurs = "That parent is not a requirement or section of this project. Pick one from its tree, or none for the top."

// place puts a new requirement under the parent its place names, which must be one of its project the caller reads.
func (r *storedRequirement) place(t *tenant, who *caller, raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var at struct {
		ParentID *string `json:"parentId"`
	}
	if json.Unmarshal(raw, &at) != nil {
		return "Send the place as a parentId and an afterId, each an id or null."
	}
	if at.ParentID == nil {
		return ""
	}
	found := slices.ContainsFunc(readableRequirements(t, who), func(p *storedRequirement) bool {
		return p.project == r.project && p.id() == *at.ParentID
	})
	if !found {
		return placeNotOurs
	}
	r.parentID = at.ParentID
	return ""
}

// listLinks answers a requirement's links in both directions, as Exciter's list_links: each with the phrase read
// from this end, the requirement at the other, and the ends its type watches that moved past the linked revision.
func listLinks(t *tenant, who *caller, args map[string]json.RawMessage) any {
	req := findRequirement(t, who, stringArg(args, "reqKey"))
	if req == nil {
		return refusal(requirementNotFound)
	}
	links := []map[string]any{}
	for _, l := range t.written.links {
		if l.source != req.key && l.target != req.key {
			continue
		}
		at := slices.IndexFunc(linkTypes, func(lt linkType) bool { return lt.key == l.typeKey })
		source, target := findRequirement(t, who, l.source), findRequirement(t, who, l.target)
		if at < 0 || source == nil || target == nil {
			continue
		}
		links = append(links, traceLink(t, l, linkTypes[at], req.key, source, target))
	}
	return answered(map[string]any{"links": links})
}

// suspectEnds are the ends of a link its type watches whose requirement has a newer revision than the link names.
func suspectEnds(lt linkType, l requirementLink, source, target *storedRequirement) []map[string]any {
	out := []map[string]any{}
	if (lt.suspectEnd == "source" || lt.suspectEnd == "both") && source.revision != l.sourceRevision {
		out = append(out, map[string]any{"end": "source", "reviewed": l.sourceRevision, "current": source.revision})
	}
	if (lt.suspectEnd == "target" || lt.suspectEnd == "both") && target.revision != l.targetRevision {
		out = append(out, map[string]any{"end": "target", "reviewed": l.targetRevision, "current": target.revision})
	}
	return out
}

// traceLink is a link in Exciter's TraceLink shape as read from the requirement with the key from.
func traceLink(t *tenant, l requirementLink, lt linkType, from string, source, target *storedRequirement) map[string]any {
	direction, other, phrase := "outgoing", target, lt.forward
	if l.source != from {
		direction, other, phrase = "incoming", source, lt.backward
	}
	return map[string]any{"id": l.id, "type": lt.answer(), "direction": direction, "phrase": phrase,
		"other": other.ref(), "sourceRevision": l.sourceRevision, "targetRevision": l.targetRevision,
		"suspect": suspectEnds(lt, l, source, target), "createdAt": l.createdAt, "createdByName": l.createdBy, "via": via(t)}
}

// clearSuspectLink confirms a link against the revisions of its ends as the caller read them, which must be the
// current ones, so a link is never cleared on a read that has gone stale; the answer reads from the source.
func clearSuspectLink(t *tenant, who *caller, args map[string]json.RawMessage) any {
	id := stringArg(args, "linkID")
	at := slices.IndexFunc(t.written.links, func(l requirementLink) bool { return l.id == id })
	if at < 0 {
		return refusal(linkNotFound + id)
	}
	l := t.written.links[at]
	lti := slices.IndexFunc(linkTypes, func(lt linkType) bool { return lt.key == l.typeKey })
	source, target := findRequirement(t, who, l.source), findRequirement(t, who, l.target)
	if lti < 0 || source == nil || target == nil {
		return refusal(linkNotFound + id)
	}
	var sourceRevision, targetRevision int
	_ = json.Unmarshal(args["sourceRevision"], &sourceRevision)
	_ = json.Unmarshal(args["targetRevision"], &targetRevision)
	switch {
	case strings.TrimSpace(stringArg(args, "comment")) == "":
		return refusal("Say in a comment why the link still holds.")
	case sourceRevision != source.revision || targetRevision != target.revision:
		return refusal("Those revisions are not the current ones of the link's ends. Read the link again with list_links and name the revisions you reviewed.")
	}
	l.sourceRevision, l.targetRevision = sourceRevision, targetRevision
	t.written.links[at] = l
	return answered(map[string]any{"link": traceLink(t, l, linkTypes[lti], l.source, source, target)})
}
