package docker

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// volumeObject reports whether an inspect object is a volume. Name,
// Mountpoint and Scope together never appear on a container, an image or
// a network, and the caller has already tried those shapes.
func volumeObject(obj map[string]interface{}) bool {
	return hasAllKeys(obj, "Name", "Mountpoint", "Scope")
}

// VolumeView is one volume from `docker volume inspect`.
type VolumeView struct {
	Name       string
	Driver     string
	Scope      string
	Mountpoint string
	Created    string
	Labels     []string
	Options    []string
}

// volumePairs flattens a label/options map into sorted `key=value` pairs;
// a null or missing map yields nothing.
func volumePairs(v interface{}) []string {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return out
}

// NormalizeVolume reads one inspect object into the view.
func NormalizeVolume(obj map[string]interface{}) VolumeView {
	return VolumeView{
		Name:       netStr(obj, "Name"),
		Driver:     netStr(obj, "Driver"),
		Scope:      netStr(obj, "Scope"),
		Mountpoint: netStr(obj, "Mountpoint"),
		Created:    netStr(obj, "CreatedAt"),
		Labels:     volumePairs(obj["Labels"]),
		Options:    volumePairs(obj["Options"]),
	}
}

// renderVolume draws one volume: bold header carrying name, driver and
// scope, then the rows that exist — empty Labels or Options never print,
// and a render of fewer than two lines falls back to raw JSON.
func renderVolume(v VolumeView) string {
	var buf bytes.Buffer

	buf.WriteString(paintBold("volume " + v.Name))
	var meta []string
	if v.Driver != "" {
		meta = append(meta, v.Driver)
	}
	if v.Scope != "" {
		meta = append(meta, "scope "+v.Scope)
	}
	if len(meta) > 0 {
		buf.WriteString("  ")
		buf.WriteString(paintDim(strings.Join(meta, " · ")))
	}
	buf.WriteByte('\n')

	row := func(label, value string) {
		fmt.Fprintf(&buf, "%-10s %s\n", label, value)
	}
	if v.Mountpoint != "" {
		row("mountpoint", v.Mountpoint)
	}
	if v.Created != "" {
		row("created", v.Created)
	}
	if len(v.Labels) > 0 {
		row("labels", strings.Join(v.Labels, " · "))
	}
	if len(v.Options) > 0 {
		row("options", strings.Join(v.Options, " · "))
	}
	return buf.String()
}

// volumeInspectBlock renders volume-shaped inspect JSON. It reports false
// when the render would say less than the raw object, leaving the caller
// to print docker's own bytes.
func volumeInspectBlock(obj map[string]interface{}) (string, bool) {
	out := renderVolume(NormalizeVolume(obj))
	if usefulOutputLines(out) < 2 {
		return rawJSONObject(obj), false
	}
	return out, true
}
