# Vino 🍷

**Run Windows applications in Linux containers with zero configuration.**

Vino is a container runtime that automatically executes Windows applications through Wine, making it seamless to containerize and deploy Windows software on Linux infrastructure.

## Why Vino?

Traditional approaches to running Windows applications in containers require:
- Manual Wine installation and configuration
- Complex wrapper scripts
- Application-specific tweaks and workarounds

Vino eliminates this complexity by automatically detecting Windows executables and routing them through Wine transparently. Your existing container orchestration tools (Docker, Kubernetes, etc.) work unchanged.

## Key Benefits

- **Zero Configuration**: Windows executables automatically run through wine
- **OCI Compatible**: Works with standard container tools and orchestration platforms
- **Transparent Operation**: No changes needed to your container images or deployment manifests
- **Production Ready**: Built on proven runc foundation with comprehensive command support

## Quick Start

### Prerequisites

- Linux system with runc installed
- Wine64 available in your container images
- Go 1.25+ for building from source

### Installation

Build vino from source:

```bash
git clone https://github.com/TheGrizzlyDev/vino.git
cd vino
go build -o bin/vino ./cmd/vino
```

### Basic Usage

Replace runc with vino in your container runtime configuration:

```bash
# Instead of:
# runc run mycontainer

# Use:
./bin/vino runc --delegate_path=/usr/bin/runc run mycontainer
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

### Kubernetes Integration

Use vino as a container runtime in your Kubernetes cluster by configuring it as an OCI runtime in your container runtime (containerd, CRI-O, etc.).

### Development and Testing

```bash
# Build the project
go build ./...

# Run tests
go test ./...

# Integration tests (requires Docker)
go test -tags e2e ./tests/integration/dind -v
```

## How It Works

1. **Process Interception**: Vino intercepts container process creation
2. **Automatic Detection**: Detects Windows executables and prepends `wine64` or `wine`
3. **Transparent Delegation**: Passes modified commands to the underlying runc runtime
4. **Hook Integration**: Uses OCI runtime hooks for seamless integration

Your Windows applications run through Wine automatically, while your container orchestration remains unchanged.

## Contributing

We welcome contributions! Please see our development guidelines in `WARP.md` for coding standards and testing procedures.
