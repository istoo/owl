package gateway

import "testing"

// H4: a second function_call's item is added while the first still streams
// its arguments, and the first's later deltas come after the second's
// started. The first stays open: its deltas are its own, not the second's,
// and both calls come out with their arguments whole.
func TestResponsesInterleavedCallsKeepTheirOwnArgs(t *testing.T) {
	d := &responsesDecoder{}
	var col collector
	lines := []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":` + jsonStr(`{"path":`) + `}`,
		// the second call's item added before the first's arguments end
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"read_file","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_2","delta":` + jsonStr(`{"path":`) + `}`,
		// the first's own tail, after the second began waiting
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":` + jsonStr(` "a.py"}`) + `}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":` + jsonStr(`{"path": "a.py"}`) + `}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":` + jsonStr(`{"path": "a.py"}`) + `}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_2","delta":` + jsonStr(` "b.py"}`) + `}`,
		`{"type":"response.function_call_arguments.done","output_index":1,"item_id":"fc_2","arguments":` + jsonStr(`{"path": "b.py"}`) + `}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"read_file","arguments":` + jsonStr(`{"path": "b.py"}`) + `}}`,
		responsesCompleted(),
	}
	for _, l := range lines {
		if err := d.decode(l, col.add); err != nil {
			t.Fatal(err)
		}
	}
	res := col.finish()
	want := map[string]string{"call_1": `{"path": "a.py"}`, "call_2": `{"path": "b.py"}`}
	seen := 0
	for _, p := range res.Parts {
		if p.Kind != ToolCall {
			continue
		}
		if want[p.ID] != string(p.Args) {
			t.Errorf("call %s args %s, want %s", p.ID, p.Args, want[p.ID])
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("%d calls: %+v", seen, res.Parts)
	}
}
