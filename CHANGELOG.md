# Changelog

## [0.2.0] - 2026-11-16

### Changed
- **Breaking**: binary renamed `check-raid` → `check-raid-status`, following the `check-<service>-status` naming. Options, environment variables and annotations are unchanged: only the check command changes.

## [0.1.0] - 2026-11-16

### Added
- `check-raid`: CRITICAL on a degraded (rebuild included), inactive or missing md array or a faulty member, WARNING on a read-only array, UNKNOWN when no array is found.
- `--array`: arrays that must exist, `--exclude`: arrays to ignore (glob), `--mdstat`: status file path.
