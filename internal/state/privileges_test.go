package state

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func statusWith(privs ...bson.M) bson.M {
	a := bson.A{}
	for _, p := range privs {
		a = append(a, p)
	}
	return bson.M{"authInfo": bson.M{
		"authenticatedUsers":          bson.A{bson.M{"user": "u", "db": "admin"}},
		"authenticatedUserPrivileges": a,
	}}
}

func priv(res bson.M, actions ...string) bson.M {
	a := bson.A{}
	for _, s := range actions {
		a = append(a, s)
	}
	return bson.M{"resource": res, "actions": a}
}

func TestParsePrivilegesNoAuth(t *testing.T) {
	p := ParsePrivileges(bson.M{"authInfo": bson.M{"authenticatedUsers": bson.A{}}})
	if p != nil {
		t.Fatal("expected nil privileges without auth")
	}
	if !p.Can(ActRemove, "x", "y") || !p.AnyWrite() {
		t.Error("nil privileges must allow everything")
	}
}

func TestPrivilegesPerDatabase(t *testing.T) {
	p := ParsePrivileges(statusWith(
		priv(bson.M{"db": "app", "collection": ""}, "find", "insert", "update", "remove"),
		priv(bson.M{"db": "", "collection": ""}, "find"),
		priv(bson.M{"db": "", "collection": "audit"}, "insert"),
		priv(bson.M{"cluster": true}, "killop"),
	))
	cases := []struct {
		action, db, coll string
		want             bool
	}{
		{ActRemove, "app", "users", true},
		{ActRemove, "other", "users", false},
		{ActRemove, "app", "system.views", false},
		{ActInsert, "other", "audit", true},
		{ActDropCollection, "app", "users", false},
		{"killop", "", "", false},
	}
	for _, c := range cases {
		if got := p.Can(c.action, c.db, c.coll); got != c.want {
			t.Errorf("Can(%s, %s.%s) = %v, want %v", c.action, c.db, c.coll, got, c.want)
		}
	}
	if !p.AnyWrite() {
		t.Error("AnyWrite should be true")
	}
}

func TestPrivilegesCustomRoleAndReadOnly(t *testing.T) {
	ro := ParsePrivileges(statusWith(priv(bson.M{"db": "", "collection": ""}, "find", "listCollections")))
	if ro.AnyWrite() {
		t.Error("read-only user reported as writable")
	}
	any := ParsePrivileges(statusWith(priv(bson.M{"anyResource": true}, "remove")))
	if !any.Can(ActRemove, "x", "system.js") {
		t.Error("anyResource should cover system collections")
	}
}
