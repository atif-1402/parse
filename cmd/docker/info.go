package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// InfoPlugin is one client CLI plugin with its version.
type InfoPlugin struct {
	Name    string
	Version string
}

// InfoView is the semantic model for `docker info`: only the fields people
// look for, with the conditional flags kept for the rows that are shown
// only when non-default.
type InfoView struct {
	ServerVersion string
	Context       string
	Host          string

	Containers int
	Running    int
	Paused     int
	Stopped    int
	Images     int

	StorageDriver string
	BackingFS     string
	RootDir       string

	CgroupVersion string
	CgroupDriver  string

	DefaultRuntime    string
	RuncVersion       string
	ContainerdVersion string

	OS     string
	Kernel string
	Arch   string
	CPUs   int
	Memory string

	LoggingDriver string
	Plugins       []InfoPlugin

	SwarmActive        bool
	InsecureRegistries []string
	SecurityOptions    []string
	LiveRestore        bool
	Debug              bool
	Experimental       bool

	Warnings []string
	Hidden   int
}

// looksLikeInfo claims only text that opens with docker's client section
// and carries both a Containers: line and a Server Version: line —
// `docker version` also opens with Client: but has neither, and a daemon
// that is down has no Server Version: at all.
func looksLikeInfo(text string) bool {
	hasClient, hasContainers, hasServerVersion := false, false, false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if !hasClient {
			if t != "Client:" {
				return false
			}
			hasClient = true
			continue
		}
		if strings.HasPrefix(t, "Containers:") {
			hasContainers = true
		}
		if strings.HasPrefix(t, "Server Version:") {
			hasServerVersion = true
		}
	}
	return hasClient && hasContainers && hasServerVersion
}

func isInfoWarning(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "WARNING") || strings.HasPrefix(t, "ERROR")
}

// insecureRegistryDefaults are the CIDRs docker ships with; a list made of
// only these is the default and stays hidden.
var insecureRegistryDefaults = map[string]bool{
	"127.0.0.0/8": true,
	"::1/128":     true,
}

// insecureNonDefault reports whether the list carries an entry docker does
// not ship with, which is the only time the row is worth printing.
func insecureNonDefault(list []string) bool {
	for _, r := range list {
		if !insecureRegistryDefaults[r] {
			return true
		}
	}
	return false
}

// shortCommit shortens a commit hash to docker's own 12-character form;
// a real version string is already short enough to pass through whole.
func shortCommit(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// securityFlag reports whether a security option is worth a row: only
// rootless and userns change how the daemon sits on its host — plain
// seccomp, its builtin profile and cgroupns stay hidden.
func securityFlag(opt string) bool {
	o := strings.ToLower(opt)
	return strings.Contains(o, "rootless") || strings.Contains(o, "userns")
}

// memHuman renders bytes the way docker's own Total Memory reads: binary
// units with four significant digits — 8196571136 → "7.634GiB".
func memHuman(b float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	u := 0
	for b >= 1024 && u < len(units)-1 {
		b /= 1024
		u++
	}
	return roundSig4(b) + units[u]
}

// infoField is one `Key: value` line of the text output and whether the
// render surfaces it; the hidden count is the fields never surfaced.
type infoField struct {
	key   string
	value string
	shown bool
}

// normalizeInfoText parses docker's own info text into the model and
// counts, per line, what the render will surface.
func normalizeInfoText(text string) InfoView {
	var v InfoView
	var fields []infoField
	add := func(key, value string, shown bool) {
		fields = append(fields, infoField{key: key, value: value, shown: shown})
	}

	server := false
	clientPlugins := false
	currentPlugin := -1
	collectingInsecure := false
	collectingSecurity := false

	for _, line := range strings.Split(text, "\n") {
		raw := strings.TrimRight(line, "\r ")
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if t == "Client:" || t == "Server:" {
			server = t == "Server:"
			clientPlugins = false
			currentPlugin = -1
			collectingInsecure = false
			collectingSecurity = false
			continue
		}
		if isInfoWarning(raw) {
			v.Warnings = append(v.Warnings, raw)
			continue
		}
		if collectingSecurity {
			// Group members sit deeper than the one-space indent the
			// fields use; the next field at depth one ends the group.
			if indent := len(raw) - len(strings.TrimLeft(raw, " ")); indent >= 2 {
				if securityFlag(t) {
					v.SecurityOptions = append(v.SecurityOptions, t)
				}
				continue
			}
			collectingSecurity = false
		}

		colon := strings.Index(t, ":")
		if collectingInsecure {
			// Entries have no colon; an IPv6 CIDR does, but carries
			// a slash. A real field key never does.
			if colon >= 0 && !strings.Contains(t, "/") {
				collectingInsecure = false
			} else {
				v.InsecureRegistries = append(v.InsecureRegistries, t)
				continue
			}
		}
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(t[:colon])
		value := strings.TrimSpace(t[colon+1:])
		if key == "" {
			continue
		}
		if value == "" {
			// A group header: it owns the lines under it but is not
			// a field itself.
			kl := strings.ToLower(key)
			if kl == "plugins" && !server {
				clientPlugins = true
			}
			if kl == "insecure registries" && server {
				collectingInsecure = true
			}
			if kl == "security options" && server {
				collectingSecurity = true
			}
			continue
		}
		kl := strings.ToLower(key)

		if !server {
			// Client section: context and the plugin versions are
			// shown; the CLI's own version and every path are not.
			switch {
			case kl == "context":
				v.Context = value
				add(kl, value, true)
			case kl == "debug mode":
				add(kl, value, false) // post-pass flips the true line
			case clientPlugins && kl == "version":
				shown := currentPlugin >= 0
				if currentPlugin >= 0 {
					v.Plugins[currentPlugin].Version = value
				}
				add(kl, value, shown)
			case clientPlugins && kl == "path":
				add(kl, value, false)
			case clientPlugins:
				v.Plugins = append(v.Plugins, InfoPlugin{Name: key})
				currentPlugin = len(v.Plugins) - 1
				add(kl, value, true)
			default:
				add(kl, value, false)
			}
			continue
		}

		// Server section.
		switch kl {
		case "server version":
			v.ServerVersion = value
			add(kl, value, true)
		case "containers":
			v.Containers, _ = strconv.Atoi(value)
			add(kl, value, true)
		case "running":
			v.Running, _ = strconv.Atoi(value)
			add(kl, value, true)
		case "paused":
			v.Paused, _ = strconv.Atoi(value)
			add(kl, value, true)
		case "stopped":
			v.Stopped, _ = strconv.Atoi(value)
			add(kl, value, true)
		case "images":
			v.Images, _ = strconv.Atoi(value)
			add(kl, value, true)
		case "storage driver":
			v.StorageDriver = value
			add(kl, value, true)
		case "backing filesystem":
			v.BackingFS = value
			add(kl, value, true)
		case "logging driver":
			v.LoggingDriver = value
			add(kl, value, true)
		case "cgroup driver":
			v.CgroupDriver = value
			add(kl, value, true)
		case "cgroup version":
			v.CgroupVersion = value
			add(kl, value, true)
		case "swarm":
			v.SwarmActive = value == "active"
			add(kl, value, v.SwarmActive)
		case "default runtime":
			v.DefaultRuntime = value
			add(kl, value, true)
		case "containerd version":
			v.ContainerdVersion = shortCommit(value)
			add(kl, value, true)
		case "runc version":
			v.RuncVersion = value
			add(kl, value, true)
		case "kernel version":
			v.Kernel = value
			add(kl, value, true)
		case "operating system":
			v.OS = value
			add(kl, value, true)
		case "architecture":
			v.Arch = value
			add(kl, value, true)
		case "cpus":
			v.CPUs, _ = strconv.Atoi(value)
			add(kl, value, true)
		case "total memory":
			v.Memory = value
			add(kl, value, true)
		case "name":
			v.Host = value
			add(kl, value, true)
		case "docker root dir":
			v.RootDir = value
			add(kl, value, true)
		case "experimental":
			v.Experimental = value == "true"
			add(kl, value, v.Experimental)
		case "live restore enabled":
			v.LiveRestore = value == "true"
			add(kl, value, v.LiveRestore)
		case "debug mode":
			add(kl, value, false) // post-pass flips the true line
		default:
			add(kl, value, false)
		}
	}

	// Debug shows when either section has it on; only the true-valued
	// lines count as surfaced.
	v.Debug = debugTrue(fields)
	if v.Debug {
		for i := range fields {
			if fields[i].key == "debug mode" && strings.EqualFold(fields[i].value, "true") {
				fields[i].shown = true
			}
		}
	}

	shown := 0
	for _, f := range fields {
		if f.shown {
			shown++
		}
	}
	v.Hidden = len(fields) - shown
	return v
}

func debugTrue(fields []infoField) bool {
	for _, f := range fields {
		if f.key == "debug mode" && strings.EqualFold(f.value, "true") {
			return true
		}
	}
	return false
}

// normalizeInfoJSON parses `docker info --format '{{json .}}'` into the
// same model, counting hidden fields as top-level keys never surfaced.
func normalizeInfoJSON(text string) (InfoView, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return InfoView{}, false
	}
	if _, ok := m["ServerVersion"]; !ok {
		return InfoView{}, false
	}
	var v InfoView
	shown := map[string]bool{}
	str := func(k string) string { s, _ := m[k].(string); return s }
	num := func(k string) int {
		if f, ok := m[k].(float64); ok {
			return int(f)
		}
		return 0
	}
	display := func(k, value string, target *string) {
		if m[k] == nil {
			return
		}
		if value == "" {
			return
		}
		*target = value
		shown[k] = true
	}

	display("ServerVersion", str("ServerVersion"), &v.ServerVersion)
	display("Name", str("Name"), &v.Host)
	display("Driver", str("Driver"), &v.StorageDriver)
	display("DockerRootDir", str("DockerRootDir"), &v.RootDir)
	display("CgroupDriver", str("CgroupDriver"), &v.CgroupDriver)
	display("CgroupVersion", str("CgroupVersion"), &v.CgroupVersion)
	display("DefaultRuntime", str("DefaultRuntime"), &v.DefaultRuntime)
	display("KernelVersion", str("KernelVersion"), &v.Kernel)
	display("OperatingSystem", str("OperatingSystem"), &v.OS)
	display("Architecture", str("Architecture"), &v.Arch)
	display("LoggingDriver", str("LoggingDriver"), &v.LoggingDriver)

	for _, k := range []string{"Containers", "ContainersRunning", "ContainersPaused", "ContainersStopped", "Images"} {
		if _, ok := m[k]; ok {
			shown[k] = true
		}
	}
	v.Containers = num("Containers")
	v.Running = num("ContainersRunning")
	v.Paused = num("ContainersPaused")
	v.Stopped = num("ContainersStopped")
	v.Images = num("Images")
	if _, ok := m["NCPU"]; ok && num("NCPU") > 0 {
		v.CPUs = num("NCPU")
		shown["NCPU"] = true
	}
	if f, ok := m["MemTotal"].(float64); ok && f > 0 {
		v.Memory = memHuman(f)
		shown["MemTotal"] = true
	}

	// containerd / runc versions live in the commit objects.
	if cm, ok := m["ContainerdCommit"].(map[string]interface{}); ok {
		if id, _ := cm["ID"].(string); id != "" {
			v.ContainerdVersion = shortCommit(id)
			shown["ContainerdCommit"] = true
		}
	}
	if rm, ok := m["RuncCommit"].(map[string]interface{}); ok {
		if id, _ := rm["ID"].(string); id != "" {
			v.RuncVersion = id
			shown["RuncCommit"] = true
		}
	}

	// A backing filesystem, when this docker reports one, sits in the
	// driver status pairs.
	if ds, ok := m["DriverStatus"].([]interface{}); ok {
		for _, p := range ds {
			pair, _ := p.([]interface{})
			if len(pair) == 2 {
				k, _ := pair[0].(string)
				val, _ := pair[1].(string)
				if strings.EqualFold(k, "Backing Filesystem") && val != "" {
					v.BackingFS = val
					shown["DriverStatus"] = true
				}
			}
		}
	}

	// Client context and plugin versions.
	if ci, ok := m["ClientInfo"].(map[string]interface{}); ok {
		if ctx, _ := ci["Context"].(string); ctx != "" {
			v.Context = ctx
			shown["ClientInfo"] = true
		}
		if clientDebug, _ := ci["Debug"].(bool); clientDebug {
			v.Debug = true
			shown["ClientInfo"] = true
		}
		if plugs, ok := ci["Plugins"].([]interface{}); ok {
			for _, p := range plugs {
				pm, _ := p.(map[string]interface{})
				if pm == nil {
					continue
				}
				name, _ := pm["Name"].(string)
				if name == "" {
					continue
				}
				version, _ := pm["Version"].(string)
				v.Plugins = append(v.Plugins, InfoPlugin{Name: name, Version: version})
				shown["ClientInfo"] = true
			}
		}
		sort.Slice(v.Plugins, func(i, j int) bool { return v.Plugins[i].Name < v.Plugins[j].Name })
	}

	// Conditionals: shown only when non-default.
	if so, ok := m["SecurityOptions"].([]interface{}); ok {
		for _, o := range so {
			if s, ok := o.(string); ok && securityFlag(s) {
				v.SecurityOptions = append(v.SecurityOptions, s)
			}
		}
		if len(v.SecurityOptions) > 0 {
			shown["SecurityOptions"] = true
		}
	}
	if sm, ok := m["Swarm"].(map[string]interface{}); ok {
		if state, _ := sm["LocalNodeState"].(string); state == "active" {
			v.SwarmActive = true
			shown["Swarm"] = true
		}
	}
	if serverDebug, _ := m["Debug"].(bool); serverDebug {
		v.Debug = true
		shown["Debug"] = true
	}
	if v.Debug && !shown["Debug"] {
		shown["Debug"] = true
	}
	if lr, _ := m["LiveRestoreEnabled"].(bool); lr {
		v.LiveRestore = true
		shown["LiveRestoreEnabled"] = true
	}
	if ex, _ := m["ExperimentalBuild"].(bool); ex {
		v.Experimental = true
		shown["ExperimentalBuild"] = true
	}
	if rc, ok := m["RegistryConfig"].(map[string]interface{}); ok {
		if regs, ok := rc["InsecureRegistryCIDRs"].([]interface{}); ok {
			for _, r := range regs {
				if s, ok := r.(string); ok {
					v.InsecureRegistries = append(v.InsecureRegistries, s)
				}
			}
			if insecureNonDefault(v.InsecureRegistries) {
				shown["RegistryConfig"] = true
			}
		}
	}
	if ws, ok := m["Warnings"].([]interface{}); ok {
		for _, w := range ws {
			if s, ok := w.(string); ok && s != "" {
				v.Warnings = append(v.Warnings, s)
			}
		}
		if len(v.Warnings) > 0 {
			shown["Warnings"] = true
		}
	}

	v.Hidden = len(m) - len(shown)
	return v, true
}

// infoRow is one labeled row on the same 10-wide label column.
func infoRow(label, value string) string {
	return fmt.Sprintf("%-10s %s\n", label, value)
}

// renderInfo renders the summary: header, the rows people look for, the
// non-default flags, every warning verbatim in red, and the hidden count.
func renderInfo(v InfoView) string {
	var buf bytes.Buffer

	header := "docker"
	if v.ServerVersion != "" {
		header += " " + v.ServerVersion
	}
	if v.Context != "" {
		header += " · context " + v.Context
	}
	if v.Host != "" {
		header += " · " + v.Host
	}
	buf.WriteString(paintBold(header))
	buf.WriteByte('\n')

	buf.WriteString(infoRow("containers", fmt.Sprintf(
		"%d total · %d running · %d paused · %d stopped · %d images",
		v.Containers, v.Running, v.Paused, v.Stopped, v.Images)))

	if v.StorageDriver != "" {
		s := v.StorageDriver
		if v.BackingFS != "" {
			s += " (" + v.BackingFS + ")"
		}
		if v.RootDir != "" {
			s += " · root " + v.RootDir
		}
		buf.WriteString(infoRow("storage", s))
	}

	var cgroup []string
	if v.CgroupVersion != "" {
		cgroup = append(cgroup, v.CgroupVersion)
	}
	if v.CgroupDriver != "" {
		cgroup = append(cgroup, v.CgroupDriver)
	}
	if len(cgroup) > 0 {
		buf.WriteString(infoRow("cgroup", strings.Join(cgroup, " · ")))
	}

	var runtime []string
	if v.DefaultRuntime != "" {
		seg := v.DefaultRuntime
		if v.RuncVersion != "" {
			seg += " " + v.RuncVersion
		}
		runtime = append(runtime, seg)
	}
	if v.ContainerdVersion != "" {
		runtime = append(runtime, "containerd "+v.ContainerdVersion)
	}
	if len(runtime) > 0 {
		buf.WriteString(infoRow("runtime", strings.Join(runtime, " · ")))
	}

	var system []string
	for _, part := range []string{v.OS, v.Kernel, v.Arch} {
		if part != "" {
			system = append(system, part)
		}
	}
	if v.CPUs > 0 {
		unit := "CPUs"
		if v.CPUs == 1 {
			unit = "CPU"
		}
		system = append(system, fmt.Sprintf("%d %s", v.CPUs, unit))
	}
	if v.Memory != "" {
		system = append(system, v.Memory)
	}
	if len(system) > 0 {
		buf.WriteString(infoRow("system", strings.Join(system, " · ")))
	}

	if v.LoggingDriver != "" {
		buf.WriteString(infoRow("logging", v.LoggingDriver))
	}
	if len(v.Plugins) > 0 {
		var plugs []string
		for _, p := range v.Plugins {
			if p.Version != "" {
				plugs = append(plugs, p.Name+" "+p.Version)
			} else {
				plugs = append(plugs, p.Name)
			}
		}
		buf.WriteString(infoRow("plugins", strings.Join(plugs, " · ")))
	}

	// Non-default flags only.
	if len(v.SecurityOptions) > 0 {
		buf.WriteString(infoRow("security", strings.Join(v.SecurityOptions, " · ")))
	}
	if v.SwarmActive {
		buf.WriteString(infoRow("swarm", "active"))
	}
	if insecureNonDefault(v.InsecureRegistries) {
		buf.WriteString(infoRow("insecure", strings.Join(v.InsecureRegistries, ", ")))
	}
	if v.LiveRestore {
		buf.WriteString(infoRow("live restore", "on"))
	}
	if v.Debug {
		buf.WriteString(infoRow("debug", "on"))
	}
	if v.Experimental {
		buf.WriteString(infoRow("experimental", "on"))
	}

	// Footer: every warning verbatim in red, then the hidden count.
	var footer []string
	for _, w := range v.Warnings {
		footer = append(footer, paintRed(w))
	}
	if v.Hidden > 0 {
		footer = append(footer, paintDim(fmt.Sprintf(
			"%d more fields hidden, run docker info for everything", v.Hidden)))
	}
	if len(footer) > 0 {
		buf.WriteByte('\n')
		for _, line := range footer {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// formatInfo renders piped docker info text; anything that is not a full
// info output — docker version, a daemon that is down, --format output —
// comes back byte for byte.
func formatInfo(w io.Writer, text string) {
	if !looksLikeInfo(text) {
		io.WriteString(w, text)
		return
	}
	out := renderInfo(normalizeInfoText(text))
	if usefulOutputLines(out) < 2 {
		io.WriteString(w, text)
		return
	}
	io.WriteString(w, out)
}
