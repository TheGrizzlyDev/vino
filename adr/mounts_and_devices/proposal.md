# ADR: Mounts and Devices for Wine-backed Containers

## Context

Linux, and in general the Unix-like family of OSes, represents devices through files in the `/dev` file system. Devices are broadly divided into:

* **Character devices:** expose a stream interface (`read/write/ioctl`), e.g. serial ports, ttys.
* **Block devices:** expose a block I/O interface, e.g. disks, CD/DVD drives.
* **Network devices:** not exposed under `/dev` (configured via netlink APIs), but sockets are still file descriptors and behave like devices from a process perspective.

This model is **low-level and semantic-agnostic**: the kernel gives you an I/O interface, not a description of what the device *is*.

Windows takes a very different approach. It defines **system-defined device setup classes**, each identified by a GUID, which describe the category of the device (disk, CD-ROM, display adapter, etc.) and the APIs that apply to it.[^win-device-classes] This categorization implicitly captures I/O semantics (e.g. DiskDevice → block I/O).

The mismatch makes it difficult to represent the relationship between a Linux device node and its Windows counterpart under the current OCI mounts/devices semantics. The situation is even harder in higher-level runtimes:

* **Volume mounts:** a request like `-v /some/host/path:C:\some\win\path` is rejected by Docker on Linux, because `C:\…` is not a valid target path.
* **Devices:** you cannot guess the “class” of a Linux device in a way that maps cleanly to Windows device classes; the Windows classification is a **superset** of Linux’s.

On top of this, Wine (our compatibility layer) expects a **Windows-like view** of the world: drive letters (`C:\`, `D:\`), COM ports (`COM1`), named pipes (`\\.\pipe\foo`), and GPU devices via DirectX. These must be constructed from Linux mounts and devices at container runtime.

## Decision

We will use **explicit labels** to describe mounts and devices in a portable, runtime-agnostic way. These labels will:

* Be attached to the OCI bundle or image metadata.
* Be consumed by a **prestart hook** in the container that translates them into Wine’s `dosdevices/` layout inside the container’s `WINEPREFIX`.
* Be validated and enforced by policy at admission time.

### Label Schema

#### Devices

```
dev.vinoc.devices.<id>.class=<enum-or-guid>
dev.vinoc.devices.<id>.path=<linux-path>
dev.vinoc.devices.<id>.label=<windows-label>
dev.vinoc.devices.<id>.mode=ro|rw
dev.vinoc.devices.<id>.optional=true|false
```

* **class**

  * SHOULD be one of the supported enums: `disk`, `cdrom`, `com`, `pipe`, `gpu`.
  * MAY be a Windows device class GUID (for forward-compatibility or fine-grained mapping).
  * Runtime must understand the enum set; GUIDs can be treated as opaque identifiers or mapped by policy.
* **path**: Linux path to the device node or directory, e.g. `/dev/ttyUSB0`, `/dev/sr0`, `/dev/dri/renderD128`.
* **label**: The Windows name to expose inside Wine: e.g. `COM1`, `D:`, `\\.\pipe\foo`, `GPU0`.
* **mode**: Access mode; enforced at the Linux mount/device level.
* **optional**: If true, the absence of the device does not fail container startup.

#### Mounts

```
dev.vinoc.mounts.<id>.source_path=<linux-path>
dev.vinoc.mounts.<id>.volume=<volume-name>
dev.vinoc.mounts.<id>.destination_label=<windows-drive-or-device>
dev.vinoc.mounts.<id>.destination_path=<relative-win-path>
dev.vinoc.mounts.<id>.mode=ro|rw
dev.vinoc.mounts.<id>.optional=true|false
```

* Exactly one of `source_path` or `volume` must be provided.
* `destination_label` specifies the drive or device label, e.g. `C:`, `D:`, `\\.\pipe`.
* `destination_path` is relative to the `destination_label`.

### Mapping Rules

* **Disk mounts**: bind-mount the Linux path into the container; prestart hook symlinks it as `dosdevices/d:`.
* **CD-ROM mounts**: must be RO; prestart hook marks the drive type as CD-ROM in Wine config so media checks pass.
* **COM devices**: `/dev/tty*` passed into container; prestart hook symlinks `dosdevices/com1 -> /dev/ttyUSB0`.
* **Named pipes**: mapped as Unix sockets/FIFOs; prestart hook configures Wine pipe namespace accordingly. Bridging may be needed for interoperability.
* **GPUs**: `/dev/dri/renderD*` or `/dev/nvidia*` passed in; prestart hook ensures Wine envs (`DXVK`, `vkd3d`) are set if backend requested.

### Hook Workflow

At `prestart`:

1. Create/verify `$WINEPREFIX` (`$XDG_RUNTIME_DIR/vino/prefix` or `/wineprefix`).
2. For each device, create the appropriate symlink in `dosdevices/` and, if needed, registry entries (CD-ROM type, etc.).
3. For each mount, ensure a drive letter exists and link the Linux path to the requested Windows path.
4. Apply access mode and security policy (read-only enforcement, SELinux/AppArmor on Linux).

### Security & Policy

* Default deny any device/mount classes not explicitly allowed.
* Validate actual Linux paths exist and match class.
* Enforce RO where required (`cdrom`).
* Prevent path escapes (`..\`).
* Admission controller may rewrite `destination_label` to avoid conflicts.

## Consequences

* **Pros**

  * Portable intent expressed in labels, not host-specific flags.
  * Clear mapping to Wine expectations (`C:\`, `COM1`, `\\.\pipe`).
  * Admission control and auditing at label level.
* **Cons**

  * Requires a prestart hook implementation and policy engine.
  * Wine emulation is incomplete (named pipes, raw disk access).
  * Some device semantics (GPU passthrough, ISO mounting) require elevated host support.

## Enum vs GUID for `class`

We will allow **both**:

* **Enum values** for the common subset (`disk`, `cdrom`, `com`, `pipe`, `gpu`), which are easy to validate and implement portably.
* **GUID values** (Windows Setup Class GUIDs) for advanced mappings or when precise Windows semantics are required.

**Rule of thumb:**

* If you want portability and Wine support → use enums.
* If you need Windows-exact classification → allow GUIDs, but require policy to decide if/how to handle them.

This keeps the schema forward-compatible and expressive while remaining practical.

## Examples

### Example A: Serial device → COM1 and data folder on D:\\

```
dev.vinoc.devices.serial1.class=com
dev.vinoc.devices.serial1.path=/dev/ttyUSB0
dev.vinoc.devices.serial1.label=COM1

dev.vinoc.mounts.data.source_path=/srv/mydata
dev.vinoc.mounts.data.destination_label=D:
dev.vinoc.mounts.data.destination_path=\data
dev.vinoc.mounts.data.mode=ro
```

**Effect:** Prestart creates `dosdevices/com1 -> /dev/ttyUSB0`, `dosdevices/d: -> /vino/drives/d`, and symlinks `/vino/drives/d/data -> /srv/mydata` (mounted RO).

---

### Example B: ISO as CD-ROM on E:\\

```
# Host (or privileged init) loop-mounts /isos/app.iso at /mnt/iso/app
dev.vinoc.devices.media.class=cdrom
dev.vinoc.devices.media.path=/mnt/iso/app
dev.vinoc.devices.media.label=E:
dev.vinoc.devices.media.mode=ro
```

**Effect:** `dosdevices/e: -> /mnt/iso/app`, drive type marked as CD-ROM so installers pass disc checks.

---

### Example C: GPU for DXVK/vkd3d

```
dev.vinoc.devices.gpu0.class=gpu
dev.vinoc.devices.gpu0.path=/dev/dri/renderD128
dev.vinoc.devices.gpu0.label=GPU0
dev.vinoc.devices.gpu0.backend=vulkan
```

**Effect:** Container gets `/dev/dri/renderD128`; prestart exports `WINEDLLOVERRIDES`, `DXVK_LOG_LEVEL`, etc., if policy enables DXVK.

## Additional references

- [labels' json-schema](./labels.schema.json)

---

[^win-device-classes]: [https://learn.microsoft.com/en-us/windows-hardware/drivers/install/system-defined-device-setup-classes-available-to-vendors](https://learn.microsoft.com/en-us/windows-hardware/drivers/install/system-defined-device-setup-classes-available-to-vendors)
