# Vino 🍷

**Run Windows applications in Linux containers with zero configuration.**

Vino is a container runtime that automatically executes Windows applications through Wine, making it seamless to containerize and deploy Windows software on Linux infrastructure.

## Why Vino?

Traditional approaches to running Windows applications in containers require:
- Manual Wine installation and configuration
- Complex wrapper scripts
- Application-specific tweaks and workarounds
- Back and forth translation between linux and windows commands and paths 

Vino eliminates this complexity by automatically detecting Windows executables and routing them through Wine transparently. Your existing container orchestration tools (Docker, Kubernetes, etc.) work unchanged.

## Key Benefits

- **Zero Configuration**: Windows executables automatically run through wine
- **OCI Compatible**: Works with standard container tools and orchestration platforms
- **Transparent Operation**: No changes needed to your container images or deployment manifests
- **Bring Your Own Container Runtime**: Because vinoc is built as a wrapper for runc, any runc compatible runtime can be used to run the container

## Quick Start

### Prerequisites

- Linux system with docker + runc installed
- Go 1.25+ for building from source

### Installation

Build vino from source:

```bash
git clone https://github.com/TheGrizzlyDev/vino.git
cd vino
go build -o bin/vino ./cmd/vino
```

### Container Image Requirements

Your container images need wine installed. You can see some examples under `./images/`

### Docker Integration

Configure Docker to use vino as the runtime:

```bash
# Create Docker daemon configuration
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json > /dev/null << EOF
{
  "runtimes": {
    "vino": {
      "path": "/path/to/vino",
      "runtimeArgs": ["runc", "--delegate_path=/usr/bin/runc"]
    }
  }
}
EOF

# Restart Docker
sudo systemctl restart docker

# Run containers with vino runtime
docker run --runtime=vino my-windows-app
```

## How It Works

1. **Process Interception**: Vino intercepts container process creation
2. **Automatic Detection**: Detects Windows executables and prepends `wine64` or `wine` and set up any forwarding required
3. **Transparent Delegation**: Passes modified commands to the underlying runc runtime
4. **Prestarts wine server**: Using bundle hooks, the wine server is prestarted before allowing the command to run
5. **Devices and mounts forwarding**: Devices and mounts are forwarded to the external linux container and then symlinked into wine's prefix

Your Windows applications run through Wine automatically, while your container orchestration remains unchanged.
