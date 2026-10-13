package telemetry

import (
	"context"
	"os"
	"time"

	"github.com/henomis/langfuse-go"
	"github.com/henomis/langfuse-go/model"
)

// ingestionAPIClient is the Langfuse v3 ingestion backend: it wraps the
// henomis/langfuse-go SDK which batches events to POST /api/public/ingestion.
type ingestionAPIClient struct {
	sdk *langfuse.Langfuse
}

// newIngestionAPIClient builds the upstream SDK client. Keys come from credentials
// deps (resolved before call); henomis/langfuse-go only reads LANGFUSE_* at
// construction, so we patch process env briefly and restore — never leave
// secrets in os.environ.
func newIngestionAPIClient(ctx context.Context, baseURL, publicKey, secretKey string, flushInterval time.Duration) *ingestionAPIClient {
	restore := patchProcessEnv(map[string]string{
		"LANGFUSE_HOST":       baseURL,
		"LANGFUSE_PUBLIC_KEY": publicKey,
		"LANGFUSE_SECRET_KEY": secretKey,
	})
	sdk := langfuse.New(ctx).WithFlushInterval(flushInterval)
	restore()
	return &ingestionAPIClient{sdk: sdk}
}

func (c *ingestionAPIClient) Trace(t *model.Trace) error {
	_, err := c.sdk.Trace(t)
	return err
}

func (c *ingestionAPIClient) Generation(g *model.Generation, parentObservationID *string) error {
	_, err := c.sdk.Generation(g, parentObservationID)
	return err
}

func (c *ingestionAPIClient) GenerationEnd(g *model.Generation) error {
	_, err := c.sdk.GenerationEnd(g)
	return err
}

func (c *ingestionAPIClient) Span(s *model.Span, parentObservationID *string) error {
	_, err := c.sdk.Span(s, parentObservationID)
	return err
}

func (c *ingestionAPIClient) SpanEnd(s *model.Span) error {
	_, err := c.sdk.SpanEnd(s)
	return err
}

func (c *ingestionAPIClient) Event(e *model.Event, parentObservationID *string) error {
	_, err := c.sdk.Event(e, parentObservationID)
	return err
}

func (c *ingestionAPIClient) Flush(ctx context.Context) error {
	c.sdk.Flush(ctx)
	return nil
}

func (c *ingestionAPIClient) Shutdown(ctx context.Context) error {
	c.sdk.Flush(ctx)
	return nil
}

func patchProcessEnv(vals map[string]string) func() {
	prev := make(map[string]string, len(vals))
	wasSet := make(map[string]bool, len(vals))
	for key, value := range vals {
		if old, ok := os.LookupEnv(key); ok {
			prev[key] = old
			wasSet[key] = true
		}
		os.Setenv(key, value)
	}
	return func() {
		for key := range vals {
			if wasSet[key] {
				os.Setenv(key, prev[key])
			} else {
				os.Unsetenv(key)
			}
		}
	}
}
