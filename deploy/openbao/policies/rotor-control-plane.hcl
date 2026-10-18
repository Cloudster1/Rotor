# The api: people's connections, the tools' client secrets and its runner certificate; sandboxes get roles of their own.
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
