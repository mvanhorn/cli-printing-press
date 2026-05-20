package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"chatbot-factory-pp-cli/internal/pipeline"
	"chatbot-factory-pp-cli/internal/store"
)

func newMigrateCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "migrate [slug]",
		Short: "Migrate a project (or all) from legacy store to v2 state.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, _ := store.DefaultPath()
			s, err := store.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			var slugs []string
			if all {
				projects, err := s.ListProjects()
				if err != nil {
					return err
				}
				for _, p := range projects {
					slugs = append(slugs, p.Slug)
				}
			} else if len(args) == 1 {
				slugs = []string{args[0]}
			} else {
				return fmt.Errorf("provide a slug or --all")
			}

			for _, slug := range slugs {
				p, err := s.GetProject(slug)
				if err != nil || p.Slug == "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "skip %s: not in store\n", slug)
					continue
				}
				dir := filepath.Join(".", slug)
				statePath := filepath.Join(dir, ".chatbot-factory", "state.json")
				st := pipeline.NewState(slug, p.Channel)

				// Best-effort: copy phase statuses from bbolt store.
				legacyPhases, _ := s.ListPhases(slug)
				for name, ph := range legacyPhases {
					if cur, ok := st.Phases[name]; ok && ph.Status != "" {
						cur.Status = ph.Status
						st.Phases[name] = cur
					}
				}

				if err := st.Save(statePath); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "save %s: %v\n", slug, err)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "migrated %s → %s\n", slug, statePath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "migrate every project in the store")
	return cmd
}
