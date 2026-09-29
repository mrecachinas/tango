// Package mjson converts between BSON values and mongo-shell-flavoured JSON:
// strict JSON extended with shell helpers such as ObjectId("…"),
// ISODate("…") and NumberLong(…). The printed form round-trips through
// ParseDoc without losing BSON types or field order.
package mjson

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Kind classifies a printed token for syntax highlighting.
type Kind uint8

const (
	Punct Kind = iota
	Key
	String
	Number
	Bool
	Null
	Helper
	Text
	Error
)

// Token is a run of printed text of a single Kind.
type Token struct {
	Text string
	Kind Kind
}

const isoLayout = "2006-01-02T15:04:05.000Z"

// Tokens prints v (a document, array or scalar) as shell-style JSON tokens.
func Tokens(v interface{}, pretty bool) []Token {
	p := &printer{pretty: pretty}
	p.value(v, 0)
	return p.toks
}

// Pretty prints v as indented shell-style JSON.
func Pretty(v interface{}) string { return Join(Tokens(v, true)) }

// Compact prints v as single-line shell-style JSON.
func Compact(v interface{}) string { return Join(Tokens(v, false)) }

// Join concatenates token text.
func Join(toks []Token) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(t.Text)
	}
	return b.String()
}

// Get returns the value for key in d.
func Get(d bson.D, key string) (interface{}, bool) {
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

type printer struct {
	toks   []Token
	pretty bool
}

func (p *printer) emit(k Kind, s string) {
	if n := len(p.toks); n > 0 && p.toks[n-1].Kind == k && (k == Punct || k == Text) {
		p.toks[n-1].Text += s
		return
	}
	p.toks = append(p.toks, Token{Text: s, Kind: k})
}

func (p *printer) newline(depth int) {
	if p.pretty {
		p.emit(Punct, "\n"+strings.Repeat("  ", depth))
	}
}

func (p *printer) sep() {
	p.emit(Punct, ",")
	if !p.pretty {
		p.emit(Punct, " ")
	}
}

func (p *printer) doc(d bson.D, depth int) {
	if len(d) == 0 {
		p.emit(Punct, "{}")
		return
	}
	p.emit(Punct, "{")
	for i, e := range d {
		if i > 0 {
			p.sep()
		}
		p.newline(depth + 1)
		p.emit(Key, Quote(e.Key))
		p.emit(Punct, ": ")
		p.value(e.Value, depth+1)
	}
	p.newline(depth)
	p.emit(Punct, "}")
}

func (p *printer) array(a []interface{}, depth int) {
	if len(a) == 0 {
		p.emit(Punct, "[]")
		return
	}
	p.emit(Punct, "[")
	for i, v := range a {
		if i > 0 {
			p.sep()
		}
		p.newline(depth + 1)
		p.value(v, depth+1)
	}
	p.newline(depth)
	p.emit(Punct, "]")
}

func (p *printer) value(v interface{}, depth int) {
	switch x := v.(type) {
	case primitive.D:
		p.doc(x, depth)
	case primitive.M:
		p.doc(mapToD(x), depth)
	case map[string]interface{}:
		p.doc(mapToD(x), depth)
	case bson.Raw:
		var d bson.D
		if err := bson.Unmarshal(x, &d); err != nil {
			p.emit(Error, Quote(err.Error()))
			return
		}
		p.doc(d, depth)
	case primitive.A:
		p.array(x, depth)
	case []interface{}:
		p.array(x, depth)
	case string:
		p.emit(String, Quote(x))
	case bool:
		p.emit(Bool, strconv.FormatBool(x))
	case nil, primitive.Null:
		p.emit(Null, "null")
	case int32:
		p.emit(Number, strconv.FormatInt(int64(x), 10))
	case int64:
		p.emit(Helper, "NumberLong("+strconv.FormatInt(x, 10)+")")
	case int:
		if x >= math.MinInt32 && x <= math.MaxInt32 {
			p.emit(Number, strconv.Itoa(x))
		} else {
			p.emit(Helper, "NumberLong("+strconv.Itoa(x)+")")
		}
	case float64:
		p.double(x)
	case float32:
		p.double(float64(x))
	case primitive.ObjectID:
		p.emit(Helper, `ObjectId("`+x.Hex()+`")`)
	case primitive.DateTime:
		p.date(int64(x))
	case time.Time:
		p.date(x.UnixMilli())
	case primitive.Decimal128:
		p.emit(Helper, `NumberDecimal("`+x.String()+`")`)
	case primitive.Binary:
		p.binary(x)
	case primitive.Regex:
		p.regex(x)
	case primitive.Timestamp:
		p.emit(Helper, fmt.Sprintf("Timestamp(%d, %d)", x.T, x.I))
	case primitive.MinKey:
		p.emit(Helper, "MinKey()")
	case primitive.MaxKey:
		p.emit(Helper, "MaxKey()")
	default:
		p.emit(Helper, extJSON(v))
	}
}

func (p *printer) double(f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		p.emit(Helper, extJSON(f))
		return
	}
	abs := math.Abs(f)
	var s string
	if abs == 0 || (abs >= 1e-6 && abs < 1e21) {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	} else {
		s = strconv.FormatFloat(f, 'g', -1, 64)
	}
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	} else if i := strings.IndexAny(s, "eE"); i >= 0 && !strings.Contains(s[:i], ".") {
		s = s[:i] + ".0" + s[i:]
	}
	p.emit(Number, s)
}

func (p *printer) date(ms int64) {
	t := time.UnixMilli(ms).UTC()
	if t.Year() < 0 || t.Year() > 9999 {
		p.emit(Helper, fmt.Sprintf(`{"$date": {"$numberLong": "%d"}}`, ms))
		return
	}
	p.emit(Helper, `ISODate("`+t.Format(isoLayout)+`")`)
}

func (p *printer) binary(b primitive.Binary) {
	if b.Subtype == 4 && len(b.Data) == 16 {
		h := hex.EncodeToString(b.Data)
		p.emit(Helper, fmt.Sprintf(`UUID("%s-%s-%s-%s-%s")`, h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]))
		return
	}
	p.emit(Helper, fmt.Sprintf(`BinData(%d, "%s")`, b.Subtype, base64.StdEncoding.EncodeToString(b.Data)))
}

func (p *printer) regex(r primitive.Regex) {
	opts := sortedFlags(r.Options)
	if r.Pattern == "" || strings.ContainsAny(r.Pattern, "/\n\r") {
		p.emit(Helper, fmt.Sprintf(`{"$regularExpression": {"pattern": %s, "options": %s}}`, Quote(r.Pattern), Quote(opts)))
		return
	}
	p.emit(Helper, "/"+r.Pattern+"/"+opts)
}

func sortedFlags(f string) string {
	b := []byte(f)
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	return string(b)
}

// extJSON renders any BSON value as canonical Extended JSON.
func extJSON(v interface{}) string {
	b, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}}, true, false)
	if err != nil {
		return Quote(fmt.Sprintf("%v", v))
	}
	s := string(b)
	return strings.TrimSuffix(strings.TrimPrefix(s, `{"v":`), "}")
}

func mapToD(m map[string]interface{}) bson.D {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	d := make(bson.D, 0, len(m))
	for _, k := range keys {
		d = append(d, bson.E{Key: k, Value: m[k]})
	}
	return d
}

// Quote returns s as a JSON string literal without HTML escaping.
func Quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
