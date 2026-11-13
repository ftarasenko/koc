package keyvrm

import (
	"context"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
)

// The interface-monitoring and overview types and their input checks are
// ported from the internal KeyVRM Go SDK by N. Kushnarenko.

// InterfaceReaction is the action taken when a monitored interface fails.
type InterfaceReaction string

// The reactions KeyVRM accepts.
const (
	ReactionSendAlert         InterfaceReaction = "send_alert"
	ReactionFenceAndEvacuate  InterfaceReaction = "fence_and_evacuate"
	ReactionDisableHypervisor InterfaceReaction = "disable_hypervisor"
)

// BondPolicy is the default policy for every bond of a host aggregate. A nil
// Enabled and an empty reaction are left out of the request and KeyVRM
// applies its defaults (true and send_alert).
type BondPolicy struct {
	Enabled          *bool             `json:"enabled,omitempty"`
	DegradedReaction InterfaceReaction `json:"degraded_reaction,omitempty"`
	DownReaction     InterfaceReaction `json:"down_reaction,omitempty"`
}

// BondPolicyOverride replaces only its non-nil fields of the default bond
// policy for one named bond.
type BondPolicyOverride struct {
	Enabled          *bool              `json:"enabled,omitempty"`
	DegradedReaction *InterfaceReaction `json:"degraded_reaction,omitempty"`
	DownReaction     *InterfaceReaction `json:"down_reaction,omitempty"`
}

// HBAPolicy is the default policy for every HBA of a host aggregate. A nil
// Enabled and an empty reaction are left out of the request and KeyVRM
// applies its defaults (true and send_alert).
type HBAPolicy struct {
	Enabled      *bool             `json:"enabled,omitempty"`
	DownReaction InterfaceReaction `json:"down_reaction,omitempty"`
}

// HBAPolicyOverride replaces only its non-nil fields of the default HBA
// policy for one named HBA.
type HBAPolicyOverride struct {
	Enabled      *bool              `json:"enabled,omitempty"`
	DownReaction *InterfaceReaction `json:"down_reaction,omitempty"`
}

// BondMonitoringPolicy is the bond half of [InterfaceMonitoring].
type BondMonitoringPolicy struct {
	DefaultPolicy BondPolicy                    `json:"default_policy"`
	Interfaces    map[string]BondPolicyOverride `json:"interfaces,omitempty"`
}

// HBAMonitoringPolicy is the HBA half of [InterfaceMonitoring].
// AllDownReaction is the reaction when every HBA of a host is down.
type HBAMonitoringPolicy struct {
	DefaultPolicy   HBAPolicy                    `json:"default_policy"`
	Interfaces      map[string]HBAPolicyOverride `json:"interfaces,omitempty"`
	AllDownReaction InterfaceReaction            `json:"all_down_reaction,omitempty"`
}

// InterfaceMonitoring is the bond and HBA monitoring configuration of one host
// aggregate (GET/PUT /v1/host_aggregates/{id}/interface_monitoring). A PUT
// replaces the whole of it: whatever the request leaves out reverts to the
// server's default, so read, change and write back to keep a setting.
type InterfaceMonitoring struct {
	Bond BondMonitoringPolicy `json:"bond"`
	HBA  HBAMonitoringPolicy  `json:"hba"`
}

// MonitoringOverview is the region's monitoring counters and enabled state
// (GET /v1/monitoring/overview).
type MonitoringOverview struct {
	Enabled           bool `json:"enabled"`
	Zones             int  `json:"zones"`
	Aggregates        int  `json:"aggregates"`
	Processes         int  `json:"processes"`
	ProblemAggregates int  `json:"problem_aggregates"`
	NoopAggregates    int  `json:"noop_aggregates"`
}

// ValidationError is an input KeyVRM would refuse, caught before the request
// is sent.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// GetInterfaceMonitoring reads the bond and HBA monitoring policies of a host
// aggregate.
func GetInterfaceMonitoring(ctx context.Context, sc *gophercloud.ServiceClient, id string) (*InterfaceMonitoring, error) {
	return get[InterfaceMonitoring](ctx, sc, sc.ServiceURL("host_aggregates", id, "interface_monitoring"))
}

// UpdateInterfaceMonitoring replaces all bond and HBA monitoring policies of a
// host aggregate and returns the result. Interface names are trimmed and
// checked for blanks and duplicates, and reactions for the values KeyVRM
// accepts, before anything is sent; policy itself is not modified.
func UpdateInterfaceMonitoring(ctx context.Context, sc *gophercloud.ServiceClient, id string, policy InterfaceMonitoring) (*InterfaceMonitoring, error) {
	policy, err := policy.normalized()
	if err != nil {
		return nil, err
	}
	return put[InterfaceMonitoring](ctx, sc, sc.ServiceURL("host_aggregates", id, "interface_monitoring"), policy)
}

// GetMonitoringOverview reads the region's monitoring counters and enabled
// state.
func GetMonitoringOverview(ctx context.Context, sc *gophercloud.ServiceClient) (*MonitoringOverview, error) {
	return get[MonitoringOverview](ctx, sc, sc.ServiceURL("monitoring", "overview"))
}

func validReaction(r InterfaceReaction) error {
	switch r {
	case ReactionSendAlert, ReactionFenceAndEvacuate, ReactionDisableHypervisor:
		return nil
	}
	return &ValidationError{Field: "interface_reaction", Message: "unsupported value " + string(r)}
}

// validDefaultReaction accepts an empty reaction, which is omitted from the
// request.
func validDefaultReaction(r InterfaceReaction) error {
	if r == "" {
		return nil
	}
	return validReaction(r)
}

func validOverrideReaction(r *InterfaceReaction) error {
	if r == nil {
		return nil
	}
	return validReaction(*r)
}

// normalizedInterfaces trims the names and refuses a blank or a duplicate one,
// as KeyVRM does. It returns a new map.
func normalizedInterfaces[T any](interfaces map[string]T) (map[string]T, error) {
	if interfaces == nil {
		return nil, nil
	}
	out := make(map[string]T, len(interfaces))
	for name, policy := range interfaces {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, &ValidationError{Field: "interfaces", Message: "interface names must not be blank"}
		}
		if _, exists := out[name]; exists {
			return nil, &ValidationError{Field: "interfaces", Message: "duplicate interface names after trimming whitespace"}
		}
		out[name] = policy
	}
	return out, nil
}

func (m InterfaceMonitoring) normalized() (InterfaceMonitoring, error) {
	for _, r := range []InterfaceReaction{
		m.Bond.DefaultPolicy.DegradedReaction, m.Bond.DefaultPolicy.DownReaction,
		m.HBA.DefaultPolicy.DownReaction, m.HBA.AllDownReaction,
	} {
		if err := validDefaultReaction(r); err != nil {
			return m, err
		}
	}
	for _, p := range m.Bond.Interfaces {
		if err := validOverrideReaction(p.DegradedReaction); err != nil {
			return m, err
		}
		if err := validOverrideReaction(p.DownReaction); err != nil {
			return m, err
		}
	}
	for _, p := range m.HBA.Interfaces {
		if err := validOverrideReaction(p.DownReaction); err != nil {
			return m, err
		}
	}
	var err error
	if m.Bond.Interfaces, err = normalizedInterfaces(m.Bond.Interfaces); err != nil {
		return m, err
	}
	m.HBA.Interfaces, err = normalizedInterfaces(m.HBA.Interfaces)
	return m, err
}
