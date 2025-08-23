package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
)

func TestRequiresStdin(t *testing.T) {
	tests := []struct {
		name string
		cmd  runc.Command
		want bool
	}{
		{"Create", runc.Create{}, true},
		{"Run", runc.Run{}, true},
		{"Exec", runc.Exec{}, true},
		{"Restore", runc.Restore{}, true},
		{"Update", runc.Update{}, true},
		{"List", runc.List{}, false},
		{"State", runc.State{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiresStdin(tt.cmd); got != tt.want {
				t.Fatalf("requiresStdin(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestStdinLoggedAndForwarded(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)

	stdinLog := NewLogWriter()
	pr, pw := io.Pipe()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(pw, stdinLog), os.Stdin)
		pw.Close()
		stdinLog.Close()
	}()

	var outBuf bytes.Buffer
	go io.Copy(&outBuf, pr)

	input := "test-input\n"
	if _, err := w.Write([]byte(input)); err != nil {
		t.Fatalf("write input: %v", err)
	}
	w.Close()
	wg.Wait()

	if got := outBuf.String(); got != input {
		t.Fatalf("forwarded input = %q, want %q", got, input)
	}
	if !strings.Contains(logBuf.String(), "test-input") {
		t.Fatalf("log does not contain input, got %q", logBuf.String())
	}
}
