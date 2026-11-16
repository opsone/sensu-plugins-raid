# check-raid-status

Sensu Go check for Linux software RAID (mdadm) arrays.

Reads `/proc/mdstat`: no `mdadm` call, no root privileges, no CGO, static binary.

## Behavior

| Situation | State |
|---|---|
| Array degraded (`[2/1] [U_]`), including while rebuilding (progress shown) | CRITICAL |
| Member flagged faulty (`sdb1[1](F)`) | CRITICAL |
| Array `inactive` | CRITICAL |
| Array given with `--array` missing | CRITICAL |
| Array `(read-only)` | WARNING |
| `resync`, `check`, `repair` or `reshape` on a complete array | OK, progress shown |
| Array `(auto-read-only)` (not written yet since assembly) | OK |
| No md array (all excluded included) | UNKNOWN |
| `/proc/mdstat` missing or unreadable, invalid argument | UNKNOWN |

An array given with `--array` is checked even if it matches an `--exclude` pattern.

A rebuild is critical: the array has no redundancy until it ends.

## Usage

```
check-raid-status [flags]

  -a, --array strings     Array that must exist and be active (repeatable, md0 or /dev/md0)
  -e, --exclude strings   Glob pattern of arrays to ignore (repeatable)
      --mdstat string     Path of the md status file (default "/proc/mdstat")
```

| Option | Environment variable |
|---|---|
| `--array` | `CHECK_RAID_ARRAYS` |
| `--exclude` | `CHECK_RAID_EXCLUDE` |
| `--mdstat` | `CHECK_RAID_MDSTAT` |

Examples:

```sh
# Every md array on the host
check-raid-status

# md0 and md1 must be present
check-raid-status -a md0 -a md1
```

## Check definition

Subscribe only the hosts with software RAID (`raid`): a host without any array reports UNKNOWN.

```yaml
---
type: CheckConfig
api_version: core/v2
metadata:
  name: check_raid
spec:
  command: check-raid-status {{ .annotations.check_raid_args | default "" }}
  runtime_assets:
    - opsone/sensu-plugins-raid
  handlers:
    - slack
  interval: 300
  timeout: 30
  publish: true
  subscriptions:
    - raid
```

Per-host extra arguments are set as an entity annotation:

```yaml
annotations:
  check_raid_args: "-a md0 -a md1"
```

## Migration from 0.1.0

The binary was renamed from `check-raid` to `check-raid-status`: replace it in the check command. Options, environment variables and the `check_raid_args` annotation are unchanged.

## Build and release

```sh
make test          # go vet + unit tests
make integration   # runs the check in a Debian container as an unprivileged user
```

The Docker Desktop kernel has no md driver: the integration test reads mdstat files captured from servers or written by hand, plus the real (missing) `/proc/mdstat`.

Release:

```sh
git tag 0.1.0 && git push origin 0.1.0
make release       # version taken from the tag, or forced: make release VERSION=0.1.0
sensuctl create -f dist/opsone-sensu-plugins-raid_0.1.0.yml
```

`make release` runs `make dist`, which runs the tests then produces in `dist/`:

- one `linux/amd64` archive in Sensu asset layout (`bin/check-raid-status`);
- the sha512 checksums file;
- `opsone-sensu-plugins-raid_<version>.yml`, the asset definition pointing to the GitHub release of the tag.

It then creates the GitHub release of the tag with `gh`, uploads the archives and checksums, and uses the asset definition as release description.

`check-raid-status version` prints the version, commit and build date.
