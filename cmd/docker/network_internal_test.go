package docker

import (
	"strings"
	"testing"
)

// formatNetwork runs the inspect formatter over text.
func formatNetwork(t *testing.T, text string) string {
	t.Helper()
	var sb strings.Builder
	Format(&sb, "inspect", text)
	return sb.String()
}

// netFixture is a network with two attached containers, deliberately
// unsorted (zeta before alpha) and with every optional flag off.
const netFixture = `[` +
	`{"Name":"appnet","Id":"abc123netid","Scope":"local","Driver":"bridge",` +
	`"EnableIPv6":false,"Internal":false,"Attachable":false,` +
	`"IPAM":{"Driver":"default","Config":[{"Subnet":"172.20.0.0/16","Gateway":"172.20.0.1"}]},` +
	`"Containers":{` +
	`"c1":{"Name":"zeta","MacAddress":"aa:bb:cc:dd:ee:01","IPv4Address":"172.20.0.10/16","IPv6Address":""},` +
	`"c2":{"Name":"alpha","MacAddress":"aa:bb:cc:dd:ee:02","IPv4Address":"172.20.0.11/16","IPv6Address":""}` +
	`}}` +
	`]`

// The header carries name, driver, scope, subnet and gateway; the table
// carries the attached containers sorted by name; the network id and the
// unset flags never appear.
func TestNetworkInspectHeaderAndTable(t *testing.T) {
	out := stripTags(formatNetwork(t, netFixture))
	if !strings.Contains(out, "appnet  bridge, local") {
		t.Errorf("header line missing name/driver/scope:\n%s", out)
	}
	if !strings.Contains(out, "subnet   172.20.0.0/16") {
		t.Errorf("subnet line missing:\n%s", out)
	}
	if !strings.Contains(out, "gateway  172.20.0.1") {
		t.Errorf("gateway line missing:\n%s", out)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "MAC") {
		t.Errorf("attached-containers table missing:\n%s", out)
	}
	if strings.Index(out, "alpha") > strings.Index(out, "zeta") {
		t.Errorf("containers are not sorted by name:\n%s", out)
	}
	for _, hidden := range []string{"internal", "attachable", "ipv6", "abc123netid", "EndpointID"} {
		if strings.Contains(out, hidden) {
			t.Errorf("unset/irrelevant field %q leaked:\n%s", hidden, out)
		}
	}
}

// A network with nothing attached prints the one dim line, and a null IPAM
// config prints no subnet or gateway — host is the live case.
func TestNetworkInspectNoContainersAttached(t *testing.T) {
	fix := `[{"Name":"host","Id":"df15","Scope":"local","Driver":"host",` +
		`"EnableIPv6":false,"Internal":false,"Attachable":false,` +
		`"IPAM":{"Driver":"default","Options":null,"Config":null},` +
		`"Containers":{}}]`
	out := stripTags(formatNetwork(t, fix))
	if !strings.Contains(out, "host  host, local") {
		t.Errorf("header missing:\n%s", out)
	}
	if !strings.Contains(out, "no containers attached") {
		t.Errorf("empty network did not get the dim line:\n%s", out)
	}
	for _, hidden := range []string{"subnet", "gateway", "NAME"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q should be hidden when unset:\n%s", hidden, out)
		}
	}
}

// A custom subnet plus an IPv6 pool print together, and ipv6 joins the
// header only because EnableIPv6 is set.
func TestNetworkInspectCustomSubnetAndIPv6(t *testing.T) {
	fix := `[{"Name":"v6net","Id":"v6id","Scope":"local","Driver":"bridge",` +
		`"EnableIPv6":true,"Internal":false,"Attachable":false,` +
		`"IPAM":{"Driver":"default","Config":[` +
		`{"Subnet":"172.30.0.0/16","Gateway":"172.30.0.1"},` +
		`{"Subnet":"fd00:dead::/64","Gateway":"fe80::1"}]},` +
		`"Containers":{"c1":{"Name":"six","MacAddress":"11:22:33:44:55:66",` +
		`"IPv4Address":"172.30.0.5/16","IPv6Address":"fd00:dead::5/64"}}}]`
	out := stripTags(formatNetwork(t, fix))
	if !strings.Contains(out, "v6net  bridge, local, ipv6") {
		t.Errorf("ipv6 flag not in header:\n%s", out)
	}
	if !strings.Contains(out, "subnet   172.30.0.0/16, fd00:dead::/64") {
		t.Errorf("both subnets not listed:\n%s", out)
	}
	if !strings.Contains(out, "gateway  172.30.0.1, fe80::1") {
		t.Errorf("both gateways not listed:\n%s", out)
	}
}

// Two networks in one call render as two blocks, each complete.
func TestNetworkInspectMultipleNetworks(t *testing.T) {
	multi := `[` +
		`{"Name":"alpha-net","Id":"a","Scope":"local","Driver":"bridge",` +
		`"IPAM":{"Config":[{"Subnet":"172.31.0.0/16"}]},` +
		`"Containers":{"c1":{"Name":"one","MacAddress":"aa","IPv4Address":"172.31.0.2/16"}}},` +
		`{"Name":"host","Id":"h","Scope":"local","Driver":"host",` +
		`"IPAM":{"Config":null},"Containers":{}}` +
		`]`
	out := stripTags(formatNetwork(t, multi))
	if !strings.Contains(out, "alpha-net  bridge, local") {
		t.Errorf("first network missing:\n%s", out)
	}
	if !strings.Contains(out, "host  host, local") {
		t.Errorf("second network missing:\n%s", out)
	}
	if !strings.Contains(out, "no containers attached") {
		t.Errorf("second network's empty state missing:\n%s", out)
	}
	if strings.Index(out, "alpha-net") > strings.Index(out, "host") {
		t.Errorf("networks out of order:\n%s", out)
	}
}

// Safety: network-shaped JSON that would render thinner than two lines
// falls back to the original input, and an empty array passes through.
func TestNetworkInspectFallbackKeepsRaw(t *testing.T) {
	thin := `[{"Name":"","Driver":"weird","IPAM":{"Config":null}}]`
	if got := formatNetwork(t, thin); got != thin {
		t.Errorf("thin render did not fall back to raw input:\ngot  %q\nwant %q", got, thin)
	}
	if got := formatNetwork(t, "[]"); got != "[]" {
		t.Errorf("empty array did not pass through:\ngot  %q", got)
	}
}
