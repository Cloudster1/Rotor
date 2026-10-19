# A single development node that keeps its data on a volume, so a restart keeps people's connections.
storage "raft" {
  path    = "/openbao/file"
  node_id = "rotor"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true
}

api_addr     = "http://127.0.0.1:8200"
cluster_addr = "http://127.0.0.1:8201"
ui           = false

# Every request and answer, secrets hashed, to the container's log (#92). OpenBao takes audit devices only from its
# configuration, never through its API, so an installation's own OpenBao declares one the same way.
audit "file" "stdout" {
  description = "Rotor's development audit log"
  options {
    file_path = "stdout"
  }
}
