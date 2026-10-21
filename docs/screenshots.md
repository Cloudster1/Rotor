# The product, in pictures

Taken from a fresh development stack by `make screenshots`, which drives the
browser suite's own image against it as a person would: it adds the tool stub
as Exciter and its stub model, connects alice, saves
[`examples/draft-requirements.yaml`](../examples/draft-requirements.yaml) and a
workflow of its own, and carries two runs to an approval, approving one. The
spec is [`e2e/screenshots/product.spec.ts`](../e2e/screenshots/product.spec.ts),
outside the gate's browser suite.

They are taken, not edited: if one of these is wrong, the page changed. Each
picture waits for what its name promises, and one that cannot be taken fails
the run. To take them again:

```sh
make clean up
make screenshots
```

## Workflows

![The workflows alice may see](screenshots/workflows.png)
![A workflow's YAML, checked as it is written](screenshots/workflow-editor.png)
![The same workflow on the canvas](screenshots/workflow-canvas.png)

## Runs

![A run's graph and what happened, word by word](screenshots/run.png)
![The same run in the dark palette](screenshots/run-dark.png)
![The runs](screenshots/runs.png)

## Approvals

![What waits for alice](screenshots/approvals.png)
![An approval, with the value it may change](screenshots/approval.png)

## Tools, assistants and the record

![A person's connections to the tools](screenshots/connections.png)
![Settings: the person, usage, and the tools](screenshots/settings.png)
![An assistant asking to connect](screenshots/assistant-consent.png)
![The audit log](screenshots/audit.png)
