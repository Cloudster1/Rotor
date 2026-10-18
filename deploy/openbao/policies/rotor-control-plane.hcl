# The api: people's connections and the tools' client secrets, and nothing else; sandboxes get roles of their own.
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
