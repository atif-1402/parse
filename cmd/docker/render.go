package docker

import (
	"bytes"
	"fmt"
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
