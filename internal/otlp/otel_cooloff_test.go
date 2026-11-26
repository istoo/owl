package otlp

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/istoo/owl/internal/settings"
)

// A collector that is down (429/5xx) spends one sub-batch's whole retry
// before a flush gives up, not the same backoff again for every sub-batch,
// and the next flushes leave it alone for a while: otherwise a flush of
// eight sub-batches held the queue for sixteen minutes and dropped the
// calls arriving meanwhile (#62).
func TestOTelOneFlushesBackoffIsNotSpentPerBatch(t *testing.T) {
	otelConfig(t, settings.OTel{Endpoint: "https://collector.test"})
	t.Setenv("OWL_OTEL_BODIES", "true")
	synctest.Test(t, func(t *testing.T) {
		e := newOTelExporter()
		defer e.cancel()
		calls := 0
		e.client.Transport = otelTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			h := http.Header{}
			h.Set("Retry-After", "60")
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: h, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		})
		cfg, err := settings.OTelExport()
		if err != nil {
			t.Fatal(err)
		}
		// eight records make two sub-batches of four (otelBodiesBatch)
		batch := make([]otelItem, 8)
		for i := range batch {
			batch[i] = otelItem{record: Record{Time: time.Now(), Status: 200}, config: cfg}
		}
		e.flush(batch)
		if calls != 3 {
			t.Fatalf("attempts=%d, want 3 (one sub-batch's retries)", calls)
		}
		e.flush(batch)
		if calls != 3 {
			t.Fatalf("a flush inside the collector's backoff tried it again: attempts=%d", calls)
		}
	})
}
