package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/TheGrizzlyDev/vino/internal/pkg/cli"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
)

type logWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	flushC chan struct{}
	quit   chan struct{}
	wg     sync.WaitGroup
}

// NewLogWriter creates a logWriter that flushes on '\n' or after 1s of inactivity.
func NewLogWriter() *logWriter {
	lw := &logWriter{
		flushC: make(chan struct{}, 1),
		quit:   make(chan struct{}),
	}
	lw.wg.Add(1)

	// Background flusher
	go func() {
		defer lw.wg.Done()
		timer := time.NewTimer(time.Second)
		defer timer.Stop()

		for {
			select {
			case <-lw.flushC:
				// reset timer on write
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(time.Second)
			case <-timer.C:
				lw.flush()
				timer.Reset(time.Second)
			case <-lw.quit:
				lw.flush() // final flush
				return
			}
		}
	}()

	return lw
}

func (lw *logWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	n := len(p)
	for _, b := range p {
		if b == '\n' {
			log.Print(lw.buf.String())
			lw.buf.Reset()
		} else {
			lw.buf.WriteByte(b)
		}
	}

	// signal activity (to reset timer)
	select {
	case lw.flushC <- struct{}{}:
	default:
	}
	return n, nil
}

func (lw *logWriter) flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	if lw.buf.Len() > 0 {
		log.Print(lw.buf.String())
		lw.buf.Reset()
	}
}

// Bytes returns a copy of the current unflushed buffer.
func (lw *logWriter) Bytes() []byte {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return append([]byte(nil), lw.buf.Bytes()...)
}

// Close stops the background flusher and flushes remaining data.
func (lw *logWriter) Close() error {
	close(lw.quit)
	lw.wg.Wait()
	return nil
}

type DelegatecCmd struct {
	Arguments     []string `cli_argument:"args"`
	DelegatecLogs string   `cli_flag:"--delegatec_logs" cli_group:"delegate"`
	DelegatePath  string   `cli_flag:"--delegate_path" cli_group:"delegate"`
}

func (d DelegatecCmd) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "delegate"},
			cli.Arguments{Name: "args"},
		},
	}
}

func main() {
	var delegatecCmd DelegatecCmd
	if err := cli.Parse(&delegatecCmd, os.Args[1:]); err != nil {
		panic(err)
	}

	f, err := os.OpenFile(delegatecCmd.DelegatecLogs, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	log.Printf("delegatec called with args: %v\n", os.Args)
	log.Printf("delegatec environment: %v\n", os.Environ())

	log.Println(os.Args)
	log.Println(delegatecCmd)

	cli, err := runc.NewDelegatingCliClient(delegatecCmd.DelegatePath, runc.InheritStdin)
	if err != nil {
		panic(err)
	}

	w := runc.Wrapper{Delegate: cli}
	if err := runc.RunWithArgs(&w, delegatecCmd.Arguments); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		panic(err)
	}
}
