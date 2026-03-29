package query

import (
	"encoding/json"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestRelaxedJSONToStrict(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "unquoted keys",
			input: `{name: "test"}`,
			want:  `{"name": "test"}`,
		},
		{
			name:  "dollar operators",
			input: `{age: {$gt: 25}}`,
			want:  `{"age": {"$gt": 25}}`,
		},
		{
			name:  "single-quoted strings",
			input: `{name: 'test'}`,
			want:  `{"name": "test"}`,
		},
		{
			name:  "boolean and null preserved",
			input: `{active: true, data: null}`,
			want:  `{"active": true, "data": null}`,
		},
		{
			name:  "false preserved",
			input: `{deleted: false}`,
			want:  `{"deleted": false}`,
		},
		{
			name:  "numbers preserved",
			input: `{age: 42, score: 3.14}`,
			want:  `{"age": 42, "score": 3.14}`,
		},
		{
			name:  "nested objects",
			input: `{a: {b: {c: 1}}}`,
			want:  `{"a": {"b": {"c": 1}}}`,
		},
		{
			name:  "arrays",
			input: `{tags: ["a", "b"]}`,
			want:  `{"tags": ["a", "b"]}`,
		},
		{
			name:  "already strict JSON passes through",
			input: `{"name": "test", "age": 42}`,
			want:  `{"name": "test", "age": 42}`,
		},
		{
			name:  "empty object",
			input: `{}`,
			want:  `{}`,
		},
		{
			name:  "mixed quoted and unquoted keys",
			input: `{"name": "Alice", age: 30}`,
			want:  `{"name": "Alice", "age": 30}`,
		},
		{
			name:  "dotted keys",
			input: `{user.name: "test"}`,
			want:  `{"user.name": "test"}`,
		},
		{
			name:  "dollar operator in key",
			input: `{$set: {name: "new"}}`,
			want:  `{"$set": {"name": "new"}}`,
		},
		{
			name:  "single-quoted value with embedded double quote",
			input: `{msg: 'say "hi"'}`,
			want:  `{"msg": "say \"hi\""}`,
		},
		{
			name:  "escaped char in double-quoted string",
			input: `{name: "te\"st"}`,
			want:  `{"name": "te\"st"}`,
		},
		{
			name:  "multiple dollar operators",
			input: `{$and: [{age: {$gte: 18}}, {age: {$lte: 65}}]}`,
			want:  `{"$and": [{"age": {"$gte": 18}}, {"age": {"$lte": 65}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relaxedJSONToStrict(tt.input)
			if got != tt.want {
				t.Errorf("relaxedJSONToStrict(%q)\n  got:  %s\n  want: %s", tt.input, got, tt.want)
			}
			// Verify the output is valid JSON
			var js json.RawMessage
			if err := json.Unmarshal([]byte(got), &js); err != nil {
				t.Errorf("output is not valid JSON: %v", err)
			}
		})
	}
}

func TestParseMongoQuery(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantColl   string
		wantMethod string
		wantArgs   string
		wantErr    bool
	}{
		{
			name:       "basic find",
			input:      `db.users.find({name: "test"})`,
			wantColl:   "users",
			wantMethod: "find",
			wantArgs:   `{name: "test"}`,
		},
		{
			name:       "findOne",
			input:      `db.orders.findOne({})`,
			wantColl:   "orders",
			wantMethod: "findOne",
			wantArgs:   `{}`,
		},
		{
			name:       "count with empty parens",
			input:      `db.logs.count()`,
			wantColl:   "logs",
			wantMethod: "count",
			wantArgs:   "",
		},
		{
			name:       "aggregate",
			input:      `db.users.aggregate([{$match: {}}])`,
			wantColl:   "users",
			wantMethod: "aggregate",
			wantArgs:   `[{$match: {}}]`,
		},
		{
			name:       "nested parens in string args",
			input:      `db.users.find({name: "test()"})`,
			wantColl:   "users",
			wantMethod: "find",
			wantArgs:   `{name: "test()"}`,
		},
		{
			name:       "empty args",
			input:      `db.users.find()`,
			wantColl:   "users",
			wantMethod: "find",
			wantArgs:   "",
		},
		{
			name:    "missing method - no parens",
			input:   `db.users`,
			wantErr: true,
		},
		{
			name:    "missing collection",
			input:   `db.find()`,
			wantErr: true,
		},
		{
			name:       "args with whitespace",
			input:      `db.users.find(  {name: "test"}  )`,
			wantColl:   "users",
			wantMethod: "find",
			wantArgs:   `{name: "test"}`,
		},
		{
			name:       "insertOne",
			input:      `db.items.insertOne({x: 1})`,
			wantColl:   "items",
			wantMethod: "insertOne",
			wantArgs:   `{x: 1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coll, method, args, err := parseMongoQuery(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got coll=%q method=%q args=%q", coll, method, args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if coll != tt.wantColl {
				t.Errorf("collection: got %q, want %q", coll, tt.wantColl)
			}
			if method != tt.wantMethod {
				t.Errorf("method: got %q, want %q", method, tt.wantMethod)
			}
			if args != tt.wantArgs {
				t.Errorf("args: got %q, want %q", args, tt.wantArgs)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bson.M
		wantErr bool
	}{
		{
			name:  "valid strict JSON filter",
			input: `{"name": "test"}`,
			want:  bson.M{"name": "test"},
		},
		{
			name:  "empty string returns empty bson.M",
			input: "",
			want:  bson.M{},
		},
		{
			name:  "whitespace only returns empty bson.M",
			input: "   ",
			want:  bson.M{},
		},
		{
			name:  "relaxed JSON with unquoted keys",
			input: `{name: "test"}`,
			want:  bson.M{"name": "test"},
		},
		{
			name:  "numeric values",
			input: `{age: 42}`,
			want:  bson.M{"age": float64(42)},
		},
		{
			name:  "boolean value",
			input: `{active: true}`,
			want:  bson.M{"active": true},
		},
		{
			name:  "null value",
			input: `{data: null}`,
			want:  bson.M{"data": nil},
		},
		{
			name:  "empty object",
			input: `{}`,
			want:  bson.M{},
		},
		{
			name:    "invalid JSON returns error",
			input:   `{not valid`,
			wantErr: true,
		},
		{
			name:  "bare word value gets quoted",
			input: `{name: test}`,
			want:  bson.M{"name": "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFilter(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseFilter(%q)\n  got:  %v\n  want: %v", tt.input, got, tt.want)
			}
		})
	}
}
