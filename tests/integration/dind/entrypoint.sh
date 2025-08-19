#!/bin/sh
set -e

if [ ! -f /etc/containerd/config.toml ]; then
    containerd config default > /etc/containerd/config.toml
fi

# Append vinoc runtime configuration if not already present
if ! grep -q vinoc /etc/containerd/config.toml; then
cat <<'EOT' >> /etc/containerd/config.toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.vinoc]
  runtime_type = "io.containerd.vinoc.v1"
  [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.vinoc.options]
    delegated_runtime_path = "/usr/local/sbin/runc"
EOT
fi

exec dockerd-entrypoint.sh "$@"
