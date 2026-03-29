package query

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

// ParseMongoQuery extracts collection, method, and raw args from
// a query like db.users.find({name: "Alice"}).
func parseMongoQuery(input string) (collection, method, argsStr string, err error) {
	rest := input[3:] // strip "db."

	parenIdx := strings.Index(rest, "(")
	if parenIdx == -1 {
		return "", "", "", fmt.Errorf("missing method call — e.g. db.collection.find({...})")
	}

	prefix := rest[:parenIdx]
	lastDot := strings.LastIndex(prefix, ".")
	if lastDot == -1 {
		return "", "", "", fmt.Errorf("missing collection — e.g. db.collection.find({...})")
	}

	collection = prefix[:lastDot]
	method = prefix[lastDot+1:]

	// Find matching closing paren (handle nested parens)
	depth := 0
	argsStart := parenIdx + 1
	argsEnd := -1
	for i := parenIdx; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				argsEnd = i
			}
		}
		if argsEnd != -1 {
			break
		}
	}
	if argsEnd == -1 {
		return "", "", "", fmt.Errorf("missing closing parenthesis")
	}

	argsStr = strings.TrimSpace(rest[argsStart:argsEnd])
	return collection, method, argsStr, nil
}

// relaxedJSONToStrict converts mongo-shell-style JSON (unquoted keys,
// single-quoted strings, $operators) to strict JSON.
func relaxedJSONToStrict(input string) string {
	var buf strings.Builder
	runes := []rune(input)
	n := len(runes)

	for i := 0; i < n; i++ {
		ch := runes[i]

		switch {
		case ch == '"':
			// Pass through double-quoted strings
			buf.WriteRune(ch)
			i++
			for i < n && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < n {
					buf.WriteRune(runes[i])
					i++
				}
				buf.WriteRune(runes[i])
				i++
			}
			if i < n {
				buf.WriteRune(runes[i])
			}

		case ch == '\'':
			// Single-quoted string → double-quoted
			buf.WriteRune('"')
			i++
			for i < n && runes[i] != '\'' {
				if runes[i] == '\\' && i+1 < n {
					buf.WriteRune(runes[i])
					i++
					buf.WriteRune(runes[i])
				} else if runes[i] == '"' {
					buf.WriteRune('\\')
					buf.WriteRune('"')
				} else {
					buf.WriteRune(runes[i])
				}
				i++
			}
			buf.WriteRune('"')

		case isIdentStart(ch):
			// Collect identifier
			start := i
			for i < n && isIdentChar(runes[i]) {
				i++
			}
			word := string(runes[start:i])
			i-- // back up; outer loop will advance

			// Look ahead past whitespace for ':'
			j := i + 1
			for j < n && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			if j < n && runes[j] == ':' {
				// It's a key — quote it
				buf.WriteRune('"')
				buf.WriteString(word)
				buf.WriteRune('"')
			} else if word == "true" || word == "false" || word == "null" {
				buf.WriteString(word)
			} else {
				// Bare identifier as value — quote it
				buf.WriteRune('"')
				buf.WriteString(word)
				buf.WriteRune('"')
			}

		default:
			buf.WriteRune(ch)
		}
	}

	return buf.String()
}

func isIdentStart(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '$'
}

func isIdentChar(ch rune) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}

// parseFilter converts a relaxed or strict JSON string into bson.M.
func parseFilter(s string) (bson.M, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return bson.M{}, nil
	}
	strict := relaxedJSONToStrict(s)
	var filter bson.M
	if err := json.Unmarshal([]byte(strict), &filter); err != nil {
		return nil, err
	}
	return filter, nil
}
