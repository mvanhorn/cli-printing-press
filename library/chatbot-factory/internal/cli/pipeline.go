package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"chatbot-factory-pp-cli/internal/pipeline"
	_ "chatbot-factory-pp-cli/internal/pipeline/phases" // register all phases
)

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Manage the chatbot delivery pipeline",
	}
	cmd.AddCommand(newPipelineInitCmd())
	cmd.AddCommand(newPipelineRunCmd())
	cmd.AddCommand(newPipelineStatusCmd())
	cmd.AddCommand(newPipelineResumeCmd())
	cmd.AddCommand(newPipelinePhaseCmd())
	return cmd
}

func newPipelineInitCmd() *cobra.Command {
	var channel string
	cmd := &cobra.Command{
		Use:   "init <slug>",
		Short: "Initialise state.json for a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			statePath := filepath.Join(".chatbot-factory", "state.json")
			if _, err := os.Stat(statePath); err == nil {
				return fmt.Errorf("state.json already exists at %s", statePath)
			}
			s := pipeline.NewState(slug, channel)
			if err := s.Save(statePath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ pipeline initialised for %s (%s) → %s\n", slug, channel, statePath)
			return nil
		},
	}
	cmd.Flags().StringVar(&channel, "channel", "telegram", "channel: telegram|whatsapp|manychat")
	return cmd
}

func newPipelineRunCmd() *cobra.Command {
	var jsonOut bool
	var statePath string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run phases until the next gate failure or completion",
		RunE: func(cmd *cobra.Command, args []string) error {
			o := pipeline.NewOrchestrator(statePath)
			o.JSONOutput = jsonOut
			return o.RunAll(context.Background())
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON progress events")
	cmd.Flags().StringVar(&statePath, "state", ".chatbot-factory/state.json", "path to state.json")
	return cmd
}

func newPipelineStatusCmd() *cobra.Command {
	var jsonOut bool
	var statePath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show phase table",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := pipeline.LoadState(statePath)
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(s)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project: %s   Channel: %s\n\n", s.Project, s.Channel)
			fmt.Fprintln(cmd.OutOrStdout(), "PHASE                STATUS")
			for _, p := range pipeline.PhaseOrder {
				st := s.Phases[p]
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %s\n", p, st.Status)
			}
			if next := s.NextPending(); next != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\nnext: %s\n", next)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "\npipeline complete")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	cmd.Flags().StringVar(&statePath, "state", ".chatbot-factory/state.json", "path to state.json")
	return cmd
}

func newPipelineResumeCmd() *cobra.Command {
	var jsonOut bool
	var statePath string
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Continue from the next pending phase (alias for run)",
		RunE: func(cmd *cobra.Command, args []string) error {
			o := pipeline.NewOrchestrator(statePath)
			o.JSONOutput = jsonOut
			return o.RunAll(context.Background())
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON progress events")
	cmd.Flags().StringVar(&statePath, "state", ".chatbot-factory/state.json", "path to state.json")
	return cmd
}

func newPipelinePhaseCmd() *cobra.Command {
	var jsonOut bool
	var statePath string
	var skip bool
	var reason string
	cmd := &cobra.Command{
		Use:   "phase <name>",
		Short: "Run a single named phase (or --skip it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !pipeline.IsPhase(args[0]) {
				return fmt.Errorf("unknown phase %q", args[0])
			}
			if skip {
				s, err := pipeline.LoadState(statePath)
				if err != nil {
					return err
				}
				s.MarkSkipped(args[0], reason)
				return s.Save(statePath)
			}
			o := pipeline.NewOrchestrator(statePath)
			o.JSONOutput = jsonOut
			return o.RunPhase(context.Background(), args[0])
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON progress events")
	cmd.Flags().StringVar(&statePath, "state", ".chatbot-factory/state.json", "path to state.json")
	cmd.Flags().BoolVar(&skip, "skip", false, "mark phase as skipped without running")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for skip")
	return cmd
}
