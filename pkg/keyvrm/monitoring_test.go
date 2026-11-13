package keyvrm_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	th "github.com/gophercloud/gophercloud/v2/testhelper"

	"github.com/ftarasenko/koc/pkg/keyvrm"
)

// The fixtures and cases are ported from the internal KeyVRM Go SDK by
// N. Kushnarenko.

//go:embed testdata/interface_monitoring.json
var interfaceMonitoringJSON string

//go:embed testdata/monitoring_overview.json
var monitoringOverviewJSON string

const interfaceMonitoringPath = "/v1/host_aggregates/ha-1/interface_monitoring"

func ptr[T any](v T) *T { return &v }

func interfaceMonitoringFixture() *keyvrm.InterfaceMonitoring {
	return &keyvrm.InterfaceMonitoring{
		Bond: keyvrm.BondMonitoringPolicy{
			DefaultPolicy: keyvrm.BondPolicy{
				Enabled:          ptr(true),
				DegradedReaction: keyvrm.ReactionSendAlert,
				DownReaction:     keyvrm.ReactionFenceAndEvacuate,
			},
			Interfaces: map[string]keyvrm.BondPolicyOverride{
				"bond0": {Enabled: ptr(false), DownReaction: ptr(keyvrm.ReactionDisableHypervisor)},
			},
		},
		HBA: keyvrm.HBAMonitoringPolicy{
			DefaultPolicy:   keyvrm.HBAPolicy{Enabled: ptr(false), DownReaction: keyvrm.ReactionSendAlert},
			Interfaces:      map[string]keyvrm.HBAPolicyOverride{"hba0": {}},
			AllDownReaction: keyvrm.ReactionFenceAndEvacuate,
		},
	}
}

// --- GetInterfaceMonitoring ---

func TestGetInterfaceMonitoring_RequestAndDecode(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()

	fakeServer.Mux.HandleFunc(interfaceMonitoringPath, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.AssertEquals(t, interfaceMonitoringPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, interfaceMonitoringJSON)
	})

	got, err := keyvrm.GetInterfaceMonitoring(context.Background(), keyvrmTestClient(fakeServer), "ha-1")
	if err != nil {
		t.Fatalf("GetInterfaceMonitoring: %v", err)
	}
	if want := interfaceMonitoringFixture(); !reflect.DeepEqual(got, want) {
		t.Errorf("decoded = %#v, want %#v", got, want)
	}
}

func TestGetInterfaceMonitoring_ErrorPath(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		fakeServer := th.SetupHTTP()
		fakeServer.Mux.HandleFunc(interfaceMonitoringPath, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		})
		if _, err := keyvrm.GetInterfaceMonitoring(context.Background(), keyvrmTestClient(fakeServer), "ha-1"); err == nil {
			t.Errorf("expected an error from a %d response, got nil", code)
		}
		fakeServer.Teardown()
	}
}

// --- UpdateInterfaceMonitoring ---

func TestUpdateInterfaceMonitoring_RequestAndDecode(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()

	fakeServer.Mux.HandleFunc(interfaceMonitoringPath, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "PUT")
		th.AssertEquals(t, interfaceMonitoringPath, r.URL.Path)
		th.TestJSONRequest(t, r, interfaceMonitoringJSON)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, interfaceMonitoringJSON)
	})

	got, err := keyvrm.UpdateInterfaceMonitoring(context.Background(), keyvrmTestClient(fakeServer), "ha-1", *interfaceMonitoringFixture())
	if err != nil {
		t.Fatalf("UpdateInterfaceMonitoring: %v", err)
	}
	if want := interfaceMonitoringFixture(); !reflect.DeepEqual(got, want) {
		t.Errorf("decoded = %#v, want %#v", got, want)
	}
}

// The request carries trimmed names and the caller's maps are left alone.
func TestUpdateInterfaceMonitoring_TrimsNamesWithoutMutating(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()

	fakeServer.Mux.HandleFunc(interfaceMonitoringPath, func(w http.ResponseWriter, r *http.Request) {
		th.TestJSONRequest(t, r, `{"bond":{"default_policy":{},"interfaces":{"bond0":{"enabled":false}}},
			"hba":{"default_policy":{},"interfaces":{"hba0":{"down_reaction":"disable_hypervisor"}}}}`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, interfaceMonitoringJSON)
	})

	policy := keyvrm.InterfaceMonitoring{
		Bond: keyvrm.BondMonitoringPolicy{Interfaces: map[string]keyvrm.BondPolicyOverride{" bond0 ": {Enabled: ptr(false)}}},
		HBA: keyvrm.HBAMonitoringPolicy{Interfaces: map[string]keyvrm.HBAPolicyOverride{
			" hba0 ": {DownReaction: ptr(keyvrm.ReactionDisableHypervisor)},
		}},
	}
	before, _ := json.Marshal(policy)
	if _, err := keyvrm.UpdateInterfaceMonitoring(context.Background(), keyvrmTestClient(fakeServer), "ha-1", policy); err != nil {
		t.Fatalf("UpdateInterfaceMonitoring: %v", err)
	}
	if after, _ := json.Marshal(policy); string(before) != string(after) {
		t.Error("policy maps were modified")
	}
}

func TestUpdateInterfaceMonitoring_ErrorPath(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()

	fakeServer.Mux.HandleFunc(interfaceMonitoringPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	if _, err := keyvrm.UpdateInterfaceMonitoring(context.Background(), keyvrmTestClient(fakeServer), "ha-1", *interfaceMonitoringFixture()); err == nil {
		t.Fatal("expected an error from a 422 response, got nil")
	}
}

// An input KeyVRM would refuse never reaches it.
func TestUpdateInterfaceMonitoring_InvalidPolicySendsNothing(t *testing.T) {
	for _, tt := range []struct {
		name   string
		policy keyvrm.InterfaceMonitoring
	}{
		{"blank bond", keyvrm.InterfaceMonitoring{Bond: keyvrm.BondMonitoringPolicy{Interfaces: map[string]keyvrm.BondPolicyOverride{" ": {}}}}},
		{"blank hba", keyvrm.InterfaceMonitoring{HBA: keyvrm.HBAMonitoringPolicy{Interfaces: map[string]keyvrm.HBAPolicyOverride{"": {}}}}},
		{"duplicate bond", keyvrm.InterfaceMonitoring{Bond: keyvrm.BondMonitoringPolicy{Interfaces: map[string]keyvrm.BondPolicyOverride{" bond0": {}, "bond0 ": {}}}}},
		{"duplicate hba", keyvrm.InterfaceMonitoring{HBA: keyvrm.HBAMonitoringPolicy{Interfaces: map[string]keyvrm.HBAPolicyOverride{" hba0": {}, "hba0 ": {}}}}},
		{"bond default reaction", keyvrm.InterfaceMonitoring{Bond: keyvrm.BondMonitoringPolicy{DefaultPolicy: keyvrm.BondPolicy{DownReaction: "bad"}}}},
		{"hba all-down reaction", keyvrm.InterfaceMonitoring{HBA: keyvrm.HBAMonitoringPolicy{AllDownReaction: "bad"}}},
		{"bond override reaction", keyvrm.InterfaceMonitoring{Bond: keyvrm.BondMonitoringPolicy{Interfaces: map[string]keyvrm.BondPolicyOverride{"bond0": {DegradedReaction: ptr(keyvrm.InterfaceReaction("bad"))}}}}},
		{"hba override empty reaction", keyvrm.InterfaceMonitoring{HBA: keyvrm.HBAMonitoringPolicy{Interfaces: map[string]keyvrm.HBAPolicyOverride{"hba0": {DownReaction: ptr(keyvrm.InterfaceReaction(""))}}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeServer := th.SetupHTTP()
			defer fakeServer.Teardown()
			fakeServer.Mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {
				t.Error("an invalid policy reached the server")
			})

			_, err := keyvrm.UpdateInterfaceMonitoring(context.Background(), keyvrmTestClient(fakeServer), "ha-1", tt.policy)
			var verr *keyvrm.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("error = %v, want a *ValidationError", err)
			}
		})
	}
}

// --- GetMonitoringOverview ---

func TestGetMonitoringOverview_RequestAndDecode(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()

	fakeServer.Mux.HandleFunc("/v1/monitoring/overview", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.AssertEquals(t, "/v1/monitoring/overview", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, monitoringOverviewJSON)
	})

	got, err := keyvrm.GetMonitoringOverview(context.Background(), keyvrmTestClient(fakeServer))
	if err != nil {
		t.Fatalf("GetMonitoringOverview: %v", err)
	}
	want := &keyvrm.MonitoringOverview{Enabled: true, Zones: 1, Aggregates: 2, Processes: 3, ProblemAggregates: 4, NoopAggregates: 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded = %#v, want %#v", got, want)
	}
}

func TestGetMonitoringOverview_ErrorPath(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()

	fakeServer.Mux.HandleFunc("/v1/monitoring/overview", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := keyvrm.GetMonitoringOverview(context.Background(), keyvrmTestClient(fakeServer)); err == nil {
		t.Fatal("expected an error from a 500 response, got nil")
	}
}
