package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/atif-1402/parse/internal/tool"
)

// NormalizeContainer converts raw docker ps JSON to our semantic Container model.
func NormalizeContainer(raw map[string]interface{}) Container {
	c := Container{}

	c.ID = getString(raw, "ID")
	c.ShortID = shortID(c.ID)
	c.Name = strings.TrimPrefix(getString(raw, "Names"), "/")
	c.Image = getString(raw, "Image")
	c.ImageTag = parseImageTag(c.Image)
	c.ImageID = getString(raw, "ImageID")
	c.Command = getString(raw, "Command")

	// Parse CreatedAt for the CREATED column (human time since creation)
	if created, ok := raw["CreatedAt"].(string); ok {
		if t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", created); err == nil {
			c.Created = Duration(time.Since(t))
		} else if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
			c.Created = Duration(time.Since(t))
		} else if t, err := time.Parse(time.RFC3339, created); err == nil {
			c.Created = Duration(time.Since(t))
		}
	} else if created, ok := raw["Created"].(float64); ok {
		c.Created = Duration(time.Since(time.Unix(int64(created), 0)))
	}

	c.Status = parseStatus(raw)
	c.Ports = parsePortList(getString(raw, "Ports"))
	c.SizeRw = Size(getInt64(raw, "SizeRw"))
	c.SizeRootFs = Size(getInt64(raw, "SizeRootFs"))

	return c
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt64(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func parseImageTag(image string) string {
	// image can be "nginx:latest" or "sha256:abc123"
	if strings.Contains(image, ":") && !strings.HasPrefix(image, "sha256:") {
		parts := strings.Split(image, ":")
		return parts[len(parts)-1]
	}
	return image
}

func parseHumanDurationStr(s string) Duration {
	// Try docker's "CreatedAt" format: "2026-10-09T17:30:30.583967143Z"
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return Duration(time.Since(t))
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return Duration(time.Since(t))
	}
	// Try human-readable format like "25 minutes ago"
	if d, err := parseHumanDuration(s); err == nil {
		return Duration(d)
	}
	return 0
}

func parseStatus(raw map[string]interface{}) ContainerStatus {
	status := ContainerStatus{}

	state := getString(raw, "State")
	switch state {
	case "running":
		status.State = "running"
	case "exited":
		status.State = "exited"
	case "created":
		status.State = "created"
	case "restarting":
		status.State = "restarting"
	case "paused":
		status.State = "paused"
	case "dead":
		status.State = "dead"
	default:
		status.State = state
	}

	status.ExitCode = getInt(raw, "ExitCode")
	status.RestartCount = getInt(raw, "RestartCount")

	// docker ps JSON has no ExitCode/RestartCount — parse from Status field.
	// Status looks like: "Exited (17) 3 minutes ago" or "Up 2 hours".
	if statusText := getString(raw, "Status"); statusText != "" {
		if status.ExitCode == 0 && strings.HasPrefix(statusText, "Exited (") {
			if idx := strings.Index(statusText, ")"); idx > 0 {
				fmt.Sscanf(statusText[8:idx], "%d", &status.ExitCode)
			}
		}
	}

	// Prefer the pre-formatted RunningFor from docker's JSON output
	if runningFor, ok := raw["RunningFor"].(string); ok {
		if d, err := parseHumanDuration(runningFor); err == nil {
			status.RunningFor = Duration(d)
		}
	} else if startedAt, ok := raw["StartedAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
			status.StartedAt = t
			status.RunningFor = Duration(time.Since(t))
		}
	}
	if finishedAt, ok := raw["FinishedAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, finishedAt); err == nil {
			status.FinishedAt = t
		}
	}

	if health, ok := raw["Health"].(map[string]interface{}); ok {
		status.Health = getString(health, "Status")
	}

	if oom, ok := raw["OOMKilled"].(bool); ok {
		status.OOMKilled = oom
	}
	if dead, ok := raw["Dead"].(bool); ok {
		status.Dead = dead
	}
	if paused, ok := raw["Paused"].(bool); ok {
		status.Paused = paused
	}
	if restarting, ok := raw["Restarting"].(bool); ok {
		status.Restarting = restarting
	}

	return status
}

func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

// parsePortList parses a docker PORTS column: one entry per binding,
// comma-space separated, e.g.
//
//	0.0.0.0:8081->80/tcp, [::]:8081->80/tcp, 8000-8002->8000-8002/tcp, 80/tcp
func parsePortList(text string) []Port {
	var ports []Port
	for _, entry := range strings.Split(text, ", ") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		ports = append(ports, parsePort(entry))
	}
	return ports
}

// parsePort parses one PORTS entry: an optional host side ("8081",
// "0.0.0.0:8081", "[::]:8081", ":::8081"), an optional "->", the container
// port or range, and the protocol after a slash.
func parsePort(entry string) Port {
	p := Port{}
	body := entry
	if host, ctr, ok := strings.Cut(entry, "->"); ok {
		p.Published = true
		p.IP, p.Public = splitHostPort(host)
		body = ctr
	}
	ctr, typ, hasType := strings.Cut(body, "/")
	p.Private = ctr
	if hasType {
		p.Type = typ
	}
	return p
}

// splitHostPort splits the host side of a binding into address and published
// port or range. "[::]:8081" is docker's own IPv6 form; ":::8081" is the
// same binding without brackets, where the port sits after the last colon.
func splitHostPort(host string) (ip, port string) {
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end > 0 {
			return host[1:end], strings.TrimPrefix(host[end+1:], ":")
		}
	}
	switch strings.Count(host, ":") {
	case 0:
		return "", host
	case 1:
		ip, port, _ = strings.Cut(host, ":")
		return ip, port
	default:
		i := strings.LastIndex(host, ":")
		return host[:i], host[i+1:]
	}
}

// SortContainers sorts already-normalized containers.
func SortContainers(containers []Container) []Container {
	sort.Slice(containers, func(i, j int) bool {
		a, b := containers[i], containers[j]

		// Running first
		aRunning := a.Status.State == "running"
		bRunning := b.Status.State == "running"
		if aRunning != bRunning {
			return aRunning
		}

		// Among running: newest first (longest running)
		if aRunning {
			if a.Status.RunningFor != b.Status.RunningFor {
				return a.Status.RunningFor > b.Status.RunningFor
			}
			return a.Name < b.Name
		}

		// Exited forms one contiguous block before the other states
		// (restarting, paused, created, dead), so a failure is always at the
		// top of the non-running section.
		aExited := a.Status.State == "exited"
		bExited := b.Status.State == "exited"
		if aExited != bExited {
			return aExited
		}

		if aExited {
			// Nonzero exit code first
			aNonZero := a.Status.ExitCode != 0
			bNonZero := b.Status.ExitCode != 0
			if aNonZero != bNonZero {
				return aNonZero
			}
			// Piped text rounds Created to whole minutes, so ties are
			// common; fall back to name so the order stays deterministic.
			if a.Created != b.Created {
				return a.Created > b.Created
			}
			return a.Name < b.Name
		}

		// Other states (restarting, paused, created, dead): by Created desc
		if a.Created != b.Created {
			return a.Created > b.Created
		}
		return a.Name < b.Name
	})
	return containers
}

// parsePSRow parses a single data row from docker ps text output.
func parsePSRow(line string, starts []int, fields []string) Container {
	c := Container{}
	for i := range starts {
		lo, hi := cellBounds([]rune(line), starts, i)
		if lo >= hi {
			continue
		}
		val := strings.TrimSpace(string([]rune(line)[lo:hi]))
		if i >= len(fields) {
			continue
		}
		switch fields[i] {
		case "CONTAINER ID":
			c.ID = val
			c.ShortID = shortID(val)
		case "NAMES":
			c.Name = strings.TrimPrefix(val, "/")
		case "IMAGE":
			c.Image = val
			c.ImageTag = parseImageTag(val)
		case "PORTS":
			c.Ports = parsePortList(val)
		case "CREATED":
			c.Created = parseHumanDurationStr(val)
		case "STATUS":
			c.Status = parseStatusFromText(val)
		}
	}
	return c
}

// parseStatusFromText parses docker ps STATUS text into ContainerStatus.
func parseStatusFromText(text string) ContainerStatus {
	s := ContainerStatus{}
	text = strings.TrimSpace(text)

	// Check paused first — it's "Up 3 minutes (Paused)", not a plain "Up ".
	if strings.Contains(text, "(Paused)") {
		s.State = "paused"
		// Strip "(Paused)" before parsing the duration.
		cleaned := strings.ReplaceAll(text, "(Paused)", "")
		cleaned = strings.TrimSpace(cleaned)
		if idx := strings.Index(cleaned, "Up "); idx >= 0 {
			s.RunningFor = parseHumanDurationStr(cleaned[idx+3:])
		}
		return s
	}

	// Strip health suffix like " (healthy)" before parsing.
	healthSuffix := ""
	if idx := strings.Index(text, " ("); idx >= 0 && strings.HasSuffix(text, ")") {
		healthSuffix = strings.TrimSuffix(strings.TrimPrefix(text[idx:], " ("), ")")
		text = strings.TrimSpace(text[:idx])
	}

	if strings.HasPrefix(text, "Up ") {
		s.State = "running"
		s.RunningFor = parseHumanDurationStr(strings.TrimPrefix(text, "Up "))
	} else if strings.HasPrefix(text, "Exited (") {
		s.State = "exited"
		// Format: "Exited (0) 2 hours ago"
		if idx := strings.Index(text, ")"); idx > 0 {
			codeStr := text[8:idx]
			fmt.Sscanf(codeStr, "%d", &s.ExitCode)
		}
		// Extract the duration part after ")"
		if idx := strings.Index(text, ")"); idx > 0 {
			durationPart := strings.TrimSpace(text[idx+1:])
			s.RunningFor = parseHumanDurationStr(durationPart)
		}
	} else if text == "Created" || strings.HasPrefix(text, "Created ") {
		s.State = "created"
		if len(text) > 8 {
			s.RunningFor = parseHumanDurationStr(strings.TrimPrefix(text, "Created "))
		}
	} else if strings.HasPrefix(text, "Restarting (") {
		s.State = "restarting"
		if idx := strings.Index(text, ")"); idx > 0 {
			fmt.Sscanf(text[11:idx], "%d", &s.RestartCount)
		}
		if idx := strings.Index(text, "ago"); idx > 0 {
			s.RunningFor = parseHumanDurationStr(text[:idx+3])
		}
	} else if strings.HasPrefix(text, "Paused ") {
		s.State = "paused"
		s.RunningFor = parseHumanDurationStr(strings.TrimPrefix(text, "Paused "))
	} else if text == "Dead" {
		s.State = "dead"
	}
	if healthSuffix != "" {
		s.Health = healthSuffix
	}
	return s
}

// NormalizeInspect converts raw docker inspect JSON to our semantic InspectView model.
func NormalizeInspect(raw []map[string]interface{}) []InspectView {
	if len(raw) == 0 {
		return nil
	}
	// One inspect run, one image lookup per image: the memo must not carry
	// answers across runs (or across tests).
	imageEnvMemo = make(map[string]imageEnvResult)
	views := make([]InspectView, len(raw))
	for i, r := range raw {
		views[i] = normalizeSingleInspect(r)
	}
	return views
}

func normalizeSingleInspect(raw map[string]interface{}) InspectView {
	view := InspectView{}

	// Identity
	view.Identity = normalizeIdentity(raw)

	// Command
	view.Command = normalizeCommand(raw)

	// State
	view.State = normalizeState(raw)

	// Network
	view.Network = normalizeNetwork(raw)

	// Ports
	view.Ports = normalizePorts(raw)

	// Mounts
	view.Mounts = normalizeMounts(raw)

	// Env
	view.Env = normalizeEnv(raw)

	// Resources
	view.Resources = normalizeResources(raw)

	// Restart
	view.Restart = normalizeRestart(raw)

	// Health
	view.Health = normalizeHealth(raw)

	// Security
	view.Security = normalizeSecurity(raw)

	return view
}

func normalizeIdentity(raw map[string]interface{}) IdentitySection {
	id := getString(raw, "Id")
	name := strings.TrimPrefix(getString(raw, "Name"), "/")

	// Config.Image holds the original image reference (e.g. "nginx:1.25").
	// The top-level Image is a sha256 hash — only use it as last resort.
	image := ""
	if config, ok := raw["Config"].(map[string]interface{}); ok {
		image = getString(config, "Image")
	}
	if image == "" || strings.HasPrefix(image, "sha256:") {
		image = getString(raw, "Image")
	}

	// If still a hash, shorten it to 12 chars — never show the full 64.
	if strings.HasPrefix(image, "sha256:") {
		image = shortID(strings.TrimPrefix(image, "sha256:"))
	}

	return IdentitySection{
		Name:     name,
		Image:    image,
		ImageTag: parseImageTag(image),
		ShortID:  shortID(id),
	}
}

func normalizeCommand(raw map[string]interface{}) string {
	if config, ok := raw["Config"].(map[string]interface{}); ok {
		var parts []string
		if entry, ok := config["Entrypoint"].([]interface{}); ok {
			for _, e := range entry {
				if s, ok := e.(string); ok {
					parts = append(parts, s)
				}
			}
		}
		if cmd, ok := config["Cmd"].([]interface{}); ok {
			for _, c := range cmd {
				if s, ok := c.(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func normalizeState(raw map[string]interface{}) StateSection {
	state := getString(raw, "State")
	status := StateSection{}

	// RestartCount lives at the top level of inspect output, not in State.
	status.RestartCount = getInt(raw, "RestartCount")

	if stateObj, ok := raw["State"].(map[string]interface{}); ok {
		status.Status = getString(stateObj, "Status")
		if runningFor := getString(stateObj, "RunningFor"); runningFor != "" {
			if d, err := parseHumanDuration(runningFor); err == nil {
				status.RunningFor = Duration(d).Human()
			}
		}
		status.OOMKilled = getBool(stateObj, "OOMKilled")
		status.ExitCode = getInt(stateObj, "ExitCode")
		status.Pid = getInt(stateObj, "Pid")
		status.Paused = getBool(stateObj, "Paused")
		status.Restarting = getBool(stateObj, "Restarting")
		status.Dead = getBool(stateObj, "Dead")

		// Running containers: compute uptime from StartedAt.
		if status.Status == "running" && status.RunningFor == "" {
			if startedAt := getString(stateObj, "StartedAt"); startedAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
					status.RunningFor = Duration(time.Since(t)).Human()
				}
			}
		}
		// Exited containers: compute how long ago it finished.
		if status.Status == "exited" && status.RunningFor == "" {
			if finishedAt := getString(stateObj, "FinishedAt"); finishedAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, finishedAt); err == nil {
					status.RunningFor = Duration(time.Since(t)).Human()
				}
			}
		}
	} else {
		status.Status = state
	}

	return status
}

func normalizeNetwork(raw map[string]interface{}) NetworkSection {
	net := NetworkSection{}

	if networkSettings, ok := raw["NetworkSettings"].(map[string]interface{}); ok {
		net.NetworkName = getString(networkSettings, "NetworkName")
		if net.NetworkName == "" {
			// Try to get from Networks map
			if networks, ok := networkSettings["Networks"].(map[string]interface{}); ok {
				for name, netInfo := range networks {
					if ni, ok := netInfo.(map[string]interface{}); ok {
						net.NetworkName = name
						net.IP = getString(ni, "IPAddress")
						net.Gateway = getString(ni, "Gateway")
						break
					}
				}
			}
		}
		net.Driver = getString(networkSettings, "Driver")
	}

	// Fallback to top-level NetworkMode
	if net.NetworkName == "" {
		net.NetworkName = getString(raw, "NetworkMode")
	}

	return net
}

func normalizePorts(raw map[string]interface{}) PortsSection {
	ports := PortsSection{}

	if networkSettings, ok := raw["NetworkSettings"].(map[string]interface{}); ok {
		if portsMap, ok := networkSettings["Ports"].(map[string]interface{}); ok {
			for containerPort, bindings := range portsMap {
				if bindings == nil {
					ports.Exposed = append(ports.Exposed, containerPort)
					continue
				}
				if bindingsArr, ok := bindings.([]interface{}); ok {
					for _, binding := range bindingsArr {
						if b, ok := binding.(map[string]interface{}); ok {
							hostIP := getString(b, "HostIp")
							hostPort := getString(b, "HostPort")
							if hostPort != "" {
								ports.Published = append(ports.Published, publishString(hostIP, hostPort, containerPort))
							} else {
								ports.Exposed = append(ports.Exposed, containerPort)
							}
						}
					}
				}
			}
		}
	}

	// A container that never started has empty NetworkSettings.Ports; the
	// intended bindings still live in HostConfig.PortBindings and the
	// declared ports in Config.ExposedPorts.
	if len(ports.Published) == 0 {
		if hostConfig, ok := raw["HostConfig"].(map[string]interface{}); ok {
			if bindings, ok := hostConfig["PortBindings"].(map[string]interface{}); ok {
				for containerPort, b := range bindings {
					if bArr, ok := b.([]interface{}); ok {
						for _, binding := range bArr {
							if bm, ok := binding.(map[string]interface{}); ok {
								hostIP := getString(bm, "HostIp")
								hostPort := getString(bm, "HostPort")
								if hostPort == "" {
									continue
								}
								ports.Published = append(ports.Published, publishString(hostIP, hostPort, containerPort))
							}
						}
					}
				}
			}
		}
	}
	if len(ports.Exposed) == 0 && len(ports.Published) == 0 {
		if config, ok := raw["Config"].(map[string]interface{}); ok {
			if exposed, ok := config["ExposedPorts"].(map[string]interface{}); ok {
				for containerPort := range exposed {
					ports.Exposed = append(ports.Exposed, containerPort)
				}
			}
		}
	}

	// One wildcard binding is reported twice, on 0.0.0.0 and on ::, and both
	// render to the same "8081->80/tcp" — keep one.
	ports.Published = dedupStrings(ports.Published)

	// Sort for consistent output
	sort.Strings(ports.Published)
	sort.Strings(ports.Exposed)

	return ports
}

// publishString renders one published binding. The wildcard addresses
// (0.0.0.0, ::, empty) all mean "every interface", so none of them is spelled
// out: 0.0.0.0:8081->80/tcp and :::8081->80/tcp are one binding, not two.
func publishString(hostIP, hostPort, containerPort string) string {
	if hostIP != "" && hostIP != "0.0.0.0" && hostIP != "::" {
		return fmt.Sprintf("%s:%s->%s", hostIP, hostPort, containerPort)
	}
	return fmt.Sprintf("%s->%s", hostPort, containerPort)
}

// dedupStrings drops repeats, keeping the first of each.
func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func normalizeMounts(raw map[string]interface{}) []MountSection {
	var mounts []MountSection

	if mountsArr, ok := raw["Mounts"].([]interface{}); ok {
		for _, m := range mountsArr {
			if mMap, ok := m.(map[string]interface{}); ok {
				mount := MountSection{
					Type:        getString(mMap, "Type"),
					Source:      getString(mMap, "Source"),
					Destination: getString(mMap, "Destination"),
					Mode:        getString(mMap, "Mode"),
					RW:          getBool(mMap, "RW"),
					Propagation: getString(mMap, "Propagation"),
				}
				// Check if source path exists (for bind mounts)
				if mount.Type == "bind" && mount.Source != "" {
					if _, err := os.Stat(mount.Source); os.IsNotExist(err) {
						mount.Missing = true
					}
				}
				mounts = append(mounts, mount)
			}
		}
	}

	// Also check HostConfig.Binds for bind mounts
	if hostConfig, ok := raw["HostConfig"].(map[string]interface{}); ok {
		if binds, ok := hostConfig["Binds"].([]interface{}); ok {
			for _, b := range binds {
				if bindStr, ok := b.(string); ok {
					parts := strings.Split(bindStr, ":")
					if len(parts) >= 2 {
						mount := MountSection{
							Type:        "bind",
							Source:      parts[0],
							Destination: parts[1],
							Mode:        "rw",
						}
						if len(parts) >= 3 {
							mount.Mode = parts[2]
						}
						// Check if source exists
						if _, err := os.Stat(mount.Source); os.IsNotExist(err) {
							mount.Missing = true
						}
						// Avoid duplicates
						found := false
						for _, m := range mounts {
							if m.Source == mount.Source && m.Destination == mount.Destination {
								found = true
								break
							}
						}
						if !found {
							mounts = append(mounts, mount)
						}
					}
				}
			}
		}
	}

	return mounts
}

// imageDefaultEnv lists env vars that come from the image itself, not from
// anything the user configured. They are noise in an inspect summary.
var imageDefaultEnv = map[string]bool{
	"PATH": true, "HOSTNAME": true, "HOME": true,
	"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LC_CTYPE": true,
	"GPG_KEY": true, "TERM": true, "SHLVL": true, "PWD": true,
	"container": true, "DOCKER_CONTAINER": true,
}

func normalizeEnv(raw map[string]interface{}) EnvSection {
	env := EnvSection{
		Vars:    make(map[string]string),
		Secrets: make(map[string]bool),
	}

	var config map[string]interface{}
	if c, ok := raw["Config"].(map[string]interface{}); ok {
		config = c
		if envArr, ok := c["Env"].([]interface{}); ok {
			for _, e := range envArr {
				if envStr, ok := e.(string); ok {
					parts := strings.SplitN(envStr, "=", 2)
					if len(parts) == 2 {
						env.Vars[parts[0]] = parts[1]
					}
				}
			}
		}
	}

	// What the image ships is not what the user set: a KEY=VALUE the image
	// defines byte for byte is dropped. When the image's env cannot be
	// fetched (no daemon, image removed, an answer in the wrong shape), the
	// static list of vars every image sets keeps the common ones quiet
	// anyway, so a missing daemon degrades to the old behaviour rather than
	// dumping PATH and friends back into the output.
	var imagePairs map[string]bool
	if config != nil {
		if img := getString(config, "Image"); img != "" {
			imagePairs = imageEnv(img)
		}
	}
	for k, v := range env.Vars {
		if imagePairs[k+"="+v] || imageDefaultEnv[k] {
			delete(env.Vars, k)
		}
	}

	env.Count = len(env.Vars)

	// Detect secrets
	secretPatterns := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "AUTH"}
	for k := range env.Vars {
		upper := strings.ToUpper(k)
		for _, pattern := range secretPatterns {
			if strings.Contains(upper, pattern) {
				env.Secrets[k] = true
				break
			}
		}
	}

	return env
}

// imageEnvResult is an image's env as KEY=VALUE pairs, or a failed lookup.
type imageEnvResult struct {
	pairs map[string]bool
	ok    bool
}

// imageEnvMemo caches one lookup per image per inspect run: a list of
// containers from the same image would otherwise ask docker again for each.
var imageEnvMemo map[string]imageEnvResult

// imageEnvOf fetches an image's own env. It is a var so tests can answer
// without a docker daemon.
var imageEnvOf = fetchImageEnv

// imageEnv returns the image's KEY=VALUE pairs, or nil when the lookup
// failed — in which case nothing is hidden on the image's account.
func imageEnv(image string) map[string]bool {
	if imageEnvMemo == nil {
		imageEnvMemo = make(map[string]imageEnvResult)
	}
	if r, hit := imageEnvMemo[image]; hit {
		if !r.ok {
			return nil
		}
		return r.pairs
	}
	pairs, ok := imageEnvOf(image)
	imageEnvMemo[image] = imageEnvResult{pairs: pairs, ok: ok}
	if !ok {
		return nil
	}
	return pairs
}

// fetchImageEnv runs `docker image inspect` for one image. The JSON must be
// shaped like an image (an image has no State — a container's inspect does):
// an answer in the wrong shape must fall back to showing vars rather than
// hiding the wrong ones.
func fetchImageEnv(image string) (map[string]bool, bool) {
	text, code, started := tool.Capture("docker", []string{"image", "inspect", image})
	if !started || code != 0 {
		return nil, false
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &arr); err != nil || len(arr) == 0 {
		return nil, false
	}
	if _, isContainer := arr[0]["State"]; isContainer {
		return nil, false
	}
	config, ok := arr[0]["Config"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	pairs := make(map[string]bool)
	if envArr, ok := config["Env"].([]interface{}); ok {
		for _, e := range envArr {
			if s, ok := e.(string); ok {
				pairs[s] = true
			}
		}
	}
	return pairs, true
}

func normalizeResources(raw map[string]interface{}) ResourcesSection {
	res := ResourcesSection{}

	if hostConfig, ok := raw["HostConfig"].(map[string]interface{}); ok {
		res.MemoryLimit = Size(getInt64(hostConfig, "Memory"))
		res.MemorySwap = Size(getInt64(hostConfig, "MemorySwap"))
		res.MemorySwappiness = getInt64(hostConfig, "MemorySwappiness")
		res.CPUQuota = getInt64(hostConfig, "CpuQuota")
		res.CPUPeriod = getInt64(hostConfig, "CpuPeriod")
		res.CPUShares = getInt64(hostConfig, "CpuShares")
		res.ShmSize = Size(getInt64(hostConfig, "ShmSize"))
		res.PidsLimit = getInt64(hostConfig, "PidsLimit")
		// `docker run --cpus 1.5` lands in NanoCpus (1.5e9), not Config.
		if nano := getInt64(hostConfig, "NanoCpus"); nano > 0 {
			res.CPUS = float64(nano) / 1e9
		}
	}

	return res
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func normalizeRestart(raw map[string]interface{}) RestartSection {
	res := RestartSection{Name: "no"}

	if hostConfig, ok := raw["HostConfig"].(map[string]interface{}); ok {
		if restartPolicy, ok := hostConfig["RestartPolicy"].(map[string]interface{}); ok {
			res.Name = getString(restartPolicy, "Name")
			res.MaximumRetryCount = getInt(restartPolicy, "MaximumRetryCount")
		}
	}
	return res
}

func normalizeHealth(raw map[string]interface{}) HealthSection {
	health := HealthSection{Status: "none"}

	// State.Health is an object {"Status": "...", "FailingStreak": n}, not a
	// string — reading it as a string always yielded nothing.
	if state, ok := raw["State"].(map[string]interface{}); ok {
		if h, ok := state["Health"].(map[string]interface{}); ok {
			if s := getString(h, "Status"); s != "" {
				health.Status = s
			}
			health.FailingStreak = getInt(h, "FailingStreak")
		}
	}

	// Whether a healthcheck exists at all, so a container that never started
	// can still report "starting" instead of hiding the question.
	var configured bool
	if config, ok := raw["Config"].(map[string]interface{}); ok {
		if _, ok := config["Healthcheck"].(map[string]interface{}); ok {
			configured = true
		}
	}

	// A container that never started has no health state at all; if a
	// healthcheck is configured, report "starting" rather than hiding it.
	if health.Status == "none" && configured {
		health.Status = "starting"
	}

	return health
}

func normalizeSecurity(raw map[string]interface{}) SecuritySection {
	sec := SecuritySection{}

	if hostConfig, ok := raw["HostConfig"].(map[string]interface{}); ok {
		sec.Privileged = getBool(hostConfig, "Privileged")
		sec.ReadonlyRootfs = getBool(hostConfig, "ReadonlyRootfs")

		if config, ok := raw["Config"].(map[string]interface{}); ok {
			sec.User = getString(config, "User")
		}

		if capAdd, ok := hostConfig["CapAdd"].([]interface{}); ok {
			for _, c := range capAdd {
				if s, ok := c.(string); ok {
					sec.CapAdd = append(sec.CapAdd, s)
				}
			}
		}
		if capDrop, ok := hostConfig["CapDrop"].([]interface{}); ok {
			for _, c := range capDrop {
				if s, ok := c.(string); ok {
					sec.CapDrop = append(sec.CapDrop, s)
				}
			}
		}
		if secOpt, ok := hostConfig["SecurityOpt"].([]interface{}); ok {
			for _, s := range secOpt {
				if str, ok := s.(string); ok {
					sec.SecurityOpt = append(sec.SecurityOpt, str)
				}
			}
		}
	} else if config, ok := raw["Config"].(map[string]interface{}); ok {
		sec.User = getString(config, "User")
	}

	return sec
}
