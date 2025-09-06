package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func repoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildImage(tag, dockerfile, contextDir string) error {
	return run("docker", "build", "-t", tag, "-f", dockerfile, contextDir)
}

func loadImage(container, image string) error {
	save := exec.Command("docker", "save", image)
	load := exec.Command("docker", "exec", "-i", container, "docker", "load")

	pr, pw := io.Pipe()
	save.Stdout = pw
	load.Stdin = pr
	save.Stderr = os.Stderr
	load.Stdout = os.Stdout
	load.Stderr = os.Stderr

	if err := load.Start(); err != nil {
		return fmt.Errorf("start load: %w", err)
	}
	if err := save.Start(); err != nil {
		return fmt.Errorf("start save: %w", err)
	}
	if err := save.Wait(); err != nil {
		pw.CloseWithError(err)
		load.Wait()
		return fmt.Errorf("save image: %w", err)
	}
	pw.Close()
	if err := load.Wait(); err != nil {
		return fmt.Errorf("load image: %w", err)
	}
	return nil
}

func main() {
	noBuild := flag.Bool("no-build", false, "skip building images")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		log.Fatalf("failed to get repo root: %v", err)
	}

	const (
		baseImage     = "vino-base-ubuntu-24_04"
		dindImage     = "vino-dind-test"
		containerName = "vino-dind-manual"
	)

	baseDockerfile := filepath.Join(root, "images/base/ubuntu-24_04.Dockerfile")
	dindDockerfile := filepath.Join(root, "internal/tests/integration/dind/Dockerfile")

	if !*noBuild {
		log.Printf("Building %s...", baseImage)
		if err := buildImage(baseImage, baseDockerfile, filepath.Dir(baseDockerfile)); err != nil {
			log.Fatalf("build base image: %v", err)
		}
		if err := run("docker", "tag", baseImage, "wine"); err != nil {
			log.Fatalf("tag wine: %v", err)
		}
		log.Printf("Building %s...", dindImage)
		if err := buildImage(dindImage, dindDockerfile, root); err != nil {
			log.Fatalf("build dind image: %v", err)
		}
	}

	_ = run("docker", "rm", "-f", containerName)
	defer run("docker", "rm", "-f", containerName)

	log.Printf("Starting container %s...", containerName)
	if err := run("docker", "run", "-d", "--privileged", "--name", containerName, dindImage); err != nil {
		log.Fatalf("start container: %v", err)
	}

	log.Print("Waiting for Docker to be ready")
	for {
		logs, err := exec.Command("docker", "logs", containerName).CombinedOutput()
		if err != nil {
			log.Fatalf("docker logs: %v", err)
		}
		if strings.Contains(string(logs), "API listen on /var/run/docker.sock") {
			break
		}
		fmt.Print(".")
		time.Sleep(time.Second)
	}
	fmt.Println()

	images := []string{"wine", "nginx:latest", "alpine:latest"}
	for _, img := range images {
		if !*noBuild && img != "wine" {
			log.Printf("Pulling %s...", img)
			if err := run("docker", "pull", img); err != nil {
				log.Fatalf("pull %s: %v", img, err)
			}
		}
		log.Printf("Loading %s into %s...", img, containerName)
		if err := loadImage(containerName, img); err != nil {
			log.Fatalf("load %s: %v", img, err)
		}
	}

	log.Println("Entering container. Exit shell to stop.")
	shell := exec.Command("docker", "exec", "-it", containerName, "sh")
	shell.Stdin = os.Stdin
	shell.Stdout = os.Stdout
	shell.Stderr = os.Stderr
	if err := shell.Run(); err != nil {
		log.Fatalf("shell: %v", err)
	}
}
