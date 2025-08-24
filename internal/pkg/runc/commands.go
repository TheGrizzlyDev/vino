package runc

// ------------------------------------------------------------
// Common, embeddable option groups (no Subcommand() here)
// ------------------------------------------------------------

// BundleOpt holds the OCI bundle path.
type BundleOpt struct {
	Bundle string `runc_flag:"--bundle" runc_flag_alternatives:"-b" runc_group:"bundle"`
}

// ConsoleSocketOpt holds the console socket path.
type ConsoleSocketOpt struct {
	ConsoleSocket string `runc_flag:"--console-socket" runc_group:"console"`
}

// PidFileOpt writes the pid to a file (used with detach).
type PidFileOpt struct {
	PidFile string `runc_flag:"--pid-file" runc_group:"lifecycle"`
}

// PivotKeyringFDsOpt groups common runtime toggles.
type PivotKeyringFDsOpt struct {
	NoPivot      bool  `runc_flag:"--no-pivot" runc_group:"runtime"`
	NoNewKeyring bool  `runc_flag:"--no-new-keyring" runc_group:"runtime"`
	PreserveFDs  *uint `runc_flag:"--preserve-fds" runc_group:"runtime"`
}

// DetachOpt for detach-capable commands (run/restore/exec).
type DetachOpt struct {
	Detach bool `runc_flag:"--detach" runc_flag_alternatives:"-d" runc_group:"lifecycle"`
}

// FormatOpt standard output-format selector (table/json).
type FormatOpt struct {
	Format string `runc_flag:"--format" runc_flag_alternatives:"-f" runc_group:"output" runc_enum:"table|json"`
}

// ------------------------------------------------------------
// Global options (no Subcommand)
// Manpage: runc(8) — https://manpages.debian.org/bookworm/runc/runc.8.en.html
// ------------------------------------------------------------

type Global struct {
	Root          string `runc_flag:"--root"            runc_group:"global"`
	Debug         bool   `runc_flag:"--debug"           runc_group:"global"`
	Log           string `runc_flag:"--log"             runc_group:"global"`
	LogFormat     string `runc_flag:"--log-format"      runc_group:"global"`
	SystemdCgroup bool   `runc_flag:"--systemd-cgroup"  runc_group:"global"`
	Criu          string `runc_flag:"--criu"            runc_group:"global"`
	Rootless      string `runc_flag:"--rootless"        runc_group:"global" runc_enum:"true|false|auto"`
}

// ------------------------------------------------------------
// checkpoint
// Manpage: runc-checkpoint(8) — https://manpages.debian.org/bookworm/runc/runc-checkpoint.8.en.html
// ------------------------------------------------------------

type Checkpoint struct {
	Global
	// flags
	ImagePath           string `runc_flag:"--image-path"         runc_group:"images"`
	WorkPath            string `runc_flag:"--work-path"          runc_group:"images"`
	ParentPath          string `runc_flag:"--parent-path"        runc_group:"images"`
	LeaveRunning        bool   `runc_flag:"--leave-running"      runc_group:"lifecycle"`
	TcpEstablished      bool   `runc_flag:"--tcp-established"    runc_group:"criu"`
	TcpSkipInFlight     bool   `runc_flag:"--tcp-skip-in-flight" runc_group:"criu"`
	LinkRemap           bool   `runc_flag:"--link-remap"         runc_group:"criu"`
	ExternalUnixSockets bool   `runc_flag:"--ext-unix-sk"        runc_group:"criu"`
	ShellJob            bool   `runc_flag:"--shell-job"          runc_group:"criu"`
	LazyPages           bool   `runc_flag:"--lazy-pages"         runc_group:"criu"`
	StatusFD            *uint  `runc_flag:"--status-fd"          runc_group:"criu"`
	PageServer          string `runc_flag:"--page-server"        runc_group:"criu"` // "IP:port"
	FileLocks           bool   `runc_flag:"--file-locks"         runc_group:"criu"`
	PreDump             bool   `runc_flag:"--pre-dump"           runc_group:"criu"`
	ManageCgroupsMode   string `runc_flag:"--manage-cgroups-mode" runc_group:"cgroups" runc_enum:"soft|full|strict|ignore"`
	EmptyNameSpace      string `runc_flag:"--empty-ns"           runc_group:"namespaces"`
	AutoDedup           bool   `runc_flag:"--auto-dedup"         runc_group:"criu"`

	// args
	ContainerID string `runc_argument:"container_id"`
}

func (Checkpoint) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "images"},
			FlagGroup{Name: "criu"},
			FlagGroup{Name: "cgroups"},
			FlagGroup{Name: "namespaces"},
			FlagGroup{Name: "lifecycle"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "checkpoint"},
			Argument{Name: "container_id"},
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
	ImagePath         string `runc_flag:"--image-path"         runc_group:"images"`
	WorkPath          string `runc_flag:"--work-path"          runc_group:"images"`
	TcpEstablished    bool   `runc_flag:"--tcp-established"    runc_group:"criu"`
	ExternalUnixSk    bool   `runc_flag:"--ext-unix-sk"        runc_group:"criu"`
	ShellJob          bool   `runc_flag:"--shell-job"          runc_group:"criu"`
	FileLocks         bool   `runc_flag:"--file-locks"         runc_group:"criu"`
	ManageCgroupsMode string `runc_flag:"--manage-cgroups-mode" runc_group:"cgroups" runc_enum:"soft|full|strict|ignore"`
	NoSubreaper       bool   `runc_flag:"--no-subreaper"       runc_group:"lifecycle"`
	EmptyNS           string `runc_flag:"--empty-ns"           runc_group:"namespaces"`
	AutoDedup         bool   `runc_flag:"--auto-dedup"         runc_group:"criu"`
	LazyPages         bool   `runc_flag:"--lazy-pages"         runc_group:"criu"`
	LSMProfile        string `runc_flag:"--lsm-profile"        runc_group:"security"` // "type:label"
	LSMMountContext   string `runc_flag:"--lsm-mount-context"  runc_group:"security"` // SELinux mount context

	// args
	ContainerID string `runc_argument:"container_id"`
}

func (Restore) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "bundle"},
			FlagGroup{Name: "console"},
			FlagGroup{Name: "runtime"},
			FlagGroup{Name: "lifecycle"},
			FlagGroup{Name: "images"},
			FlagGroup{Name: "criu"},
			FlagGroup{Name: "cgroups"},
			FlagGroup{Name: "namespaces"},
			FlagGroup{Name: "security"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "restore"},
			Argument{Name: "container_id"},
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
	ContainerID string `runc_argument:"container_id"`
}

func (Create) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "bundle"},
			FlagGroup{Name: "console"},
			FlagGroup{Name: "runtime"},
			FlagGroup{Name: "lifecycle"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "create"},
			Argument{Name: "container_id"},
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

	NoSubreaper bool   `runc_flag:"--no-subreaper" runc_group:"lifecycle"`
	Keep        bool   `runc_flag:"--keep"         runc_group:"lifecycle"`
	ContainerID string `runc_argument:"container_id"`
}

func (Run) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "bundle"},
			FlagGroup{Name: "console"},
			FlagGroup{Name: "runtime"},
			FlagGroup{Name: "lifecycle"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "run"},
			Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// start
// Manpage: runc-start(8) — https://manpages.debian.org/bookworm/runc/runc-start.8.en.html
// ------------------------------------------------------------

type Start struct {
	Global
	ContainerID string `runc_argument:"container_id"`
}

func (Start) Slots() Slot {
	return Group{
		Ordered: []Slot{
			FlagGroup{"global"},
			Subcommand{Value: "start"},
			Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// delete
// Manpage: runc-delete(8) — https://manpages.debian.org/bookworm/runc/runc-delete.8.en.html
// ------------------------------------------------------------

type Delete struct {
	Global
	Force       bool   `runc_flag:"--force" runc_flag_alternatives:"-f" runc_group:"common"`
	ContainerID string `runc_argument:"container_id"`
}

func (Delete) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{"common"},
		},
		Ordered: []Slot{
			FlagGroup{"global"},
			Subcommand{Value: "delete"},
			Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// pause
// Manpage: runc-pause(8) — https://manpages.debian.org/bookworm/runc/runc-pause.8.en.html
// ------------------------------------------------------------

type Pause struct {
	Global
	ContainerID string `runc_argument:"container_id"`
}

func (Pause) Slots() Slot {
	return Group{
		Ordered: []Slot{
			FlagGroup{"global"},
			Subcommand{Value: "pause"},
			Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// resume
// Manpage: runc-resume(8) — https://manpages.debian.org/bookworm/runc/runc-resume.8.en.html
// ------------------------------------------------------------

type Resume struct {
	Global
	ContainerID string `runc_argument:"container_id"`
}

func (Resume) Slots() Slot {
	return Group{
		Ordered: []Slot{
			FlagGroup{"global"},
			Subcommand{Value: "resume"},
			Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// kill
// Manpage: runc-kill(8) — https://manpages.debian.org/bookworm/runc/runc-kill.8.en.html
// ------------------------------------------------------------

type Kill struct {
	Global
	All         bool   `runc_flag:"--all" runc_group:"common"`
	ContainerID string `runc_argument:"container_id"`
	Signal      string `runc_argument:"signal"` // optional; defaults to SIGTERM if empty
}

func (Kill) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "common"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "kill"},
			Argument{Name: "container_id"},
			Argument{Name: "signal"},
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
	Quiet bool `runc_flag:"--quiet" runc_flag_alternatives:"-q" runc_group:"output"`
}

func (List) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "output"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "list"},
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

	ContainerID string   `runc_argument:"container_id"`
	PsArgs      []string `runc_argument:"ps_args"` // forwarded to ps(1)
}

func (Ps) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "output"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "ps"},
			Argument{Name: "container_id"},
			Arguments{Name: "ps_args"},
		},
	}
}

// ------------------------------------------------------------
// state
// Manpage: runc-state(8) — https://manpages.debian.org/bookworm/runc/runc-state.8.en.html
// ------------------------------------------------------------

type State struct {
	Global
	ContainerID string `runc_argument:"container_id"`
}

func (State) Slots() Slot {
	return Group{
		Ordered: []Slot{
			FlagGroup{"global"},
			Subcommand{Value: "state"},
			Argument{Name: "container_id"},
		},
	}
}

// ------------------------------------------------------------
// events
// Manpage: runc-events(8) — https://manpages.debian.org/bookworm/runc/runc-events.8.en.html
// ------------------------------------------------------------

type Events struct {
	Global
	Interval    string `runc_flag:"--interval" runc_group:"events"` // e.g. "5s"
	Stats       bool   `runc_flag:"--stats"    runc_group:"events"`
	ContainerID string `runc_argument:"container_id"`
}

func (Events) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "events"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "events"},
			Argument{Name: "container_id"},
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
	Cwd            string   `runc_flag:"--cwd"            runc_group:"process"`
	Env            []string `runc_flag:"--env"            runc_flag_alternatives:"-e" runc_group:"process"` // key=value
	Tty            bool     `runc_flag:"--tty"            runc_flag_alternatives:"-t" runc_group:"process"`
	User           string   `runc_flag:"--user"           runc_flag_alternatives:"-u" runc_group:"process"` // uid[:gid]
	AdditionalGids []uint   `runc_flag:"--additional-gids" runc_flag_alternatives:"-g" runc_group:"process"`
	Process        string   `runc_flag:"--process"        runc_flag_alternatives:"-p" runc_group:"process"` // process.json
	ProcessLabel   string   `runc_flag:"--process-label"  runc_group:"security"`
	AppArmor       string   `runc_flag:"--apparmor"       runc_group:"security"`
	NoNewPrivs     bool     `runc_flag:"--no-new-privs"   runc_group:"security"`
	Cap            []string `runc_flag:"--cap"            runc_flag_alternatives:"-c" runc_group:"security"`
	IgnorePaused   bool     `runc_flag:"--ignore-paused"  runc_group:"lifecycle"`
	Cgroup         string   `runc_flag:"--cgroup"         runc_group:"cgroups"` // v1 semantics

	// args
	ContainerID string   `runc_argument:"container_id"`
	Command     string   `runc_argument:"command"`
	Args        []string `runc_argument:"args"`
}

func (Exec) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{"console"},
			FlagGroup{"lifecycle"},
			FlagGroup{"runtime"},
			FlagGroup{"process"},
			FlagGroup{"security"},
			FlagGroup{"cgroups"},
		},
		Ordered: []Slot{
			FlagGroup{"global"},
			Subcommand{Value: "exec"},
			Argument{Name: "container_id"},
			Group{
				Ordered: []Slot{
					Literal{Value: "--"},
					Argument{Name: "command"},
					Arguments{Name: "args"},
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
	Rootless bool `runc_flag:"--rootless" runc_group:"spec"`
}

func (Spec) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "bundle"},
			FlagGroup{Name: "spec"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "spec"},
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
	ContainerID string `runc_argument:"container_id"`

	// flags (grouped)
	ReadFromJSON string  `runc_flag:"-r"               runc_flag_alternatives:"--resources" runc_group:"mode"` // path or "-" for stdin
	CPUQuota     *int64  `runc_flag:"--cpu-quota"      runc_group:"cpu"`
	CPUPeriod    *uint64 `runc_flag:"--cpu-period"     runc_group:"cpu"`
	CPUShares    *uint64 `runc_flag:"--cpu-shares"     runc_group:"cpu"`
	CPUSetCPUs   string  `runc_flag:"--cpuset-cpus"    runc_group:"cpu"`
	CPUSetMems   string  `runc_flag:"--cpuset-mems"    runc_group:"cpu"`

	Memory            *int64 `runc_flag:"--memory"             runc_group:"memory"`
	MemorySwap        *int64 `runc_flag:"--memory-swap"        runc_group:"memory"`
	MemoryReservation *int64 `runc_flag:"--memory-reservation" runc_group:"memory"`
	KernelMemory      *int64 `runc_flag:"--kernel-memory"      runc_group:"memory"`

	PidsLimit   *int64  `runc_flag:"--pids-limit"  runc_group:"pids"`
	BlkioWeight *uint16 `runc_flag:"--blkio-weight" runc_group:"io"`
}

func (Update) Slots() Slot {
	return Group{
		Unordered: []Slot{
			FlagGroup{Name: "mode"},
			FlagGroup{Name: "cpu"},
			FlagGroup{Name: "memory"},
			FlagGroup{Name: "pids"},
			FlagGroup{Name: "io"},
		},
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "update"},
			Argument{Name: "container_id"},
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

func (Features) Slots() Slot {
	return Group{
		Ordered: []Slot{
			FlagGroup{Name: "global"},
			Subcommand{Value: "features"},
		},
	}
}
