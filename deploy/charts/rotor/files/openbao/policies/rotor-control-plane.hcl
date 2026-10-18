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

# Checkpoints are encrypted and decrypted with this one key, which never leaves OpenBao (#19).
path "transit/encrypt/rotor-checkpoints" {
  capabilities = ["update"]
}

path "transit/decrypt/rotor-checkpoints" {
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
