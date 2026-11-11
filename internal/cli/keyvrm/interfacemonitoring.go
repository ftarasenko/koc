package keyvrm

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/spf13/cobra"

	"github.com/ftarasenko/koc/internal/auth"
	"github.com/ftarasenko/koc/internal/output"
	keyvrmapi "github.com/ftarasenko/koc/pkg/keyvrm"
)

// host-aggregate-config interface-monitoring show|set <ha-id>
func newInterfaceMonitoringCommand(a *auth.Options, o *output.Options) *cobra.Command {
	cmd := &cobra.Command{Use: "interface-monitoring", Short: "Bond and HBA monitoring policy of a host aggregate"}
	cmd.AddCommand(newInterfaceMonitoringShow(a, o), newInterfaceMonitoringSet(a, o))
	return cmd
}

func newInterfaceMonitoringShow(a *auth.Options, o *output.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "show <ha-id>",
		Short: "Show the bond and HBA monitoring policy of a host aggregate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(); err != nil {
				return err
			}
			sc, err := newKeyVRMClient(cmd.Context(), a)
			if err != nil {
				return err
			}
			return runInterfaceMonitoringShow(cmd.Context(), sc, o, args[0], cmd.OutOrStdout())
		},
	}
}

func runInterfaceMonitoringShow(ctx context.Context, sc *gophercloud.ServiceClient, o *output.Options, id string, w io.Writer) error {
	m, err := keyvrmapi.GetInterfaceMonitoring(ctx, sc, id)
	if err != nil {
		return err
	}
	fields, values := interfaceMonitoringView(m)
	return o.WriteSingle(w, fields, values)
}

// interfaceMonitoringEdit is the changes one "set" asks for. KeyVRM replaces
// the whole policy on a PUT, so set reads it first and writes it back edited.
type interfaceMonitoringEdit struct {
	bondEnabled, hbaEnabled *bool
	bondDegraded, bondDown  *string
	hbaDown, hbaAllDown     *string
	bondOverrides           []string // NAME:KEY=VALUE[,KEY=VALUE...]
	hbaOverrides            []string
	removeBond, removeHBA   []string
}

func (e interfaceMonitoringEdit) empty() bool {
	return e.bondEnabled == nil && e.hbaEnabled == nil && e.bondDegraded == nil && e.bondDown == nil &&
		e.hbaDown == nil && e.hbaAllDown == nil && len(e.bondOverrides) == 0 && len(e.hbaOverrides) == 0 &&
		len(e.removeBond) == 0 && len(e.removeHBA) == 0
}

// overrideEdit is one parsed --bond-override / --hba-override value.
type overrideEdit struct {
	name     string
	enabled  *bool
	degraded *keyvrmapi.InterfaceReaction
	down     *keyvrmapi.InterfaceReaction
}

// parseOverride reads NAME:KEY=VALUE[,KEY=VALUE...]; keys are enabled,
// down-reaction and, for bonds, degraded-reaction. NAME alone makes an empty override.
func parseOverride(s string, bond bool) (overrideEdit, error) {
	name, rest, _ := strings.Cut(s, ":")
	ed := overrideEdit{name: strings.TrimSpace(name)}
	if ed.name == "" {
		return ed, fmt.Errorf("override %q: interface name is empty", s)
	}
	for _, kv := range strings.Split(rest, ",") {
		if kv == "" {
			continue
		}
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			return ed, fmt.Errorf("override %q: %q is not KEY=VALUE", s, kv)
		}
		switch {
		case key == "enabled":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return ed, fmt.Errorf("override %q: enabled must be true or false", s)
			}
			ed.enabled = &b
		case key == "down-reaction":
			r := keyvrmapi.InterfaceReaction(val)
			ed.down = &r
		case key == "degraded-reaction" && bond:
			r := keyvrmapi.InterfaceReaction(val)
			ed.degraded = &r
		default:
			return ed, fmt.Errorf("override %q: unknown key %q", s, key)
		}
	}
	return ed, nil
}

func reactionOf(flag string, v *string) (*keyvrmapi.InterfaceReaction, error) {
	if v == nil {
		return nil, nil
	}
	if *v == "" {
		return nil, fmt.Errorf("--%s must not be empty", flag)
	}
	r := keyvrmapi.InterfaceReaction(*v)
	return &r, nil
}

// apply edits m, which is the caller's own copy. Reaction values are checked
// by the request layer when the policy is sent.
func (e interfaceMonitoringEdit) apply(m *InterfaceMonitoring) error {
	if err := e.applyDefaults(m); err != nil {
		return err
	}
	if err := e.applyBondOverrides(m); err != nil {
		return err
	}
	if err := e.applyHBAOverrides(m); err != nil {
		return err
	}
	return e.applyRemovals(m)
}

func (e interfaceMonitoringEdit) applyDefaults(m *InterfaceMonitoring) error {
	if e.bondEnabled != nil {
		m.Bond.DefaultPolicy.Enabled = e.bondEnabled
	}
	if e.hbaEnabled != nil {
		m.HBA.DefaultPolicy.Enabled = e.hbaEnabled
	}
	for _, d := range []struct {
		flag string
		from *string
		set  func(keyvrmapi.InterfaceReaction)
	}{
		{"bond-degraded-reaction", e.bondDegraded, func(r keyvrmapi.InterfaceReaction) { m.Bond.DefaultPolicy.DegradedReaction = r }},
		{"bond-down-reaction", e.bondDown, func(r keyvrmapi.InterfaceReaction) { m.Bond.DefaultPolicy.DownReaction = r }},
		{"hba-down-reaction", e.hbaDown, func(r keyvrmapi.InterfaceReaction) { m.HBA.DefaultPolicy.DownReaction = r }},
		{"hba-all-down-reaction", e.hbaAllDown, func(r keyvrmapi.InterfaceReaction) { m.HBA.AllDownReaction = r }},
	} {
		r, err := reactionOf(d.flag, d.from)
		if err != nil {
			return err
		}
		if r != nil {
			d.set(*r)
		}
	}
	return nil
}

func (e interfaceMonitoringEdit) applyBondOverrides(m *InterfaceMonitoring) error {
	for _, s := range e.bondOverrides {
		ed, err := parseOverride(s, true)
		if err != nil {
			return err
		}
		if m.Bond.Interfaces == nil {
			m.Bond.Interfaces = map[string]keyvrmapi.BondPolicyOverride{}
		}
		p := m.Bond.Interfaces[ed.name]
		if ed.enabled != nil {
			p.Enabled = ed.enabled
		}
		if ed.degraded != nil {
			p.DegradedReaction = ed.degraded
		}
		if ed.down != nil {
			p.DownReaction = ed.down
		}
		m.Bond.Interfaces[ed.name] = p
	}
	return nil
}

func (e interfaceMonitoringEdit) applyHBAOverrides(m *InterfaceMonitoring) error {
	for _, s := range e.hbaOverrides {
		ed, err := parseOverride(s, false)
		if err != nil {
			return err
		}
		if m.HBA.Interfaces == nil {
			m.HBA.Interfaces = map[string]keyvrmapi.HBAPolicyOverride{}
		}
		p := m.HBA.Interfaces[ed.name]
		if ed.enabled != nil {
			p.Enabled = ed.enabled
		}
		if ed.down != nil {
			p.DownReaction = ed.down
		}
		m.HBA.Interfaces[ed.name] = p
	}
	return nil
}

func (e interfaceMonitoringEdit) applyRemovals(m *InterfaceMonitoring) error {
	for _, name := range e.removeBond {
		if _, ok := m.Bond.Interfaces[name]; !ok {
			return fmt.Errorf("no bond override for %q", name)
		}
		delete(m.Bond.Interfaces, name)
	}
	for _, name := range e.removeHBA {
		if _, ok := m.HBA.Interfaces[name]; !ok {
			return fmt.Errorf("no HBA override for %q", name)
		}
		delete(m.HBA.Interfaces, name)
	}
	return nil
}

func newInterfaceMonitoringSet(a *auth.Options, o *output.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <ha-id>",
		Short: "Change the bond and HBA monitoring policy of a host aggregate",
		Long: "Change the bond and HBA monitoring policy of a host aggregate. KeyVRM replaces the whole policy\n" +
			"on every write, so set reads the current policy, applies the given flags and writes it back:\n" +
			"whatever you do not name stays as it is.\n\n" +
			"Reactions are send_alert, fence_and_evacuate and disable_hypervisor. An override is\n" +
			"NAME:KEY=VALUE[,KEY=VALUE...] with the keys enabled, down-reaction and (bonds only)\n" +
			"degraded-reaction; it changes only the keys given, and a bare NAME adds an empty override.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(); err != nil {
				return err
			}
			edit, err := interfaceMonitoringEditOf(cmd)
			if err != nil {
				return err
			}
			if edit.empty() {
				return errNoUpdateFields
			}
			sc, err := newKeyVRMClient(cmd.Context(), a)
			if err != nil {
				return err
			}
			return runInterfaceMonitoringSet(cmd.Context(), sc, o, args[0], edit, cmd.OutOrStdout())
		},
	}
	fs := cmd.Flags()
	fs.Bool("bond-enabled", false, "enable/disable bond monitoring by default")
	fs.String("bond-degraded-reaction", "", "default reaction to a degraded bond")
	fs.String("bond-down-reaction", "", "default reaction to a down bond")
	fs.Bool("hba-enabled", false, "enable/disable HBA monitoring by default")
	fs.String("hba-down-reaction", "", "default reaction to a down HBA")
	fs.String("hba-all-down-reaction", "", "reaction when every HBA of a host is down")
	fs.StringArray("bond-override", nil, "per-bond override NAME:KEY=VALUE,... (repeatable)")
	fs.StringArray("hba-override", nil, "per-HBA override NAME:KEY=VALUE,... (repeatable)")
	fs.StringArray("remove-bond-override", nil, "remove the override of a bond (repeatable)")
	fs.StringArray("remove-hba-override", nil, "remove the override of an HBA (repeatable)")
	return cmd
}

// interfaceMonitoringEditOf collects the flags that were given.
func interfaceMonitoringEditOf(cmd *cobra.Command) (interfaceMonitoringEdit, error) {
	fs := cmd.Flags()
	var e interfaceMonitoringEdit
	for _, b := range []struct {
		flag string
		to   **bool
	}{{"bond-enabled", &e.bondEnabled}, {"hba-enabled", &e.hbaEnabled}} {
		if fs.Changed(b.flag) {
			v, err := fs.GetBool(b.flag)
			if err != nil {
				return e, err
			}
			*b.to = &v
		}
	}
	for _, s := range []struct {
		flag string
		to   **string
	}{
		{"bond-degraded-reaction", &e.bondDegraded}, {"bond-down-reaction", &e.bondDown},
		{"hba-down-reaction", &e.hbaDown}, {"hba-all-down-reaction", &e.hbaAllDown},
	} {
		if fs.Changed(s.flag) {
			v, err := fs.GetString(s.flag)
			if err != nil {
				return e, err
			}
			*s.to = &v
		}
	}
	var err error
	for _, l := range []struct {
		flag string
		to   *[]string
	}{
		{"bond-override", &e.bondOverrides}, {"hba-override", &e.hbaOverrides},
		{"remove-bond-override", &e.removeBond}, {"remove-hba-override", &e.removeHBA},
	} {
		if *l.to, err = fs.GetStringArray(l.flag); err != nil {
			return e, err
		}
	}
	return e, nil
}

// runInterfaceMonitoringSet reads the policy, applies edit and writes it back. -c is
// checked first: once KeyVRM has the PUT, a failed render reads as a failed write.
func runInterfaceMonitoringSet(ctx context.Context, sc *gophercloud.ServiceClient, o *output.Options, id string, edit interfaceMonitoringEdit, w io.Writer) error {
	if err := o.CheckColumns(interfaceMonitoringColumns...); err != nil {
		return err
	}
	m, err := keyvrmapi.GetInterfaceMonitoring(ctx, sc, id)
	if err != nil {
		return err
	}
	if err := edit.apply(m); err != nil {
		return err
	}
	m, err = keyvrmapi.UpdateInterfaceMonitoring(ctx, sc, id, *m)
	if err != nil {
		return err
	}
	fields, values := interfaceMonitoringView(m)
	return o.WriteSingle(w, fields, values)
}
