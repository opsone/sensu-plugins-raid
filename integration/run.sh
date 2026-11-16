#!/usr/bin/env bash
# Runs check-raid in a Debian container, as an unprivileged user like sensu-agent.
# The Docker Desktop kernel has no md driver: real arrays cannot be created, so the check
# reads mdstat files captured from servers (healthy) or written by hand (failures), plus
# the real /proc/mdstat, which is missing here.
set -euo pipefail
cd "$(dirname "$0")"

image=check-raid-integration
docker build -q -t "$image" . >/dev/null

failures=0
env_args=()
expect() {
  local want=$1; shift
  local out rc=0
  out=$(docker run --rm --user nobody ${env_args[@]+"${env_args[@]}"} "$image" check-raid "$@") || rc=$?
  if [[ $rc -eq $want ]]; then
    echo "ok   [$rc] check-raid $* -> $out"
  else
    echo "FAIL [$rc, want $want] check-raid $* -> $out"
    failures=$((failures + 1))
  fi
}

expect 0 --mdstat /mdstat/raid5
expect 0 --mdstat /mdstat/raid1-nvme -a md1 -a /dev/md3
expect 2 --mdstat /mdstat/raid1-nvme -a md0
expect 2 --mdstat /mdstat/degraded
expect 2 --mdstat /mdstat/degraded -e '*' -a md0
expect 1 --mdstat /mdstat/read-only
expect 0 --mdstat /mdstat/read-only -e md5
expect 3 --mdstat /mdstat/empty
expect 3
expect 3 --mdstat /mdstat/raid5 -e '[broken'
env_args=(-e CHECK_RAID_ARRAYS=md9 -e CHECK_RAID_MDSTAT=/mdstat/raid5)
expect 2

exit $failures
