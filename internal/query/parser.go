package query

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
)

type call struct {
	Name string
	Args bson.A
}

// shellQuery is a parsed db.<coll>.<method>(…)[.<cursorMethod>(…)…] call.
type shellQuery struct {
	Coll   string
	Method string
	Args   bson.A
	Chain  []call
}

// parseShellQuery parses mongo-shell queries such as
//
//	db.users.find({age: {$gt: 30}}, {name: 1}).sort({age: -1}).limit(5)
//	db.getCollection("my.coll").countDocuments({})
func parseShellQuery(input string) (shellQuery, error) {
	var q shellQuery
	input = strings.TrimSuffix(strings.TrimSpace(input), ";")
	if !strings.HasPrefix(input, "db.") {
		return q, fmt.Errorf("queries must start with db. — e.g. db.collection.find({...})")
	}
	rest := input[3:]

	if strings.HasPrefix(rest, "getCollection(") {
		open := len("getCollection")
		end, err := matchParen(rest, open)
		if err != nil {
			return q, err
		}
		args, err := mjson.ParseArgs(rest[open+1 : end])
		if err != nil {
			return q, fmt.Errorf("getCollection(): %w", err)
		}
		name, ok := firstString(args)
		if !ok {
			return q, fmt.Errorf("getCollection() takes a collection name string")
		}
		q.Coll = name
		rest = strings.TrimSpace(rest[end+1:])
		if !strings.HasPrefix(rest, ".") {
			return q, fmt.Errorf("missing method call — e.g. db.getCollection(\"c\").find({...})")
		}
		rest = rest[1:]
	} else {
		paren := strings.Index(rest, "(")
		if paren == -1 {
			return q, fmt.Errorf("missing method call — e.g. db.collection.find({...})")
		}
		lastDot := strings.LastIndex(rest[:paren], ".")
		if lastDot <= 0 {
			return q, fmt.Errorf("missing collection — e.g. db.collection.find({...})")
		}
		q.Coll = rest[:lastDot]
		rest = rest[lastDot+1:]
	}

	var calls []call
	for {
		paren := strings.Index(rest, "(")
		if paren == -1 {
			return q, fmt.Errorf("missing '(' after %q", rest)
		}
		name := strings.TrimSpace(rest[:paren])
		if name == "" || strings.ContainsAny(name, " .{}[]\"'") {
			return q, fmt.Errorf("invalid method name %q", name)
		}
		end, err := matchParen(rest, paren)
		if err != nil {
			return q, err
		}
		args, err := mjson.ParseArgs(rest[paren+1 : end])
		if err != nil {
			return q, fmt.Errorf("%s(): %w", name, err)
		}
		calls = append(calls, call{Name: name, Args: args})
		rest = strings.TrimSuffix(strings.TrimSpace(rest[end+1:]), ";")
		if rest == "" {
			break
		}
		if !strings.HasPrefix(rest, ".") {
			return q, fmt.Errorf("unexpected text after %s(): %q", name, rest)
		}
		rest = rest[1:]
	}
	q.Method, q.Args, q.Chain = calls[0].Name, calls[0].Args, calls[1:]
	return q, nil
}

func firstString(args bson.A) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	s, ok := args[0].(string)
	return s, ok && s != ""
}

// matchParen returns the index of the ')' matching the '(' at s[open],
// skipping string and regex literals.
func matchParen(s string, open int) (int, error) {
	depth := 0
	prev := byte('(')
	for i := open; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"' || ch == '\'':
			j := i + 1
			for j < len(s) && s[j] != ch {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				return -1, fmt.Errorf("unterminated string")
			}
			i = j
		case ch == '/' && strings.IndexByte("(:[,", prev) >= 0 && i+1 < len(s) && s[i+1] != '/' && s[i+1] != '*':
			j, inClass := i+1, false
			for j < len(s) && (s[j] != '/' || inClass) {
				switch s[j] {
				case '\\':
					j++
				case '[':
					inClass = true
				case ']':
					inClass = false
				}
				j++
			}
			if j >= len(s) {
				return -1, fmt.Errorf("unterminated regular expression")
			}
			i = j
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		if ch != ' ' && ch != '\t' && ch != '\n' {
			prev = ch
		}
	}
	return -1, fmt.Errorf("missing closing parenthesis")
}

// parseFilter parses a filter in shell syntax or Extended JSON.
func parseFilter(s string) (bson.D, error) {
	return mjson.ParseDoc(s)
}
