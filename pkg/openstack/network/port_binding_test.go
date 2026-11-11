package network_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"

	"github.com/ftarasenko/koc/pkg/openstack/network"
)

const bindPortID = "2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d"

func TestPortBindings(t *testing.T) {
	path := "/ports/" + bindPortID + "/bindings"
	active := `{"host":"cmp-01","vif_type":"ovs","vif_details":{"port_filter":true},"vnic_type":"normal","profile":{},"status":"ACTIVE"}`
	inactive := `{"host":"cmp-02","vif_type":"ovs","vif_details":{},"vnic_type":"normal","profile":{"migrating_to":"cmp-02"},"status":"INACTIVE"}`
	seen, sc := fakeNeutron(t, map[string]reply{
		"GET " + path:                      {Body: `{"bindings":[` + active + `,` + inactive + `]}`},
		"POST " + path:                     {Status: http.StatusCreated, Body: `{"binding":` + inactive + `}`},
		"PUT " + path + "/cmp-02/activate": {Body: `{"binding":` + strings.Replace(inactive, "INACTIVE", "ACTIVE", 1) + `}`},
		"DELETE " + path + "/cmp-01":       {Status: http.StatusNoContent},
	})
	ctx := context.Background()

	all, err := network.ListPortBindings(ctx, sc, bindPortID)
	if err != nil || len(all) != 2 || all[0].Status != "ACTIVE" || all[1].Host != "cmp-02" || all[1].Profile["migrating_to"] != "cmp-02" {
		t.Fatalf("ListPortBindings = %+v, %v", all, err)
	}
	created, err := network.CreatePortBinding(ctx, sc, bindPortID, network.PortBindingCreateOpts{Host: "cmp-02", VNICType: "normal"})
	if err != nil || created.Host != "cmp-02" || created.Status != "INACTIVE" {
		t.Fatalf("CreatePortBinding = %+v, %v", created, err)
	}
	act, err := network.ActivatePortBinding(ctx, sc, bindPortID, "cmp-02")
	if err != nil || act.Status != "ACTIVE" {
		t.Fatalf("ActivatePortBinding = %+v, %v", act, err)
	}
	if err := network.DeletePortBinding(ctx, sc, bindPortID, "cmp-01"); err != nil {
		t.Fatalf("DeletePortBinding: %v", err)
	}

	want := []req{
		{Method: "GET", Path: path},
		{Method: "POST", Path: path, Body: `{"binding":{"host":"cmp-02","vnic_type":"normal"}}`},
		{Method: "PUT", Path: path + "/cmp-02/activate"},
		{Method: "DELETE", Path: path + "/cmp-01"},
	}
	if len(*seen) != len(want) {
		t.Fatalf("sent %d requests, want %d: %+v", len(*seen), len(want), *seen)
	}
	for i, w := range want {
		if got := (*seen)[i]; got != w {
			t.Errorf("request %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestPortBindings_Errors(t *testing.T) {
	path := "/ports/" + bindPortID + "/bindings"
	conflict := `{"NeutronError":{"type":"PortBindingAlreadyActive","message":"already active"}}`
	_, sc := fakeNeutron(t, map[string]reply{
		"PUT " + path + "/cmp-01/activate": {Status: http.StatusConflict, Body: conflict},
		"POST " + path:                     {Status: http.StatusConflict, Body: conflict},
		"DELETE " + path + "/cmp-01":       {Status: http.StatusConflict, Body: conflict},
	})
	ctx := context.Background()

	_, err := network.ListPortBindings(ctx, sc, "missing")
	is404(t, err, "listing bindings of port missing: ")

	b, err := network.ActivatePortBinding(ctx, sc, bindPortID, "cmp-01")
	if b != nil || !gophercloud.ResponseCodeIs(err, http.StatusConflict) ||
		!strings.HasPrefix(err.Error(), "activating the binding of port "+bindPortID+" to host cmp-01: ") {
		t.Errorf("ActivatePortBinding = %+v, %v", b, err)
	}
	b, err = network.CreatePortBinding(ctx, sc, bindPortID, network.PortBindingCreateOpts{Host: "cmp-01"})
	if b != nil || !gophercloud.ResponseCodeIs(err, http.StatusConflict) ||
		!strings.HasPrefix(err.Error(), "binding port "+bindPortID+" to host cmp-01: ") {
		t.Errorf("CreatePortBinding = %+v, %v", b, err)
	}
	err = network.DeletePortBinding(ctx, sc, bindPortID, "cmp-01")
	if !gophercloud.ResponseCodeIs(err, http.StatusConflict) ||
		!strings.HasPrefix(err.Error(), "deleting the binding of port "+bindPortID+" to host cmp-01: ") {
		t.Errorf("DeletePortBinding = %v", err)
	}
}
