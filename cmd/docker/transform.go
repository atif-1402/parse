package docker

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
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
