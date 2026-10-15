package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ImageEnvVar is one env entry left after the image's own and the base
// image's variables have been removed, with the display value already
// masked when it carries a secret.
type ImageEnvVar struct {
	Key   string
	Value string
	Dim   bool // masked: show the mask dimmed
}

// ImageView is the semantic model for one inspected image.
type ImageView struct {
	Tags      []string
	ShortID   string
	HasSize   bool
	SizeBytes float64
	Created   string // "3w ago", already humanized
	Arch      string
	OS        string
	Layers    int

	Entrypoint string
	Cmd        string
	User       string
	UserRoot   bool
	Workdir    string
	Ports      []string
	Volumes    []string

	EnvCount    int
	EnvVars     []ImageEnvVar
	EnvWarn     bool // secrets are baked into the image
	Health      string
	LabelsCount int
	Labels      []string // first few, "k=v"
}

// imageObject reports whether one inspect object is an image: Id and RootFS
// present, State absent (a container's inspect carries State). A network
// carries neither and stays with its own formatter.
func imageObject(first map[string]interface{}) bool {
	if !hasAllKeys(first, "Id", "RootFS") {
		return false
	}
	_, isContainer := first["State"]
	return !isContainer
}

// imageObjectBlock renders one image object. A render that says less than
// raw docker (fewer than two useful lines) hands back the object's own raw
// JSON instead: a formatter may never make output thinner than the pipe had.
func imageObjectBlock(first map[string]interface{}, show bool) (string, bool) {
	out := RenderImages(NormalizeImages([]map[string]interface{}{first}, show))
	if usefulOutputLines(out) < 2 {
		return rawJSONObject(first), false
	}
	return out, true
}

// NormalizeImages converts raw image inspect JSON to ImageView models. The
// image-env memo is reset per run so one inspect cannot answer the next.
func NormalizeImages(raw []map[string]interface{}, show bool) []ImageView {
	if len(raw) == 0 {
		return nil
	}
	imageEnvMemo = make(map[string]imageEnvResult)
	views := make([]ImageView, len(raw))
	for i, r := range raw {
		views[i] = normalizeImage(r, show)
	}
	return views
}

func normalizeImage(raw map[string]interface{}, show bool) ImageView {
	v := ImageView{
		// Image ids carry a "sha256:" prefix docker never shows in
		// its short form; drop it before cutting the id short.
		ShortID: shortID(strings.TrimPrefix(getString(raw, "Id"), "sha256:")),
	}
	if tags, ok := raw["RepoTags"].([]interface{}); ok {
		for _, t := range tags {
			if s, ok := t.(string); ok && s != "" {
				v.Tags = append(v.Tags, s)
			}
		}
	}
	if f, ok := raw["Size"].(float64); ok && f > 0 {
		v.HasSize = true
		v.SizeBytes = f
	}
	v.Created = imageAge(getString(raw, "Created"))
	v.Arch = getString(raw, "Architecture")
	v.OS = getString(raw, "Os")
	if rootFS, ok := raw["RootFS"].(map[string]interface{}); ok {
		if layers, ok := rootFS["Layers"].([]interface{}); ok {
			v.Layers = len(layers)
		}
	}

	config, _ := raw["Config"].(map[string]interface{})
	if config != nil {
		v.Entrypoint = strings.Join(imageStrSlice(config["Entrypoint"]), " ")
		v.Cmd = strings.Join(imageStrSlice(config["Cmd"]), " ")
		v.User = getString(config, "User")
		v.UserRoot = v.User == "" || v.User == "root" || v.User == "0"
		v.Workdir = getString(config, "WorkingDir")
		v.Ports = imageMapKeys(config["ExposedPorts"])
		v.Volumes = imageMapKeys(config["Volumes"])
		v.Health = imageHealth(config)
		v.LabelsCount, v.Labels = imageLabels(config)
	}
	v.EnvCount, v.EnvVars, v.EnvWarn = normalizeImageEnv(raw, show)
	return v
}

// imageAge humanizes an RFC3339 timestamp as docker's own ages read —
// "3w ago". Anything unparseable or in the future is hidden.
func imageAge(created string) string {
	if created == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		return ""
	}
	return Duration(d).Human() + " ago"
}

func imageStrSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// imageMapKeys returns the sorted keys of an object-valued map such as
// ExposedPorts, Volumes or Labels.
func imageMapKeys(v interface{}) []string {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// imageHealth answers "<cmd> (every <interval>)" when the image defines a
// healthcheck; a missing one — or docker's explicit NONE — says nothing at
// all. The label column is added by the renderer, like every other row.
func imageHealth(config map[string]interface{}) string {
	hc, ok := config["Healthcheck"].(map[string]interface{})
	if !ok {
		return ""
	}
	test := imageStrSlice(hc["Test"])
	if len(test) == 0 || test[0] == "NONE" {
		return ""
	}
	var cmd string
	if len(test) > 1 {
		cmd = strings.Join(test[1:], " ")
	}
	if cmd == "" {
		return ""
	}
	if iv, ok := hc["Interval"].(float64); ok && iv > 0 {
		return fmt.Sprintf("%s (every %s)", cmd, Duration(int64(iv)).Human())
	}
	return cmd
}

// imageLabels counts every label and keeps the first few for the line.
func imageLabels(config map[string]interface{}) (int, []string) {
	m, ok := config["Labels"].(map[string]interface{})
	if !ok || len(m) == 0 {
		return 0, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	const firstFew = 3
	var shown []string
	for i, k := range keys {
		if i == firstFew {
			break
		}
		val, _ := m[k].(string)
		shown = append(shown, k+"="+val)
	}
	return len(m), shown
}

// normalizeImageEnv keeps only the variables the user's own Dockerfile
// added. When the image records its parent, the parent's env is the
// baseline; otherwise the known base-image defaults quiet PATH and friends.
// Secrets are masked by name and by URL credentials unless --show-secrets.
func normalizeImageEnv(raw map[string]interface{}, show bool) (int, []ImageEnvVar, bool) {
	config, _ := raw["Config"].(map[string]interface{})
	if config == nil {
		return 0, nil, false
	}
	var pairs map[string]bool
	if parent := getString(raw, "Parent"); parent != "" {
		pairs = imageEnv(parent)
	}
	type kv struct{ k, v string }
	var kept []kv
	for _, e := range imageStrSlice(config["Env"]) {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if pairs != nil {
			if pairs[e] {
				continue
			}
		} else if imageDefaultEnv[parts[0]] {
			continue
		}
		kept = append(kept, kv{parts[0], parts[1]})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].k < kept[j].k })

	vars := make([]ImageEnvVar, 0, len(kept))
	warn := false
	for _, e := range kept {
		out := ImageEnvVar{Key: e.k, Value: e.v}
		if !show {
			switch {
			case imageSecretName(e.k):
				out.Value, out.Dim, warn = "********", true, true
			default:
				if mv, ok := maskURLPassword(e.v); ok {
					out.Value, out.Dim, warn = mv, true, true
				}
			}
		}
		vars = append(vars, out)
	}
	return len(kept), vars, warn
}

// imageSecretName reports whether an env var's name marks its value as a
// secret: PASSWORD, SECRET, TOKEN or KEY anywhere in the name.
func imageSecretName(k string) bool {
	upper := strings.ToUpper(k)
	for _, p := range []string{"PASSWORD", "SECRET", "TOKEN", "KEY"} {
		if strings.Contains(upper, p) {
			return true
		}
	}
	return false
}

// RenderImages renders the inspected images, one block per image, blank
// line between.
func RenderImages(views []ImageView) string {
	if len(views) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, v := range views {
		if i > 0 {
			buf.WriteByte('\n')
			buf.WriteByte('\n')
		}
		buf.WriteString(renderImageView(v))
	}
	return buf.String()
}

func renderImageView(v ImageView) string {
	var buf bytes.Buffer

	// Header: every tag, then the short id.
	if len(v.Tags) > 0 {
		buf.WriteString(paintBold(strings.Join(v.Tags, ", ")))
	} else {
		buf.WriteString(paintDim("<none>"))
	}
	buf.WriteString("  ")
	buf.WriteString(paintDim(v.ShortID))
	buf.WriteByte('\n')

	// Size · created · os/arch: what the image is on disk and where it runs.
	var meta []string
	if v.HasSize {
		meta = append(meta, formatBytes(v.SizeBytes))
	}
	if v.Created != "" {
		meta = append(meta, "created "+v.Created)
	}
	switch {
	case v.OS != "" && v.Arch != "":
		meta = append(meta, v.OS+"/"+v.Arch)
	case v.Arch != "":
		meta = append(meta, v.Arch)
	case v.OS != "":
		meta = append(meta, v.OS)
	}
	if len(meta) > 0 {
		buf.WriteString("size      " + strings.Join(meta, " · "))
		buf.WriteByte('\n')
	}

	if v.Layers > 0 {
		buf.WriteString(fmt.Sprintf("layers    %d\n", v.Layers))
	}
	if v.Entrypoint != "" {
		buf.WriteString("entrypoint " + paintDim(v.Entrypoint))
		buf.WriteByte('\n')
	}
	if v.Cmd != "" {
		buf.WriteString("cmd       " + paintDim(v.Cmd))
		buf.WriteByte('\n')
	}

	// User is always answered when the image has a config: an empty user
	// means the image runs as root. Root itself is the docker default on
	// every public image, so it is dimmed — red is saved for the real
	// problems below, like secrets baked into the image.
	if v.User != "" || v.UserRoot {
		if v.UserRoot {
			buf.WriteString("user      " + paintDim("root"))
		} else {
			buf.WriteString("user      " + v.User)
		}
		buf.WriteByte('\n')
	}

	if v.Workdir != "" && v.Workdir != "/" {
		buf.WriteString("workdir   " + v.Workdir)
		buf.WriteByte('\n')
	}
	if len(v.Ports) > 0 {
		buf.WriteString("ports     " + strings.Join(v.Ports, ", "))
		buf.WriteByte('\n')
	}
	if len(v.Volumes) > 0 {
		buf.WriteString("volumes   " + strings.Join(v.Volumes, ", "))
		buf.WriteByte('\n')
	}

	if v.EnvCount > 0 {
		unit := "vars"
		if v.EnvCount == 1 {
			unit = "var"
		}
		buf.WriteString(fmt.Sprintf("env       %d %s\n", v.EnvCount, unit))
		for _, e := range v.EnvVars {
			value := e.Value
			if e.Dim {
				value = paintDim(value)
			}
			buf.WriteString("env       " + e.Key + "=" + value)
			buf.WriteByte('\n')
		}
		if v.EnvWarn {
			buf.WriteString("env       " + paintRed("(secrets baked into the image, use --show-secrets)"))
			buf.WriteByte('\n')
		}
	}

	if v.Health != "" {
		buf.WriteString("health    " + v.Health)
		buf.WriteByte('\n')
	}

	if v.LabelsCount > 0 {
		unit := "labels"
		if v.LabelsCount == 1 {
			unit = "label"
		}
		line := fmt.Sprintf("labels    %d %s", v.LabelsCount, unit)
		if len(v.Labels) > 0 {
			line += " · " + strings.Join(v.Labels, " · ")
			if v.LabelsCount > len(v.Labels) {
				line += " " + paintDim("…")
			}
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	return buf.String()
}

// rawJSONObject re-encodes one inspect object as docker's own pretty JSON:
// an object the renderer does not understand is printed raw, never dropped.
func rawJSONObject(obj map[string]interface{}) string {
	b, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		return ""
	}
	return string(b)
}
