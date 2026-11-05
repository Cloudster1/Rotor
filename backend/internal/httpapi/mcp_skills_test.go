package httpapi

import (
	"slices"
	"testing"
)

var skillTools = []string{"create_skill", "get_skill", "get_skill_file", "get_skill_version", "list_skills", "save_skill", "validate_skill"}

// skillReads are the skill tools a grant made to read is offered.
var skillReads = []string{"get_skill", "get_skill_file", "get_skill_version", "list_skills", "validate_skill"}

func TestTheSkillToolsTakeTheirInputs(t *testing.T) {
	want := map[string][]string{
		"list_skills": {"q"}, "get_skill": {"skillID"}, "get_skill_version": {"skillID", "versionNumber"},
		"get_skill_file": {"skillID", "versionNumber", "path"}, "validate_skill": {"files"},
		"create_skill": {"files", "comment"}, "save_skill": {"skillID", "baseVersion", "files", "comment"},
	}
	for name, fields := range want {
		tool, ok := toolByName(name)
		if !ok {
			t.Errorf("%s is no tool", name)
			continue
		}
		for _, f := range fields {
			if _, ok := tool.Input.Properties[f]; !ok {
				t.Errorf("%s does not take %s", name, f)
			}
		}
		if tool.Description == "" {
			t.Errorf("%s has no help", name)
		}
	}
	save, _ := toolByName("save_skill")
	if !slices.Contains(save.Input.Required, "baseVersion") {
		t.Error("save_skill does not require baseVersion")
	}
}

func TestAReadGrantIsOfferedTheFiveSkillReadsAndNoSkillWrite(t *testing.T) {
	var all, reading []string
	for _, tool := range listTools(false) {
		all = append(all, tool["name"].(string))
	}
	for _, tool := range listTools(true) {
		reading = append(reading, tool["name"].(string))
	}
	for _, name := range skillTools {
		if !slices.Contains(all, name) {
			t.Errorf("%s is not offered", name)
		}
		if slices.Contains(reading, name) != slices.Contains(skillReads, name) {
			t.Errorf("a read grant is offered %s: %v", name, slices.Contains(reading, name))
		}
	}
}

func TestNoSkillOperationThatSharesArchivesOrImportsIsATool(t *testing.T) {
	for _, op := range operations {
		if op.tool == "" || op.tag != "skills" {
			continue
		}
		if !slices.Contains(skillTools, op.tool) {
			t.Errorf("%s %s is the tool %s, which the contract does not name", op.method, op.path, op.tool)
		}
	}
}
