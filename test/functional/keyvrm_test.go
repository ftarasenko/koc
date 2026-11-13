//go:build functional

package functional

import (
	"testing"
)

// The KeyVRM suite, for a devstack stacked with a "keyvrm" feature: the
// monitoring overview and the interface-monitoring policy, edited and put back.

func TestKeyVRMMonitoring(t *testing.T) {
	c := ft(t)
	requireFeature(t, "keyvrm")
	r := defaultRunner(c)

	o := r.show(t, "keyvrm", "monitoring", "show")
	for _, k := range []string{"enabled", "zones", "aggregates", "processes", "problem_aggregates", "noop_aggregates"} {
		if _, ok := o[k]; !ok {
			t.Errorf("monitoring show has no %q: %v", k, o)
		}
	}
}

func TestKeyVRMInterfaceMonitoring(t *testing.T) {
	c := ft(t)
	requireFeature(t, "keyvrm")
	r := defaultRunner(c)

	rows := r.list(t, "keyvrm", "host-aggregate-config", "list")
	if len(rows) == 0 {
		t.Skip("KeyVRM has no host-aggregate configuration to edit")
	}
	id := field(rows[0], "ID")
	before := r.show(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "show", id)

	// A policy edit is a read-modify-write: the override lands and the rest stays.
	name := uniq("bond")
	t.Cleanup(func() {
		r.run(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "set", id, "--remove-bond-override", name)
	})
	set := r.show(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "set", id,
		"--bond-override", name+":enabled=false,down-reaction=send_alert")
	if got := field(set, "bond_down_reaction"); got != field(before, "bond_down_reaction") {
		t.Errorf("set changed the default bond down reaction: %q -> %q", field(before, "bond_down_reaction"), got)
	}
	shown := r.show(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "show", id)
	if bonds, _ := shown["bond_interfaces"].(map[string]any); bonds[name] == nil {
		t.Errorf("bond override %q missing after set: %v", name, shown["bond_interfaces"])
	}

	r.ok(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "set", id, "--remove-bond-override", name)
	after := r.show(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "show", id)
	if bonds, _ := after["bond_interfaces"].(map[string]any); bonds[name] != nil {
		t.Errorf("bond override %q still there after removal: %v", name, after["bond_interfaces"])
	}
	r.fails(t, "keyvrm", "host-aggregate-config", "interface-monitoring", "set", id, "--bond-down-reaction", "explode")
}
