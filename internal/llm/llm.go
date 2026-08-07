package llm

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Available returns true if any supported LLM CLI is installed.
func Available() bool {
	_, err1 := exec.LookPath("claude")
	_, err2 := exec.LookPath("codex")
	return err1 == nil || err2 == nil
}

// Run sends a prompt to the best available LLM CLI and returns the response.
// It tries claude first, then falls back to codex. Long Claude prompts are
// written to a temp file and passed by reference to avoid ARG_MAX issues.
func Run(prompt string) (string, error) {
	// Try claude first (-p / --print mode, prompt as positional arg)
	if path, err := exec.LookPath("claude"); err == nil {
		// For short prompts, pass directly. For long prompts, use a temp file referenced in the prompt.
		var cmd *exec.Cmd
		if len(prompt) < 100000 {
			cmd = exec.Command(path, "-p", prompt, "--output-format", "text")
		} else {
			tmpFile, cleanup, err := writePromptTempFile(prompt)
			if err != nil {
				return "", err
			}
			defer cleanup()

			// Write to temp file and tell Claude to read it
			metaPrompt := fmt.Sprintf("Read the file at %s and follow the instructions inside it exactly.", tmpFile)
			cmd = exec.Command(path, "-p", metaPrompt, "--output-format", "text")
		}
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
		// Fall through to codex
		fmt.Fprintf(os.Stderr, "warning: claude failed (%v), trying codex\n", err)
	}

	// Try codex (--quiet --prompt runs non-interactively)
	if path, err := exec.LookPath("codex"); err == nil {
		cmd := exec.Command(path, "--quiet", "--prompt", prompt)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
		return "", fmt.Errorf("codex failed: %w", err)
	}

	return "", fmt.Errorf("no LLM CLI found (install claude or codex)")
}

func writePromptTempFile(prompt string) (string, func(), error) {
	promptFile, err := os.CreateTemp("", "llm-prompt-*.md")
	if err != nil {
		return "", nil, fmt.Errorf("creating prompt file: %w", err)
	}
	tmpFile := promptFile.Name()
	if _, err := promptFile.WriteString(prompt); err != nil {
		_ = promptFile.Close()
		_ = os.Remove(tmpFile)
		return "", nil, fmt.Errorf("writing prompt: %w", err)
	}
	if err := promptFile.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return "", nil, fmt.Errorf("closing prompt file: %w", err)
	}

	return tmpFile, func() { _ = os.Remove(tmpFile) }, nil
}
