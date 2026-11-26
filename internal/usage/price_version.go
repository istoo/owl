package usage

import (
	"strconv"
	"strings"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

// PriceVersion names what prices a call and what a call is counted under:
// the user's own model prices and aliases (settings), the providers' key
// rates, rate windows and renames (providers.json), and the model catalog
// behind both. A reader that keeps sums across calls (budget.AllOf, the
// gateway's per-key sums) keeps one of these and counts again when it
// changes, so a price the user edits is what the next call is billed at,
// not the one kept from before.
//
// It is each file's stat and the catalog's own content key, not a read of
// the whole settings: cheap enough for a request's path, and an unreadable
// file's stat still changes when it is written.
func PriceVersion() string {
	return strings.Join([]string{
		statKey(settings.Path()),
		statKey(provider.Path()),
		catalog.CacheKey(),
		strconv.FormatInt(catalog.Resets(), 10),
	}, "|")
}
