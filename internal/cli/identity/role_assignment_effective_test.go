package identity

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	th "github.com/gophercloud/gophercloud/v2/testhelper"

	"github.com/ftarasenko/koc/internal/output"
)

const effectiveUserID = "9a25c19b0000000000000000000000aa"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serveAssignments answers /users with no match (the ID passes through) and
// /role_assignments with body, recording the query of the latter.
func serveAssignments(t *testing.T, body []byte) (th.FakeServer, *url.Values) {
	t.Helper()
	fakeServer := th.SetupHTTP()
	t.Cleanup(fakeServer.Teardown)
	var q url.Values
	fakeServer.Mux.HandleFunc("/users", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users":[]}`))
	})
	fakeServer.Mux.HandleFunc("/role_assignments", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	return fakeServer, &q
}

// The 16 rows `openstack role assignment list --user <id> --names --effective`
// prints for a user with a direct role, an admin role through a group on a
// project and two domains, and the implied manager/member/reader below admin.
// The reply is a Keystone-shaped fixture (synthetic names and IDs).
func TestRunRoleAssignmentList_EffectiveNamesMatchesUpstream(t *testing.T) {
	fakeServer, q := serveAssignments(t, readTestdata(t, "role_assignments_effective.json"))
	client := identityClient(fakeServer)

	var buf bytes.Buffer
	f := &assignmentListFlags{user: effectiveUserID, names: true, effective: true}
	if err := runRoleAssignmentList(context.Background(), client, &output.Options{Format: output.FormatCSV}, f, &buf); err != nil {
		t.Fatalf("runRoleAssignmentList: %v", err)
	}
	if got := q.Get("effective"); got != "true" {
		t.Errorf("effective query = %q, want true", got)
	}
	if got := q.Get("include_names"); got != "true" {
		t.Errorf("include_names query = %q, want true", got)
	}
	if got := q.Get("user.id"); got != effectiveUserID {
		t.Errorf("user.id query = %q", got)
	}
	want := string(readTestdata(t, "role_assignments_effective.csv"))
	if buf.String() != want {
		t.Errorf("rows:\n%s\nwant:\n%s", buf.String(), want)
	}
	if n := strings.Count(buf.String(), "\n") - 1; n != 16 {
		t.Errorf("rows = %d, want 16", n)
	}
}

func TestRunRoleAssignmentList_FlagsReachTheQuery(t *testing.T) {
	cases := []struct {
		name  string
		flags assignmentListFlags
		want  url.Values
		not   []string
	}{
		{"none", assignmentListFlags{}, url.Values{}, []string{"effective", "include_subtree", "scope.OS-INHERIT:inherited_to"}},
		{"effective", assignmentListFlags{effective: true}, url.Values{"effective": {"true"}}, []string{"include_subtree"}},
		{"inherited", assignmentListFlags{inherited: true}, url.Values{"scope.OS-INHERIT:inherited_to": {"projects"}}, []string{"effective"}},
		{"subtree", assignmentListFlags{project: "p1", subtree: true},
			url.Values{"include_subtree": {"true"}, "scope.project.id": {"p1"}}, []string{"effective"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeServer, q := serveAssignments(t, []byte(`{"role_assignments":[]}`))
			fakeServer.Mux.HandleFunc("/projects", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"projects":[]}`))
			})
			err := runRoleAssignmentList(context.Background(), identityClient(fakeServer), &output.Options{Format: output.FormatCSV}, &tc.flags, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.want {
				if got := q.Get(k); got != v[0] {
					t.Errorf("%s = %q, want %q", k, got, v[0])
				}
			}
			for _, k := range tc.not {
				if q.Has(k) {
					t.Errorf("%s must not be sent, got %q", k, q.Get(k))
				}
			}
		})
	}
}

func TestRunRoleAssignmentList_EffectiveRejectsGroup(t *testing.T) {
	fakeServer := th.SetupHTTP()
	defer fakeServer.Teardown()
	f := &assignmentListFlags{group: "ops", effective: true}
	err := runRoleAssignmentList(context.Background(), identityClient(fakeServer), &output.Options{Format: output.FormatCSV}, f, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--effective cannot be combined with --group") {
		t.Fatalf("err = %v", err)
	}
}

func TestRoleAssignmentList_HelpDocumentsNewFlags(t *testing.T) {
	cmd := newRoleAssignmentListCommand(nil, &output.Options{})
	for _, name := range []string{"effective", "inherited", "include-subtree"} {
		fl := cmd.Flags().Lookup(name)
		if fl == nil || fl.Usage == "" {
			t.Errorf("flag --%s missing or without help", name)
		}
	}
}
