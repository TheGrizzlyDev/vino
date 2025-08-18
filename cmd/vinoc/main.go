package main

import (
	"context"

	vinoShim "github.com/TheGrizzlyDev/vino/internal/pkg/shim"
	"github.com/containerd/containerd/v2/pkg/shim"
)

func main() {
	shim.Run(context.Background(), vinoShim.NewManager("io.containerd.vinoc.v1"))
}
