package docker

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// NetworkView is one element of `docker network inspect` JSON, reduced to
// what a person reads: the header facts and who is attached.
type NetworkView struct {
	Name       string
	Driver     string
	Scope      string
	Internal   bool
	Attachable bool
	EnableIPv6 bool
	Subnets    []string
	Gateways   []string
	Endpoints  []NetworkEndpoint
	// HasContainers marks a present Containers map (even an empty one):
	// only then is "no containers attached" the truth rather than a gap
	// in the JSON.
	HasContainers bool
}

// NetworkEndpoint is one container attached to the network.
type NetworkEndpoint struct {
	Name string
	IPv4 string
	IPv6 string
	MAC  string
}

// NormalizeNetworks reduces network inspect JSON to views, sorting each
// network's attached containers by name so the table is deterministic.
func NormalizeNetworks(raw []map[string]interface{}) []NetworkView {
	views := make([]NetworkView, 0, len(raw))
	for _, n := range raw {
		v := NetworkView{
			Name:       netStr(n, "Name"),
			Driver:     netStr(n, "Driver"),
			Scope:      netStr(n, "Scope"),
			Internal:   netBool(n, "Internal"),
			Attachable: netBool(n, "Attachable"),
			EnableIPv6: netBool(n, "EnableIPv6"),
		}
		if ipam, ok := n["IPAM"].(map[string]interface{}); ok {
			if cfgs, ok := ipam["Config"].([]interface{}); ok {
				for _, c := range cfgs {
					cm, ok := c.(map[string]interface{})
					if !ok {
						continue
					}
					if s := netStr(cm, "Subnet"); s != "" {
						v.Subnets = append(v.Subnets, s)
					}
					if g := netStr(cm, "Gateway"); g != "" {
						v.Gateways = append(v.Gateways, g)
					}
				}
			}
		}
		if ctrs, ok := n["Containers"].(map[string]interface{}); ok {
			v.HasContainers = true
			for _, ce := range ctrs {
				cm, ok := ce.(map[string]interface{})
				if !ok {
					continue
				}
				v.Endpoints = append(v.Endpoints, NetworkEndpoint{
					Name: netStr(cm, "Name"),
					IPv4: netStr(cm, "IPv4Address"),
					IPv6: netStr(cm, "IPv6Address"),
					MAC:  netStr(cm, "MacAddress"),
				})
			}
			sort.SliceStable(v.Endpoints, func(i, j int) bool {
				return v.Endpoints[i].Name < v.Endpoints[j].Name
			})
		}
		views = append(views, v)
	}
	return views
}

// RenderNetworkInspects renders each network as a header of name, driver,
// scope, subnet and gateway (flags only when set) followed by the attached
// containers table. Blocks are blank-line separated.
func RenderNetworkInspects(views []NetworkView) string {
	if len(views) == 0 {
		return ""
	}
	blocks := make([]string, len(views))
	for i, v := range views {
		blocks[i] = renderNetworkView(v)
	}
	return strings.Join(blocks, "\n\n")
}

func renderNetworkView(v NetworkView) string {
	var buf bytes.Buffer
	buf.WriteString(paintBold(v.Name))
	meta := []string{v.Driver, v.Scope}
	if v.Internal {
		meta = append(meta, "internal")
	}
	if v.EnableIPv6 {
		meta = append(meta, "ipv6")
	}
	if v.Attachable {
		meta = append(meta, "attachable")
	}
	buf.WriteString("  ")
	buf.WriteString(paintDim(strings.Join(meta, ", ")))
	buf.WriteByte('\n')
	if len(v.Subnets) > 0 {
		fmt.Fprintf(&buf, "%-8s %s\n", "subnet", paintDim(strings.Join(v.Subnets, ", ")))
	}
	if len(v.Gateways) > 0 {
		fmt.Fprintf(&buf, "%-8s %s\n", "gateway", paintDim(strings.Join(v.Gateways, ", ")))
	}
	if len(v.Endpoints) > 0 {
		buf.WriteByte('\n')
		headers := []string{"NAME", "IPv4", "MAC"}
		rows := make([][]string, len(v.Endpoints))
		for i, ep := range v.Endpoints {
			rows[i] = []string{ep.Name, ep.IPv4, ep.MAC}
		}
		buf.WriteString(renderDFTable(headers, rows, dfAligns(headers, rows)))
	} else if v.HasContainers {
		buf.WriteString(paintDim("no containers attached"))
		buf.WriteByte('\n')
	}
	return buf.String()
}

// networkInspectText renders network-shaped inspect JSON. It reports false
// when the JSON is not a network inspect at all, leaving the caller to the
// container path. When the render would say less than raw docker (fewer
// than two useful lines), the original text comes back untouched: a
// formatter may never make output thinner than the pipe already had.
func networkInspectText(text string, raw []map[string]interface{}) (string, bool) {
	if len(raw) == 0 || !hasAllKeys(raw[0], "Driver", "IPAM") {
		return "", false
	}
	out := RenderNetworkInspects(NormalizeNetworks(raw))
	if usefulOutputLines(out) < 2 {
		return text, true
	}
	return out, true
}

func usefulOutputLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func netStr(m map[string]interface{}, k string) string {
	if s, ok := m[k].(string); ok {
		return s
	}
	return ""
}

func netBool(m map[string]interface{}, k string) bool {
	if b, ok := m[k].(bool); ok {
		return b
	}
	return false
}
