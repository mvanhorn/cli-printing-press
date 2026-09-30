package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPaginatedGetStringHasMoreNextLink locks Graph-style paging.next URLs
// declared as has_more_field: a bool unmarshal must not end --all after page 1,
// and a string that yields no cursor must not report complete.
func TestPaginatedGetStringHasMoreNextLink(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paginate-next-link")
	outputDir := filepath.Join(t.TempDir(), "paginate-next-link-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	behaviorTest := `package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

type nextLinkClient struct {
	responses []json.RawMessage
	params    []map[string]string
}

func (c *nextLinkClient) GetWithHeaders(_ context.Context, _ string, params map[string]string, _ map[string]string) (json.RawMessage, error) {
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.params = append(c.params, copied)
	if len(c.responses) == 0 {
		return json.RawMessage("[]"), nil
	}
	next := c.responses[0]
	c.responses = c.responses[1:]
	return next, nil
}

func captureNextLinkStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = oldErr }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(out)
}

func assertNoReplayToken(t *testing.T, params map[string]string) {
	t.Helper()
	if _, ok := params["access_token"]; ok {
		t.Fatalf("replayed access_token from next URL: %#v", params)
	}
	for key, value := range params {
		if strings.Contains(value, "SECRET_TOKEN") || strings.Contains(value, "graph.facebook.com") {
			t.Fatalf("param %s replayed next URL value %q", key, value)
		}
	}
}

func TestHasMoreGraphNextLinkFetchesSecondPage(t *testing.T) {
	page1 := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"paging\":{\"cursors\":{\"before\":\"BEFORE1\",\"after\":\"CURSOR1\"},\"next\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25&after=CURSOR1\"}}")
	page2 := json.RawMessage("{\"data\":[{\"id\":\"m2\"}],\"paging\":{\"cursors\":{\"before\":\"BEFORE2\",\"after\":\"CURSOR2\"}}}")
	client := &nextLinkClient{responses: []json.RawMessage{page1, page2}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, true, "after", "cursor", "limit", 25, "paging.cursors.after", "paging.next")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 || got[0]["id"] != "m1" || got[1]["id"] != "m2" {
		t.Fatalf("items = %#v, want m1 then m2", got)
	}
	if len(client.params) != 2 {
		t.Fatalf("got %d requests, want 2: %#v", len(client.params), client.params)
	}
	if _, ok := client.params[0]["after"]; ok {
		t.Fatalf("first request sent after: %#v", client.params[0])
	}
	if client.params[1]["after"] != "CURSOR1" || client.params[1]["limit"] != "25" {
		t.Fatalf("second request = %#v, want after=CURSOR1 limit=25", client.params[1])
	}
	assertNoReplayToken(t, client.params[0])
	assertNoReplayToken(t, client.params[1])
	if !strings.Contains(stderr, "\"event\":\"complete\"") || !strings.Contains(stderr, "\"pages\":2") {
		t.Fatalf("stderr missing complete two-page event: %s", stderr)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") {
		t.Fatalf("successful --all reported truncated: %s", stderr)
	}
}

func TestHasMoreGraphNextLinkSinglePageHintsAll(t *testing.T) {
	page := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"paging\":{\"cursors\":{\"before\":\"BEFORE1\",\"after\":\"CURSOR1\"},\"next\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25&after=CURSOR1\"}}")
	client := &nextLinkClient{responses: []json.RawMessage{page}}
	stderr := captureNextLinkStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, false, "after", "cursor", "limit", 25, "", "paging.next")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if !strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "pass --all to fetch every page") {
		t.Fatalf("stderr missing pass --all hint: %s", stderr)
	}
}

func TestHasMoreBooleanFalseDoesNotOverrideCursor(t *testing.T) {
	client := &nextLinkClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"cursor\":\"c2\",\"has_more\":false}"),
		json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"has_more\":false}"),
	}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/orders", nil, nil, true, "cursor", "cursor", "", 100, "cursor", "has_more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("items = %#v, want two; a false has_more must not block a declared cursor", got)
	}
	if len(client.params) != 2 || client.params[1]["cursor"] != "c2" {
		t.Fatalf("requests = %#v, want second cursor c2", client.params)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") {
		t.Fatalf("boolean has_more false with a cursor truncated: %s", stderr)
	}
	if !strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("stderr missing complete: %s", stderr)
	}
}

func TestHasMoreBooleanTrueWithoutCursorStillCompletes(t *testing.T) {
	client := &nextLinkClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"has_more\":true}"),
	}}
	stderr := captureNextLinkStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", nil, nil, true, "cursor", "cursor", "", 100, "", "has_more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1", len(client.params))
	}
	if !strings.Contains(stderr, "\"reason\":\"pagination_cursor_missing\"") {
		t.Fatalf("stderr missing cursor-missing truncation: %s", stderr)
	}
	if !strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("boolean has_more true without a cursor must still emit complete: %s", stderr)
	}
}

func TestHasMoreBooleanPageStillAdvances(t *testing.T) {
	client := &nextLinkClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"has_more\":true}"),
		json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"has_more\":false}"),
	}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/orders", map[string]string{"page": "1"}, nil, true, "page", "page", "", 100, "", "has_more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("items = %#v, want two", got)
	}
	if len(client.params) != 2 || client.params[0]["page"] != "1" || client.params[1]["page"] != "2" {
		t.Fatalf("requests = %#v, want page 1 then 2", client.params)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("boolean page walk stderr = %s", stderr)
	}
}

func TestHasMoreStringURLWithoutCursorTruncates(t *testing.T) {
	page := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"paging\":{\"next\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25\"}}")
	client := &nextLinkClient{responses: []json.RawMessage{page, json.RawMessage("{\"data\":[{\"id\":\"unexpected\"}]}")}}
	stderr := captureNextLinkStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, true, "after", "cursor", "limit", 25, "", "paging.next")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1: %#v", len(client.params), client.params)
	}
	assertNoReplayToken(t, client.params[0])
	if !strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"reason\":\"pagination_cursor_missing\"") {
		t.Fatalf("stderr missing pagination_cursor_missing: %s", stderr)
	}
	if strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("unusable next-link has_more reported complete: %s", stderr)
	}
}

func TestHasMoreOpaqueStringWithoutCursorTruncates(t *testing.T) {
	page := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"meta\":{\"more\":\"yes\"}}")
	client := &nextLinkClient{responses: []json.RawMessage{page, json.RawMessage("{\"data\":[{\"id\":\"unexpected\"}]}")}}
	stderr := captureNextLinkStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/123/media", nil, nil, true, "after", "cursor", "", 25, "", "meta.more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	if len(client.params) != 1 {
		t.Fatalf("got %d requests, want 1: %#v", len(client.params), client.params)
	}
	if !strings.Contains(stderr, "\"reason\":\"pagination_cursor_missing\"") {
		t.Fatalf("stderr missing pagination_cursor_missing: %s", stderr)
	}
	if strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("opaque string has_more reported complete: %s", stderr)
	}
}

func TestHasMorePagingNextProbeWithoutDeclaration(t *testing.T) {
	page1 := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"paging\":{\"next\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25&after=CURSOR1\"}}")
	page2 := json.RawMessage("{\"data\":[{\"id\":\"m2\"}],\"paging\":{\"cursors\":{\"after\":\"CURSOR2\"}}}")
	client := &nextLinkClient{responses: []json.RawMessage{page1, page2}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, true, "after", "cursor", "limit", 25, "", "")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("items = %#v, want two", got)
	}
	if len(client.params) != 2 || client.params[1]["after"] != "CURSOR1" {
		t.Fatalf("requests = %#v, want second after=CURSOR1", client.params)
	}
	assertNoReplayToken(t, client.params[1])
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"pages\":2") {
		t.Fatalf("probe stderr = %s", stderr)
	}
}

func TestHasMoreDeclaredCursorKeptWhenNextURLHasNoToken(t *testing.T) {
	page1 := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"paging\":{\"cursors\":{\"after\":\"DECLARED\"},\"next\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25\"}}")
	page2 := json.RawMessage("{\"data\":[{\"id\":\"m2\"}],\"paging\":{\"cursors\":{\"after\":\"STILL_THERE\"}}}")
	client := &nextLinkClient{responses: []json.RawMessage{page1, page2, json.RawMessage("{\"data\":[{\"id\":\"unexpected\"}]}")}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, true, "after", "cursor", "limit", 25, "paging.cursors.after", "paging.next")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 || got[0]["id"] != "m1" || got[1]["id"] != "m2" {
		t.Fatalf("items = %#v, want m1 then m2", got)
	}
	if len(client.params) != 2 || client.params[1]["after"] != "DECLARED" {
		t.Fatalf("requests = %#v, want second after=DECLARED", client.params)
	}
	assertNoReplayToken(t, client.params[1])
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"pages\":2") {
		t.Fatalf("declared cursor with tokenless next stderr = %s", stderr)
	}
}

func TestHasMoreDeclaredCursorNotReplacedByURLToken(t *testing.T) {
	page1 := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"paging\":{\"cursors\":{\"after\":\"DECLARED\"},\"next\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25&after=FROM_URL\"}}")
	page2 := json.RawMessage("{\"data\":[{\"id\":\"m2\"}],\"paging\":{\"cursors\":{\"after\":\"OTHER\"}}}")
	client := &nextLinkClient{responses: []json.RawMessage{page1, page2}}
	stderr := captureNextLinkStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, true, "after", "cursor", "limit", 25, "paging.cursors.after", "paging.next")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	if len(client.params) != 2 || client.params[1]["after"] != "DECLARED" {
		t.Fatalf("requests = %#v, want second after=DECLARED not FROM_URL", client.params)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"pages\":2") {
		t.Fatalf("declared cursor replaced or walk stopped: %s", stderr)
	}
}

func TestHasMoreNextURLCursorWhenDeclaredEmpty(t *testing.T) {
	page1 := json.RawMessage("{\"data\":[{\"id\":\"m1\"}],\"meta\":{\"next_url\":\"https://graph.facebook.com/v22.0/123/media?access_token=SECRET_TOKEN&limit=25&after=CURSOR1\"}}")
	page2 := json.RawMessage("{\"data\":[{\"id\":\"m2\"}],\"meta\":{}}")
	client := &nextLinkClient{responses: []json.RawMessage{page1, page2, json.RawMessage("{\"data\":[{\"id\":\"unexpected\"}]}")}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/123/media", map[string]string{"limit": "25"}, nil, true, "after", "cursor", "limit", 25, "", "meta.next_url")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("items = %#v, want two", got)
	}
	if len(client.params) != 2 || client.params[1]["after"] != "CURSOR1" {
		t.Fatalf("requests = %#v, want second after=CURSOR1", client.params)
	}
	assertNoReplayToken(t, client.params[1])
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"pages\":2") {
		t.Fatalf("next_url cursor stderr = %s", stderr)
	}
}

func TestHasMoreStringPageStillAdvances(t *testing.T) {
	client := &nextLinkClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"has_more\":\"yes\"}"),
		json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"has_more\":false}"),
	}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/orders", map[string]string{"page": "1"}, nil, true, "page", "page", "", 100, "", "has_more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 2 {
		t.Fatalf("items = %#v, want two", got)
	}
	if len(client.params) != 2 || client.params[0]["page"] != "1" || client.params[1]["page"] != "2" {
		t.Fatalf("requests = %#v, want page 1 then 2", client.params)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("string page walk stderr = %s", stderr)
	}
}

func TestHasMoreStringOffsetStillAdvances(t *testing.T) {
	client := &nextLinkClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"},{\"id\":\"two\"}],\"has_more\":\"yes\"}"),
		json.RawMessage("{\"items\":[{\"id\":\"three\"}],\"has_more\":false}"),
	}}
	var data json.RawMessage
	stderr := captureNextLinkStderr(t, func() {
		var err error
		data, err = paginatedGet(context.Background(), client, "/orders", map[string]string{"limit": "2", "offset": "0"}, nil, true, "offset", "offset", "limit", 2, "", "has_more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal data: %v; data=%s", err, data)
	}
	if len(got) != 3 {
		t.Fatalf("items = %#v, want three", got)
	}
	if len(client.params) != 2 || client.params[0]["offset"] != "0" || client.params[1]["offset"] != "2" {
		t.Fatalf("requests = %#v, want offset 0 then 2", client.params)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("string offset walk stderr = %s", stderr)
	}
}

func TestHasMoreFollowableURLWithoutTokenStillAdvancesPage(t *testing.T) {
	client := &nextLinkClient{responses: []json.RawMessage{
		json.RawMessage("{\"items\":[{\"id\":\"one\"}],\"has_more\":\"https://api.example/orders?limit=25\"}"),
		json.RawMessage("{\"items\":[{\"id\":\"two\"}],\"has_more\":false}"),
	}}
	stderr := captureNextLinkStderr(t, func() {
		_, err := paginatedGet(context.Background(), client, "/orders", map[string]string{"page": "1"}, nil, true, "page", "page", "", 100, "", "has_more")
		if err != nil {
			t.Fatalf("paginatedGet: %v", err)
		}
	})
	if len(client.params) != 2 || client.params[1]["page"] != "2" {
		t.Fatalf("requests = %#v, want page 1 then 2", client.params)
	}
	if strings.Contains(stderr, "\"event\":\"truncated\"") || !strings.Contains(stderr, "\"event\":\"complete\"") {
		t.Fatalf("tokenless next URL on page stderr = %s", stderr)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "paginated_next_link_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestHasMore", "-count=1")
}
