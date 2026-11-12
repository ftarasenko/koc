package apierr

import (
	"fmt"
	"regexp"
)

// ironicState matches Ironic's refusal of a provision action in a state that
// does not allow it.
var ironicState = regexp.MustCompile(`The requested action "([^"]+)" can not be performed on node "([^"]+)" while it is in state "([^"]+)"`)

// hint is a suggestion for a message it recognises, "" otherwise. A hint is
// only worth printing when it is right for the exact case, so each one cites
// the transition table it rests on.
func hint(msg string) string {
	m := ironicState.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	action, node, state := m[1], m[2], m[3]
	// Ironic's provision state machine (ironic/common/states.py): a node
	// waiting for the deploy call-back has "fail", "hold", "resume" and "delete"
	// transitions. "abort" is only there in recent releases, so older ones
	// refuse it with this very message, while "delete" (koc: undeploy) is
	// allowed in that state in every release.
	if action == "abort" && state == "wait call-back" {
		return fmt.Sprintf("this Ironic does not abort a deploy that waits for its call-back; tear it down instead: koc baremetal node undeploy %s", node)
	}
	return ""
}
