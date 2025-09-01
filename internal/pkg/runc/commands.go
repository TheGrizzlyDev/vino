package runc

import cli "github.com/TheGrizzlyDev/vino/internal/pkg/cli"

// ------------------------------------------------------------
// Common, embeddable option groups (no cli.Subcommand() here)
// ------------------------------------------------------------

// BundleOpt holds the OCI bundle path.
type BundleOpt struct {
	Bundle string `cli_flag:"--bundle" cli_flag_alternatives:"-b" cli_group:"bundle"`
}

// ConsoleSocketOpt holds the console socket path.
type ConsoleSocketOpt struct {
	ConsoleSocket string `cli_flag:"--console-socket" cli_group:"console"`
}

// PidFileOpt writes the pid to a file (used with detach).
type PidFileOpt struct {
	PidFile string `cli_flag:"--pid-file" cli_group:"lifecycle"`
}

// PivotKeyringFDsOpt groups common runtime toggles.
type PivotKeyringFDsOpt struct {
	NoPivot      bool  `cli_flag:"--no-pivot" cli_group:"runtime"`
	NoNewKeyring bool  `cli_flag:"--no-new-keyring" cli_group:"runtime"`
	PreserveFDs  *uint `cli_flag:"--preserve-fds" cli_group:"runtime"`
}

// DetachOpt for detach-capable commands (run/restore/exec).
type DetachOpt struct {
	Detach bool `cli_flag:"--detach" cli_flag_alternatives:"-d" cli_group:"lifecycle"`
}

// FormatOpt standard output-format selector (table/json).
type FormatOpt struct {
	Format string `cli_flag:"--format" cli_flag_alternatives:"-f" cli_group:"output" cli_enum:"table|json"`
}

// ------------------------------------------------------------
// Global options (no cli.Subcommand)
// Manpage: runc(8) — https://manpages.debian.org/bookworm/runc/runc.8.en.html
// ------------------------------------------------------------

type Global struct {
	Root          string `cli_flag:"--root"            cli_group:"global"`
	Debug         bool   `cli_flag:"--debug"           cli_group:"global"`
	Log           string `cli_flag:"--log"             cli_group:"global"`
	LogFormat     string `cli_flag:"--log-format"      cli_group:"global"`
	SystemdCgroup bool   `cli_flag:"--systemd-cgroup"  cli_group:"global"`
	Criu          string `cli_flag:"--criu"            cli_group:"global"`
	Rootless      string `cli_flag:"--rootless"        cli_group:"global" cli_enum:"true|false|auto"`
}

// ------------------------------------------------------------
// checkpoint
// Manpage: runc-checkpoint(8) — https://manpages.debian.org/bookworm/runc/runc-checkpoint.8.en.html
// ------------------------------------------------------------

type Checkpoint struct {
	Global
	// flags
	ImagePath           string `cli_flag:"--image-path"         cli_group:"images"`
	WorkPath            string `cli_flag:"--work-path"          cli_group:"images"`
	ParentPath          string `cli_flag:"--parent-path"        cli_group:"images"`
	LeaveRunning        bool   `cli_flag:"--leave-running"      cli_group:"lifecycle"`
	TcpEstablished      bool   `cli_flag:"--tcp-established"    cli_group:"criu"`
	TcpSkipInFlight     bool   `cli_flag:"--tcp-skip-in-flight" cli_group:"criu"`
	LinkRemap           bool   `cli_flag:"--link-remap"         cli_group:"criu"`
	ExternalUnixSockets bool   `cli_flag:"--ext-unix-sk"        cli_group:"criu"`
	ShellJob            bool   `cli_flag:"--shell-job"          cli_group:"criu"`
	LazyPages           bool   `cli_flag:"--lazy-pages"         cli_group:"criu"`
	StatusFD            *uint  `cli_flag:"--status-fd"          cli_group:"criu"`
	PageServer          string `cli_flag:"--page-server"        cli_group:"criu"` // "IP:port"
	FileLocks           bool   `cli_flag:"--file-locks"         cli_group:"criu"`
	PreDump             bool   `cli_flag:"--pre-dump"           cli_group:"criu"`
	ManageCgroupsMode   string `cli_flag:"--manage-cgroups-mode" cli_group:"cgroups" cli_enum:"soft|full|strict|ignore"`
	EmptyNameSpace      string `cli_flag:"--empty-ns"           cli_group:"namespaces"`
	AutoDedup           bool   `cli_flag:"--auto-dedup"         cli_group:"criu"`

	// args
	ContainerID string `cli_argument:"container_id"`
}

func (Checkpoint) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "images"},
			cli.FlagGroup{Name: "criu"},
			cli.FlagGroup{Name: "cgroups"},
			cli.FlagGroup{Name: "namespaces"},
			cli.FlagGroup{Name: "lifecycle"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "checkpoint"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// restore
// Manpage: runc-restore(8) — https://manpages.debian.org/bookworm/runc/runc-restore.8.en.html
// ------------------------------------------------------------

type Restore struct {
	Global
	BundleOpt
	ConsoleSocketOpt
	PivotKeyringFDsOpt
	DetachOpt
	PidFileOpt

	// flags
	ImagePath         string `cli_flag:"--image-path"         cli_group:"images"`
	WorkPath          string `cli_flag:"--work-path"          cli_group:"images"`
	TcpEstablished    bool   `cli_flag:"--tcp-established"    cli_group:"criu"`
	ExternalUnixSk    bool   `cli_flag:"--ext-unix-sk"        cli_group:"criu"`
	ShellJob          bool   `cli_flag:"--shell-job"          cli_group:"criu"`
	FileLocks         bool   `cli_flag:"--file-locks"         cli_group:"criu"`
	ManageCgroupsMode string `cli_flag:"--manage-cgroups-mode" cli_group:"cgroups" cli_enum:"soft|full|strict|ignore"`
	NoSubreaper       bool   `cli_flag:"--no-subreaper"       cli_group:"lifecycle"`
	EmptyNS           string `cli_flag:"--empty-ns"           cli_group:"namespaces"`
	AutoDedup         bool   `cli_flag:"--auto-dedup"         cli_group:"criu"`
	LazyPages         bool   `cli_flag:"--lazy-pages"         cli_group:"criu"`
	LSMProfile        string `cli_flag:"--lsm-profile"        cli_group:"security"` // "type:label"
	LSMMountContext   string `cli_flag:"--lsm-mount-context"  cli_group:"security"` // SELinux mount context

	// args
	ContainerID string `cli_argument:"container_id"`
}

func (Restore) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "bundle"},
			cli.FlagGroup{Name: "console"},
			cli.FlagGroup{Name: "runtime"},
			cli.FlagGroup{Name: "lifecycle"},
			cli.FlagGroup{Name: "images"},
			cli.FlagGroup{Name: "criu"},
			cli.FlagGroup{Name: "cgroups"},
			cli.FlagGroup{Name: "namespaces"},
			cli.FlagGroup{Name: "security"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "restore"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// create
// Manpage: runc-create(8) — https://manpages.debian.org/bookworm/runc/runc-create.8.en.html
// ------------------------------------------------------------

type Create struct {
	Global
	BundleOpt
	ConsoleSocketOpt
	PivotKeyringFDsOpt
	PidFileOpt

	// args
	ContainerID string `cli_argument:"container_id"`
}

func (Create) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "bundle"},
			cli.FlagGroup{Name: "console"},
			cli.FlagGroup{Name: "runtime"},
			cli.FlagGroup{Name: "lifecycle"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "create"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// run
// Manpage: runc-run(8) — https://manpages.debian.org/bookworm/runc/runc-run.8.en.html
// ------------------------------------------------------------

type Run struct {
	Global
	BundleOpt
	ConsoleSocketOpt
	PivotKeyringFDsOpt
	DetachOpt
	PidFileOpt

	NoSubreaper bool   `cli_flag:"--no-subreaper" cli_group:"lifecycle"`
	Keep        bool   `cli_flag:"--keep"         cli_group:"lifecycle"`
	ContainerID string `cli_argument:"container_id"`
}

func (Run) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "bundle"},
			cli.FlagGroup{Name: "console"},
			cli.FlagGroup{Name: "runtime"},
			cli.FlagGroup{Name: "lifecycle"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "run"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// start
// Manpage: runc-start(8) — https://manpages.debian.org/bookworm/runc/runc-start.8.en.html
// ------------------------------------------------------------

type Start struct {
	Global
	ContainerID string `cli_argument:"container_id"`
}

func (Start) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "start"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// delete
// Manpage: runc-delete(8) — https://manpages.debian.org/bookworm/runc/runc-delete.8.en.html
// ------------------------------------------------------------

type Delete struct {
	Global
	Force       bool   `cli_flag:"--force" cli_flag_alternatives:"-f" cli_group:"common"`
	ContainerID string `cli_argument:"container_id"`
}

func (Delete) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "common"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "delete"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// pause
// Manpage: runc-pause(8) — https://manpages.debian.org/bookworm/runc/runc-pause.8.en.html
// ------------------------------------------------------------

type Pause struct {
	Global
	ContainerID string `cli_argument:"container_id"`
}

func (Pause) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "pause"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// resume
// Manpage: runc-resume(8) — https://manpages.debian.org/bookworm/runc/runc-resume.8.en.html
// ------------------------------------------------------------

type Resume struct {
	Global
	ContainerID string `cli_argument:"container_id"`
}

func (Resume) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "resume"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// kill
// Manpage: runc-kill(8) — https://manpages.debian.org/bookworm/runc/runc-kill.8.en.html
// ------------------------------------------------------------

type Kill struct {
	Global
	All         bool   `cli_flag:"--all" cli_group:"common"`
	ContainerID string `cli_argument:"container_id"`
	Signal      string `cli_argument:"signal"` // optional; defaults to SIGTERM if empty
}

func (Kill) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "common"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "kill"},
			cli.Argument{Name: "container_id"},
			cli.Argument{Name: "signal"},
		},
	}
}

// ------------------------------------------------------------
// list
// Manpage: runc-list(8) — https://manpages.debian.org/bookworm/runc/runc-list.8.en.html
// ------------------------------------------------------------

type List struct {
	Global
	FormatOpt
	Quiet bool `cli_flag:"--quiet" cli_flag_alternatives:"-q" cli_group:"output"`
}

func (List) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "output"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "list"},
		},
	}
}

// ------------------------------------------------------------
// ps
// Manpage: runc-ps(8) — https://manpages.debian.org/bookworm/runc/runc-ps.8.en.html
// ------------------------------------------------------------

type Ps struct {
	Global
	FormatOpt

	ContainerID string   `cli_argument:"container_id"`
	PsArgs      []string `cli_argument:"ps_args"` // forwarded to ps(1)
}

func (Ps) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "output"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "ps"},
			cli.Argument{Name: "container_id"},
			cli.Arguments{Name: "ps_args"},
		},
	}
}

// ------------------------------------------------------------
// state
// Manpage: runc-state(8) — https://manpages.debian.org/bookworm/runc/runc-state.8.en.html
// ------------------------------------------------------------

type State struct {
	Global
	ContainerID string `cli_argument:"container_id"`
}

func (State) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "state"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// events
// Manpage: runc-events(8) — https://manpages.debian.org/bookworm/runc/runc-events.8.en.html
// ------------------------------------------------------------

type Events struct {
	Global
	Interval    string `cli_flag:"--interval" cli_group:"events"` // e.g. "5s"
	Stats       bool   `cli_flag:"--stats"    cli_group:"events"`
	ContainerID string `cli_argument:"container_id"`
}

func (Events) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "events"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "events"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// exec
// Manpage: runc-exec(8) — https://manpages.debian.org/bookworm/runc/runc-exec.8.en.html
// ------------------------------------------------------------

type Exec struct {
	Global
	ConsoleSocketOpt
	DetachOpt
	PidFileOpt
	PivotKeyringFDsOpt

	// flags
	Cwd            string   `cli_flag:"--cwd"            cli_group:"process"`
	Env            []string `cli_flag:"--env"            cli_flag_alternatives:"-e" cli_group:"process"` // key=value
	Tty            bool     `cli_flag:"--tty"            cli_flag_alternatives:"-t" cli_group:"process"`
	User           string   `cli_flag:"--user"           cli_flag_alternatives:"-u" cli_group:"process"` // uid[:gid]
	AdditionalGids []uint   `cli_flag:"--additional-gids" cli_flag_alternatives:"-g" cli_group:"process"`
	Process        string   `cli_flag:"--process"        cli_flag_alternatives:"-p" cli_group:"process"` // process.json
	ProcessLabel   string   `cli_flag:"--process-label"  cli_group:"security"`
	AppArmor       string   `cli_flag:"--apparmor"       cli_group:"security"`
	NoNewPrivs     bool     `cli_flag:"--no-new-privs"   cli_group:"security"`
	Cap            []string `cli_flag:"--cap"            cli_flag_alternatives:"-c" cli_group:"security"`
	IgnorePaused   bool     `cli_flag:"--ignore-paused"  cli_group:"lifecycle"`
	Cgroup         string   `cli_flag:"--cgroup"         cli_group:"cgroups"` // v1 semantics

	// args
	ContainerID string   `cli_argument:"container_id"`
	Command     string   `cli_argument:"command"`
	Args        []string `cli_argument:"args"`
}

func (Exec) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "console"},
			cli.FlagGroup{Name: "lifecycle"},
			cli.FlagGroup{Name: "runtime"},
			cli.FlagGroup{Name: "process"},
			cli.FlagGroup{Name: "security"},
			cli.FlagGroup{Name: "cgroups"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "exec"},
			cli.Argument{Name: "container_id"},
			cli.Group{
				Ordered: []cli.Slot{
					cli.Literal{Value: "--"},
					cli.Argument{Name: "command"},
					cli.Arguments{Name: "args"},
				},
			},
		},
	}
}

// ------------------------------------------------------------
// spec
// Manpage: runc-spec(8) — https://manpages.debian.org/bookworm/runc/runc-spec.8.en.html
// ------------------------------------------------------------

type Spec struct {
	Global
	BundleOpt
	Rootless bool `cli_flag:"--rootless" cli_group:"spec"`
}

func (Spec) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "bundle"},
			cli.FlagGroup{Name: "spec"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "spec"},
		},
	}
}

// ------------------------------------------------------------
// update
// Manpage: runc-update(8) — https://manpages.debian.org/bookworm/runc/runc-update.8.en.html
// ------------------------------------------------------------

type Update struct {
	Global
	// args
	ContainerID string `cli_argument:"container_id"`

	// flags (grouped)
	ReadFromJSON string  `cli_flag:"-r"               cli_flag_alternatives:"--resources" cli_group:"mode"` // path or "-" for stdin
	CPUQuota     *int64  `cli_flag:"--cpu-quota"      cli_group:"cpu"`
	CPUPeriod    *uint64 `cli_flag:"--cpu-period"     cli_group:"cpu"`
	CPUShares    *uint64 `cli_flag:"--cpu-shares"     cli_group:"cpu"`
	CPUSetCPUs   string  `cli_flag:"--cpuset-cpus"    cli_group:"cpu"`
	CPUSetMems   string  `cli_flag:"--cpuset-mems"    cli_group:"cpu"`

	Memory            *int64 `cli_flag:"--memory"             cli_group:"memory"`
	MemorySwap        *int64 `cli_flag:"--memory-swap"        cli_group:"memory"`
	MemoryReservation *int64 `cli_flag:"--memory-reservation" cli_group:"memory"`
	KernelMemory      *int64 `cli_flag:"--kernel-memory"      cli_group:"memory"`

	PidsLimit   *int64  `cli_flag:"--pids-limit"  cli_group:"pids"`
	BlkioWeight *uint16 `cli_flag:"--blkio-weight" cli_group:"io"`
}

func (Update) Slots() cli.Slot {
	return cli.Group{
		Unordered: []cli.Slot{
			cli.FlagGroup{Name: "mode"},
			cli.FlagGroup{Name: "cpu"},
			cli.FlagGroup{Name: "memory"},
			cli.FlagGroup{Name: "pids"},
			cli.FlagGroup{Name: "io"},
		},
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "update"},
			cli.Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// features
// Manpage: not available
// ------------------------------------------------------------

type Features struct {
	Global
}

func (Features) Slots() cli.Slot {
	return cli.Group{
		Ordered: []cli.Slot{
			cli.FlagGroup{Name: "global"},
			cli.Subcommand{Value: "features"},
		},
	}
}
