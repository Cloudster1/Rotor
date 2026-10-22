package toolstub

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Armature's tools as its MCP endpoint answers them (#129): each is its HTTP operation run as the caller, whose
// answer body is the structured result, so the shapes are those of api/tools/armature/openapi.json.

// Issue is an Armature issue of a tenant's options; readers are the usernames that may see it, as in Armature's
// projects, and reporter the username that filed it.
type Issue struct {
	Type                string   `json:"type,omitempty"`
	Summary             string   `json:"summary"`
	DescriptionMarkdown string   `json:"descriptionMarkdown,omitempty"`
	Reporter            string   `json:"reporter,omitempty"`
	Readers             []string `json:"readers"`
}

// The Armature tools the stub answers.
const (
	GetIssueTool        = "get_issue"
	ListCommentsTool    = "list_comments"
	AddCommentTool      = "add_comment"
	ListRemoteLinksTool = "list_remote_links"
	AddRemoteLinkTool   = "add_remote_link"
)

// Armature's words and bounds, as its API has them.
const (
	issueNotFound       = "That issue was not found."
	readOnlyToken       = "This token can only read. Make a token without the read-only mark to change anything."
	commentNeedsText    = "A comment needs some text."
	armatureUploadBytes = 50 << 20
	maxRemoteLinkURL    = 2048
	maxRemoteLinkTitle  = 255
	maxRemoteLinkSource = 100
	defaultIssueType    = "Story"
)

// armatureWrites are the scopes a connected app needs for each writing tool the stub answers, from Armature's
// appWrites; allows is how Armature words each in a refusal.
var (
	armatureWrites = map[string]string{AddCommentTool: ArmatureCommentWrite, AddRemoteLinkTool: ArmatureIssueWrite}
	allows         = map[string]string{ArmatureIssueWrite: "file and edit issues", ArmatureCommentWrite: "comment on issues"}
)

// callArmature runs one of Armature's tools as Armature does: the arguments checked as its router would take them,
// a write refused for a token that may not make it, and the operation's answer as the result.
func (s *Stub) callArmature(c *call, who *caller, tool map[string]any, arguments json.RawMessage) (any, *rpcError) {
	name, _ := tool["name"].(string)
	args := map[string]json.RawMessage{}
	_ = json.Unmarshal(arguments, &args)
	schema, _ := tool["inputSchema"].(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, r := range required {
		if key, _ := r.(string); key == "issueKey" && args[key] == nil {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "The argument " + key + " is required."}
		}
	}
	if readOnlyTool(tool) {
		for key := range args {
			if _, ok := properties[key]; !ok {
				return nil, &rpcError{Code: rpcInvalidParams, Message: "The tool does not take " + key + "."}
			}
		}
	} else if refused := armatureWriteRefused(who, name); refused != "" {
		return refusal(refused), nil
	}
	key := stringArg(args, "issueKey")
	issue, readable := c.t.opts.Issues[key]
	readable = readable && slices.Contains(issue.Readers, who.username)
	switch name {
	case WhoamiTool:
		return answered(armatureWhoami(c.t, who)), nil
	case GetIssueTool, ListCommentsTool, AddCommentTool, ListRemoteLinksTool, AddRemoteLinkTool:
		if !readable {
			return refusal(issueNotFound), nil
		}
	default:
		return refusal("The test double does not answer " + name + "; it is listed so clients see Armature's tools."), nil
	}
	written := c.t.written
	switch name {
	case GetIssueTool:
		return answered(map[string]any{"issue": armatureIssue(key, issue, len(written.comments[key]))}), nil
	case ListCommentsTool:
		return answered(map[string]any{"comments": orEmpty(written.comments[key])}), nil
	case ListRemoteLinksTool:
		return answered(map[string]any{"remoteLinks": orEmpty(written.remoteLinks[key])}), nil
	case AddCommentTool:
		return s.addComment(c, who, key, args), nil
	}
	return s.addRemoteLink(c, who, key, args), nil
}

// armatureWriteRefused is what Armature answers a write the caller may not make: a read-only token, or a connected
// app whose grant does not reach the call.
func armatureWriteRefused(who *caller, tool string) string {
	scope, answered := armatureWrites[tool]
	switch {
	case who.app == "" && who.readOnly:
		return readOnlyToken
	case who.app == "" || !answered || slices.Contains(who.scopes, scope):
		return ""
	case len(who.scopes) == 1 && who.scopes[0] == ArmatureRead:
		return who.app + " was allowed to read only. Connect it again and allow it to change issues."
	}
	return who.app + " was not allowed to " + allows[scope] + ". Connect it again and allow it to " + allows[scope] + "."
}

func orEmpty(list []map[string]any) []map[string]any {
	if list == nil {
		return []map[string]any{}
	}
	return list
}

// armatureUser is a person as Armature's UserRef names them, with the ids whoami gives them.
func armatureUser(username string) map[string]any {
	return map[string]any{"id": userID(username), "name": username, "email": username + "@suite.test"}
}

// armatureWhoami answers GET /auth/me: the person, the installation's limits and the calling token.
func armatureWhoami(t *tenant, who *caller) map[string]any {
	scopes := who.scopes
	if scopes == nil {
		scopes = []string{}
	}
	token := map[string]any{"id": stubID(t.name, "token", who.username), "name": "Rotor", "scopes": scopes,
		"createdAt": stubTime.Format(time.RFC3339), "projects": []string{}}
	if who.app != "" {
		token["app"] = map[string]any{"clientId": stubID(t.name, "client", who.clientID), "name": who.app}
	}
	return map[string]any{
		"principal": map[string]any{
			"user": map[string]any{"id": userID(who.username), "email": who.username + "@suite.test", "name": who.username,
				"timezone": "UTC", "locale": "en", "isActive": true},
			"role": "member", "installation": map[string]any{"id": stubID(t.name, "installation"), "name": t.name},
		},
		"limits": map[string]any{"uploadBytes": armatureUploadBytes},
		"token":  token,
	}
}

// armatureIssue is an issue of the options in Armature's Issue shape, in its project's first status.
func armatureIssue(key string, is Issue, comments int) map[string]any {
	project, _, _ := strings.Cut(key, "-")
	kind := is.Type
	if kind == "" {
		kind = defaultIssueType
	}
	at := stubTime.Format(time.RFC3339)
	out := map[string]any{
		"id": stubID("issue", key), "key": key, "projectId": stubID("project", project), "projectKey": project,
		"type": map[string]any{"id": stubID("issue-type", kind), "name": kind, "icon": strings.ToLower(kind), "level": 0,
			"isSubtask": false},
		"summary": is.Summary, "priority": "medium", "timeSpentMinutes": 0, "commentCount": comments,
		"status": map[string]any{"id": stubID("status", project, "todo"), "name": "To Do", "category": "todo", "position": 0},
		"labels": []any{}, "fixVersions": []any{}, "affectsVersions": []any{}, "components": []any{},
		"createdAt": at, "updatedAt": at,
	}
	if doc := markdownDocument(is.DescriptionMarkdown); doc != nil {
		out["description"] = doc
	}
	if is.Reporter != "" {
		out["reporter"] = armatureUser(is.Reporter)
	}
	return out
}

// addComment keeps a comment on the issue, its id derived from the tenant, the issue and its place among the
// issue's comments, so a comment.added fixture can name it before it is made.
func (s *Stub) addComment(c *call, who *caller, key string, args map[string]json.RawMessage) any {
	var body map[string]any
	_ = json.Unmarshal(args["body"], &body)
	markdown, text := stringArg(args, "bodyMarkdown"), stringArg(args, "text")
	switch {
	case body != nil && markdown != "":
		return refusal("Send the comment as a document or as Markdown, not both.")
	case body != nil:
	case markdown != "":
		if body = markdownDocument(markdown); body == nil {
			return refusal(commentNeedsText)
		}
	case strings.TrimSpace(text) == "":
		return refusal(commentNeedsText)
	default:
		body = textDocument(text)
	}
	comments := c.t.written.comments[key]
	comment := map[string]any{"id": CommentID(c.t.name, key, len(comments)+1), "issueId": stubID("issue", key), "body": body,
		"internal": false, "author": armatureUser(who.username), "createdAt": s.stubNow()}
	c.t.written.comments[key] = append(comments, comment)
	return answered(map[string]any{"comment": comment})
}

// CommentID is the id of the nth comment added to an issue of a tenant, counting from 1.
func CommentID(tenant, issueKey string, n int) string {
	return stubID(tenant, "comment", issueKey, strconv.Itoa(n))
}

// addRemoteLink puts a page on the issue, or retitles the one of that address, as Armature's PutRemoteLink does.
func (s *Stub) addRemoteLink(c *call, who *caller, key string, args map[string]json.RawMessage) any {
	address, title := strings.TrimSpace(stringArg(args, "url")), strings.TrimSpace(stringArg(args, "title"))
	source, icon, relation := strings.TrimSpace(stringArg(args, "source")), strings.TrimSpace(stringArg(args, "iconUrl")), stringArg(args, "relation")
	if refused := checkRemoteLink(address, title, source, icon, relation); refused != "" {
		return refusal(refused)
	}
	var iconURL any
	if icon != "" {
		iconURL = icon
	}
	links := c.t.written.remoteLinks[key]
	for _, link := range links {
		if link["url"] == address {
			link["title"], link["iconUrl"], link["updatedAt"] = title, iconURL, s.stubNow()
			if relation != "" {
				link["relation"] = relation
			}
			return answered(map[string]any{"remoteLink": link})
		}
	}
	if relation == "" {
		relation = "page"
	}
	at := s.stubNow()
	link := map[string]any{"id": stubID(c.t.name, "remote-link", key, address), "url": address, "title": title, "source": source,
		"relation": relation, "iconUrl": iconURL, "createdBy": userID(who.username), "createdAt": at, "updatedAt": at}
	c.t.written.remoteLinks[key] = append(links, link)
	return answered(map[string]any{"remoteLink": link})
}

// checkRemoteLink refuses what Armature's checkRemoteLink refuses, in its words.
func checkRemoteLink(address, title, source, icon, relation string) string {
	const bad = "That page link cannot be added: "
	for i, a := range []struct{ raw, what string }{{address, "the page's address"}, {icon, "the icon's address"}} {
		parsed, err := url.Parse(a.raw)
		switch {
		case a.raw == "" && i > 0:
		case a.raw == "":
			return bad + "give " + a.what + "."
		case len([]rune(a.raw)) > maxRemoteLinkURL:
			return bad + "shorten " + a.what + " to " + strconv.Itoa(maxRemoteLinkURL) + " characters or fewer."
		case err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsAny(a.raw, " \t\r\n"):
			return bad + a.what + " has to be a web address starting with http:// or https://."
		}
	}
	switch {
	case title == "":
		return bad + "give the page a title."
	case len([]rune(title)) > maxRemoteLinkTitle:
		return bad + "shorten the title to " + strconv.Itoa(maxRemoteLinkTitle) + " characters or fewer."
	case source == "":
		return bad + "name the application the page lives in as its source."
	case len([]rune(source)) > maxRemoteLinkSource:
		return bad + "shorten the source to " + strconv.Itoa(maxRemoteLinkSource) + " characters or fewer."
	case relation != "" && relation != "page" && relation != "requirement":
		return bad + "a link's relation is page or requirement; send one of those, or leave it out for a page."
	}
	return ""
}
