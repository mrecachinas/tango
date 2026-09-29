package state

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

// Privilege is one entry of connectionStatus authenticatedUserPrivileges.
type Privilege struct {
	DB          string
	Coll        string
	Cluster     bool
	AnyResource bool
	Actions     map[string]bool
}

// Privileges is the effective privilege set of the connected user.
type Privileges struct {
	List []Privilege
}

// Actions used by tango's write operations.
const (
	ActInsert         = "insert"
	ActUpdate         = "update"
	ActRemove         = "remove"
	ActDropCollection = "dropCollection"
	ActDropIndex      = "dropIndex"
)

var writeActions = []string{ActInsert, ActUpdate, ActRemove, ActDropCollection, ActDropIndex}

// ParsePrivileges extracts privileges from a connectionStatus reply
// (run with showPrivileges: true). It returns nil when no user is
// authenticated, i.e. the server isn't enforcing access control.
func ParsePrivileges(status bson.M) *Privileges {
	authInfo, _ := status["authInfo"].(bson.M)
	users, _ := authInfo["authenticatedUsers"].(bson.A)
	if len(users) == 0 {
		return nil
	}
	p := &Privileges{}
	privs, _ := authInfo["authenticatedUserPrivileges"].(bson.A)
	for _, raw := range privs {
		m, ok := raw.(bson.M)
		if !ok {
			continue
		}
		res, _ := m["resource"].(bson.M)
		pr := Privilege{Actions: map[string]bool{}}
		pr.DB, _ = res["db"].(string)
		pr.Coll, _ = res["collection"].(string)
		pr.Cluster, _ = res["cluster"].(bool)
		pr.AnyResource, _ = res["anyResource"].(bool)
		acts, _ := m["actions"].(bson.A)
		for _, a := range acts {
			if s, ok := a.(string); ok {
				pr.Actions[s] = true
			}
		}
		p.List = append(p.List, pr)
	}
	return p
}

func (pr Privilege) matches(db, coll string) bool {
	if pr.AnyResource {
		return true
	}
	if pr.Cluster {
		return false
	}
	if pr.DB != "" && pr.DB != db {
		return false
	}
	if pr.Coll == "" {
		// Generic collection resources don't cover system collections.
		return !strings.HasPrefix(coll, "system.")
	}
	return pr.Coll == coll
}

// Can reports whether action is allowed on db.coll.
func (p *Privileges) Can(action, db, coll string) bool {
	if p == nil {
		return true
	}
	for _, pr := range p.List {
		if pr.Actions[action] && pr.matches(db, coll) {
			return true
		}
	}
	return false
}

// AnyWrite reports whether any write action is allowed anywhere.
func (p *Privileges) AnyWrite() bool {
	if p == nil {
		return true
	}
	for _, pr := range p.List {
		if pr.Cluster {
			continue
		}
		for _, a := range writeActions {
			if pr.Actions[a] {
				return true
			}
		}
	}
	return false
}

// CheckWrite reports whether action is allowed on ns, showing an error if not.
func CheckWrite(action string, ns Namespace) bool {
	if S.Privileges.Can(action, ns.DB, ns.Coll) {
		return true
	}
	ShowError(fmt.Sprintf("Not permitted: your user lacks the %q privilege on %s", action, ns))
	return false
}
