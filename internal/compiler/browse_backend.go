package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BrowseRecordOptions configures zero-popup Chrome network capture via OpenCode
// `browse` (ECDH-paired Chrome extension, zero Chrome 144 remote debugging popups).
type BrowseRecordOptions struct {
	TargetURL    string
	NameOverride string
	WaitSeconds  int
	BrowseBin    string
}

// RecordAuto picks the best available zero-popup browser recorder on this
// machine: Ardia (`ardia browse`) first, then OpenCode `browse` for Chrome.
func RecordAuto(ctx context.Context, targetURL, nameOverride string, waitSeconds int) (*CompileResult, error) {
	if ardiaBin, ok := findCLI("ardia"); ok {
		return RecordWithArdia(ctx, ArdiaRecordOptions{
			TargetURL:    targetURL,
			NameOverride: nameOverride,
			WaitSeconds:  waitSeconds,
			ArdiaBin:     ardiaBin,
		})
	}
	if browseBin, ok := findCLI("browse"); ok {
		return RecordWithOpenCodeBrowse(ctx, BrowseRecordOptions{
			TargetURL:    targetURL,
			NameOverride: nameOverride,
			WaitSeconds:  waitSeconds,
			BrowseBin:    browseBin,
		})
	}
	return nil, fmt.Errorf("no background browser recorder found: install Ardia (`ardia`) or OpenCode `browse`, or pass --from-har <capture.har>")
}

// RecordWithOpenCodeBrowse drives Google Chrome through OpenCode `browse`'s
// paired extension (zero Chrome 144 remote-debugging popups, zero focus steal),
// captures XHR/Fetch exchanges via `browse network`, synthesizes a HAR, and
// compiles it into a validated host-locked pp-cartridge/v1 bundle.
func RecordWithOpenCodeBrowse(ctx context.Context, opts BrowseRecordOptions) (*CompileResult, error) {
	start := time.Now()
	target := strings.TrimSpace(opts.TargetURL)
	if target == "" {
		return nil, fmt.Errorf("target URL is required")
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}

	browseBin := opts.BrowseBin
	if browseBin == "" {
		var ok bool
		browseBin, ok = findCLI("browse")
		if !ok {
			return nil, fmt.Errorf("OpenCode `browse` CLI not found on PATH (install with `curl -fsSL https://opencode.ai/browse/install | bash`, or pass --from-har <file>)")
		}
	}

	runBrowseJSON := func(callCtx context.Context, tool string, payload map[string]any) ([]byte, error) {
		rawPayload, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(callCtx, browseBin, tool, "--json", string(rawPayload))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("browse %s: %w (%s)", tool, err, strings.TrimSpace(string(out)))
		}
		return out, nil
	}

	tabLabel := fmt.Sprintf("pp-rec-%d", time.Now().UnixNano())
	if _, err := runBrowseJSON(ctx, "tabs", map[string]any{"action": "new", "id": tabLabel}); err != nil {
		return nil, err
	}
	captureActive := false
	defer func() {
		// Use a fresh bounded background context so cancellation or timeouts
		// still cleanly cancel active network capture and close the tab.
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if captureActive {
			_, _ = runBrowseJSON(cleanCtx, "network", map[string]any{"action": "cancel", "tab": tabLabel})
		}
		_, _ = runBrowseJSON(cleanCtx, "tabs", map[string]any{"action": "close", "tab": tabLabel})
	}()

	if _, err := runBrowseJSON(ctx, "network", map[string]any{
		"action":        "start",
		"tab":           tabLabel,
		"resourceTypes": []string{"XHR", "Fetch"},
		"bodies":        true,
	}); err != nil {
		return nil, err
	}
	captureActive = true
	if _, err := runBrowseJSON(ctx, "navigate", map[string]any{
		"tab":       tabLabel,
		"url":       target,
		"waitUntil": "commit",
	}); err != nil {
		return nil, err
	}

	waitSec := opts.WaitSeconds
	if waitSec <= 0 {
		waitSec = 3
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(waitSec) * time.Second):
	}

	rawNet, err := runBrowseJSON(ctx, "network", map[string]any{"action": "stop", "tab": tabLabel})
	captureActive = false
	if err != nil {
		return nil, err
	}

	harBytes, err := convertBrowseNetworkToHAR(rawNet)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "pp-browse-rec-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	harPath := filepath.Join(tmpDir, "capture.har")
	if err := os.WriteFile(harPath, harBytes, 0o600); err != nil {
		return nil, err
	}

	res, err := CompileFromHAR(harPath, opts.NameOverride)
	if err != nil {
		return nil, err
	}
	res.Source = "browse:" + target
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res, nil
}

// convertBrowseNetworkToHAR translates OpenCode `browse network stop` JSON
// exchanges into standard HAR 1.2 JSON so internal/browsersniff analyzes them
// through the same pipeline as Ardia HAR captures.
func convertBrowseNetworkToHAR(raw []byte) ([]byte, error) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parsing browse network capture JSON: %w", err)
	}
	var exchanges []any
	for _, key := range []string{"exchanges", "requests", "entries"} {
		if list, ok := parsed[key].([]any); ok {
			exchanges = list
			break
		}
	}
	if len(exchanges) == 0 {
		return nil, fmt.Errorf("browse network capture returned zero exchanges")
	}

	harEntries := make([]map[string]any, 0, len(exchanges))
	for _, item := range exchanges {
		ex, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// Support both OpenCode browse v0.1.5's nested {request, response} shape
		// and flat exchange objects.
		reqMap, _ := ex["request"].(map[string]any)
		respMap, _ := ex["response"].(map[string]any)

		reqURL, _ := ex["url"].(string)
		if reqURL == "" && reqMap != nil {
			reqURL, _ = reqMap["url"].(string)
		}
		if reqURL == "" {
			continue
		}
		method, _ := ex["method"].(string)
		if method == "" && reqMap != nil {
			method, _ = reqMap["method"].(string)
		}
		if method == "" {
			method = "GET"
		}
		status := 200
		if st, ok := ex["status"].(float64); ok && st > 0 {
			status = int(st)
		} else if respMap != nil {
			if st, ok := respMap["status"].(float64); ok && st > 0 {
				status = int(st)
			}
		}

		mimeType := "application/json"
		respText := ""
		rawRespBody := ex["responseBody"]
		if rawRespBody == nil && respMap != nil {
			if b := respMap["body"]; b != nil {
				rawRespBody = b
			} else if c := respMap["content"]; c != nil {
				rawRespBody = c
			}
		}
		switch body := rawRespBody.(type) {
		case string:
			respText = body
		case map[string]any:
			if mt, ok := body["mimeType"].(string); ok && mt != "" {
				mimeType = mt
			}
			if txt, ok := body["text"].(string); ok {
				respText = txt
			}
		}

		rawReqHeaders := ex["requestHeaders"]
		if rawReqHeaders == nil && reqMap != nil {
			rawReqHeaders = reqMap["headers"]
		}
		rawRespHeaders := ex["responseHeaders"]
		if rawRespHeaders == nil && respMap != nil {
			rawRespHeaders = respMap["headers"]
		}
		reqHeaders := normalizeHARHeaders(rawReqHeaders)
		respHeaders := normalizeHARHeaders(rawRespHeaders)

		reqObj := map[string]any{
			"method":  method,
			"url":     reqURL,
			"headers": reqHeaders,
		}
		reqBodyText, reqBodyMime := extractBodyText(ex["requestBody"])
		if reqBodyText == "" && reqMap != nil {
			reqBodyText, reqBodyMime = extractBodyText(reqMap["body"])
		}
		if reqBodyText != "" {
			if reqBodyMime == "" {
				reqBodyMime = "application/json"
			}
			reqObj["postData"] = map[string]any{
				"mimeType": reqBodyMime,
				"text":     reqBodyText,
			}
		}

		harEntries = append(harEntries, map[string]any{
			"request": reqObj,
			"response": map[string]any{
				"status":  status,
				"headers": respHeaders,
				"content": map[string]any{
					"mimeType": mimeType,
					"size":     len(respText),
					"text":     respText,
				},
			},
		})
	}

	harDoc := map[string]any{
		"log": map[string]any{
			"version": "1.2",
			"entries": harEntries,
		},
	}
	return json.Marshal(harDoc)
}

func extractBodyText(v any) (string, string) {
	switch b := v.(type) {
	case string:
		return b, ""
	case map[string]any:
		txt, _ := b["text"].(string)
		mt, _ := b["mimeType"].(string)
		return txt, mt
	default:
		return "", ""
	}
}

func normalizeHARHeaders(v any) []map[string]string {
	switch h := v.(type) {
	case map[string]any:
		out := make([]map[string]string, 0, len(h))
		for k, val := range h {
			out = append(out, map[string]string{
				"name":  k,
				"value": fmt.Sprint(val),
			})
		}
		return out
	case []any:
		out := make([]map[string]string, 0, len(h))
		for _, item := range h {
			if m, ok := item.(map[string]any); ok {
				name, _ := m["name"].(string)
				if name != "" {
					out = append(out, map[string]string{
						"name":  name,
						"value": fmt.Sprint(m["value"]),
					})
				}
			}
		}
		return out
	default:
		return []map[string]string{}
	}
}

func findCLI(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}
