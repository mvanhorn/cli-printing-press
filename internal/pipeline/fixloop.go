package pipeline

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type FixLoopReport struct {
	Iterations  []FixIteration `json:"iterations"`
	FinalReport *VerifyReport  `json:"final_report"`
	Improved    bool           `json:"improved"`
}

type FixIteration struct {
	Number     int     `json:"number"`
	Fixes      []Fix   `json:"fixes"`
	BeforeRate float64 `json:"before_rate"`
	AfterRate  float64 `json:"after_rate"`
	Delta      float64 `json:"delta"`
}

type Fix struct {
	Command string `json:"command"`
	Issue   string `json:"issue"`
	File    string `json:"file"`
	Applied bool   `json:"applied"`
}

func RunFixLoop(cfg VerifyConfig, initialReport *VerifyReport, maxIterations int) (*FixLoopReport, error) {
	if initialReport == nil {
		return nil, fmt.Errorf("run fix loop: nil initial report")
	}
	if maxIterations <= 0 {
		maxIterations = 3
	}

	current := initialReport
	out := &FixLoopReport{FinalReport: initialReport}

	for i := 1; i <= maxIterations; i++ {
		fixes := classifyFailures(current, cfg.Dir)
		if len(fixes) == 0 {
			break
		}

		backups := map[string][]byte{}
		appliedAny := false
		for j := range fixes {
			if fixes[j].File != "" {
				if _, ok := backups[fixes[j].File]; !ok {
					data, err := os.ReadFile(fixes[j].File)
					if err != nil {
						return nil, fmt.Errorf("backup %s: %w", fixes[j].File, err)
					}
					backups[fixes[j].File] = data
				}
			}
			if err := applyFix(fixes[j], cfg.Dir); err != nil {
				log.Printf("fix loop: skip %s for %s: %v", fixes[j].Issue, fixes[j].Command, err)
				continue
			}
			fixes[j].Applied = true
			appliedAny = true
		}

		iter := FixIteration{Number: i, Fixes: fixes, BeforeRate: current.PassRate, AfterRate: current.PassRate}
		if !appliedAny {
			out.Iterations = append(out.Iterations, iter)
			break
		}

		if err := runBuildChecks(cfg.Dir); err != nil {
			revertFiles(backups)
			log.Printf("fix loop: reverting iteration %d after build failure: %v", i, err)
			out.Iterations = append(out.Iterations, iter)
			break
		}

		next, err := RunVerify(cfg)
		if err != nil {
			revertFiles(backups)
			return nil, fmt.Errorf("re-run verify: %w", err)
		}

		if next.PassRate <= current.PassRate {
			revertFiles(backups)
			log.Printf("fix loop: reverting iteration %d due to no improvement", i)
			out.Iterations = append(out.Iterations, iter)
			break
		}

		iter.AfterRate = next.PassRate
		iter.Delta = next.PassRate - current.PassRate
		out.Iterations = append(out.Iterations, iter)
		current = next
		out.FinalReport = next
	}

	out.Improved = out.FinalReport != nil && out.FinalReport.PassRate > initialReport.PassRate
	return out, nil
}

func classifyFailures(report *VerifyReport, dir string) []Fix {
	if report == nil {
		return nil
	}

	var fixes []Fix
	for _, result := range report.Results {
		if result.Help && result.DryRun && result.Execute {
			continue
		}

		file, _ := findCommandFile(dir, result.Command)
		src := ""
		if file != "" {
			if data, err := os.ReadFile(file); err == nil {
				src = string(data)
			}
		}

		switch {
		case !result.Help:
			fixes = append(fixes, Fix{Command: result.Command, Issue: "help_fail", File: file})
		case !result.DryRun && file != "" && !strings.Contains(src, "flags.dryRun"):
			fixes = append(fixes, Fix{Command: result.Command, Issue: "dryrun_fail", File: file})
		case !result.DryRun:
			fixes = append(fixes, Fix{Command: result.Command, Issue: "exec_fail", File: file})
		case !result.Execute:
			fixes = append(fixes, Fix{Command: result.Command, Issue: "exec_fail", File: file})
		}
	}
	return fixes
}

func applyFix(fix Fix, dir string) error {
	switch fix.Issue {
	case "help_fail":
		log.Printf("fix loop: help registration for %s requires manual fix", fix.Command)
		return fmt.Errorf("manual registration required")
	case "exec_fail":
		log.Printf("fix loop: execution failure for %s requires manual fix", fix.Command)
		return fmt.Errorf("manual execution fix required")
	case "dryrun_fail":
	default:
		return fmt.Errorf("unknown issue %q", fix.Issue)
	}

	if fix.File == "" {
		return fmt.Errorf("no source file for %s", fix.Command)
	}
	if !hasPrintDryRunHelper(dir) {
		return fmt.Errorf("printDryRun helper not found")
	}

	data, err := os.ReadFile(fix.File)
	if err != nil {
		return fmt.Errorf("read %s: %w", fix.File, err)
	}
	src := string(data)
	if strings.Contains(src, "flags.dryRun") {
		return fmt.Errorf("dry-run check already present")
	}

	method := detectHTTPMethod(src)
	if method == "" {
		return fmt.Errorf("unable to infer HTTP method")
	}

	var target string
	switch method {
	case "GET":
		if strings.Contains(src, "data, err := paginatedGetWithResponsePath(") {
			target = "data, err := paginatedGetWithResponsePath("
		} else if strings.Contains(src, "data, err := paginatedGet(") {
			target = "data, err := paginatedGet("
		} else {
			target = "data, err := c.Get("
		}
	case "POST":
		target = "data, err := c.Post("
	case "PUT":
		target = "data, err := c.Put("
	case "PATCH":
		target = "data, err := c.Patch("
	case "DELETE":
		target = "data, err := c.Delete("
	}
	if target == "" || !strings.Contains(src, target) {
		return fmt.Errorf("unable to locate API call")
	}

	patch := fmt.Sprintf("if flags.dryRun {\n\t\t\tmethod := %q\n\t\t\treturn printDryRun(cmd, method, path)\n\t\t}\n\n\t\t\t%s", method, target)
	updated := strings.Replace(src, target, patch, 1)
	if updated == src {
		return fmt.Errorf("no patch applied")
	}

	if err := os.WriteFile(fix.File, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", fix.File, err)
	}
	return nil
}

func runBuildChecks(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, string(out))
	}

	vet := exec.CommandContext(ctx, "go", "vet", "./...")
	vet.Dir = dir
	if out, err := vet.CombinedOutput(); err != nil {
		return fmt.Errorf("go vet: %w\n%s", err, string(out))
	}

	return nil
}

func findCommandFile(dir, command string) (string, error) {
	cliDir := filepath.Join(dir, "internal", "cli")
	candidates := []string{
		filepath.Join(cliDir, command+".go"),
		filepath.Join(cliDir, strings.ReplaceAll(command, "-", "_")+".go"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	var found string
	err := filepath.WalkDir(cliDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(path), ".go")
		if base == command || strings.ReplaceAll(base, "_", "-") == command {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", fmt.Errorf("walk command files: %w", err)
	}
	return found, nil
}

func hasPrintDryRunHelper(dir string) bool {
	cliDir := filepath.Join(dir, "internal", "cli")
	found := false
	_ = filepath.WalkDir(cliDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(data), "printDryRun(") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func detectHTTPMethod(src string) string {
	for _, candidate := range []string{"Get", "Post", "Put", "Patch", "Delete"} {
		if strings.Contains(src, "c."+candidate+"(") {
			return strings.ToUpper(candidate)
		}
	}
	if strings.Contains(src, "paginatedGetWithResponsePath(") || strings.Contains(src, "paginatedGet(") {
		return "GET"
	}
	return ""
}

func revertFiles(backups map[string][]byte) {
	for path, data := range backups {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Printf("fix loop: failed to revert %s: %v", path, err)
		}
	}
}
