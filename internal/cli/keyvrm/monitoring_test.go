package keyvrm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	th "github.com/gophercloud/gophercloud/v2/testhelper"

	"github.com/ftarasenko/koc/internal/auth"
	"github.com/ftarasenko/koc/internal/output"
)

const interfaceMonitoringBody = `{
	"bond":{"default_policy":{"enabled":true,"degraded_reaction":"send_alert","down_reaction":"fence_and_evacuate"},
		"interfaces":{"bond0":{"enabled":false,"down_reaction":"disable_hypervisor"}}},
	"hba":{"default_policy":{"enabled":false,"down_reaction":"send_alert"},
		"interfaces":{"hba0":{}},"all_down_reaction":"fence_and_evacuate"}}`

const imPath = "/v1/host_aggregates/ha-1/interface_monitoring"

func str(s string) *string { return &s }
func flag(b bool) *bool    { return &b }

func TestRunMonitoringShow_Output(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()
	fakeServer.Mux.HandleFunc("/v1/monitoring/overview", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"enabled":true,"zones":1,"aggregates":2,"processes":3,"problem_aggregates":4,"noop_aggregates":5}`)
	})

	var buf bytes.Buffer
	o := &output.Options{Format: output.FormatJSON}
	if err := runMonitoringShow(context.Background(), keyvrmTestClient(fakeServer), o, &buf); err != nil {
		t.Fatalf("runMonitoringShow: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if got["enabled"] != true || got["problem_aggregates"] != float64(4) || got["noop_aggregates"] != float64(5) {
		t.Errorf("output = %v", got)
	}
}

func TestRunInterfaceMonitoringShow_Output(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()
	fakeServer.Mux.HandleFunc(imPath, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, interfaceMonitoringBody)
	})

	var buf bytes.Buffer
	o := &output.Options{Format: output.FormatJSON}
	if err := runInterfaceMonitoringShow(context.Background(), keyvrmTestClient(fakeServer), o, "ha-1", &buf); err != nil {
		t.Fatalf("runInterfaceMonitoringShow: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if got["bond_down_reaction"] != "fence_and_evacuate" || got["hba_all_down_reaction"] != "fence_and_evacuate" || got["hba_enabled"] != false {
		t.Errorf("output = %v", got)
	}
	bond, _ := got["bond_interfaces"].(map[string]any)
	if _, ok := bond["bond0"]; !ok {
		t.Errorf("bond_interfaces = %v, want bond0", got["bond_interfaces"])
	}
}

// YAML keeps the API's field names and shows an empty override as {}.
func TestRunInterfaceMonitoringShow_YAMLKeepsAPINames(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()
	fakeServer.Mux.HandleFunc(imPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, interfaceMonitoringBody)
	})

	var buf bytes.Buffer
	o := &output.Options{Format: output.FormatYAML}
	if err := runInterfaceMonitoringShow(context.Background(), keyvrmTestClient(fakeServer), o, "ha-1", &buf); err != nil {
		t.Fatalf("runInterfaceMonitoringShow: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"down_reaction: disable_hypervisor", "hba0: {}"} {
		if !strings.Contains(out, want) {
			t.Errorf("YAML lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "downreaction") || strings.Contains(out, "null") {
		t.Errorf("YAML has Go field names or nulls:\n%s", out)
	}
}

// serveInterfaceMonitoring serves the fixture on GET and records the PUT body.
func serveInterfaceMonitoring(t *testing.T, s th.FakeServer, puts *[]map[string]any) {
	t.Helper()
	s.Mux.HandleFunc(imPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*puts = append(*puts, body)
		}
		_, _ = io.WriteString(w, interfaceMonitoringBody)
	})
}

func TestRunInterfaceMonitoringSet_WritesBackTheWholePolicyEdited(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()
	var puts []map[string]any
	serveInterfaceMonitoring(t, fakeServer, &puts)

	edit := interfaceMonitoringEdit{
		bondDown:      str("send_alert"),
		hbaEnabled:    flag(true),
		bondOverrides: []string{"bond0:enabled=true", "bond1:degraded-reaction=fence_and_evacuate"},
		hbaOverrides:  []string{"hba1"},
		removeHBA:     []string{"hba0"},
	}
	o := &output.Options{Format: output.FormatValue}
	if err := runInterfaceMonitoringSet(context.Background(), keyvrmTestClient(fakeServer), o, "ha-1", edit, io.Discard); err != nil {
		t.Fatalf("runInterfaceMonitoringSet: %v", err)
	}
	if len(puts) != 1 {
		t.Fatalf("%d PUTs, want 1", len(puts))
	}
	want := map[string]any{}
	if err := json.Unmarshal([]byte(`{
		"bond":{"default_policy":{"enabled":true,"degraded_reaction":"send_alert","down_reaction":"send_alert"},
			"interfaces":{"bond0":{"enabled":true,"down_reaction":"disable_hypervisor"},
				"bond1":{"degraded_reaction":"fence_and_evacuate"}}},
		"hba":{"default_policy":{"enabled":true,"down_reaction":"send_alert"},
			"interfaces":{"hba1":{}},"all_down_reaction":"fence_and_evacuate"}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(puts[0], want) {
		t.Errorf("PUT body = %v\nwant %v", puts[0], want)
	}
}

// An edit that fails, or that KeyVRM would refuse, sends no PUT.
func TestRunInterfaceMonitoringSet_BadEditSendsNoPut(t *testing.T) {
	for name, edit := range map[string]interfaceMonitoringEdit{
		"unknown reaction":        {bondDown: str("explode")},
		"empty reaction":          {hbaAllDown: str("")},
		"unknown override key":    {bondOverrides: []string{"bond0:colour=red"}},
		"degraded on an hba":      {hbaOverrides: []string{"hba0:degraded-reaction=send_alert"}},
		"override without a name": {bondOverrides: []string{":enabled=true"}},
		"remove a missing bond":   {removeBond: []string{"bond9"}},
		"remove a missing hba":    {removeHBA: []string{"hba9"}},
	} {
		t.Run(name, func(t *testing.T) {
			fakeServer := th.SetupHTTP()
			defer fakeServer.Teardown()
			var puts []map[string]any
			serveInterfaceMonitoring(t, fakeServer, &puts)

			err := runInterfaceMonitoringSet(context.Background(), keyvrmTestClient(fakeServer), &output.Options{Format: output.FormatValue}, "ha-1", edit, io.Discard)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if len(puts) != 0 {
				t.Errorf("%d PUT(s) sent despite the error", len(puts))
			}
		})
	}
}

func TestParseOverride(t *testing.T) {
	ed, err := parseOverride(" bond0 :enabled=false,down-reaction=send_alert,degraded-reaction=disable_hypervisor", true)
	if err != nil {
		t.Fatal(err)
	}
	if ed.name != "bond0" || ed.enabled == nil || *ed.enabled || ed.down == nil || *ed.down != "send_alert" ||
		ed.degraded == nil || *ed.degraded != "disable_hypervisor" {
		t.Errorf("parsed = %+v", ed)
	}
	for _, bad := range []string{"", "bond0:enabled", "bond0:enabled=maybe", "bond0:x=1"} {
		if _, err := parseOverride(bad, true); err == nil {
			t.Errorf("parseOverride(%q) = nil error", bad)
		}
	}
}

func TestInterfaceMonitoringSet_NoFlagsIsRefused(t *testing.T) {
	cmd := newInterfaceMonitoringSet(&auth.Options{}, &output.Options{Format: output.FormatValue})
	cmd.SetArgs([]string{"ha-1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); !errors.Is(err, errNoUpdateFields) {
		t.Fatalf("err = %v, want errNoUpdateFields", err)
	}
}

func TestInterfaceMonitoringEditOf_CollectsOnlyGivenFlags(t *testing.T) {
	cmd := newInterfaceMonitoringSet(&auth.Options{}, &output.Options{})
	err := cmd.ParseFlags([]string{
		"--bond-enabled=false", "--hba-all-down-reaction", "send_alert",
		"--bond-override", "bond0:enabled=true", "--remove-hba-override", "hba0",
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := interfaceMonitoringEditOf(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if e.bondEnabled == nil || *e.bondEnabled || e.hbaEnabled != nil || e.bondDown != nil ||
		e.hbaAllDown == nil || *e.hbaAllDown != "send_alert" ||
		!reflect.DeepEqual(e.bondOverrides, []string{"bond0:enabled=true"}) ||
		!reflect.DeepEqual(e.removeHBA, []string{"hba0"}) || e.empty() {
		t.Errorf("edit = %+v", e)
	}
	if !strings.Contains(cmd.Long, "KEY=VALUE") {
		t.Error("help does not describe the override format")
	}
}
