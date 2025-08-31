package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"time"

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

type DelegatecCmd[T runc.Command] struct {
	Command      T      `runc_embed:""`
	DelegatePath string `runc_flag:"--delegate_path" runc_group:"delegate"`
}

func (d DelegatecCmd[T]) Slots() runc.Slot {
	return runc.Group{
		Unordered: []runc.Slot{
			runc.FlagGroup{Name: "delegate"},
		},
		Ordered: []runc.Slot{
			d.Command.Slots(),
		},
	}
}

type Commands struct {
	Checkpoint *DelegatecCmd[runc.Checkpoint]
	Restore    *DelegatecCmd[runc.Restore]
	Create     *DelegatecCmd[runc.Create]
	Run        *DelegatecCmd[runc.Run]
	Start      *DelegatecCmd[runc.Start]
	Delete     *DelegatecCmd[runc.Delete]
	Pause      *DelegatecCmd[runc.Pause]
	Resume     *DelegatecCmd[runc.Resume]
	Kill       *DelegatecCmd[runc.Kill]
	List       *DelegatecCmd[runc.List]
	Ps         *DelegatecCmd[runc.Ps]
	State      *DelegatecCmd[runc.State]
	Events     *DelegatecCmd[runc.Events]
	Exec       *DelegatecCmd[runc.Exec]
	Spec       *DelegatecCmd[runc.Spec]
	Update     *DelegatecCmd[runc.Update]
	Features   *DelegatecCmd[runc.Features]
}

func main() {
	f, err := os.OpenFile("/var/log/delegatec.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	log.Printf("delegatec called with args: %v\n", os.Args)
	log.Printf("delegatec environment: %v\n", os.Environ())

	cmds := Commands{}
	if err := runc.ParseAny(&cmds, os.Args[1:]); err != nil {
		log.Printf("failed to parse args: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "failed to parse args: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}

	var (
		cmd          runc.Command
		delegatePath string
	)

	v := reflect.ValueOf(cmds)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.IsNil() {
			continue
		}
		delegatePath = f.Elem().FieldByName("DelegatePath").String()
		cmdIface := f.Elem().FieldByName("Command").Interface()
		cmd = cmdIface.(runc.Command)
		break
	}

	log.Printf("delegatec parsed command: %#v\n", cmd)
	log.Printf("delegatec delegate path: %s\n", delegatePath)

	if cmd == nil {
		fmt.Fprintln(os.Stderr, "no command specified")
		os.Exit(1)
	}

	cli, err := runc.NewDelegatingCliClient(delegatePath, runc.InheritStdin)
	if err != nil {
		log.Printf("failed to create delegating client: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "failed to create delegating client: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}

	w := runc.Wrapper{Delegate: cli}
	if err := w.Run(cmd); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		log.Printf("command run failed: %v\nenv: %v", err, os.Environ())
		fmt.Fprintf(os.Stderr, "command run failed: %v\nenv: %v", err, os.Environ())
		os.Exit(1)
	}
}
