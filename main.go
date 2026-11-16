package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	corev2 "github.com/sensu/core/v2"
	"github.com/sensu/sensu-plugin-sdk/sensu"
)

// Config represents the check plugin config.
type Config struct {
	sensu.PluginConfig
	Arrays  []string
	Exclude []string
	Mdstat  string
}

var (
	plugin = Config{
		PluginConfig: sensu.PluginConfig{
			Name:     "check-raid",
			Short:    "Alert on degraded, failed or inactive Linux software RAID (mdadm) arrays",
			Keyspace: "sensu.io/plugins/check-raid/config",
		},
	}

	options = []sensu.ConfigOption{
		&sensu.SlicePluginConfigOption[string]{
			Path:      "array",
			Env:       "CHECK_RAID_ARRAYS",
			Argument:  "array",
			Shorthand: "a",
			Default:   []string{},
			Usage:     "Array that must exist and be active (repeatable, e.g. -a md0 -a md1)",
			Value:     &plugin.Arrays,
		},
		&sensu.SlicePluginConfigOption[string]{
			Path:      "exclude",
			Env:       "CHECK_RAID_EXCLUDE",
			Argument:  "exclude",
			Shorthand: "e",
			Default:   []string{},
			Usage:     "Glob pattern of arrays to ignore (repeatable, e.g. -e 'md12*')",
			Value:     &plugin.Exclude,
		},
		&sensu.PluginConfigOption[string]{
			Path:     "mdstat",
			Env:      "CHECK_RAID_MDSTAT",
			Argument: "mdstat",
			Default:  "/proc/mdstat",
			Usage:    "Path of the md status file",
			Value:    &plugin.Mdstat,
		},
	}
)

func main() {
	check := sensu.NewGoCheck(&plugin.PluginConfig, options, checkArgs, executeCheck, false)
	check.Execute()
}

func checkArgs(event *corev2.Event) (int, error) {
	for _, p := range plugin.Exclude {
		if _, err := path.Match(p, ""); err != nil {
			return sensu.CheckStateUnknown, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
	}
	return sensu.CheckStateOK, nil
}

func executeCheck(event *corev2.Event) (int, error) {
	f, err := os.Open(plugin.Mdstat)
	if err != nil {
		return sensu.CheckStateUnknown, fmt.Errorf("reading md status (is the md driver loaded?): %w", err)
	}
	defer f.Close()

	arrays, err := parseMdstat(f)
	if err != nil {
		return sensu.CheckStateUnknown, fmt.Errorf("parsing %s: %w", plugin.Mdstat, err)
	}

	result := evaluate(arrays, normalizeNames(plugin.Arrays), plugin.Exclude)
	fmt.Printf("%s %s: %s\n", plugin.Name, stateLabel(result.State), result.Message)
	return result.State, nil
}

// Array is an md array as described in /proc/mdstat.
type Array struct {
	Name     string
	Active   bool
	ReadOnly bool     // "(read-only)"; "(auto-read-only)" is a normal state and is not flagged
	Level    string   // raid1, raid5… (empty for an inactive array)
	Faulty   []string // members flagged (F)
	Total    int      // expected members, from "[n/m]" (0 for levels without redundancy)
	Working  int      // working members, from "[n/m]"
	Map      string   // member map, e.g. "UU_"
	Sync     string   // running operation: resync, recovery, check, repair, reshape
	Progress string   // progress of Sync, e.g. "8.5%" or "DELAYED"
}

// Degraded reports whether redundant members are missing.
func (a Array) Degraded() bool { return a.Working < a.Total }

var (
	headerRe = regexp.MustCompile(`^(md\S+) : (.*)$`)
	memberRe = regexp.MustCompile(`^(\S+)\[\d+\]((?:\([A-Z]\))*)$`)
	statusRe = regexp.MustCompile(`\[(\d+)/(\d+)\] \[([U_]+)\]`)
	syncRe   = regexp.MustCompile(`\b(resync|recovery|check|repair|reshape)\s*=\s*(\S+)`)
)

// parseMdstat reads the arrays from the content of /proc/mdstat.
func parseMdstat(r io.Reader) ([]Array, error) {
	var (
		arrays    []Array
		cur       *Array
		seenTitle bool
	)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Personalities :") {
			seenTitle = true
			continue
		}
		if m := headerRe.FindStringSubmatch(line); m != nil {
			arrays = append(arrays, parseHeader(m[1], strings.Fields(m[2])))
			cur = &arrays[len(arrays)-1]
			continue
		}
		if cur == nil || !strings.HasPrefix(line, " ") {
			cur = nil // "unused devices", blank line: end of the array block
			continue
		}
		if m := statusRe.FindStringSubmatch(line); m != nil {
			cur.Total, _ = strconv.Atoi(m[1])
			cur.Working, _ = strconv.Atoi(m[2])
			cur.Map = m[3]
		}
		if m := syncRe.FindStringSubmatch(line); m != nil {
			cur.Sync, cur.Progress = m[1], m[2]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !seenTitle {
		return nil, fmt.Errorf("unexpected format: no Personalities line")
	}
	return arrays, nil
}

// parseHeader parses the fields after "mdX : ", e.g. "active (auto-read-only) raid1 sdb1[1](F) sda1[0]".
func parseHeader(name string, fields []string) Array {
	a := Array{Name: name}
	for i, f := range fields {
		switch {
		case i == 0:
			a.Active = f == "active"
		case f == "(read-only)":
			a.ReadOnly = true
		case strings.HasPrefix(f, "("):
			// (auto-read-only) and other array flags
		case memberRe.MatchString(f):
			m := memberRe.FindStringSubmatch(f)
			if strings.Contains(m[2], "(F)") {
				a.Faulty = append(a.Faulty, m[1])
			}
		case a.Level == "":
			a.Level = f
		}
	}
	return a
}

// Result is the outcome of the evaluation of the arrays.
type Result struct {
	State   int
	Message string
}

type problem struct {
	detail string
	state  int
}

// evaluate turns the arrays read from /proc/mdstat into a check result.
// required holds the arrays given with --array.
func evaluate(arrays []Array, required, exclude []string) Result {
	problems := map[string]problem{}
	found := map[string]bool{}
	var healthy, running []string

	for _, a := range arrays {
		found[a.Name] = true
		if excluded(a.Name, exclude) && !slices.Contains(required, a.Name) {
			continue
		}
		if p, ok := assess(a); ok {
			problems[a.Name] = p
			continue
		}
		healthy = append(healthy, a.Name)
		if a.Sync != "" {
			running = append(running, fmt.Sprintf("%s %s %s", a.Name, a.Sync, a.Progress))
		}
	}

	for _, name := range required {
		if !found[name] {
			problems[name] = problem{"missing", sensu.CheckStateCritical}
		}
	}

	if len(problems) == 0 {
		if len(healthy) == 0 {
			return Result{sensu.CheckStateUnknown, "no md array found"}
		}
		sort.Strings(healthy)
		msg := fmt.Sprintf("%d array(s) healthy: %s", len(healthy), strings.Join(healthy, ", "))
		if len(running) > 0 {
			sort.Strings(running)
			msg += " (" + strings.Join(running, ", ") + ")"
		}
		return Result{sensu.CheckStateOK, msg}
	}

	names := make([]string, 0, len(problems))
	worst := sensu.CheckStateOK
	for name, p := range problems {
		names = append(names, name)
		worst = max(worst, p.state)
	}
	sort.Strings(names)

	details := make([]string, 0, len(names))
	for _, name := range names {
		details = append(details, fmt.Sprintf("%s (%s)", name, problems[name].detail))
	}
	return Result{worst, fmt.Sprintf("%d array(s) in trouble: %s", len(names), strings.Join(details, ", "))}
}

// assess returns the problem of an array, if any.
func assess(a Array) (problem, bool) {
	if !a.Active {
		return problem{"inactive", sensu.CheckStateCritical}, true
	}

	var details []string
	state := sensu.CheckStateOK
	if a.Degraded() {
		details = append(details, fmt.Sprintf("%s degraded [%d/%d] [%s]", a.Level, a.Total, a.Working, a.Map))
		if a.Sync != "" {
			details = append(details, fmt.Sprintf("%s %s", a.Sync, a.Progress))
		}
		state = sensu.CheckStateCritical
	}
	if len(a.Faulty) > 0 {
		details = append(details, "faulty "+strings.Join(a.Faulty, " "))
		state = sensu.CheckStateCritical
	}
	if a.ReadOnly {
		details = append(details, "read-only")
		state = max(state, sensu.CheckStateWarning)
	}
	if state == sensu.CheckStateOK {
		return problem{}, false
	}
	return problem{strings.Join(details, ", "), state}, true
}

// normalizeNames accepts "md0" as well as "/dev/md0".
func normalizeNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, strings.TrimPrefix(n, "/dev/"))
	}
	return out
}

func excluded(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

func stateLabel(state int) string {
	switch state {
	case sensu.CheckStateOK:
		return "OK"
	case sensu.CheckStateWarning:
		return "WARNING"
	case sensu.CheckStateCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
