# Deployment

Rotor runs from Docker Compose for development (`make up`) and from the Helm chart in `deploy/charts/rotor`
anywhere else. The chart is adapted from Exciter's, so the tools of the suite install alike. `values.yaml`
describes every setting; this page says how they fit together. Each release publishes the chart as
`oci://ghcr.io/cloudster1/charts/rotor` (`docs/releasing.md`), which installs the same way as the checkout's
`deploy/charts/rotor` below.

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
  on its own. It must want a password: set `valkey.auth` and name the Secret in `secrets.names.valkey`, which the
  chart and the api refuse to go without outside development (#92). `valkey.tls` connects over TLS (`rediss://`).
- **OpenBao** at `openbao.addr`. It keeps every refresh token, provider key and run token, wraps every run's data
  key with the transit key `rotor-run-data` (#41) and issues the runner protocol's certificate. Back OpenBao up with
  the database: without the transit key no run's data opens again. The key rotates every 90 days and keeps its
  older versions, so older runs still open; an installation upgraded from before #41 may delete the unused key
  `rotor-checkpoints`.
  Finished runs are deleted after the retention an administrator sets under Settings (90 days by default); traces
  sent to a collector or to LangSmith follow their own retention there. The api and the worker sign in with their own ServiceAccount tokens
  (`openbao.auth: kubernetes`); sandboxes hold no OpenBao credential at all, only the single-use wrapping tokens they
  are handed.
  - With `openbao.configure.enabled` a hook Job runs Rotor's init script (`deploy/openbao/init.sh`) against it with
    a token from the Secret you name: the `rotor` KV mount, the transit key, the PKI role for the api's Service name,
    the `rotor-control-plane` policy and the Kubernetes auth role bound to the api's and the worker's accounts. It is
    safe to run on every upgrade.
  - OpenBao reviews those tokens with the cluster's TokenReview API, so its own account needs
    `system:auth-delegator` when it runs in the cluster; `scripts/kind/openbao.yaml` shows the binding.
  - OpenBao must keep an audit log, so who read which secret can be told afterwards (#92). It takes audit devices
    from its configuration only, never through its API; declare one in its server configuration, as
    `deploy/openbao/config.hcl` does for the development stack:

    ```hcl
    audit "file" "rotor" {
      options {
        file_path = "/openbao/audit/audit.log"
      }
    }
    ```

    The init script refuses an OpenBao without one, so the token it runs with must be allowed to list audit
    devices (`sys/audit`, which needs `sudo`); `openbao.configure.requireAudit: false` lets a throwaway test server
    through.
- **Keycloak**, the realm people sign in through: `oidc.issuer`, `oidc.clientId` and the Secret holding the client
  secret in `oidc.clientSecret`. Outside development the chart refuses to render without it.
  - Turn on the client's service account and give it the `realm-management` role `view-users`. When an
    administrator signs a person out of every session, Keycloak's single logout token names only one of them;
    Rotor then reads the person's sign-out time from the admin API and ends their other sessions too. Without the
    role Rotor ends only the named session and logs a warning saying so.

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
  was reviewed; see [Images you can check](#images-you-can-check).
- **Dialling back.** Sandboxes dial the api's Service (`<fullname>-api.<namespace>.svc`, port
  `api.runnerPort`) over TLS; the certificate comes from OpenBao's PKI for that name.

### Workspaces

A run whose workflow has workspace steps (#136) gets, in the same gVisor pod, one toolchain container per image
those steps name, beside the runner. `docs/decisions.md` ("Workspaces in a toolchain container beside the runner")
says why this shape.

- **The exec agent.** An init container from `images.agent` (`ghcr.io/cloudster1/rotor-agent`, released and signed
  with the other images) copies a small static binary into a memory volume, and each toolchain container starts it
  as its entrypoint, so any toolchain image works as it is. The runner reaches it over the pod's loopback with a
  secret of the sandbox's, which code nodes never hold; between steps it ends every process a step started and
  empties the caches. Name it by digest in `images.agent.digest` as you do the runner.
- **The images.** Administrators allow toolchain images in Rotor's settings, by a name workflows give and a
  reference pinned by digest. With `sandbox.admission.requireSignature`, list globs of them in
  `sandbox.workspaces.toolchainImages`, such as `docker.io/library/golang@sha256:*`; they are no image of Rotor's, so
  the admission lets them run as they are, and refuses a workspace pod whose toolchain matches none.
- **Their size.** Each toolchain container is hardened as the runner is, gets
  `sandbox.workspaces.toolchain.memoryMiB` (3072) and `milliCPUs` (2000), 4 GiB of caches on disk and a work tree in
  memory as large as the settings allow (1 GiB unless an administrator says). Plan the nodes for the runner's and
  the toolchains' memory together.
- **The package mirror.** Builds fetch dependencies from one mirror an administrator runs, such as Athens for Go and
  Verdaccio for npm, and names in the settings; the toolchains get it as `GOPROXY`, `GOSUMDB` and
  `npm_config_registry`. Let workspace pods reach it with `sandbox.workspaces.packageMirror.host` (needs Cilium) or
  `.cidr`; the policy selects pods labelled `rotor.dev/workspace`, so other sandboxes never reach it. Name the same
  host in both places. The public Go proxy is no mirror: it fetches any module asked for and redirects to Cloud
  Storage, which would open every bucket.
- **git.** The runner alone fetches and pushes, through Armature's git proxy, which the tools' egress already
  allows; the run's token never reaches a toolchain.

### What a sandbox may reach

A prompt-injected run must not send what it read anywhere else, so sandbox egress is denied by default
(`sandbox.egress`). Nothing reaches a sandbox; a sandbox reaches:
- cluster DNS (`sandbox.egress.dns`)
- the api's runner port
- OpenBao, by namespace and labels for one in the cluster (`sandbox.egress.openbao.namespace`, `podLabels`) or by
  range for one outside it (`sandbox.egress.openbao.cidr`)
- the tools and the model provider:
  - with Cilium as the cluster's network plugin, by hostname: `sandbox.egress.cilium: true` and
    `sandbox.egress.hosts`, such as the Exciter, Armature and Stator addresses and `api.anthropic.com`. Cilium learns
    which addresses a name stands for from the lookups it proxies.
  - without Cilium, plain NetworkPolicy matches addresses only, so give their ranges in `sandbox.egress.cidrs`; the
    chart refuses hostnames without Cilium.

Everything else is refused, cloud metadata (`169.254.169.254`), the api's HTTP port, the rest of the cluster and
the Kubernetes API included. The install test proves each of these from a pod with the sandbox's label.

### Images you can check

Rotor's images, backend, web, runner and agent, are signed with cosign and carry their SPDX SBOMs as signed attestations
(#42), so you can tell what runs in a sandbox and that it is the image that was built. A release (#44,
`docs/releasing.md`) signs them keyless, as its own workflow at the release's tag; the targets below do the same with a
key for images you build.

- **By digest.** Set `images.backend.digest`, `images.web.digest`, `images.runner.digest` and `images.agent.digest` to the digests you
  checked, which the release notes list; a digest wins over the tag, and a tag can be moved after it was signed while
  a digest cannot.
- **Check one.** `make verify IMAGES=<image>@sha256:... COSIGN_KEYLESS=1` checks a released image's signature and SBOM
  attestation against the release workflow's identity; with the public key of images you signed yourself,
  `COSIGN_PUB=cosign.pub` instead. `make sbom IMAGES=...` writes the SBOM to `reports/sbom/`, and `cosign verify`
  does the same without the Makefile.
- **Refuse unsigned sandboxes.** With sigstore's
  [policy-controller](https://docs.sigstore.dev/policy-controller/overview/) installed (its chart 0.10.8 is what the
  install test runs), `sandbox.admission.requireSignature: true` makes the cluster refuse any pod in the sandbox
  namespace whose image does not carry a trusted signature and a signed SBOM; an image from another repository is
  refused too. Which signature is trusted:
  - by default, Rotor's release workflow, keyless (`sandbox.admission.keyless.issuer` and `subjectRegExp`), which
    policy-controller checks against sigstore's public Fulcio and transparency log, so the cluster needs to reach
    them;
  - with `sandbox.admission.publicKey` set, that key alone, for a runner image you build and sign yourself, as the
    install test does.

  The chart labels the sandbox namespace `policy.sigstore.dev/include=true` when it creates it; label one you made
  yourself by hand. Install policy-controller first, since the chart's `ClusterImagePolicy` needs its resource type.

To sign images you build yourself, `make sbom`, `make sign` and `make attest` take `IMAGES` by digest and the key in
`COSIGN_KEY` (with `COSIGN_PASSWORD` in the environment); `REGISTRY_HTTP=1` speaks plain HTTP to a local registry.
They sign with the key alone and upload nothing to a transparency log.

### Secrets

The secrets Job generates the database and Valkey passwords once, inside the cluster, before anything else starts,
and never rewrites one that exists. To bring your own, name existing `kubernetes.io/basic-auth` Secrets in
`secrets.names` (`dbOwner`, `dbApp`, `dbAdmin`, `valkey`). With your own Postgres the owner's password is yours:
create its Secret before installing.

### Traces and metrics

Set `telemetry.otlpEndpoint` to your collector's OTLP/HTTP traces URL, such as
`http://otel-collector.observability:4318/v1/traces`, and the api and the worker send their spans there, each run one
trace: the request that started it, the worker starting and removing its sandbox, and the runner's nodes, model calls
and MCP calls. The runner's spans travel over the runner protocol and the api sends them on, so sandboxes need no
route to the collector. `telemetry.sampleRatio` keeps a share of new traces.

Prometheus scrapes the worker on `worker.metricsPort` (9464: sandboxes, runs by state, run durations) and the api on
`api.metricsPort` (9465: token mint failures); both also serve the process's own Go and process metrics.

LangSmith is off until an administrator turns it on in Rotor's settings with a key, which Rotor keeps in OpenBao. The
api then sends runs' spans, with prompts and answers, to `telemetry.langsmithEndpoint`, so the api's pods must reach
that host; the sandboxes never do. `docs/api-contract-m7-telemetry.md` says what is sent.

In development, `make observe` starts the stack with a collector, Jaeger and Prometheus and says where they are.

## Tests

- `tests/test-helm.sh`, in the shell layer of `./run-tests.sh`, lints the chart with `helm lint --strict` and
  `ct lint`, renders a trial and a production install, checks what the chart must refuse and the sandbox
  namespace's hardening, and compares the renders with `tests/helm/*.yaml`. After a deliberate change, run
  `UPDATE=1 tests/test-helm.sh` and review the diff.
- `make kind-up && make test-chart-install` builds the images, pushes them to a registry beside a throwaway kind
  cluster, lists and signs them with a key made for the test, and installs the chart from them by digest beside a dev
  OpenBao. It carries a run to its end in a sandbox pod, checks the pod is gone and tries what a sandbox may and may
  not reach; the cluster runs Cilium, so the egress policies hold as in production. It then installs
  policy-controller, upgrades in place to admit signed sandbox images only, carries a second run and checks an
  unsigned image is refused. `KEEP=1` keeps the release for a look, and `make kind-down` removes the cluster and the
  registry. The `kube` workflow runs it nightly and on pull requests that change what a cluster runs.
- `make audit-deps` looks for known vulnerabilities in what Rotor ships: govulncheck over the backend, pip-audit
  over the runner's lock file and npm audit over the web client's runtime dependencies. The `deps` workflow runs it
  nightly and on pull requests that change a dependency, and Renovate (`renovate.json`) proposes updates once a
  release is two weeks old.
