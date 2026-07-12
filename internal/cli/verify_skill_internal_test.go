// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"slices"
	"testing"
)

// TestPythonUTF8Env_AppendsEncodingVarsWhenAbsent guards the Windows cp1252
// crash fix: the verify-skill Python subprocess must receive UTF-8 stdio
// env vars so its ✓/✘ glyph prints don't raise UnicodeEncodeError on Windows.
func TestPythonUTF8Env_AppendsEncodingVarsWhenAbsent(t *testing.T) {
	t.Parallel()

	got := pythonUTF8Env([]string{"PATH=/usr/bin", "HOME=/tmp"})

	if !slices.Contains(got, "PYTHONIOENCODING=utf-8") {
		t.Errorf("expected PYTHONIOENCODING=utf-8 in env, got %v", got)
	}
	if !slices.Contains(got, "PYTHONUTF8=1") {
		t.Errorf("expected PYTHONUTF8=1 in env, got %v", got)
	}
	if !slices.Contains(got, "PATH=/usr/bin") {
		t.Errorf("expected PATH=/usr/bin to be preserved, got %v", got)
	}
}

// TestPythonUTF8Env_OverridesUserSetting ensures a user's existing
// PYTHONIOENCODING (e.g. inherited cp1252 on Windows) cannot defeat the fix:
// the helper drops conflicting entries and re-appends the UTF-8 values.
func TestPythonUTF8Env_OverridesUserSetting(t *testing.T) {
	t.Parallel()

	got := pythonUTF8Env([]string{
		"PYTHONIOENCODING=cp1252",
		"PYTHONUTF8=0",
		"PATH=/usr/bin",
	})

	for _, kv := range got {
		if kv == "PYTHONIOENCODING=cp1252" || kv == "PYTHONUTF8=0" {
			t.Errorf("conflicting env entry %q should have been removed, got env %v", kv, got)
		}
	}
	if !slices.Contains(got, "PYTHONIOENCODING=utf-8") {
		t.Errorf("expected PYTHONIOENCODING=utf-8 in env, got %v", got)
	}
	if !slices.Contains(got, "PYTHONUTF8=1") {
		t.Errorf("expected PYTHONUTF8=1 in env, got %v", got)
	}
}

// TestPythonUTF8Env_EmptyBase covers the edge case where os.Environ()
// returns nothing — the helper should still emit both UTF-8 vars.
func TestPythonUTF8Env_EmptyBase(t *testing.T) {
	t.Parallel()

	got := pythonUTF8Env(nil)
	if len(got) != 2 {
		t.Fatalf("expected exactly the two UTF-8 entries, got %v", got)
	}
	if !slices.Contains(got, "PYTHONIOENCODING=utf-8") || !slices.Contains(got, "PYTHONUTF8=1") {
		t.Errorf("expected both UTF-8 entries, got %v", got)
	}
}

func TestIsWindowsStorePython(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"store stub python3", `C:\Users\alice\AppData\Local\Microsoft\WindowsApps\python3.exe`, true},
		{"store stub python", `C:\Users\alice\AppData\Local\Microsoft\WindowsApps\python.exe`, true},
		{"store stub versioned python3.13", `C:\Users\alice\AppData\Local\Microsoft\WindowsApps\python3.13.exe`, true},
		{"store stub mixed case", `C:\Users\alice\AppData\Local\Microsoft\WINDOWSAPPS\Python3.exe`, true},
		{"store stub forward slashes", `C:/Users/alice/AppData/Local/Microsoft/WindowsApps/python3.exe`, true},
		{"real python install", `C:\Python314\python.exe`, false},
		{"py launcher", `C:\Windows\py.exe`, false},
		{"unix path", "/usr/bin/python3", false},
		{"unrelated windowsapps binary", `C:\Users\alice\AppData\Local\Microsoft\WindowsApps\winget.exe`, false},
		{"empty path", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isWindowsStorePython(tc.path); got != tc.want {
				t.Errorf("isWindowsStorePython(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
