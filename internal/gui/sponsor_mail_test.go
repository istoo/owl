package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The Add sheet's "Become a sponsor" opens a mail to the owner, the same
// one usemagpie.ai's link writes; /api/open opens that one mailto: link,
// not any address a page names.
func TestSponsorMailOpensThatAddressOnly(t *testing.T) {
	sandboxHome(t)
	w := &importWindows{}
	mux := http.NewServeMux()
	providerRoutes(mux, w)
	for _, link := range []string{
		sponsorMail,
		"mailto:someone@example.com",
		"mailto:yetoneful@gmail.com",
		sponsorMail + "&body=hello",
		"MAILTO:yetoneful@gmail.com?subject=Sponsoring%20owl",
	} {
		w.opened = ""
		body, _ := json.Marshal(map[string]string{"url": link})
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/open", strings.NewReader(string(body))))
		if want := link == sponsorMail; (w.opened == link) != want {
			t.Errorf("%s: opened %q", link, w.opened)
		}
	}

	// the page asks for the same link the server lets through. v0.2.27's
	// page has no "Become a sponsor" row (that entry is later than it
	// ships), so the rule here is: any mailto: link it does name is this
	// one, never another address.
	app, err := os.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`mailto:[^"'\\\s)]*`).FindAllString(string(app), -1) {
		if m != sponsorMail {
			t.Errorf("app.js opens %s, not just %s", m, sponsorMail)
		}
	}
	site, err := os.ReadFile("../../site/public/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(site), `href="`+sponsorMail+`"`) {
		t.Errorf("usemagpie.ai's sponsor link is no longer %s", sponsorMail)
	}
}
