package docker

import (
	"fmt"
	"strings"
	"time"
)

// Duration is a human-readable duration.
type Duration time.Duration

func parseHumanDuration(s string) (time.Duration, error) {
	// Docker formats: "37 minutes ago", "2 hours ago", "3 days ago", "5 seconds ago"
	// Also handles "About an hour ago", "About a minute ago"
	s = strings.TrimSuffix(s, " ago")
	s = strings.TrimPrefix(s, "About ")
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0, nil
	}
	val := parts[0]
	unit := parts[1]
	var mult time.Duration
	switch {
	case strings.HasPrefix(unit, "second"):
		mult = time.Second
	case strings.HasPrefix(unit, "minute"):
		mult = time.Minute
	case strings.HasPrefix(unit, "hour"):
		mult = time.Hour
	case strings.HasPrefix(unit, "day"):
		mult = 24 * time.Hour
	case strings.HasPrefix(unit, "week"):
		mult = 7 * 24 * time.Hour
	case strings.HasPrefix(unit, "month"):
		mult = 30 * 24 * time.Hour
	case strings.HasPrefix(unit, "year"):
		mult = 365 * 24 * time.Hour
	default:
		return 0, nil
	}
	var n int
	if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
		// Handle "an" or "a" as 1
		if val == "an" || val == "a" {
			return 1 * mult, nil
		}
		return 0, nil
	}
	return time.Duration(n) * mult, nil
}

func (d Duration) Human() string {
	td := time.Duration(d)
	if td < time.Minute {
		return fmt.Sprintf("%ds", int(td.Seconds()))
	}
	if td < time.Hour {
		return fmt.Sprintf("%dm", int(td.Minutes()))
	}
	if td < 24*time.Hour {
		return fmt.Sprintf("%dh", int(td.Hours()))
	}
	if td < 7*24*time.Hour {
		return fmt.Sprintf("%dd", int(td.Hours()/24))
	}
	return fmt.Sprintf("%dw", int(td.Hours()/(7*24)))
}

// Size is a human-readable byte size.
type Size int64

func (s Size) Human() string {
	b := int64(s)
	if b == 0 {
		return "0B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	val := float64(b)
	i := 0
	for val >= 1024 && i < len(units)-1 {
		val /= 1024
		i++
	}
	if val == float64(int64(val)) {
		return fmt.Sprintf("%.0f%s", val, units[i])
	}
	return fmt.Sprintf("%.1f%s", val, units[i])
}

// Container represents a normalized docker container.
type Container struct {
	ID         string
	ShortID    string
	Name       string
	Image      string // full ref: "nginx", "alpine:3.19"
	ImageTag   string // tag part only (kept for compatibility)
	ImageID    string
	Command    string
	Created    Duration
	Status     ContainerStatus
	Ports      []Port
	SizeRw     Size
	SizeRootFs Size
}

type ContainerStatus struct {
	State        string // running, exited, created, restarting, paused, dead
	ExitCode     int
	StartedAt    time.Time
	FinishedAt   time.Time
	RunningFor   Duration
	Health       string // none, healthy, unhealthy
	RestartCount int
	OOMKilled    bool
	Dead         bool
	Paused       bool
	Restarting   bool
}

type Port struct {
	// IP is the host address, brackets stripped: "", "0.0.0.0", "::",
	// "127.0.0.1". Private and Public are strings because a published range
	// ("8000-8002") is not a number.
	IP        string
	Private   string
	Public    string
	Type      string
	Published bool // true if host port is mapped
}

// wildcardIPs all mean "every interface" and none of them is worth spelling
// out: docker prints the same binding once on 0.0.0.0 and once on ::.
func (p Port) wildcard() bool {
	return p.IP == "" || p.IP == "0.0.0.0" || p.IP == "::"
}

func (p Port) String() string {
	if p.Published {
		if p.wildcard() {
			return fmt.Sprintf("%s->%s/%s", p.Public, p.Private, p.Type)
		}
		// An address with colons in it is IPv6 and needs brackets to stay
		// readable: [::1]:8081->80/tcp, never ::1:8081->80/tcp.
		if strings.Contains(p.IP, ":") {
			return fmt.Sprintf("[%s]:%s->%s/%s", p.IP, p.Public, p.Private, p.Type)
		}
		return fmt.Sprintf("%s:%s->%s/%s", p.IP, p.Public, p.Private, p.Type)
	}
	return fmt.Sprintf("%s/%s", p.Private, p.Type)
}

// InspectView is the semantic model for rendering docker inspect.
type InspectView struct {
	Identity  IdentitySection
	Command   string
	State     StateSection
	Network   NetworkSection
	Ports     PortsSection
	Mounts    []MountSection
	Env       EnvSection
	Resources ResourcesSection
	Restart   RestartSection
	Health    HealthSection
	Security  SecuritySection
}

type IdentitySection struct {
	Name     string
	Image    string // full image ref: "nginx", "alpine:3.19"
	ImageTag string // just the tag or repo for ps table
	ShortID  string
}

type StateSection struct {
	Status       string
	RunningFor   string
	RestartCount int
	OOMKilled    bool
	ExitCode     int
	Pid          int
	Paused       bool
	Restarting   bool
	Dead         bool
}

type NetworkSection struct {
	NetworkName string
	IP          string
	Gateway     string
	Driver      string
}

type PortsSection struct {
	Published []string
	Exposed   []string
}

type MountSection struct {
	Type        string // bind, volume, tmpfs
	Source      string
	Destination string
	Mode        string
	RW          bool
	Propagation string
	Missing     bool // source path doesn't exist
}

type EnvSection struct {
	Count       int
	Vars        map[string]string
	Secrets     map[string]bool // masked
	ShowSecrets bool
}

type ResourcesSection struct {
	MemoryLimit      Size
	CPUQuota         int64
	CPUPeriod        int64
	ShmSize          Size
	PidsLimit        int64
	CPUShares        int64
	CPUS             float64
	MemorySwap       Size
	MemorySwappiness int64
}

type RestartSection struct {
	Name              string
	MaximumRetryCount int
}

type HealthSection struct {
	Status        string // none, healthy, unhealthy, starting
	FailingStreak int
}

type SecuritySection struct {
	Privileged     bool
	ReadonlyRootfs bool
	User           string
	CapAdd         []string
	CapDrop        []string
	SecurityOpt    []string
}
