package capability

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	pb "proxycontrol/pkg/contracts/agentpb"
	tuningcontract "proxycontrol/pkg/contracts/tuning"
)

func detectVirtualization(root string) string {
	if raw, err := os.ReadFile(filepath.Join(root, "proc/1/environ")); err == nil {
		for _, entry := range strings.FieldsFunc(string(raw), func(r rune) bool { return r == 0 || r == '\n' }) {
			if entry == "container=lxc" {
				return "lxc"
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".dockerenv")); err == nil {
		return "docker"
	}
	if info, err := os.Stat(filepath.Join(root, "proc/vz")); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(root, "proc/bc")); os.IsNotExist(err) {
			return "openvz"
		}
	}
	for _, name := range []string{"sys_vendor", "product_name"} {
		if raw, err := os.ReadFile(filepath.Join(root, "sys/class/dmi/id", name)); err == nil {
			s := strings.ToUpper(string(raw))
			if strings.Contains(s, "KVM") || strings.Contains(s, "QEMU") {
				return "kvm"
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(root, "proc/cpuinfo")); err == nil {
		hasFlags := false
		hypervisor := false
		for _, line := range strings.Split(string(raw), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			if k == "flags" || k == "Features" {
				hasFlags = true
				if slices.Contains(strings.Fields(v), "hypervisor") {
					hypervisor = true
				}
			}
		}
		if hasFlags && !hypervisor {
			return "none"
		}
	}
	return "unknown"
}

type procMount struct {
	path     string
	readOnly bool
}

func procMounts(root string) []procMount {
	raw, _ := os.ReadFile(filepath.Join(root, "proc/self/mountinfo"))
	var mounts []procMount
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[4] != "/proc" && !strings.HasPrefix(fields[4], "/proc/") {
			continue
		}
		ro := slices.Contains(strings.Split(fields[5], ","), "ro")
		if idx := slices.Index(fields, "-"); idx >= 0 && idx+3 < len(fields) {
			ro = ro || slices.Contains(strings.Split(fields[idx+3], ","), "ro")
		}
		mounts = append(mounts, procMount{path: fields[4], readOnly: ro})
	}
	return mounts
}

func procPathReadOnly(mounts []procMount, path string) bool {
	// mountinfo lists mounts in mount order, so a later mount stacked on the
	// same point (>=) is the one that is visible.
	longest, ro := 0, false
	for _, mount := range mounts {
		if len(mount.path) >= longest && (path == mount.path || strings.HasPrefix(path, mount.path+"/")) {
			longest, ro = len(mount.path), mount.readOnly
		}
	}
	return ro
}

func tuningKeyStates(root string) []*pb.TuningKeyState {
	mounts := procMounts(root)
	keys := tuningcontract.Keys()
	out := make([]*pb.TuningKeyState, 0, len(keys))
	for _, k := range keys {
		// mountinfo uses namespace paths, independent of the fixture root.
		p := "/proc/sys/" + strings.ReplaceAll(k, ".", "/")
		info, err := os.Stat(filepath.Join(root, p))
		present := err == nil && !info.IsDir()
		out = append(out, &pb.TuningKeyState{
			Key:      k,
			Present:  present,
			Writable: present && !procPathReadOnly(mounts, p),
		})
	}
	return out
}

func algorithms(root, kernel string) (ccAvail, ccLoad, qAvail, qLoad []string) {
	ccAllowed := tuningcontract.Choices["net.ipv4.tcp_congestion_control"]
	qAllowed := tuningcontract.Choices["net.core.default_qdisc"]

	ccAvailSet := map[string]bool{}
	qAvailSet := map[string]bool{"pfifo_fast": true}

	if raw, err := os.ReadFile(filepath.Join(root, "proc/sys/net/ipv4/tcp_available_congestion_control")); err == nil {
		for _, f := range strings.Fields(string(raw)) {
			if slices.Contains(ccAllowed, f) {
				ccAvailSet[f] = true
			}
		}
	}
	if kernel != "" {
		if raw, err := os.ReadFile(filepath.Join(root, "lib/modules", kernel, "modules.builtin")); err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				base := filepath.Base(strings.TrimSpace(line))
				name, _, _ := strings.Cut(base, ".ko")
				if cc, ok := strings.CutPrefix(name, "tcp_"); ok && slices.Contains(ccAllowed, cc) {
					ccAvailSet[cc] = true
				}
				if q, ok := strings.CutPrefix(name, "sch_"); ok && slices.Contains(qAllowed, q) {
					qAvailSet[q] = true
				}
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(root, "proc/modules")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			if q, ok := strings.CutPrefix(fields[0], "sch_"); ok && slices.Contains(qAllowed, q) {
				qAvailSet[q] = true
			}
		}
	}

	for k := range ccAvailSet {
		ccAvail = append(ccAvail, k)
	}
	sort.Strings(ccAvail)

	for k := range qAvailSet {
		qAvail = append(qAvail, k)
	}
	sort.Strings(qAvail)

	if kernel != "" {
		for _, cc := range ccAllowed {
			if ccAvailSet[cc] {
				continue
			}
			pattern := filepath.Join(root, "lib/modules", kernel, "kernel/net/ipv4", "tcp_"+cc+".ko*")
			if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
				ccLoad = append(ccLoad, cc)
			}
		}
		for _, q := range qAllowed {
			if qAvailSet[q] {
				continue
			}
			pattern := filepath.Join(root, "lib/modules", kernel, "kernel/net/sched", "sch_"+q+".ko*")
			if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
				qLoad = append(qLoad, q)
			}
		}
	}
	sort.Strings(ccLoad)
	sort.Strings(qLoad)
	return ccAvail, ccLoad, qAvail, qLoad
}

func sysctlSources(root string) []*pb.SysctlSource {
	dirs := []string{
		filepath.Join(root, "usr/lib/sysctl.d"),
		filepath.Join(root, "run/sysctl.d"),
		filepath.Join(root, "etc/sysctl.d"),
	}
	byName := map[string]string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
				continue
			}
			byName[entry.Name()] = filepath.Join(dir, entry.Name())
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	type fileEntry struct {
		label string
		path  string
	}
	files := make([]fileEntry, 0, len(names)+1)
	for _, name := range names {
		files = append(files, fileEntry{label: name, path: byName[name]})
	}
	files = append(files, fileEntry{label: "sysctl.conf", path: filepath.Join(root, "etc/sysctl.conf")})

	winners := map[string]*pb.SysctlSource{}
	for _, f := range files {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.ReplaceAll(strings.TrimSpace(k), "/", ".")
			v = strings.Join(strings.Fields(v), " ")
			if !tuningcontract.Known(k) || v == "" {
				continue
			}
			winners[k] = &pb.SysctlSource{Key: k, File: f.label, Value: v}
		}
	}

	keys := make([]string, 0, len(winners))
	for k := range winners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*pb.SysctlSource, 0, len(keys))
	for _, k := range keys {
		out = append(out, winners[k])
	}
	return out
}

func processLimits(root string, self int) []*pb.ProcessLimit {
	pids := []int{self}
	seen := map[int]bool{self: true}
	pattern := filepath.Join(root, "proc", strconv.Itoa(self), "task/*/children")
	if matches, err := filepath.Glob(pattern); err == nil {
		var children []int
		for _, m := range matches {
			raw, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			for _, field := range strings.Fields(string(raw)) {
				if pid, err := strconv.Atoi(field); err == nil && pid > 0 && !seen[pid] {
					seen[pid] = true
					children = append(children, pid)
				}
			}
		}
		sort.Ints(children)
		pids = append(pids, children...)
	}

	var out []*pb.ProcessLimit
	for _, pid := range pids {
		commRaw, err := os.ReadFile(filepath.Join(root, "proc", strconv.Itoa(pid), "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(commRaw))
		if name == "" {
			continue
		}
		limitsRaw, err := os.ReadFile(filepath.Join(root, "proc", strconv.Itoa(pid), "limits"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(limitsRaw), "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Max open files")
			if !ok {
				continue
			}
			fields := strings.Fields(rest)
			if len(fields) < 2 {
				continue
			}
			soft, err1 := strconv.ParseUint(fields[0], 10, 64)
			hard, err2 := strconv.ParseUint(fields[1], 10, 64)
			if err1 == nil && err2 == nil {
				out = append(out, &pb.ProcessLimit{Name: name, Soft: soft, Hard: hard})
			}
			break
		}
	}
	return out
}
