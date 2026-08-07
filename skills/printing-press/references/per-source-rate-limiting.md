# Per-source rate limiting

Hand-written clients in sibling internal packages (`internal/source/<name>/`,
`internal/recipes/`, `internal/phgraphql/`, etc.) MUST use
`cliutil.AdaptiveLimiter` and surface `*cliutil.RateLimitError` from public
methods when 429 retries are exhausted. Returning empty results on throttle is
silent corruption: downstream queries cannot tell "the source has no data" from
"the source rate-limited us."

## Reference shape

```go
import (
    "context"
    "errors"
    "net/http"

    "<modulePath>/internal/cliutil"
)

type Client struct {
    HTTP    *http.Client
    limiter *cliutil.AdaptiveLimiter
}

func New() *Client {
    return &Client{
        HTTP:    &http.Client{},
        limiter: cliutil.NewAdaptiveLimiter(2.0),
    }
}

func (c *Client) Fetch(ctx context.Context, url string) ([]byte, error) {
    c.limiter.Wait()
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.HTTP.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusTooManyRequests {
        c.limiter.OnRateLimit()
        return nil, &cliutil.RateLimitError{
            URL:        url,
            RetryAfter: cliutil.RetryAfter(resp),
        }
    }
    c.limiter.OnSuccess()
    // ... read + return body ...
    return nil, nil
}
```

## Higher-level fanout commands

Aggregations that fan out N filings/items MUST `errors.As(err, &rateErr)` and
propagate the failure rather than `continue`-ing past it:

```go
for _, item := range items {
    detail, err := c.FetchDetail(ctx, item.ID)
    if err != nil {
        var rateErr *cliutil.RateLimitError
        if errors.As(err, &rateErr) {
            return nil, err
        }
        continue // other errors are still skippable
    }
    out = append(out, detail)
}
```

## Enforcement

`cli-printing-press dogfood` runs `source_client_check`, which scans every
`internal/<pkg>/*.go` file outside the generator-emitted set. A file that
makes outbound HTTP calls but lacks a limiter signal or a typed-error signal
produces a finding.
