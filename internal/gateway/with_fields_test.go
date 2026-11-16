package gateway

import (
	"bytes"
	"encoding/json"
	"testing"
)

// withFieldsOld is what withFields did while it decoded the whole body:
// kept here, its only caller is the test below, to hold the new one to
// its bytes.
func withFieldsOld(body []byte, fields map[string]any) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return body
	}
	for k, v := range fields {
		m[k] = v
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(m) != nil {
		return body
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}

// The bytes withFields writes are json.Encoder's, as they were when it
// decoded and encoded the whole body: compact, its keys sorted, every
// string written as encoding/json writes it, and the rest copied.
func TestWithFieldsKeepsItsBytes(t *testing.T) {
	bodies := []string{
		`{}`,
		`{"model":"m","stream":false}`,
		` { "z" : 1 , "a" : 2 } `,
		`{"b":{"d":1,"c":[3,2,{"y":true,"x":null}]},"a":"s"}`,
		`{"k":"a\u0041b","l":"\/","m":"\b\f\n\r\t","n":"\u2028\u2029","o":"<>&","p":"\u00e9","q":"\ud83d\ude00","r":"\ud800"}`,
		"{\"s\":\"raw \u2028 and \u2029\",\"t\":\"\xff\xfe\"}",
		`{"num":1e3,"neg":-0,"dec":1.0,"exp":1E+2,"big":12345678901234567890}`,
		`{"empty":{},"list":[],"nested":[[[]]]}`,
		`{"a":1,"a":2,"b":3}`,
		`{"dup":{"k":1,"k":2}}`,
		`{"\u0061\u0062":1,"a b":2,"\"":3,"\\":4}`,
		`{"a":1,"str":"<script>&\"x\"\\y"}`,
		`{"a":1,"deep":{"a":{"b":{"c":{"d":[{"e":1}]}}}}}`,
		`{"a" : 1 , "unicode" : "héllo 世界"}`,
		/* invalid, an array, and a scalar: the old path answers */
		`not json`,
		`{"a":1} trailing`,
		`[1,2,3]`,
		`"just a string"`,
		`12`,
	}
	changes := []map[string]any{
		{"stream": true},
		{"a": "replaced"},
		{"o": map[string]any{"z": 1, "a": []any{"<x>", 2}}},
		{"q": json.RawMessage(`{"b": 1, "a": 2}`)},
		{"num": 42.5, "b": true},
	}
	for _, body := range bodies {
		for _, fields := range changes {
			want := withFieldsOld([]byte(body), fields)
			got := withFields([]byte(body), fields)
			if !bytes.Equal(got, want) {
				t.Errorf("body %q, fields %v:\n got %s\nwant %s", body, fields, got, want)
			}
		}
	}
}

func FuzzWithFieldsKeepsItsBytes(f *testing.F) {
	f.Add([]byte(`{"model":"m","stream":false,"messages":[{"role":"user","content":"hi\n"}]}`), "stream", "true")
	f.Add([]byte(`{"a":1,"a":2,"u":"\u0041","bad":"\xff"}`), "a", "x")
	f.Add([]byte(` { "z" : [1, {"y":2,"x":3}] } `), "z", "3")
	f.Fuzz(func(t *testing.T, body []byte, key string, val string) {
		if key == "" {
			return
		}
		fields := map[string]any{key: val}
		want := withFieldsOld(body, fields)
		got := withFields(body, fields)
		if !bytes.Equal(got, want) {
			t.Fatalf("body %q, %q=%v:\n got %s\nwant %s", body, key, val, got, want)
		}
	})
}
