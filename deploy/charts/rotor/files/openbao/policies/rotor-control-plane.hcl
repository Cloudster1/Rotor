# The control plane (the api and the worker): connections, client secrets, run tokens and the runner certificate.
path "rotor/data/connections/*" {
  capabilities = ["create", "read", "update", "delete"]
}

path "rotor/metadata/connections/*" {
  capabilities = ["read", "list", "delete"]
}

path "rotor/data/tools/*" {
  capabilities = ["create", "read", "update", "delete"]
}

path "rotor/metadata/tools/*" {
  capabilities = ["read", "list", "delete"]
}

# The runner protocol's server certificate, for the host api alone (#18).
path "pki/issue/runnerctl" {
  capabilities = ["create", "update"]
}

path "pki/cert/ca" {
  capabilities = ["read"]
}

# Every run's data is sealed with a data key of its own (#41): this key makes and unwraps them and never leaves
# OpenBao. Nothing else is encrypted with it, so encrypting through it is not allowed.
path "transit/datakey/plaintext/rotor-run-data" {
  capabilities = ["update"]
}

path "transit/decrypt/rotor-run-data" {
  capabilities = ["update"]
}

# The language model providers' keys (#20).
path "rotor/data/providers/*" {
  capabilities = ["create", "read", "update", "delete"]
}

path "rotor/metadata/providers/*" {
  capabilities = ["read", "list", "delete"]
}

# A run's access tokens, kept until they are revoked at their tools when the run ends (#23).
path "rotor/data/runs/*" {
  capabilities = ["create", "read", "update", "delete"]
}

path "rotor/metadata/runs/*" {
  capabilities = ["read", "list", "delete"]
}

# Tokens and keys reach a sandbox wrapped: single use and short lived, unwrapped by the runner itself (#23).
path "sys/wrapping/wrap" {
  capabilities = ["update"]
}

# The LangSmith key an administrator gives, which the control plane alone sends runs' spans with (#37).
path "rotor/data/settings/langsmith/*" {
  capabilities = ["create", "read", "update", "delete"]
}

path "rotor/metadata/settings/langsmith/*" {
  capabilities = ["read", "list", "delete"]
}
