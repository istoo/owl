package gateway

import "testing"

// M9: text the Chat decoder held to see whether a thinking tag began goes
// out when [DONE] ends the reply, as it does at a finish_reason; a stream
// that gives no finish_reason lost its tail otherwise.
func TestChatDoneGivesTheHeldTail(t *testing.T) {
	d := &chatDecoder{started: true, tool: -1, lead: "tail"}
	var col collector
	if err := d.decode("[DONE]", col.add); err != nil {
		t.Fatal(err)
	}
	res := col.finish()
	if len(res.Parts) != 1 || res.Parts[0].Kind != Text || res.Parts[0].Text != "tail" {
		t.Fatalf("held text after [DONE]: %+v", res.Parts)
	}
}
