package docker

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/atif-1402/parse/internal/tool"
)

const (
	colGap = "  "

	// defaultShmSize is docker's default /dev/shm for a container: 64MB is
	// what every container that never asked for anything else has, so
	// printing it says nothing.
	defaultShmSize = 64 * 1024 * 1024
)

// PSColumn represents a column in the ps output.
type PSColumn struct {
	Name  string
	Width int
	Align string // "left" or "right"
	Color func(string) string
}

// RenderPS renders the docker ps table with the new layout.
func RenderPS(containers []Container) string {
	if len(containers) == 0 {
		return ""
	}

	// Define columns in priority order
	cols := []PSColumn{
		{Name: "NAME", Align: "left", Color: paintBold},
		{Name: "STATE", Align: "left"},
		{Name: "IMAGE", Align: "left"},
		{Name: "PORTS", Align: "left"},
		{Name: "CREATED", Align: "left"},
		{Name: "ID", Align: "left", Color: paintDim},
	}

	// Compute widths
	rows := make([][]string, len(containers))
	for i, c := range containers {
		rows[i] = []string{
			orDash(c.Name),
			formatState(c.Status),
			orDash(c.Image),
			orDash(formatPorts(c.Ports)),
			orDash(c.Created.Human()),
			c.ShortID,
		}
	}

	widths := make([]int, len(cols))
	for i, col := range cols {
		widths[i] = len(col.Name)
	}
	for _, row := range rows {
		for i, cell := range row {
			if visibleLen(cell) > widths[i] {
				widths[i] = visibleLen(cell)
			}
		}
	}

	// Build output
	var buf bytes.Buffer

	// Header
	for i, col := range cols {
		if i > 0 {
			buf.WriteString(colGap)
		}
		cell := padRight(col.Name, widths[i], col.Align)
		buf.WriteString(tool.PaintColor(tool.Bold, cell))
	}
	buf.WriteByte('\n')

	// Rows
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				buf.WriteString(colGap)
			}
			col := cols[i]
			padded := padRight(cell, widths[i], col.Align)
			if col.Color != nil {
				buf.WriteString(col.Color(padded))
			} else {
				buf.WriteString(padded)
			}
		}
		buf.WriteByte('\n')
	}

	// Summary line
	summary := buildSummary(containers)
	if summary != "" {
		buf.WriteString(paintDim(summary))
		buf.WriteByte('\n')
	}

	return buf.String()
}

// orDash returns the string or a dim dash if empty.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return paintDim("-")
	}
	return s
}

// buildSummary creates a summary line like "4 containers: 2 running, 2 exited"
func buildSummary(containers []Container) string {
	if len(containers) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, c := range containers {
		counts[c.Status.State]++
	}
	var parts []string
	total := len(containers)
	order := []string{"running", "exited", "created", "restarting", "paused", "dead"}
	for _, state := range order {
		if count := counts[state]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, state))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("%d container%s: %s", total, plural(total), strings.Join(parts, ", "))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func formatState(s ContainerStatus) string {
	switch s.State {
	case "running":
		if s.RunningFor > 0 {
			return paintGreen(fmt.Sprintf("%s %s", s.State, s.RunningFor.Human()))
		}
		return paintGreen(s.State)
	case "exited":
		if s.ExitCode == 0 {
			if s.RunningFor > 0 {
				return paintDim(fmt.Sprintf("%s %d %s ago", s.State, s.ExitCode, s.RunningFor.Human()))
			}
			return paintDim(fmt.Sprintf("%s %d", s.State, s.ExitCode))
		}
		if s.RunningFor > 0 {
			return paintRed(fmt.Sprintf("%s %d %s ago", s.State, s.ExitCode, s.RunningFor.Human()))
		}
		return paintRed(fmt.Sprintf("%s %d", s.State, s.ExitCode))
	case "created", "restarting", "paused":
		if s.RunningFor > 0 {
			return paintYellow(fmt.Sprintf("%s %s", s.State, s.RunningFor.Human()))
		}
		return paintYellow(s.State)
	case "dead":
		return paintRed(s.State)
	default:
		return s.State
	}
}

func formatPorts(ports []Port) string {
	if len(ports) == 0 {
		return ""
	}
	var published, exposed []string
	seen := make(map[string]bool, len(ports))
	for _, p := range ports {
		if p.Published {
			// 0.0.0.0:8081->80/tcp and [::]:8081->80/tcp are one binding
			// written twice; they render to the same string, so the second
			// one is dropped.
			s := p.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			published = append(published, s)
		} else {
			exposed = append(exposed, paintDim(p.String()))
		}
	}
	var all []string
	all = append(all, published...)
	all = append(all, exposed...)
	return strings.Join(all, ", ")
}

// visibleLen returns the length of a string without ANSI codes.
func visibleLen(s string) int {
	// Simple approximation: count non-ANSI chars
	count := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == 'm' {
				inEscape = false
			}
			continue
		}
		count++
	}
	return count
}

func padRight(s string, width int, align string) string {
	vlen := visibleLen(s)
	if vlen >= width {
		return s
	}
	pad := strings.Repeat(" ", width-vlen)
	if align == "right" {
		return pad + s
	}
	return s + pad
}

func paintGreen(s string) string  { return tool.PaintColor(tool.Green, s) }
func paintRed(s string) string    { return tool.PaintColor(tool.Red, s) }
func paintYellow(s string) string { return tool.PaintColor(tool.Yellow, s) }
func paintDim(s string) string    { return tool.PaintColor(tool.Dim, s) }
func paintBold(s string) string   { return tool.PaintColor(tool.Bold, s) }

// RenderInspect renders the docker inspect output in the new semantic format.
func RenderInspect(views []InspectView) string {
	if len(views) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, view := range views {
		if i > 0 {
			buf.WriteByte('\n')
			buf.WriteByte('\n')
		}
		buf.WriteString(renderInspectView(view))
	}
	return buf.String()
}

func renderInspectView(view InspectView) string {
	var buf bytes.Buffer

	// Identity
	buf.WriteString(paintBold(view.Identity.Name))
	buf.WriteString("  ")
	if view.Identity.Image != "" {
		buf.WriteString(paintDim("(" + view.Identity.Image + ")"))
		buf.WriteString("  ")
	}
	buf.WriteString(paintDim(view.Identity.ShortID))
	buf.WriteByte('\n')

	// Command — what the container is doing
	if view.Command != "" {
		buf.WriteString("command   " + paintDim(view.Command))
		buf.WriteByte('\n')
	}

	// State
	if view.State.Status != "" {
		stateStr := view.State.Status
		if view.State.RunningFor != "" {
			if view.State.Status == "exited" {
				stateStr += " " + view.State.RunningFor + " ago"
			} else {
				stateStr += " " + view.State.RunningFor
			}
		}
		if view.State.RestartCount > 0 {
			stateStr += " · restarts " + fmt.Sprintf("%d", view.State.RestartCount)
		}
		if view.State.OOMKilled {
			stateStr += " " + tool.PaintColor(tool.Red, "OOM killed")
		}
		if view.State.ExitCode != 0 && view.State.Status == "exited" {
			stateStr += " · exit " + tool.PaintColor(tool.Red, fmt.Sprintf("%d", view.State.ExitCode))
		}

		// Color the entire state string based on status
		var coloredStateStr string
		switch view.State.Status {
		case "running":
			coloredStateStr = paintGreen(stateStr)
		case "exited":
			if view.State.ExitCode == 0 {
				coloredStateStr = paintDim(stateStr)
			} else {
				coloredStateStr = paintRed(stateStr)
			}
		case "created", "restarting", "paused":
			coloredStateStr = paintYellow(stateStr)
		case "dead":
			coloredStateStr = paintRed(stateStr)
		default:
			coloredStateStr = stateStr
		}

		buf.WriteString("state     " + coloredStateStr)
		buf.WriteByte('\n')
	}

	// Network — skip default bridge with no IP on a stopped container
	if showNetwork(view) {
		netStr := view.Network.NetworkName
		if view.Network.IP != "" {
			netStr += " · " + view.Network.IP
		}
		if view.Network.Gateway != "" {
			netStr += " · gw " + view.Network.Gateway
		}
		if view.Network.Driver != "" {
			netStr += " · " + view.Network.Driver
		}
		buf.WriteString("network   " + netStr)
		buf.WriteByte('\n')
	}

	// Ports
	if len(view.Ports.Published) > 0 || len(view.Ports.Exposed) > 0 {
		if len(view.Ports.Published) > 0 {
			buf.WriteString("ports     " + strings.Join(view.Ports.Published, ", "))
			buf.WriteByte('\n')
		}
		if len(view.Ports.Exposed) > 0 {
			buf.WriteString("ports     " + paintDim(strings.Join(view.Ports.Exposed, ", ")))
			buf.WriteByte('\n')
		}
	}

	// Mounts
	if len(view.Mounts) > 0 {
		for _, m := range view.Mounts {
			var mountStr string
			if m.Type == "bind" {
				mountStr = m.Source + " -> " + m.Destination
			} else if m.Type == "volume" {
				mountStr = m.Source + " -> " + m.Destination
			} else if m.Type == "tmpfs" {
				mountStr = "tmpfs " + m.Destination
			} else {
				mountStr = m.Source + " -> " + m.Destination
			}
			if m.Mode != "" {
				mountStr += " (" + m.Mode + ")"
			}
			if m.Missing {
				mountStr += " " + tool.PaintColor(tool.Red, "(missing)")
			}
			buf.WriteString("mounts    " + mountStr)
			buf.WriteByte('\n')
		}
	}

	// Env — normalizeEnv has already dropped what the image ships and what
	// the static list knows is image noise; everything left is the user's.
	if view.Env.Count > 0 {
		var varNames []string
		for k := range view.Env.Vars {
			varNames = append(varNames, k)
		}
		sort.Strings(varNames)
		if len(varNames) > 0 {
			unit := "vars"
			if len(varNames) == 1 {
				unit = "var"
			}
			buf.WriteString(fmt.Sprintf("env       %d %s\n", len(varNames), unit))
			anyMasked := false
			for _, k := range varNames {
				value := view.Env.Vars[k]
				masked := false
				if !view.Env.ShowSecrets {
					if view.Env.Secrets[k] {
						value = "********"
						masked = true
					} else if mv, ok := maskURLPassword(view.Env.Vars[k]); ok {
						value = mv
						masked = true
					}
				}
				if masked {
					anyMasked = true
					value = paintDim(value)
				}
				buf.WriteString("env       " + k + "=" + value)
				buf.WriteByte('\n')
			}
			if anyMasked {
				buf.WriteString("env       " + paintDim("(secrets masked, use --show-secrets)"))
				buf.WriteByte('\n')
			}
		}
	}

	// Resources
	if view.Resources.MemoryLimit > 0 || view.Resources.CPUQuota > 0 || view.Resources.CPUS > 0 {
		var resParts []string
		if view.Resources.MemoryLimit > 0 {
			resParts = append(resParts, "memory: "+view.Resources.MemoryLimit.Human())
		}
		if view.Resources.CPUQuota > 0 && view.Resources.CPUPeriod > 0 {
			resParts = append(resParts, "cpu: "+formatCPU(float64(view.Resources.CPUQuota)/float64(view.Resources.CPUPeriod)))
		}
		if view.Resources.CPUS > 0 {
			resParts = append(resParts, "cpu: "+formatCPU(view.Resources.CPUS))
		}
		if view.Resources.ShmSize > 0 && view.Resources.ShmSize != defaultShmSize {
			resParts = append(resParts, "shm: "+view.Resources.ShmSize.Human())
		}
		if view.Resources.PidsLimit > 0 {
			resParts = append(resParts, fmt.Sprintf("pids: %d", view.Resources.PidsLimit))
		}
		if len(resParts) > 0 {
			buf.WriteString("limits    " + strings.Join(resParts, " · "))
			buf.WriteByte('\n')
		}
	}

	// Restart
	if view.Restart.Name != "" && view.Restart.Name != "no" {
		restartStr := view.Restart.Name
		if view.Restart.MaximumRetryCount > 0 {
			restartStr += " (max " + fmt.Sprintf("%d", view.Restart.MaximumRetryCount) + ")"
		}
		buf.WriteString("restart   " + restartStr)
		buf.WriteByte('\n')
	}

	// Health — the status alone; the command that produced it is one
	// `docker inspect` away and reads as noise on this line.
	if view.Health.Status != "none" && view.Health.Status != "" {
		healthColor := tool.Green
		if view.Health.Status == "unhealthy" {
			healthColor = tool.Red
		} else if view.Health.Status == "starting" {
			healthColor = tool.Yellow
		}
		buf.WriteString("health    " + tool.PaintColor(healthColor, view.Health.Status))
		buf.WriteByte('\n')
	}

	// Security
	secParts := []string{}
	if view.Security.Privileged {
		secParts = append(secParts, tool.PaintColor(tool.Red, "privileged"))
	}
	if view.Security.ReadonlyRootfs {
		secParts = append(secParts, "readonly")
	}
	if len(view.Security.CapAdd) > 0 {
		secParts = append(secParts, "caps: "+strings.Join(view.Security.CapAdd, ","))
	}
	if len(view.Security.CapDrop) > 0 {
		secParts = append(secParts, "drop: "+strings.Join(view.Security.CapDrop, ","))
	}
	if len(view.Security.SecurityOpt) > 0 {
		secParts = append(secParts, "secopt: "+strings.Join(view.Security.SecurityOpt, ","))
	}
	// Docker reports root as an empty User. Flag it only alongside other
	// security risks — root alone is the default and would be pure noise.
	isRoot := view.Security.User == "" || view.Security.User == "root"
	if !isRoot {
		secParts = append(secParts, "user: "+view.Security.User)
	} else if len(secParts) > 0 {
		secParts = append(secParts, tool.PaintColor(tool.Red, "root"))
	}
	if len(secParts) > 0 {
		buf.WriteString("security  " + strings.Join(secParts, " · "))
		buf.WriteByte('\n')
	}

	return buf.String()
}

// formatCPU renders a CPU count without pointless trailing zeros (1.5 not 1.50).
func formatCPU(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// maskURLPassword masks the password inside a URL value like
// postgres://user:pass@host/db → postgres://user:********@host/db.
func maskURLPassword(v string) (string, bool) {
	scheme := strings.Index(v, "://")
	if scheme < 0 {
		return v, false
	}
	rest := v[scheme+3:]
	at := strings.Index(rest, "@")
	if at < 0 {
		return v, false
	}
	cred := rest[:at]
	colon := strings.Index(cred, ":")
	if colon < 0 {
		return v, false
	}
	return v[:scheme+3] + cred[:colon] + ":********" + rest[at:], true
}

// showNetwork decides whether the network line carries information.
// Default bridge with no IP on a stopped container is noise.
func showNetwork(view InspectView) bool {
	if view.Network.NetworkName == "" && view.Network.IP == "" {
		return false
	}
	// Stopped on default bridge with no IP: no information
	if view.Network.IP == "" &&
		(view.Network.NetworkName == "bridge" || view.Network.NetworkName == "default") {
		return false
	}
	return true
}
