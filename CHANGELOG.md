# Changelog

## [Unreleased] - 0.1.0

### Added
- `check-raid`: CRITICAL on a degraded (rebuild included), inactive or missing md array or a faulty member, WARNING on a read-only array, UNKNOWN when no array is found.
- `--array`: arrays that must exist, `--exclude`: arrays to ignore (glob), `--mdstat`: status file path.
