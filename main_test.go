package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// Real /proc/mdstat of healthy servers.
const (
	raid5 = `Personalities : [raid1] [raid6] [raid5] [raid4] [linear] [multipath] [raid0] [raid10]
md2 : active raid5 sda4[0] sdb4[2] sdc4[1]
      455813120 blocks super 1.2 level 5, 512k chunk, algorithm 2 [3/3] [UUU]
      bitmap: 1/2 pages [4KB], 65536KB chunk

md1 : active raid5 sdb3[2] sda3[0] sdc3[1]
      41908224 blocks super 1.2 level 5, 512k chunk, algorithm 2 [3/3] [UUU]

md0 : active raid1 sdb2[2] sda2[0] sdc2[1]
      523264 blocks super 1.2 [3/3] [UUU]

unused devices: <none>
`
	// md1 uses 0.90 metadata (no "super" field).
	raid1NVMe = `Personalities : [raid1] [raid0] [raid6] [raid5] [raid4] [raid10]
md3 : active raid1 nvme1n1p3[1] nvme0n1p3[0]
      935460864 blocks super 1.2 [2/2] [UU]
      bitmap: 7/7 pages [28KB], 65536KB chunk

md1 : active raid1 nvme1n1p1[1] nvme0n1p1[0]
      523200 blocks [2/2] [UU]

md2 : active raid1 nvme0n1p2[0] nvme1n1p2[1]
      1046528 blocks super 1.2 [2/2] [UU]

unused devices: <none>
`
	degradedRecovery = `Personalities : [raid1] [raid6] [raid5] [raid4]
md1 : active raid5 sdc3[3] sdb3[1] sda3[0]
      997853184 blocks super 1.2 level 5, 512k chunk, algorithm 2 [3/2] [UU_]
      [=>...................]  recovery =  8.5% (42454784/498926592) finish=40.1min speed=189632K/sec
      bitmap: 4/4 pages [16KB], 65536KB chunk

md0 : active raid1 sdb2[1] sda2[0](F)
      523264 blocks super 1.2 [2/1] [_U]

unused devices: <none>
`
	misc = `Personalities : [raid1] [raid0]
md127 : inactive sdd1[1](S)
      1046528 blocks super 1.2

md5 : active (read-only) raid1 sdf1[1] sde1[0]
      1046528 blocks super 1.2 [2/2] [UU]

md4 : active (auto-read-only) raid1 sdh1[1] sdg1[0]
      1046528 blocks super 1.2 [2/2] [UU]
      	resync=PENDING

md3 : active raid0 sdj1[1] sdi1[0]
      2093056 blocks super 1.2 512k chunks

md2 : active raid1 sdl1[1] sdk1[0] sdm1[2](S)
      1046528 blocks super 1.2 [2/2] [UU]
      [=====>...............]  check = 27.4% (287104/1046528) finish=0.1min speed=95701K/sec

unused devices: <none>
`
	noArray = `Personalities : [raid1] [linear] [multipath] [raid0] [raid6] [raid5] [raid4] [raid10]
unused devices: <none>
`
)

func parse(t *testing.T, s string) []Array {
	t.Helper()
	arrays, err := parseMdstat(strings.NewReader(s))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	return arrays
}

func TestParseMdstat(t *testing.T) {
	got := parse(t, degradedRecovery)
	want := []Array{
		{Name: "md1", Active: true, Level: "raid5", Total: 3, Working: 2, Map: "UU_", Sync: "recovery", Progress: "8.5%"},
		{Name: "md0", Active: true, Level: "raid1", Faulty: []string{"sda2"}, Total: 2, Working: 1, Map: "_U"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseMdstat() =\n%+v\nwant\n%+v", got, want)
	}

	got = parse(t, misc)
	want = []Array{
		{Name: "md127"},
		{Name: "md5", Active: true, ReadOnly: true, Level: "raid1", Total: 2, Working: 2, Map: "UU"},
		{Name: "md4", Active: true, Level: "raid1", Total: 2, Working: 2, Map: "UU", Sync: "resync", Progress: "PENDING"},
		{Name: "md3", Active: true, Level: "raid0"},
		{Name: "md2", Active: true, Level: "raid1", Total: 2, Working: 2, Map: "UU", Sync: "check", Progress: "27.4%"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseMdstat() =\n%+v\nwant\n%+v", got, want)
	}

	if _, err := parseMdstat(strings.NewReader("garbage\n")); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		mdstat   string
		required []string
		exclude  []string
		want     Result
	}{
		{
			name:   "healthy raid5 and raid1",
			mdstat: raid5,
			want:   Result{sensu.CheckStateOK, "3 array(s) healthy: md0, md1, md2"},
		},
		{
			name:     "0.90 metadata and required arrays",
			mdstat:   raid1NVMe,
			required: []string{"md1", "md3"},
			want:     Result{sensu.CheckStateOK, "3 array(s) healthy: md1, md2, md3"},
		},
		{
			name:   "degraded with recovery and faulty member",
			mdstat: degradedRecovery,
			want: Result{sensu.CheckStateCritical, "2 array(s) in trouble: " +
				"md0 (raid1 degraded [2/1] [_U], faulty sda2), md1 (raid5 degraded [3/2] [UU_], recovery 8.5%)"},
		},
		{
			name:   "inactive is critical, read-only is a warning",
			mdstat: misc,
			want:   Result{sensu.CheckStateCritical, "2 array(s) in trouble: md127 (inactive), md5 (read-only)"},
		},
		{
			name:    "read-only alone is a warning, running operations are reported",
			mdstat:  misc,
			exclude: []string{"md12*"},
			want:    Result{sensu.CheckStateWarning, "1 array(s) in trouble: md5 (read-only)"},
		},
		{
			name:    "healthy arrays with running operations",
			mdstat:  misc,
			exclude: []string{"md127", "md5"},
			want:    Result{sensu.CheckStateOK, "3 array(s) healthy: md2, md3, md4 (md2 check 27.4%, md4 resync PENDING)"},
		},
		{
			name:     "required array missing",
			mdstat:   raid5,
			required: []string{"md0", "md9"},
			want:     Result{sensu.CheckStateCritical, "1 array(s) in trouble: md9 (missing)"},
		},
		{
			name:     "required array wins over exclusion",
			mdstat:   degradedRecovery,
			required: []string{"md0"},
			exclude:  []string{"*"},
			want:     Result{sensu.CheckStateCritical, "1 array(s) in trouble: md0 (raid1 degraded [2/1] [_U], faulty sda2)"},
		},
		{
			name:   "no array",
			mdstat: noArray,
			want:   Result{sensu.CheckStateUnknown, "no md array found"},
		},
		{
			name:    "every array excluded",
			mdstat:  raid5,
			exclude: []string{"*"},
			want:    Result{sensu.CheckStateUnknown, "no md array found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evaluate(parse(t, tt.mdstat), tt.required, tt.exclude); got != tt.want {
				t.Errorf("evaluate() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNormalizeNames(t *testing.T) {
	want := []string{"md0", "md1"}
	if got := normalizeNames([]string{"/dev/md0", "md1"}); !reflect.DeepEqual(got, want) {
		t.Errorf("normalizeNames() = %v, want %v", got, want)
	}
}
