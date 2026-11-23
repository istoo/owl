package gui

import (
	"testing"

	"github.com/istoo/owl/internal/plugin"
)

// a market icon name no bundled picture has is left out, so the page draws
// its glyph and never asks for icons/<name>.svg (warp: 404 on every open)
func TestListingIconsDropNamesWithNoBundledPicture(t *testing.T) {
	ls := []pluginListingJSON{
		{Listing: plugin.Listing{Package: "a", Icon: "warp-not-bundled"}},
		{Listing: plugin.Listing{Package: "b", Icon: "githubcopilot"}},
		{Listing: plugin.Listing{Package: "c", Icon: "aigocode"}},
		{Listing: plugin.Listing{Package: "d"}},
	}
	listingIcons(ls, 0)
	for i, want := range []string{"", "githubcopilot", "aigocode", ""} {
		if ls[i].Icon != want {
			t.Errorf("%s: icon = %q, want %q", ls[i].Package, ls[i].Icon, want)
		}
	}
}
