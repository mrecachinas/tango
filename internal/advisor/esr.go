package advisor

import (
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Key is one field of an index key pattern. Dir is 1 or -1 for ordinary
// keys; Special holds the type of text/2dsphere/hashed/wildcard keys.
type Key struct {
	Field   string
	Dir     int
	Special string
}

// Index is the subset of a listIndexes spec the advisor reasons about.
type Index struct {
	Name                                 string
	Keys                                 []Key
	Unique, Sparse, Partial, TTL, Hidden bool
	Collation, Special                   bool
}

// ParseIndex reads a listIndexes document.
func ParseIndex(spec bson.D) Index {
	ix := Index{
		Name:      getStr(spec, "name"),
		Unique:    getBool(spec, "unique"),
		Sparse:    getBool(spec, "sparse"),
		Partial:   get(spec, "partialFilterExpression") != nil,
		Hidden:    getBool(spec, "hidden"),
		Collation: get(spec, "collation") != nil,
	}
	_, ix.TTL = getNum(spec, "expireAfterSeconds")
	ix.Keys = parseKeys(get(spec, "key"))
	for _, k := range ix.Keys {
		if k.Special != "" {
			ix.Special = true
		}
	}
	return ix
}

// parseKeys reads a key pattern; Mongo 8's EXPRESS stages report it as a
// "{ _id: 1 }" string, which is parsed loosely.
func parseKeys(v interface{}) []Key {
	if s, ok := v.(string); ok {
		var keys []Key
		for _, part := range strings.Split(strings.Trim(strings.TrimSpace(s), "{}"), ",") {
			field, val, ok := strings.Cut(part, ":")
			if !ok {
				continue
			}
			k := Key{Field: strings.Trim(strings.TrimSpace(field), `"'`), Dir: 1}
			val = strings.TrimSpace(val)
			if strings.HasPrefix(val, "-") {
				k.Dir = -1
			} else if strings.ContainsAny(val, `"'`) {
				k.Special, k.Dir = strings.Trim(val, `"'`), 0
			}
			keys = append(keys, k)
		}
		return keys
	}
	var keys []Key
	for _, e := range toD(v) {
		k := Key{Field: e.Key, Dir: 1}
		if n, ok := num(e.Value); ok {
			if n < 0 {
				k.Dir = -1
			}
		} else {
			k.Dir = 0
			k.Special, _ = e.Value.(string)
			if k.Special == "" {
				k.Special = "special"
			}
		}
		if strings.Contains(k.Field, "$**") {
			k.Special = "wildcard"
		}
		keys = append(keys, k)
	}
	return keys
}

// KeysDoc renders keys as an index key pattern document.
func KeysDoc(keys []Key) bson.D {
	d := make(bson.D, 0, len(keys))
	for _, k := range keys {
		if k.Special != "" && k.Dir == 0 {
			d = append(d, bson.E{Key: k.Field, Value: k.Special})
		} else {
			d = append(d, bson.E{Key: k.Field, Value: int32(k.Dir)})
		}
	}
	return d
}

// KeysString renders keys like {a: 1, b: -1}.
func KeysString(keys []Key) string { return compact(KeysDoc(keys), 0) }

func (ix Index) usable(needsFilter bool) bool {
	if ix.Hidden || ix.Partial || ix.Special || ix.Collation {
		return false
	}
	return !ix.Sparse || needsFilter
}

// IsPrefixOf reports whether ix's keys lead other's, in the same or fully
// reversed direction, so other can serve every query ix serves.
func (ix Index) IsPrefixOf(other Index) bool {
	if len(ix.Keys) == 0 || len(ix.Keys) > len(other.Keys) {
		return false
	}
	flip := 0
	for i, k := range ix.Keys {
		o := other.Keys[i]
		if k.Field != o.Field || k.Special != o.Special {
			return false
		}
		if k.Special != "" {
			continue
		}
		rel := k.Dir * o.Dir
		if flip == 0 {
			flip = rel
		} else if rel != flip {
			return false
		}
	}
	return true
}

type class int

const (
	clsLow class = iota + 1
	clsRange
	clsIn
	clsEq
)

type preds struct {
	order []string
	cls   map[string]class
	notes []string
	or    bson.A
}

func (p *preds) add(field string, c class) {
	if c == 0 {
		return
	}
	if old, ok := p.cls[field]; ok {
		if c > old {
			p.cls[field] = c
		}
		return
	}
	p.cls[field] = c
	p.order = append(p.order, field)
}

func (p *preds) note(n string) {
	for _, existing := range p.notes {
		if existing == n {
			return
		}
	}
	p.notes = append(p.notes, n)
}

var anchoredRe = regexp.MustCompile(`^(\^|\\A)[^|]*$`)

func regexClass(pattern, options string) class {
	if anchoredRe.MatchString(pattern) && !strings.Contains(options, "i") {
		return clsRange
	}
	return clsLow
}

func (p *preds) collect(filter bson.D) {
	for _, e := range filter {
		switch e.Key {
		case "$and":
			for _, sub := range asDocs(toArr(e.Value)) {
				p.collect(sub)
			}
		case "$or":
			if p.or == nil {
				p.or = toArr(e.Value)
			} else {
				p.note("Nested $or branches are not analyzed; each branch needs its own index.")
			}
		case "$nor":
			p.note("$nor predicates are low-selectivity and rarely use an index well.")
		case "$text":
			p.note("$text requires a text index; it can't be combined into a regular compound index.")
		case "$expr":
			p.note("$expr only uses indexes for simple $eq/$lt/$lte/$gt/$gte comparisons against constants.")
		case "$where":
			p.note("$where runs JavaScript per document and can't use an index.")
		case "$comment", "$jsonSchema", "$sampleRate":
		default:
			if strings.HasPrefix(e.Key, "$") {
				continue
			}
			p.add(e.Key, p.classify(e.Key, e.Value))
		}
	}
}

func toArr(v interface{}) bson.A {
	switch a := v.(type) {
	case bson.A:
		return a
	case []interface{}:
		return a
	}
	return nil
}

func (p *preds) classify(field string, v interface{}) class {
	switch x := v.(type) {
	case primitive.Regex:
		return regexClass(x.Pattern, x.Options)
	case bson.D, bson.M:
		d := toD(x)
		if len(d) == 0 || !strings.HasPrefix(d[0].Key, "$") {
			return clsEq
		}
		best := class(0)
		options := getStr(d, "$options")
		for _, op := range d {
			c := clsLow
			switch op.Key {
			case "$eq", "$all", "$elemMatch":
				c = clsEq
			case "$in":
				if len(toArr(op.Value)) <= 1 {
					c = clsEq
				} else {
					c = clsIn
				}
			case "$gt", "$gte", "$lt", "$lte", "$type":
				c = clsRange
			case "$exists":
				if b, ok := op.Value.(bool); !ok || b {
					c = clsRange
				}
			case "$regex":
				switch r := op.Value.(type) {
				case string:
					c = regexClass(r, options)
				case primitive.Regex:
					c = regexClass(r.Pattern, r.Options+options)
				}
			case "$options", "$comment":
				continue
			case "$near", "$nearSphere", "$geoWithin", "$geoIntersects":
				p.note("Geospatial operators on " + field + " need a 2dsphere/2d index.")
				return 0
			}
			if c > best {
				best = c
			}
		}
		return best
	}
	return clsEq
}

// Suggestion is an index shaped by the ESR rule: equality fields first, then
// sort fields in order, then range fields, then low-selectivity predicates.
type Suggestion struct {
	Equality []string
	Sort     []Key
	Range    []string
	Low      []string
	Notes    []string
	// Extends names an existing index that is a prefix of this one and could
	// be dropped once this index is built.
	Extends string
}

// Keys returns the suggested key pattern.
func (s Suggestion) Keys() []Key {
	var keys []Key
	for _, f := range s.Equality {
		keys = append(keys, Key{Field: f, Dir: 1})
	}
	keys = append(keys, s.Sort...)
	for _, f := range s.Range {
		keys = append(keys, Key{Field: f, Dir: 1})
	}
	for _, f := range s.Low {
		keys = append(keys, Key{Field: f, Dir: 1})
	}
	return keys
}

// Rationale explains the key order.
func (s Suggestion) Rationale() string {
	var parts []string
	if len(s.Equality) > 0 {
		parts = append(parts, "equality "+strings.Join(s.Equality, ", "))
	}
	if len(s.Sort) > 0 {
		var fs []string
		for _, k := range s.Sort {
			arrow := "↑"
			if k.Dir < 0 {
				arrow = "↓"
			}
			fs = append(fs, k.Field+" "+arrow)
		}
		parts = append(parts, "sort "+strings.Join(fs, ", "))
	}
	if len(s.Range) > 0 {
		parts = append(parts, "range "+strings.Join(s.Range, ", "))
	}
	if len(s.Low) > 0 {
		parts = append(parts, "then "+strings.Join(s.Low, ", ")+" (low selectivity)")
	}
	return strings.Join(parts, " → ")
}

// Suggest derives index suggestions for a filter and sort. A top-level $or
// yields one suggestion per branch, since each branch is planned separately.
func Suggest(filter, sort bson.D) []Suggestion {
	p := &preds{cls: map[string]class{}}
	p.collect(filter)
	if p.or != nil {
		var rest bson.D
		for _, e := range filter {
			if e.Key != "$or" {
				rest = append(rest, e)
			}
		}
		var out []Suggestion
		for _, branch := range asDocs(p.or) {
			merged := append(append(bson.D{}, rest...), bson.E{Key: "$and", Value: bson.A{branch}})
			for _, s := range Suggest(merged, sort) {
				s.Notes = mergeNotes(s.Notes, p.notes)
				out = appendUnique(out, s)
			}
		}
		return out
	}
	if s, ok := build(p, sort); ok {
		return []Suggestion{s}
	}
	return nil
}

func mergeNotes(a, b []string) []string {
	for _, n := range b {
		found := false
		for _, m := range a {
			found = found || m == n
		}
		if !found {
			a = append(a, n)
		}
	}
	return a
}

func appendUnique(list []Suggestion, s Suggestion) []Suggestion {
	key := KeysString(s.Keys())
	for _, existing := range list {
		if KeysString(existing.Keys()) == key {
			return list
		}
	}
	return append(list, s)
}

func build(p *preds, sort bson.D) (Suggestion, bool) {
	s := Suggestion{Notes: p.notes}
	var ins []string
	for _, f := range p.order {
		switch p.cls[f] {
		case clsEq:
			s.Equality = append(s.Equality, f)
		case clsIn:
			ins = append(ins, f)
		case clsRange:
			s.Range = append(s.Range, f)
		case clsLow:
			s.Low = append(s.Low, f)
		}
	}
	for _, f := range s.Equality {
		if f == "_id" {
			return s, false
		}
	}
	isEq := map[string]bool{}
	for _, f := range s.Equality {
		isEq[f] = true
	}
	inSort := map[string]bool{}
	for _, e := range sort {
		if e.Key == "$natural" || isEq[e.Key] {
			continue
		}
		n, ok := num(e.Value)
		if !ok {
			continue // {$meta: "textScore"}
		}
		dir := 1
		if n < 0 {
			dir = -1
		}
		s.Sort = append(s.Sort, Key{Field: e.Key, Dir: dir})
		inSort[e.Key] = true
	}
	// Without a sort, a multi-value $in is a set of equality seeks; with one,
	// it fans out like a range and would break the index's sort order.
	if len(s.Sort) == 0 {
		s.Equality = append(s.Equality, ins...)
	} else {
		s.Range = append(ins, s.Range...)
	}
	s.Range = without(s.Range, inSort)
	s.Low = without(s.Low, inSort)
	if len(s.Equality)+len(s.Sort)+len(s.Range)+len(s.Low) == 0 {
		return s, false
	}
	return s, true
}

func without(fields []string, drop map[string]bool) []string {
	var out []string
	for _, f := range fields {
		if !drop[f] {
			out = append(out, f)
		}
	}
	return out
}

// prefixMatch returns how many leading keys of idx are consistent with the
// suggestion: equality and range groups match as sets, sort fields in order
// with one consistent direction (an index can be walked backwards).
func (s Suggestion) prefixMatch(idx []Key) int {
	pos := 0
	set := func(fields []string) bool {
		remaining := map[string]bool{}
		for _, f := range fields {
			remaining[f] = true
		}
		for len(remaining) > 0 {
			if pos >= len(idx) || !remaining[idx[pos].Field] || idx[pos].Special != "" {
				return false
			}
			delete(remaining, idx[pos].Field)
			pos++
		}
		return true
	}
	if !set(s.Equality) {
		return pos
	}
	flip := 0
	for _, k := range s.Sort {
		if pos >= len(idx) || idx[pos].Field != k.Field || idx[pos].Special != "" {
			return pos
		}
		rel := idx[pos].Dir * k.Dir
		if flip == 0 {
			flip = rel
		} else if rel != flip {
			return pos
		}
		pos++
	}
	if !set(s.Range) {
		return pos
	}
	set(s.Low)
	return pos
}

// Against compares the suggestion with existing indexes. satisfied names an
// index that already serves the query shape; otherwise Extends is set when
// an existing non-unique index is a prefix of the suggestion.
func (s Suggestion) Against(indexes []Index) (satisfied string, out Suggestion) {
	out = s
	essential := len(s.Equality) + len(s.Sort) + len(s.Range)
	total := essential + len(s.Low)
	if essential == 0 {
		essential = total
	}
	needsFilter := len(s.Equality)+len(s.Range)+len(s.Low) > 0
	best := 0
	for _, ix := range indexes {
		if !ix.usable(needsFilter) {
			continue
		}
		n := s.prefixMatch(ix.Keys)
		if n >= essential {
			return ix.Name, s
		}
		if n == len(ix.Keys) && n > best && !ix.Unique && ix.Name != "_id_" {
			best, out.Extends = n, ix.Name
		}
	}
	return "", out
}
