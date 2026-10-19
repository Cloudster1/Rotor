# The workflow spec's schema lives in api/; the backend embeds a copy and the runner packages one, and a test on
# each side refuses a copy that differs.
.PHONY: workflow-schema
workflow-schema: ## Copy api/workflow.v1.schema.json to where the backend embeds it and the runner packages it
	cp $(ROOT)/api/workflow.v1.schema.json $(ROOT)/backend/internal/workflow/spec/workflow.v1.schema.json
	cp $(ROOT)/api/workflow.v1.schema.json $(ROOT)/runner/src/rotor_runner/workflow.v1.schema.json

# The credentials Rotor cuts from what it keeps live in api/ too (#41); the backend embeds a copy, the runner packages
# one, and a test on each side refuses a copy that differs.
.PHONY: secret-patterns
secret-patterns: ## Copy api/secret-patterns.json to where the backend embeds it and the runner packages it
	cp $(ROOT)/api/secret-patterns.json $(ROOT)/backend/internal/redact/secret-patterns.json
	cp $(ROOT)/api/secret-patterns.json $(ROOT)/runner/src/rotor_runner/secret-patterns.json
