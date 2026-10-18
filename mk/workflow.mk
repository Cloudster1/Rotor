# The workflow spec's schema lives in api/; the backend embeds a copy and the runner packages one, and a test on
# each side refuses a copy that differs.
.PHONY: workflow-schema
workflow-schema: ## Copy api/workflow.v1.schema.json to where the backend embeds it and the runner packages it
	cp $(ROOT)/api/workflow.v1.schema.json $(ROOT)/backend/internal/workflow/spec/workflow.v1.schema.json
	cp $(ROOT)/api/workflow.v1.schema.json $(ROOT)/runner/src/rotor_runner/workflow.v1.schema.json
