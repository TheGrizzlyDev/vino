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
		{"Create", runc.Create{}, false},
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

func TestInheritedFDs(t *testing.T) {
	r1, w1, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe1: %v", err)
	}
	defer r1.Close()
	defer w1.Close()

	fds, err := inheritedFDs()
	if err != nil {
		t.Fatalf("inheritedFDs: %v", err)
	}
	want := map[int]bool{
		int(r1.Fd()): false,
		int(w1.Fd()): false,
	}
	for _, fd := range fds {
		if _, ok := want[fd]; ok {
			want[fd] = true
		}
	}
	for fd, seen := range want {
		if !seen {
			t.Fatalf("missing forwarded fd %d", fd)
		}
	}
}

func TestInheritedFDsExclude(t *testing.T) {
	r1, w1, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe1: %v", err)
	}
	defer r1.Close()
	defer w1.Close()

	fds, err := inheritedFDs(int(r1.Fd()))
	if err != nil {
		t.Fatalf("inheritedFDs: %v", err)
	}
	want := map[int]bool{
		int(w1.Fd()): false,
	}
	for _, fd := range fds {
		if fd == int(r1.Fd()) {
			t.Fatalf("found excluded fd %d", fd)
		}
		if _, ok := want[fd]; ok {
			want[fd] = true
		}
	}
	for fd, seen := range want {
		if !seen {
			t.Fatalf("missing forwarded fd %d", fd)
		}
	}
}
