package runc

// ------------------------------------------------------------
// Common, embeddable option groups (no Subcommand() here)
// ------------------------------------------------------------

// BundleOpt holds the OCI bundle path.
type BundleOpt struct {
	Bundle string `runc_flag:"--bundle" runc_group:"bundle"`
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
	NoPivot      bool `runc_flag:"--no-pivot" runc_group:"runtime"`
	NoNewKeyring bool `runc_flag:"--no-new-keyring" runc_group:"runtime"`
	PreserveFDs  uint `runc_flag:"--preserve-fds" runc_group:"runtime"`
}

// DetachOpt for detach-capable commands (run/restore/exec).
type DetachOpt struct {
	Detach bool `runc_flag:"--detach" runc_group:"lifecycle"`
}

// FormatOpt standard output-format selector (table/json).
type FormatOpt struct {
	Format string `runc_flag:"--format" runc_group:"output" runc_enum:"table|json"`
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

func (g Global) Groups() []string { return []string{"global"} }

// ------------------------------------------------------------
// checkpoint
// Manpage: runc-checkpoint(8) — https://manpages.debian.org/bookworm/runc/runc-checkpoint.8.en.html
// ------------------------------------------------------------

type Checkpoint struct {
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
	StatusFD            uint   `runc_flag:"--status-fd"          runc_group:"criu"`
	PageServer          string `runc_flag:"--page-server"        runc_group:"criu"` // "IP:port"
	FileLocks           bool   `runc_flag:"--file-locks"         runc_group:"criu"`
	PreDump             bool   `runc_flag:"--pre-dump"           runc_group:"criu"`
	ManageCgroupsMode   string `runc_flag:"--manage-cgroups-mode" runc_group:"cgroups" runc_enum:"soft|full|strict|ignore"`
	EmptyNameSpace      string `runc_flag:"--empty-ns"           runc_group:"namespaces"`
	AutoDedup           bool   `runc_flag:"--auto-dedup"         runc_group:"criu"`

	// args
	ContainerID string `runc_argument:"container_id"`
}

func (c Checkpoint) Subcommand() string { return "checkpoint" }
func (c Checkpoint) Groups() []string {
	return []string{"global", "images", "criu", "cgroups", "namespaces", "lifecycle", "container_id"}
}

// ------------------------------------------------------------
// restore
// Manpage: runc-restore(8) — https://manpages.debian.org/bookworm/runc/runc-restore.8.en.html
// ------------------------------------------------------------

type Restore struct {
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

func (r Restore) Subcommand() string { return "restore" }
func (r Restore) Groups() []string {
	return []string{"global", "bundle", "console", "runtime", "lifecycle", "images", "criu", "cgroups", "namespaces", "security", "container_id"}
}

// ------------------------------------------------------------
// create
// Manpage: runc-create(8) — https://manpages.debian.org/bookworm/runc/runc-create.8.en.html
// ------------------------------------------------------------

type Create struct {
	BundleOpt
	ConsoleSocketOpt
	PivotKeyringFDsOpt
	PidFileOpt

	// args
	ContainerID string `runc_argument:"container_id"`
}

func (c Create) Subcommand() string { return "create" }
func (c Create) Groups() []string {
	return []string{"global", "bundle", "console", "runtime", "lifecycle", "container_id"}
}

// ------------------------------------------------------------
// run
// Manpage: runc-run(8) — https://manpages.debian.org/bookworm/runc/runc-run.8.en.html
// ------------------------------------------------------------

type Run struct {
	BundleOpt
	ConsoleSocketOpt
	PivotKeyringFDsOpt
	DetachOpt
	PidFileOpt

	NoSubreaper bool   `runc_flag:"--no-subreaper" runc_group:"lifecycle"`
	Keep        bool   `runc_flag:"--keep"         runc_group:"lifecycle"`
	ContainerID string `runc_argument:"container_id"`
}

func (r Run) Subcommand() string { return "run" }
func (r Run) Groups() []string {
	return []string{"global", "bundle", "console", "runtime", "lifecycle", "container_id"}
}

// ------------------------------------------------------------
// start
// Manpage: runc-start(8) — https://manpages.debian.org/bookworm/runc/runc-start.8.en.html
// ------------------------------------------------------------

type Start struct {
	ContainerID string `runc_argument:"container_id"`
}

func (s Start) Subcommand() string { return "start" }
func (s Start) Groups() []string   { return []string{"global", "container_id"} }

// ------------------------------------------------------------
// delete
// Manpage: runc-delete(8) — https://manpages.debian.org/bookworm/runc/runc-delete.8.en.html
// ------------------------------------------------------------

type Delete struct {
	Force       bool   `runc_flag:"--force" runc_group:"common"`
	ContainerID string `runc_argument:"container_id"`
}

func (d Delete) Subcommand() string { return "delete" }
func (d Delete) Groups() []string   { return []string{"global", "common", "container_id"} }

// ------------------------------------------------------------
// pause
// Manpage: runc-pause(8) — https://manpages.debian.org/bookworm/runc/runc-pause.8.en.html
// ------------------------------------------------------------

type Pause struct {
	ContainerID string `runc_argument:"container_id"`
}

func (p Pause) Subcommand() string { return "pause" }
func (p Pause) Groups() []string   { return []string{"global", "container_id"} }

// ------------------------------------------------------------
// resume
// Manpage: runc-resume(8) — https://manpages.debian.org/bookworm/runc/runc-resume.8.en.html
// ------------------------------------------------------------

type Resume struct {
	ContainerID string `runc_argument:"container_id"`
}

func (r Resume) Subcommand() string { return "resume" }
func (r Resume) Groups() []string   { return []string{"global", "container_id"} }

// ------------------------------------------------------------
// kill
// Manpage: runc-kill(8) — https://manpages.debian.org/bookworm/runc/runc-kill.8.en.html
// ------------------------------------------------------------

type Kill struct {
	All         bool   `runc_flag:"--all" runc_group:"common"`
	ContainerID string `runc_argument:"container_id"`
	Signal      string `runc_argument:"signal"` // optional; defaults to SIGTERM if empty
}

func (k Kill) Subcommand() string { return "kill" }
func (k Kill) Groups() []string   { return []string{"global", "common", "container_id", "signal"} }

// ------------------------------------------------------------
// list
// Manpage: runc-list(8) — https://manpages.debian.org/bookworm/runc/runc-list.8.en.html
// ------------------------------------------------------------

type List struct {
	FormatOpt
	Quiet bool `runc_flag:"--quiet" runc_group:"output"`
}

func (l List) Subcommand() string { return "list" }
func (l List) Groups() []string   { return []string{"global", "output"} }

// ------------------------------------------------------------
// ps
// Manpage: runc-ps(8) — https://manpages.debian.org/bookworm/runc/runc-ps.8.en.html
// ------------------------------------------------------------

type Ps struct {
	FormatOpt

	ContainerID string   `runc_argument:"container_id"`
	PsArgs      []string `runc_argument:"ps_args"` // forwarded to ps(1)
}

func (p Ps) Subcommand() string { return "ps" }
func (p Ps) Groups() []string   { return []string{"global", "output", "container_id", "ps_args"} }

// ------------------------------------------------------------
// state
// Manpage: runc-state(8) — https://manpages.debian.org/bookworm/runc/runc-state.8.en.html
// ------------------------------------------------------------

type State struct {
	ContainerID string `runc_argument:"container_id"`
}

func (s State) Subcommand() string { return "state" }
func (s State) Groups() []string   { return []string{"global", "container_id"} }

// ------------------------------------------------------------
// events
// Manpage: runc-events(8) — https://manpages.debian.org/bookworm/runc/runc-events.8.en.html
// ------------------------------------------------------------

type Events struct {
	Interval    string `runc_flag:"--interval" runc_group:"events"` // e.g. "5s"
	Stats       bool   `runc_flag:"--stats"    runc_group:"events"`
	ContainerID string `runc_argument:"container_id"`
}

func (e Events) Subcommand() string { return "events" }
func (e Events) Groups() []string   { return []string{"global", "events", "container_id"} }

// ------------------------------------------------------------
// exec
// Manpage: runc-exec(8) — https://manpages.debian.org/bookworm/runc/runc-exec.8.en.html
// ------------------------------------------------------------

type Exec struct {
	ConsoleSocketOpt
	DetachOpt
	PidFileOpt
	PivotKeyringFDsOpt

	// flags
	Cwd            string   `runc_flag:"--cwd"            runc_group:"process"`
	Env            []string `runc_flag:"--env"            runc_group:"process"` // key=value
	Tty            bool     `runc_flag:"--tty"            runc_group:"process"`
	User           string   `runc_flag:"--user"           runc_group:"process"` // uid[:gid]
	AdditionalGids []uint   `runc_flag:"--additional-gids" runc_group:"process"`
	Process        string   `runc_flag:"--process"        runc_group:"process"` // process.json
	ProcessLabel   string   `runc_flag:"--process-label"  runc_group:"security"`
	AppArmor       string   `runc_flag:"--apparmor"       runc_group:"security"`
	NoNewPrivs     bool     `runc_flag:"--no-new-privs"   runc_group:"security"`
	Cap            []string `runc_flag:"--cap"            runc_group:"security"`
	IgnorePaused   bool     `runc_flag:"--ignore-paused"  runc_group:"lifecycle"`
	Cgroup         string   `runc_flag:"--cgroup"         runc_group:"cgroups"` // v1 semantics

	// args
	ContainerID string   `runc_argument:"container_id"`
	Command     string   `runc_argument:"command"`
	Args        []string `runc_argument:"args"`
}

func (e Exec) Subcommand() string { return "exec" }
func (e Exec) Groups() []string {
	// Insert "--" as a literal group between container_id and command.
	return []string{"global", "console", "lifecycle", "runtime", "process", "security", "cgroups", "container_id", "--", "command", "args"}
}

// ------------------------------------------------------------
// spec
// Manpage: runc-spec(8) — https://manpages.debian.org/bookworm/runc/runc-spec.8.en.html
// ------------------------------------------------------------

type Spec struct {
	BundleOpt
	Rootless bool `runc_flag:"--rootless" runc_group:"spec"`
}

func (s Spec) Subcommand() string { return "spec" }
func (s Spec) Groups() []string   { return []string{"global", "bundle", "spec"} }

// ------------------------------------------------------------
// update
// Manpage: runc-update(8) — https://manpages.debian.org/bookworm/runc/runc-update.8.en.html
// ------------------------------------------------------------

type Update struct {
	// args
	ContainerID string `runc_argument:"container_id"`

	// flags (grouped)
	ReadFromJSON string  `runc_flag:"-r"               runc_group:"mode"` // path or "-" for stdin
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

func (u Update) Subcommand() string { return "update" }
func (u Update) Groups() []string {
	return []string{"global", "container_id", "mode", "cpu", "memory", "pids", "io"}
}
