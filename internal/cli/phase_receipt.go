package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
)

type phaseReceiptFlags struct {
	path     string
	runID    string
	phase    string
	evidence []string
	note     string
	resume   bool
	skip     bool
	failed   bool
}

func newPhaseReceiptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "phase-receipt",
		Short:  "Record skill phase transitions",
		Hidden: true,
		Args:   cobra.NoArgs,
	}
	cmd.AddCommand(newPhaseReceiptInitCmd())
	cmd.AddCommand(newPhaseReceiptEnterCmd())
	cmd.AddCommand(newPhaseReceiptCompleteCmd())
	cmd.AddCommand(newPhaseReceiptStopCmd())
	cmd.AddCommand(newPhaseReceiptStatusCmd())
	return cmd
}

func newPhaseReceiptInitCmd() *cobra.Command {
	var flags phaseReceiptFlags
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a phase receipt ledger",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			receipt, recorded, err := pipeline.InitPhaseReceipts(flags.options())
			if err != nil {
				return phaseReceiptError(err)
			}
			return writePhaseReceiptJSON(cmd, map[string]any{
				"recorded": recorded,
				"receipt":  receipt,
			})
		},
	}
	addPhaseReceiptCommonFlags(cmd, &flags)
	cmd.Flags().StringArrayVar(&flags.evidence, "evidence", nil, "Existing evidence path (repeatable)")
	return cmd
}

func newPhaseReceiptEnterCmd() *cobra.Command {
	var flags phaseReceiptFlags
	cmd := &cobra.Command{
		Use:   "enter",
		Short: "Enter the next expected phase",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			previous, err := pipeline.LatestPhaseReceipt(flags.path, flags.runID)
			if err != nil {
				return phaseReceiptError(err)
			}
			receipt, recorded, err := pipeline.EnterPhase(flags.options())
			if err != nil {
				return phaseReceiptError(err)
			}
			return writePhaseReceiptJSON(cmd, map[string]any{
				"recorded": recorded,
				"previous": previous,
				"receipt":  receipt,
			})
		},
	}
	addPhaseReceiptCommonFlags(cmd, &flags)
	cmd.Flags().BoolVar(&flags.resume, "resume", false, "Resume the same phase after a recorded blocker or failure")
	return cmd
}

func newPhaseReceiptCompleteCmd() *cobra.Command {
	var flags phaseReceiptFlags
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Complete or explicitly skip the active phase",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			receipt, recorded, err := pipeline.CompletePhase(flags.options(), flags.skip)
			if err != nil {
				return phaseReceiptError(err)
			}
			return writePhaseReceiptJSON(cmd, map[string]any{
				"recorded": recorded,
				"receipt":  receipt,
			})
		},
	}
	addPhaseReceiptCommonFlags(cmd, &flags)
	cmd.Flags().StringArrayVar(&flags.evidence, "evidence", nil, "Existing evidence path (repeatable)")
	cmd.Flags().StringVar(&flags.note, "note", "", "Concise decision or skip reason")
	cmd.Flags().BoolVar(&flags.skip, "skip", false, "Record an explicit phase skip")
	return cmd
}

func newPhaseReceiptStopCmd() *cobra.Command {
	var flags phaseReceiptFlags
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Record a blocked or failed active phase",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			receipt, recorded, err := pipeline.StopPhase(flags.options(), flags.failed)
			if err != nil {
				return phaseReceiptError(err)
			}
			return writePhaseReceiptJSON(cmd, map[string]any{
				"recorded": recorded,
				"receipt":  receipt,
			})
		},
	}
	addPhaseReceiptCommonFlags(cmd, &flags)
	cmd.Flags().StringArrayVar(&flags.evidence, "evidence", nil, "Existing evidence path (repeatable)")
	cmd.Flags().StringVar(&flags.note, "note", "", "Concise blocker or failure reason")
	cmd.Flags().BoolVar(&flags.failed, "failed", false, "Record failure instead of a resumable blocker")
	return cmd
}

func newPhaseReceiptStatusCmd() *cobra.Command {
	var path string
	var runID string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Read the latest phase receipt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return phaseReceiptError(fmt.Errorf("phase receipt path is required"))
			}
			if runID == "" {
				return phaseReceiptError(fmt.Errorf("run ID is required"))
			}
			receipt, err := pipeline.LatestPhaseReceipt(path, runID)
			if err != nil {
				return phaseReceiptError(err)
			}
			return writePhaseReceiptJSON(cmd, receipt)
		},
	}
	cmd.Flags().StringVar(&path, "file", "", "Phase receipt JSONL path")
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID")
	return cmd
}

func addPhaseReceiptCommonFlags(cmd *cobra.Command, flags *phaseReceiptFlags) {
	cmd.Flags().StringVar(&flags.path, "file", "", "Phase receipt JSONL path")
	cmd.Flags().StringVar(&flags.runID, "run-id", "", "Run ID")
	cmd.Flags().StringVar(&flags.phase, "phase", "", "Phase ID")
}

func (flags phaseReceiptFlags) options() pipeline.PhaseReceiptOptions {
	return pipeline.PhaseReceiptOptions{
		Path:     flags.path,
		RunID:    flags.runID,
		Phase:    flags.phase,
		Evidence: flags.evidence,
		Note:     flags.note,
		Resume:   flags.resume,
	}
}

func phaseReceiptError(err error) error {
	return &ExitError{Code: ExitInputError, Err: err}
}

func writePhaseReceiptJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
