package mjson

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ParseDoc parses a document written in mongo-shell syntax (unquoted keys,
// single quotes, trailing commas, ObjectId(), ISODate(), regex literals, …)
// or Extended JSON. Field order and numeric types are preserved: integers
// become int32 (or int64 when they don't fit), decimals become doubles.
func ParseDoc(s string) (bson.D, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return bson.D{}, nil
	}
	j, err := ToExtJSON(s)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(j), false, &d); err != nil {
		return nil, fmt.Errorf("invalid document: %w", err)
	}
	return d, nil
}

// ParseArgs parses a comma-separated shell argument list such as
// `{a: 1}, {b: 1}` or `"field", {x: 1}`.
func ParseArgs(s string) (bson.A, error) {
	if strings.TrimSpace(s) == "" {
		return bson.A{}, nil
	}
	j, err := ToExtJSON("[" + s + "]")
	if err != nil {
		return nil, err
	}
	var a bson.A
	if err := bson.UnmarshalExtJSON([]byte(j), false, &a); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return a, nil
}

// ToExtJSON converts mongo-shell syntax to relaxed Extended JSON text.
func ToExtJSON(input string) (string, error) {
	c := &converter{src: []rune(input)}
	if err := c.run(); err != nil {
		return "", err
	}
	return string(c.out), nil
}

type converter struct {
	src []rune
	i   int
	out []byte
}

func (c *converter) peek(off int) rune {
	if c.i+off < len(c.src) {
		return c.src[c.i+off]
	}
	return 0
}

func (c *converter) write(s string) { c.out = append(c.out, s...) }

func (c *converter) lastSignificant() byte {
	for j := len(c.out) - 1; j >= 0; j-- {
		if !isSpaceByte(c.out[j]) {
			return c.out[j]
		}
	}
	return 0
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// valuePosition reports whether a value (rather than a key or operator) may
// start here, used to tell regex literals apart from other slashes.
func (c *converter) valuePosition() bool {
	switch c.lastSignificant() {
	case 0, ':', '[', ',':
		return true
	}
	return false
}

func (c *converter) run() error {
	for c.i < len(c.src) {
		ch := c.src[c.i]
		switch {
		case ch == '/' && c.peek(1) == '/':
			for c.i < len(c.src) && c.src[c.i] != '\n' {
				c.i++
			}
		case ch == '/' && c.peek(1) == '*':
			c.i += 2
			for c.i < len(c.src) && (c.src[c.i] != '*' || c.peek(1) != '/') {
				c.i++
			}
			if c.i >= len(c.src) {
				return fmt.Errorf("unterminated comment")
			}
			c.i += 2
		case ch == '"' || ch == '\'':
			s, err := c.readString()
			if err != nil {
				return err
			}
			c.write(Quote(s))
		case ch == '/' && c.valuePosition():
			if err := c.readRegex(); err != nil {
				return err
			}
		case ch == '}' || ch == ']':
			c.trimTrailingComma()
			c.out = append(c.out, byte(ch))
			c.i++
		case ch >= '0' && ch <= '9':
			start := c.i
			for c.i < len(c.src) {
				r := c.src[c.i]
				if (r >= '0' && r <= '9') || r == '.' || r == 'e' || r == 'E' ||
					((r == '+' || r == '-') && (c.src[c.i-1] == 'e' || c.src[c.i-1] == 'E')) {
					c.i++
					continue
				}
				break
			}
			c.write(string(c.src[start:c.i]))
		case isIdentStart(ch):
			if err := c.ident(); err != nil {
				return err
			}
		default:
			c.write(string(ch))
			c.i++
		}
	}
	return nil
}

func (c *converter) trimTrailingComma() {
	for j := len(c.out) - 1; j >= 0; j-- {
		if isSpaceByte(c.out[j]) {
			continue
		}
		if c.out[j] == ',' {
			c.out = append(c.out[:j], c.out[j+1:]...)
		}
		return
	}
}

// readString reads a single- or double-quoted JS string starting at c.i and
// returns its decoded value.
func (c *converter) readString() (string, error) {
	q := c.src[c.i]
	c.i++
	var b strings.Builder
	for c.i < len(c.src) {
		ch := c.src[c.i]
		if ch == q {
			c.i++
			return b.String(), nil
		}
		if ch == '\\' && c.i+1 < len(c.src) {
			c.i++
			esc := c.src[c.i]
			switch esc {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case 'r':
				b.WriteRune('\r')
			case 'b':
				b.WriteRune('\b')
			case 'f':
				b.WriteRune('\f')
			case 'v':
				b.WriteRune('\v')
			case '0':
				b.WriteRune(0)
			case 'u', 'x':
				n := 4
				if esc == 'x' {
					n = 2
				}
				if c.i+n >= len(c.src) {
					return "", fmt.Errorf("invalid escape in string")
				}
				v, err := strconv.ParseUint(string(c.src[c.i+1:c.i+1+n]), 16, 32)
				if err != nil {
					return "", fmt.Errorf("invalid escape in string")
				}
				b.WriteRune(rune(v))
				c.i += n
			default:
				b.WriteRune(esc)
			}
			c.i++
			continue
		}
		b.WriteRune(ch)
		c.i++
	}
	return "", fmt.Errorf("unterminated string")
}

func (c *converter) readRegex() error {
	c.i++ // opening slash
	var pat strings.Builder
	inClass := false
	for {
		if c.i >= len(c.src) || c.src[c.i] == '\n' {
			return fmt.Errorf("unterminated regular expression")
		}
		ch := c.src[c.i]
		if ch == '\\' && c.i+1 < len(c.src) {
			if c.src[c.i+1] == '/' {
				pat.WriteRune('/')
			} else {
				pat.WriteRune(ch)
				pat.WriteRune(c.src[c.i+1])
			}
			c.i += 2
			continue
		}
		if ch == '[' {
			inClass = true
		} else if ch == ']' {
			inClass = false
		} else if ch == '/' && !inClass {
			c.i++
			break
		}
		pat.WriteRune(ch)
		c.i++
	}
	start := c.i
	for c.i < len(c.src) && unicode.IsLetter(c.src[c.i]) {
		c.i++
	}
	c.writeRegex(pat.String(), string(c.src[start:c.i]))
	return nil
}

func (c *converter) writeRegex(pattern, flags string) {
	c.write(`{"$regularExpression": {"pattern": ` + Quote(pattern) + `, "options": ` + Quote(sortedFlags(flags)) + `}}`)
}

func isIdentStart(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '$'
}

func isIdentChar(ch rune) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}

func (c *converter) readIdent() string {
	start := c.i
	for c.i < len(c.src) && isIdentChar(c.src[c.i]) {
		c.i++
	}
	return string(c.src[start:c.i])
}

func (c *converter) skipSpace() {
	for c.i < len(c.src) && unicode.IsSpace(c.src[c.i]) {
		c.i++
	}
}

func (c *converter) nextNonSpace() rune {
	j := c.i
	for j < len(c.src) && unicode.IsSpace(c.src[j]) {
		j++
	}
	if j < len(c.src) {
		return c.src[j]
	}
	return 0
}

func (c *converter) ident() error {
	word := c.readIdent()
	if c.nextNonSpace() == ':' {
		c.write(Quote(word))
		return nil
	}
	if word == "new" {
		c.skipSpace()
		word = c.readIdent()
		if word == "" {
			return fmt.Errorf("expected constructor after 'new'")
		}
		if c.nextNonSpace() != '(' {
			return c.helper(word, nil)
		}
	}
	if c.nextNonSpace() == '(' {
		c.skipSpace()
		args, err := c.readCallArgs()
		if err != nil {
			return fmt.Errorf("%s(): %w", word, err)
		}
		return c.helper(word, args)
	}
	switch word {
	case "true", "false", "null":
		c.write(word)
	case "undefined":
		c.write(`{"$undefined": true}`)
	case "MinKey", "MaxKey":
		return c.helper(word, nil)
	case "NaN":
		c.write(`{"$numberDouble": "NaN"}`)
	case "Infinity":
		if n := len(c.out); n > 0 && c.out[n-1] == '-' {
			c.out = c.out[:n-1]
			c.write(`{"$numberDouble": "-Infinity"}`)
		} else {
			c.write(`{"$numberDouble": "Infinity"}`)
		}
	default:
		// Bare identifiers used as values are treated as strings.
		c.write(Quote(word))
	}
	return nil
}

type callArg struct {
	s      string
	quoted bool
}

func (c *converter) readCallArgs() ([]callArg, error) {
	c.i++ // '('
	var args []callArg
	for {
		c.skipSpace()
		if c.i >= len(c.src) {
			return nil, fmt.Errorf("missing ')'")
		}
		if c.src[c.i] == ')' {
			c.i++
			return args, nil
		}
		if len(args) > 0 {
			if c.src[c.i] != ',' {
				return nil, fmt.Errorf("expected ',' between arguments")
			}
			c.i++
			c.skipSpace()
		}
		if c.i < len(c.src) && (c.src[c.i] == '"' || c.src[c.i] == '\'') {
			s, err := c.readString()
			if err != nil {
				return nil, err
			}
			args = append(args, callArg{s: s, quoted: true})
			continue
		}
		start := c.i
		for c.i < len(c.src) && c.src[c.i] != ',' && c.src[c.i] != ')' {
			c.i++
		}
		raw := strings.TrimSpace(string(c.src[start:c.i]))
		if raw == "" {
			return nil, fmt.Errorf("empty argument")
		}
		args = append(args, callArg{s: raw})
	}
}

func argCount(name string, args []callArg, min, max int) error {
	if len(args) < min || len(args) > max {
		if min == max {
			return fmt.Errorf("%s() takes %d argument(s)", name, min)
		}
		return fmt.Errorf("%s() takes %d to %d arguments", name, min, max)
	}
	return nil
}

func (c *converter) helper(name string, args []callArg) error {
	switch name {
	case "ObjectId", "ObjectID":
		if err := argCount(name, args, 0, 1); err != nil {
			return err
		}
		if len(args) == 0 {
			c.write(`{"$oid": "` + primitive.NewObjectID().Hex() + `"}`)
			return nil
		}
		oid, err := primitive.ObjectIDFromHex(args[0].s)
		if err != nil {
			return fmt.Errorf("ObjectId(%q): must be 24 hex characters", args[0].s)
		}
		c.write(`{"$oid": "` + oid.Hex() + `"}`)
	case "ISODate", "Date":
		if err := argCount(name, args, 0, 1); err != nil {
			return err
		}
		t := time.Now()
		if len(args) == 1 {
			var err error
			if t, err = parseDateArg(args[0]); err != nil {
				return fmt.Errorf("%s(%q): %w", name, args[0].s, err)
			}
		}
		c.write(fmt.Sprintf(`{"$date": {"$numberLong": "%d"}}`, t.UnixMilli()))
	case "NumberLong", "Long":
		if err := argCount(name, args, 1, 1); err != nil {
			return err
		}
		n, err := strconv.ParseInt(args[0].s, 10, 64)
		if err != nil {
			return fmt.Errorf("%s(%q): not a 64-bit integer", name, args[0].s)
		}
		c.write(fmt.Sprintf(`{"$numberLong": "%d"}`, n))
	case "NumberInt", "Int32":
		if err := argCount(name, args, 1, 1); err != nil {
			return err
		}
		n, err := strconv.ParseInt(args[0].s, 10, 32)
		if err != nil {
			return fmt.Errorf("%s(%q): not a 32-bit integer", name, args[0].s)
		}
		c.write(fmt.Sprintf(`{"$numberInt": "%d"}`, n))
	case "NumberDecimal", "Decimal128":
		if err := argCount(name, args, 1, 1); err != nil {
			return err
		}
		if _, err := primitive.ParseDecimal128(args[0].s); err != nil {
			return fmt.Errorf("%s(%q): not a decimal", name, args[0].s)
		}
		c.write(`{"$numberDecimal": ` + Quote(args[0].s) + `}`)
	case "Double":
		if err := argCount(name, args, 1, 1); err != nil {
			return err
		}
		f, err := strconv.ParseFloat(args[0].s, 64)
		if err != nil {
			return fmt.Errorf("Double(%q): not a number", args[0].s)
		}
		c.write(`{"$numberDouble": "` + strconv.FormatFloat(f, 'g', -1, 64) + `"}`)
	case "UUID":
		if err := argCount(name, args, 0, 1); err != nil {
			return err
		}
		var raw []byte
		if len(args) == 0 {
			raw = make([]byte, 16)
			_, _ = rand.Read(raw)
			raw[6] = raw[6]&0x0f | 0x40
			raw[8] = raw[8]&0x3f | 0x80
		} else {
			var err error
			raw, err = hex.DecodeString(strings.ReplaceAll(args[0].s, "-", ""))
			if err != nil || len(raw) != 16 {
				return fmt.Errorf("UUID(%q): invalid UUID", args[0].s)
			}
		}
		c.write(`{"$binary": {"base64": "` + base64.StdEncoding.EncodeToString(raw) + `", "subType": "04"}}`)
	case "BinData":
		if err := argCount(name, args, 2, 2); err != nil {
			return err
		}
		sub, err := strconv.ParseUint(args[0].s, 10, 8)
		if err != nil {
			return fmt.Errorf("BinData: invalid subtype %q", args[0].s)
		}
		if _, err := base64.StdEncoding.DecodeString(args[1].s); err != nil {
			return fmt.Errorf("BinData: invalid base64")
		}
		c.write(fmt.Sprintf(`{"$binary": {"base64": %s, "subType": "%02x"}}`, Quote(args[1].s), sub))
	case "Timestamp":
		if err := argCount(name, args, 2, 2); err != nil {
			return err
		}
		t, err1 := strconv.ParseUint(args[0].s, 10, 32)
		i, err2 := strconv.ParseUint(args[1].s, 10, 32)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("Timestamp(t, i): arguments must be unsigned 32-bit integers")
		}
		c.write(fmt.Sprintf(`{"$timestamp": {"t": %d, "i": %d}}`, t, i))
	case "MinKey":
		c.write(`{"$minKey": 1}`)
	case "MaxKey":
		c.write(`{"$maxKey": 1}`)
	case "RegExp":
		if err := argCount(name, args, 1, 2); err != nil {
			return err
		}
		flags := ""
		if len(args) == 2 {
			flags = args[1].s
		}
		c.writeRegex(args[0].s, flags)
	default:
		return fmt.Errorf("unsupported shell helper %s()", name)
	}
	return nil
}

var dateLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04",
	"2006-01-02",
}

func parseDateArg(a callArg) (time.Time, error) {
	if !a.quoted {
		ms, err := strconv.ParseFloat(a.s, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("expected a date string or milliseconds")
		}
		return time.UnixMilli(int64(ms)), nil
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, a.s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date format")
}
