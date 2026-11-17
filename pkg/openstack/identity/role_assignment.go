package identity

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/roles"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

// RoleAssignment is a keystone role assignment with the two fields
// gophercloud's roles.RoleAssignment leaves out.
type RoleAssignment struct {
	roles.RoleAssignment
	// RoleDomain is a domain-specific role's domain. Keystone sends its name
	// only when the listing asked for names (IncludeNames).
	RoleDomain roles.Domain
	// InheritedTo is the OS-INHERIT marker of an inherited assignment
	// ("projects"); empty for a direct one.
	InheritedTo string
}

// assignmentExtra is the part of an assignment gophercloud does not decode.
type assignmentExtra struct {
	Role struct {
		Domain roles.Domain `json:"domain"`
	} `json:"role"`
	Scope struct {
		InheritedTo string `json:"OS-INHERIT:inherited_to"`
	} `json:"scope"`
}

// ListRoleAssignments lists the role assignments opts selects.
func ListRoleAssignments(ctx context.Context, c *gophercloud.ServiceClient, opts roles.ListAssignmentsOpts) ([]RoleAssignment, error) {
	return listAll(ctx, roles.ListAssignments(c, opts), extractRoleAssignments, "listing role assignments")
}

// inheritedToQuery is keystone's filter on the OS-INHERIT marker of an assignment.
const inheritedToQuery = "scope.OS-INHERIT:inherited_to"

// ListRoleAssignmentsOpts is roles.ListAssignmentsOpts plus the one filter
// gophercloud lacks: only assignments inherited by the projects of a domain
// or project subtree (InheritedTo "projects").
type ListRoleAssignmentsOpts struct {
	roles.ListAssignmentsOpts
	// InheritedTo selects inherited assignments; the only value keystone
	// knows is "projects". Empty adds no filter.
	InheritedTo string
}

// ToRolesListAssignmentsQuery formats the options as a query string.
func (o ListRoleAssignmentsOpts) ToRolesListAssignmentsQuery() (string, error) {
	q, err := o.ListAssignmentsOpts.ToRolesListAssignmentsQuery()
	if err != nil || o.InheritedTo == "" {
		return q, err
	}
	vals, err := url.ParseQuery(strings.TrimPrefix(q, "?"))
	if err != nil {
		return "", err
	}
	vals.Set(inheritedToQuery, o.InheritedTo)
	return "?" + vals.Encode(), nil
}

// ListRoleAssignmentsFiltered lists the role assignments opts selects,
// including the inherited filter. Set Effective for the expanded view:
// group memberships and implied roles resolved into per-user rows.
func ListRoleAssignmentsFiltered(ctx context.Context, c *gophercloud.ServiceClient, opts ListRoleAssignmentsOpts) ([]RoleAssignment, error) {
	return listAll(ctx, roles.ListAssignments(c, opts), extractRoleAssignments, "listing role assignments")
}

func extractRoleAssignments(page pagination.Page) ([]RoleAssignment, error) {
	all, err := roles.ExtractRoleAssignments(page)
	if err != nil {
		return nil, err
	}
	p, ok := page.(roles.RoleAssignmentPage)
	if !ok {
		return nil, fmt.Errorf("unexpected role assignment page type %T", page)
	}
	var extra []assignmentExtra
	if err := p.ExtractIntoSlicePtr(&extra, "role_assignments"); err != nil {
		return nil, err
	}
	out := make([]RoleAssignment, len(all))
	for i, ra := range all {
		out[i].RoleAssignment = ra
		if i < len(extra) {
			out[i].RoleDomain = extra[i].Role.Domain
			out[i].InheritedTo = extra[i].Scope.InheritedTo
		}
	}
	return out, nil
}
