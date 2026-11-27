package gateway

import "testing"

// C1: the gateway's own http.Server marks each connection with the OS
// user its peer runs as (peerUserContext). Without it sameOSUser is
// fail-open, and another user on this computer's loopback is answered as
// the owner.
func TestGatewayServerMarksThePeerUser(t *testing.T) {
	srv := New().gatewayServer()
	if srv.ConnContext == nil {
		t.Fatal("the gateway's http.Server has no ConnContext: another OS user on loopback is the owner")
	}
	if srv.Handler == nil || srv.ConnState == nil {
		t.Fatalf("the server lost what it serves with: %+v", srv)
	}
}
