# Deployment

Rotor runs from Docker Compose for development (`make up`) and from the Helm chart in `deploy/charts/rotor`
anywhere else. The chart is adapted from Exciter's, so the tools of the suite install alike. `values.yaml`
describes every setting; this page says how they fit together.

## A trial

```sh
helm install rotor deploy/charts/rotor -f deploy/charts/rotor/values-trial.yaml \
  --set openbao.addr=http://openbao.example:8200 \
  --set openbao.configure.enabled=true --set openbao.configure.tokenSecret.name=openbao-admin
```

The trial brings its own Postgres and Valkey, one pod each without backups, and serves plain HTTP. It still needs
an OpenBao (below) and a cluster that can run sandboxes. Sign-in needs `oidc.*`; without it nobody signs in, which
is enough to see the release come up and nothing more.

## A real installation

Four things come from outside the chart.

- **Postgres** at `database.host`. The roles Job creates `rotor_app`, which every row level security policy binds,
  and `rotor_admin`, exempt by policy, with the passwords the secrets Job generated. The migrate Job then runs the
  migrations as `database.ownerRole` before every upgrade. The bundled `postgresql` is for a trial only.
- **Valkey** at `valkey.host`. It carries the signals between api replicas: a run's new events and a cancel reach
  the api holding the run's stream through it. Losing it costs latency, never data, since every stream also looks
  on its own.
- **OpenBao** at `openbao.addr`. It keeps every refresh token, provider key and run token, encrypts the checkpoints
  and issues the runner protocol's certificate. The api and the worker sign in with their own ServiceAccount tokens
  (`openbao.auth: kubernetes`); sandboxes hold no OpenBao credential at all, only the single-use wrapping tokens they
  are handed.
  - With `openbao.configure.enabled` a hook Job runs Rotor's init script (`deploy/openbao/init.sh`) against it with
    a token from the Secret you name: the `rotor` KV mount, the transit key, the PKI role for the api's Service name,
    the `rotor-control-plane` policy and the Kubernetes auth role bound to the api's and the worker's accounts. It is
    safe to run on every upgrade.
  - OpenBao reviews those tokens with the cluster's TokenReview API, so its own account needs
    `system:auth-delegator` when it runs in the cluster; `scripts/kind/openbao.yaml` shows the binding.
- **Keycloak**, the realm people sign in through: `oidc.issuer`, `oidc.clientId` and the Secret holding the client
  secret in `oidc.clientSecret`. Outside development the chart refuses to render without it.

### Sandboxes

Every run executes in its own pod in the sandbox namespace (`sandbox.namespace`, `<release namespace>-sandboxes`
by default), which the chart creates under the restricted Pod Security standard.

- **gVisor.** The pods run under the RuntimeClass `sandbox.runtimeClass`, `gvisor` by default; install gVisor's
  `runsc` on the nodes and a RuntimeClass of that name first. An empty runtime class runs sandboxes under runc,
  which the chart and Rotor refuse unless `sandbox.allowRunc` says so, and never with `env: production`.
- **Hardening.** Each pod runs as the runner image's non-root user with a read-only root filesystem, no
  capabilities, no privilege escalation, the runtime's seccomp profile, a memory `/tmp` and an
  `activeDeadlineSeconds` as a backstop. It carries no ServiceAccount token; its bootstrap token comes from a Secret
  the pod owns.
- **The worker's rights.** It may create, read, update and delete pods and Secrets in the sandbox namespace, and
  nothing else anywhere; the api has no Kubernetes rights.
- **The runner image.** Name it by digest in `images.runner.digest`, so every run executes exactly the image that
  was reviewed.
- **Dialling back.** Sandboxes dial the api's Service (`<fullname>-api.<namespace>.svc`, port
  `api.runnerPort`) over TLS; the certificate comes from OpenBao's PKI for that name.

### Secrets

The secrets Job generates the database and Valkey passwords once, inside the cluster, before anything else starts,
and never rewrites one that exists. To bring your own, name existing `kubernetes.io/basic-auth` Secrets in
`secrets.names` (`dbOwner`, `dbApp`, `dbAdmin`, `valkey`). With your own Postgres the owner's password is yours:
create its Secret before installing.

## Tests

- `tests/test-helm.sh`, in the shell layer of `./run-tests.sh`, lints the chart with `helm lint --strict` and
  `ct lint`, renders a trial and a production install, checks what the chart must refuse and the sandbox
  namespace's hardening, and compares the renders with `tests/helm/*.yaml`. After a deliberate change, run
  `UPDATE=1 tests/test-helm.sh` and review the diff.
- `make kind-up && make test-chart-install` installs the chart into a throwaway kind cluster beside a dev OpenBao,
  carries a run to its end in a sandbox pod, checks the pod is gone and upgrades in place. `KEEP=1` keeps the
  release for a look. The `kube` workflow runs it nightly and on pull requests that change what a cluster runs.
