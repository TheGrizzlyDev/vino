package shim

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TheGrizzlyDev/vino/internal/pkg/runc"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	apitypes "github.com/containerd/containerd/api/types"
	tasktypes "github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	ptypes "github.com/containerd/containerd/v2/pkg/protobuf/types"
	"github.com/containerd/containerd/v2/pkg/shim"
	"github.com/containerd/containerd/v2/pkg/shutdown"
	"github.com/containerd/containerd/v2/plugins"
	"github.com/containerd/errdefs"
	"github.com/containerd/plugin"
	"github.com/containerd/plugin/registry"
	"github.com/containerd/ttrpc"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	registry.Register(&plugin.Registration{
		Type: plugins.TTRPCPlugin,
		ID:   "task",
		Requires: []plugin.Type{
			plugins.EventPlugin,
			plugins.InternalPlugin,
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			pp, err := ic.GetByID(plugins.EventPlugin, "publisher")
			if err != nil {
				return nil, err
			}
			ss, err := ic.GetByID(plugins.InternalPlugin, "shutdown")
			if err != nil {
				return nil, err
			}
			return newTaskService(ic.Context, pp.(shim.Publisher), ss.(shutdown.Service))
		},
	})
}

func NewManager(name string) shim.Manager {
	return manager{name: name}
}

type manager struct {
	name string
}

func (m manager) Name() string {
	return m.name
}

func (m manager) Start(ctx context.Context, id string, opts shim.StartOpts) (shim.BootstrapParams, error) {
	params := shim.BootstrapParams{
		Version:  2,
		Protocol: "ttrpc",
	}

	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return params, err
	}
	self, err := os.Executable()
	if err != nil {
		return params, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return params, err
	}

	args := []string{"-namespace", ns, "-id", id, "-address", opts.Address}
	if opts.Debug {
		args = append(args, "-debug")
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GOMAXPROCS=4")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	address, err := shim.SocketAddress(ctx, opts.Address, id, false)
	if err != nil {
		return params, err
	}
	l, err := shim.NewSocket(address)
	if err != nil {
		return params, err
	}
	f, err := l.File()
	if err != nil {
		l.Close()
		return params, err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, f)

	if err := cmd.Start(); err != nil {
		f.Close()
		l.Close()
		return params, err
	}
	// Keep the socket and file descriptors open until the shim has
	// accepted containerd's connection. Once the shim process exits the
	// descriptors are closed allowing the socket file to be removed.
	go func() {
		_ = cmd.Wait()
		f.Close()
		l.Close()
	}()

	if err := shim.AdjustOOMScore(cmd.Process.Pid); err != nil {
		return params, err
	}

	params.Address = address
	return params, nil
}

func (m manager) Stop(ctx context.Context, id string) (shim.StopStatus, error) {
	return shim.StopStatus{
		ExitStatus: 0,
		ExitedAt:   time.Now(),
	}, nil
}

func (m manager) Info(ctx context.Context, optionsR io.Reader) (*apitypes.RuntimeInfo, error) {
	info := &apitypes.RuntimeInfo{
		Name: "io.containerd.vinoc.v1",
		Version: &apitypes.RuntimeVersion{
			Version: "v1.0.0",
		},
	}
	return info, nil
}

func newTaskService(ctx context.Context, publisher shim.Publisher, sd shutdown.Service) (taskAPI.TaskService, error) {
	return &vinoTaskService{}, nil
}

var (
	_ = shim.TTRPCService(&vinoTaskService{})
)

type VinoOptions struct {
	DelegatedRuntimePath string `json:"delegated_runtime_path"`
}

type vinoTaskService struct {
	cli runc.Cli
}

func (v *vinoTaskService) RegisterTTRPC(server *ttrpc.Server) error {
	taskAPI.RegisterTaskService(server, v)
	return nil
}

func (v *vinoTaskService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	var opts VinoOptions
	if o := r.GetOptions(); o != nil && len(o.GetValue()) > 0 {
		if err := json.Unmarshal(o.GetValue(), &opts); err != nil {
			return nil, errdefs.ErrInvalidArgument.WithMessage(err.Error())
		}
	}
	path := opts.DelegatedRuntimePath
	if path == "" {
		p, err := exec.LookPath("runc")
		if err != nil {
			return nil, errdefs.ErrInvalidArgument.WithMessage("delegated runtime path not provided")
		}
		path = p
	}
	cli, err := runc.NewDelegatingCliClient(path)
	if err != nil {
		return nil, errdefs.ErrInvalidArgument.WithMessage(err.Error())
	}
	v.cli = cli

	pidFilePath := filepath.Join(r.Bundle, "pidfile")
	cmd := runc.Create{
		BundleOpt:        runc.BundleOpt{Bundle: r.Bundle},
		ConsoleSocketOpt: runc.ConsoleSocketOpt{ConsoleSocket: r.Stdin},
		PidFileOpt:       runc.PidFileOpt{PidFile: pidFilePath},
		ContainerID:      r.ID,
	}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if err := ecmd.Run(); err != nil {
		return nil, err
	}
	pidData, err := os.ReadFile(pidFilePath)
	if err != nil {
		return nil, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return nil, err
	}
	resp := &taskAPI.CreateTaskResponse{Pid: uint32(pid)}
	return resp, nil
}

func (v *vinoTaskService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	cmd := runc.Start{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if err := ecmd.Run(); err != nil {
		return nil, err
	}
	stateCmd := runc.State{ContainerID: r.ID}
	stateEcmd, err := v.cli.Command(ctx, stateCmd)
	if err != nil {
		return nil, err
	}
	out, err := stateEcmd.Output()
	if err != nil {
		return nil, err
	}
	var rs struct {
		Pid uint32 `json:"pid"`
	}
	if err := json.Unmarshal(out, &rs); err != nil {
		return nil, err
	}
	return &taskAPI.StartResponse{Pid: rs.Pid}, nil
}

func (v *vinoTaskService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	cmd := runc.Delete{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	err = ecmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return nil, err
		}
	}
	resp := &taskAPI.DeleteResponse{ExitStatus: uint32(exitCode)}
	return resp, nil
}

func (v *vinoTaskService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	var proc specs.Process
	if r.Spec != nil {
		json.Unmarshal(r.Spec.Value, &proc)
	}
	cmd := runc.Exec{
		ContainerID: r.ID,
	}
	if len(proc.Args) > 0 {
		cmd.Command = proc.Args[0]
		if len(proc.Args) > 1 {
			cmd.Args = proc.Args[1:]
		}
	} else {
		cmd.Command = r.ExecID
	}
	cmd.Cwd = proc.Cwd
	cmd.Env = proc.Env
	cmd.Tty = proc.Terminal || r.Terminal
	if r.Stdin != "" {
		cmd.ConsoleSocketOpt = runc.ConsoleSocketOpt{ConsoleSocket: r.Stdin}
	}

	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if err := ecmd.Run(); err != nil {
		return nil, err
	}
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	cmd := runc.Exec{
		ContainerID: r.ID,
		Command:     "resize",
		Args: []string{
			r.ExecID,
			strconv.Itoa(int(r.Width)),
			strconv.Itoa(int(r.Height)),
		},
	}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	_ = ecmd.Run()
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	cmd := runc.State{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	out, err := ecmd.Output()
	if err != nil {
		return nil, err
	}
	var rs struct {
		ID     string `json:"id"`
		Bundle string `json:"bundle"`
		Pid    uint32 `json:"pid"`
		Status string `json:"status"`
	}
	json.Unmarshal(out, &rs)
	statusMap := map[string]tasktypes.Status{
		"created": tasktypes.Status_CREATED,
		"running": tasktypes.Status_RUNNING,
		"stopped": tasktypes.Status_STOPPED,
		"paused":  tasktypes.Status_PAUSED,
		"pausing": tasktypes.Status_PAUSING,
	}
	resp := &taskAPI.StateResponse{
		ID:     rs.ID,
		Bundle: rs.Bundle,
		Pid:    rs.Pid,
		Status: statusMap[rs.Status],
	}
	return resp, nil
}

func (v *vinoTaskService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	cmd := runc.Pause{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	_ = ecmd.Run()
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	cmd := runc.Resume{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	_ = ecmd.Run()
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	cmd := runc.Kill{ContainerID: r.ID, Signal: strconv.Itoa(int(r.Signal)), All: r.All}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	_ = ecmd.Run()
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	cmd := runc.Ps{ContainerID: r.ID, FormatOpt: runc.FormatOpt{Format: "json"}}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	out, err := ecmd.Output()
	if err != nil {
		return nil, err
	}
	var procs []struct {
		Pid uint32 `json:"pid"`
	}
	json.Unmarshal(out, &procs)
	resp := &taskAPI.PidsResponse{}
	for _, p := range procs {
		resp.Processes = append(resp.Processes, &tasktypes.ProcessInfo{Pid: p.Pid})
	}
	return resp, nil
}

func (v *vinoTaskService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	cmd := runc.Exec{ContainerID: r.ID, Command: "close-io", Args: []string{r.ExecID}}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err == nil {
		_ = ecmd.Run()
	}
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	cmd := runc.Checkpoint{ImagePath: r.Path, ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if err := ecmd.Run(); err != nil {
		return nil, err
	}
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	cmd := runc.State{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err == nil {
		_ = ecmd.Run()
	}
	resp := &taskAPI.ConnectResponse{ShimPid: uint32(os.Getpid())}
	return resp, nil
}

func (v *vinoTaskService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	cmd := runc.Delete{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err == nil {
		_ = ecmd.Run()
	}
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	cmd := runc.Events{ContainerID: r.ID, Stats: true}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	out, err := ecmd.Output()
	if err != nil {
		return nil, err
	}
	any := &anypb.Any{Value: out}
	return &taskAPI.StatsResponse{Stats: any}, nil
}

func (v *vinoTaskService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	cmd := runc.Update{ContainerID: r.ID}
	ecmd, err := v.cli.Command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	_ = ecmd.Run()
	return &ptypes.Empty{}, nil
}

func (v *vinoTaskService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	cmd, err := v.cli.Command(ctx, runc.Events{ContainerID: r.ID})
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer cmd.Wait()

	errCh := make(chan error, 1)
	eventCh := make(chan *taskAPI.WaitResponse, 1)

	go func() {
		decoder := json.NewDecoder(stdout)
		for {
			var event struct {
				Type string `json:"type"`
				Data struct {
					ExitStatus int `json:"exit_status"`
				} `json:"data"`
			}
			if err := decoder.Decode(&event); err != nil {
				if err == io.EOF {
					errCh <- errdefs.ErrNotFound
					return
				}
				errCh <- err
				return
			}
			if event.Type == "exit" {
				eventCh <- &taskAPI.WaitResponse{ExitStatus: uint32(event.Data.ExitStatus)}
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case event := <-eventCh:
		return event, nil
	}
}
