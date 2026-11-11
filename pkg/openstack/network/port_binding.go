package network

import (
	"context"
	"net/http"

	"github.com/gophercloud/gophercloud/v2"
)

// PortBindingExtension is the alias of neutron's extended port bindings
// extension (binding-extended): a port with one binding per host, of which
// one is active. Nova uses it to live-migrate a port to the destination host.
const PortBindingExtension = "binding-extended"

// PortBinding is one binding of a port to a host.
type PortBinding struct {
	Host       string         `json:"host"`
	VIFType    string         `json:"vif_type"`
	VIFDetails map[string]any `json:"vif_details"`
	VNICType   string         `json:"vnic_type"`
	Profile    map[string]any `json:"profile"`
	// Status is ACTIVE or INACTIVE.
	Status string `json:"status"`
}

// PortBindingCreateOpts is a new inactive binding of a port to Host.
type PortBindingCreateOpts struct {
	Host     string         `json:"host"`
	VNICType string         `json:"vnic_type,omitempty"`
	Profile  map[string]any `json:"profile,omitempty"`
}

// ListPortBindings lists a port's bindings, the active one and any inactive
// ones (one per host).
func ListPortBindings(ctx context.Context, c *gophercloud.ServiceClient, portID string) ([]PortBinding, error) {
	var out struct {
		Bindings []PortBinding `json:"bindings"`
	}
	err := getJSON(ctx, c, c.ServiceURL("ports", portID, "bindings"), &out)
	return out.Bindings, wrap(err, "listing bindings of port %s", portID)
}

// CreatePortBinding binds a port to another host. The binding is inactive
// until ActivatePortBinding.
func CreatePortBinding(ctx context.Context, c *gophercloud.ServiceClient, portID string, opts PortBindingCreateOpts) (*PortBinding, error) {
	var out struct {
		Binding PortBinding `json:"binding"`
	}
	resp, err := c.Post(ctx, c.ServiceURL("ports", portID, "bindings"), map[string]any{"binding": opts}, &out,
		&gophercloud.RequestOpts{OkCodes: []int{http.StatusOK, http.StatusCreated}})
	closeBody(resp)
	if err != nil {
		return nil, wrap(err, "binding port %s to host %s", portID, opts.Host)
	}
	return &out.Binding, nil
}

// ActivatePortBinding makes the port's binding to host the active one; the
// binding that was active becomes inactive. Neutron answers 409 when the
// binding is already active.
func ActivatePortBinding(ctx context.Context, c *gophercloud.ServiceClient, portID, host string) (*PortBinding, error) {
	var out struct {
		Binding PortBinding `json:"binding"`
	}
	resp, err := c.Put(ctx, c.ServiceURL("ports", portID, "bindings", host, "activate"), nil, &out,
		&gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})
	closeBody(resp)
	if err != nil {
		return nil, wrap(err, "activating the binding of port %s to host %s", portID, host)
	}
	return &out.Binding, nil
}

// DeletePortBinding deletes the port's inactive binding to host. Neutron
// refuses to delete the active one.
func DeletePortBinding(ctx context.Context, c *gophercloud.ServiceClient, portID, host string) error {
	resp, err := c.Delete(ctx, c.ServiceURL("ports", portID, "bindings", host),
		&gophercloud.RequestOpts{OkCodes: []int{http.StatusNoContent}})
	closeBody(resp)
	return wrap(err, "deleting the binding of port %s to host %s", portID, host)
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}
