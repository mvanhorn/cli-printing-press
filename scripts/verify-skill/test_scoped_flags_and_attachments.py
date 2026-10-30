"""Regression tests for scoped flags and command attachments."""
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).parent))
import verify_skill


class ScopedCommandContracts(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.cli = Path(self.temp.name)
        self.source = self.cli / "internal" / "cli"
        self.source.mkdir(parents=True)

    def write(self, source):
        (self.source / "root.go").write_text(source, encoding="utf-8")

    def test_local_string_flags_shadow_global_and_same_file_booleans(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.PersistentFlags().BoolVar(&csv, "csv", false, "CSV output")
 rootCmd.AddCommand(newBatchCmd())
 rootCmd.AddCommand(newOtherCmd())
}
func newBatchCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "batch <id>"}
 cmd.Flags().StringVar(&csv, "csv", "", "Input CSV")
 cmd.Flags().StringVar(&title, "title", "", "Title")
 cmd.Flags().StringVar(&active, "active", "", "True, Any, False")
 cmd.Flags().BoolVar(&json, "json", false, "JSON output")
 return cmd
}
func newOtherCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "other"}
 cmd.Flags().BoolVar(&title, "title", false, "Title only")
 cmd.Flags().BoolVar(&active, "active", false, "Active only")
 return cmd
}
''')
        path, positional, flags = verify_skill._cli_invocation_from_tokens(
            ["batch", "--csv", "contacts.csv", "--title", "Sample", "--active", "True", "--json", "42"], self.cli)
        self.assertEqual(path, ["batch"])
        self.assertEqual(positional, ["42"])
        self.assertEqual(flags, ["--csv", "--title", "--active", "--json"])
        # A sibling's local flag remains boolean and cannot hide a stray value.
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["other", "--active", "false"], self.cli)[1], ["false"])

    def test_option_separator_preserves_negative_and_flag_shaped_positionals(self):
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["point", "--elev", "8530", "--", "-111.5838,40.5884", "--literal"], None),
            (["point"], ["-111.5838,40.5884", "--literal"], ["--elev"]))

    def test_constructor_variables_attach_only_to_the_returned_parent(self):
        self.write('''package cli
func Execute() { rootCmd.AddCommand(newUsersCmd()) }
func newUsersCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "users <user_id>"}
 { sub := newItemsCmd(); cmd.AddCommand(sub) }
 { sub := newCollectionsCmd(); cmd.AddCommand(sub) }
 orphan := newOrphanCmd()
 unrelated.AddCommand(orphan)
 return cmd
}
func newItemsCmd() *cobra.Command { return &cobra.Command{Use: "items"} }
func newCollectionsCmd() *cobra.Command { return &cobra.Command{Use: "collections"} }
func newOrphanCmd() *cobra.Command { return &cobra.Command{Use: "orphan"} }
''')
        for name in ("items", "collections"):
            self.assertIsNotNone(verify_skill.resolve_command_path(self.cli, ["users", name])[0])
        self.assertIsNone(verify_skill.resolve_command_path(self.cli, ["users", "orphan"])[0])

    def test_find_attachment_requires_existing_parent_and_matching_receiver(self):
        self.write('''package cli
func Execute() {
 rootCmd.AddCommand(newUsersCmd())
 if bm, _, err := rootCmd.Find([]string{"users", "bookmarks"}); err == nil && bm.Name() == "bookmarks" {
  bm.AddCommand(newNovelFindCmd())
  other.AddCommand(newWrongCmd())
 }
 if missing, _, err := rootCmd.Find([]string{"absent"}); err == nil {
  missing.AddCommand(newOrphanCmd())
 }
}
func newUsersCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "users"}
 cmd.AddCommand(newBookmarksCmd())
 return cmd
}
func newBookmarksCmd() *cobra.Command { return &cobra.Command{Use: "bookmarks"} }
func newNovelFindCmd() *cobra.Command { return &cobra.Command{Use: "find [query]"} }
func newWrongCmd() *cobra.Command { return &cobra.Command{Use: "wrong"} }
func newOrphanCmd() *cobra.Command { return &cobra.Command{Use: "orphan"} }
''')
        self.assertEqual(verify_skill.resolve_command_path(self.cli, ["users", "bookmarks", "find"])[1], "find [query]")
        for path in (["users", "bookmarks", "wrong"], ["absent", "orphan"], ["find"]):
            self.assertIsNone(verify_skill.resolve_command_path(self.cli, path)[0])

    def test_every_addcommand_argument_is_a_child_edge(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newTodoCmd(flags), newAuthCmd(flags))
}
func newTodoCmd(flags *rootFlags) *cobra.Command {
 cmd := &cobra.Command{Use: "todo"}
 cmd.AddCommand(
  newTodoListCmd(flags),
  newTodoAddCmd(flags),
 )
 return cmd
}
func newTodoListCmd(flags *rootFlags) *cobra.Command {
 return &cobra.Command{Use: "list"}
}
func newTodoAddCmd(flags *rootFlags) *cobra.Command {
 cmd := &cobra.Command{Use: "add [title]"}
 cmd.Flags().BoolVar(&stdin, "stdin", false, "Read stdin")
 return cmd
}
func newAuthCmd(flags *rootFlags) *cobra.Command {
 cmd := &cobra.Command{Use: "auth"}
 cmd.AddCommand(newSetCmd(flags), newStatusCmd(flags))
 return cmd
}
func newSetCmd(flags *rootFlags) *cobra.Command {
 return &cobra.Command{Use: "set"}
}
func newStatusCmd(flags *rootFlags) *cobra.Command {
 return &cobra.Command{Use: "status"}
}
''')
        for tokens, expected in (
            (["todo", "add", "book dentist"], ["todo", "add"]),
            (["auth", "status"], ["auth", "status"]),
        ):
            path, _, _ = verify_skill._cli_invocation_from_tokens(tokens, self.cli)
            self.assertEqual(path, expected)
        # The children of a later root AddCommand argument resolve too.
        self.assertIn("newAuthCmd", verify_skill.find_root_children(self.cli))

    def test_addcommand_mixes_constructors_and_inline_variables(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newSavedCmd())
}
func newSavedCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "saved"}
 add := &cobra.Command{Use: "add <name>"}
 cmd.AddCommand(newListCmd(), add)
 return cmd
}
func newListCmd() *cobra.Command {
 return &cobra.Command{Use: "list"}
}
''')
        for tokens in (["saved", "list"], ["saved", "add", "name"]):
            path, _, _ = verify_skill._cli_invocation_from_tokens(tokens, self.cli)
            self.assertEqual(path, tokens[:2])

    def test_helper_called_with_local_command_variable_declares_flags(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newSavedCmd())
}
func newSavedCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "saved"}
 add := &cobra.Command{Use: "add <name>"}
 addCritFlags(add, &crit)
 cmd.AddCommand(add)
 return cmd
}
func addCritFlags(c *cobra.Command, crit *criteria) {
 c.Flags().StringVar(&crit.deal, "deal", "", "Deal")
}
''')
        self.assertTrue(verify_skill.flag_declared_via_helper(
            self.cli, [self.source / "root.go"], "deal"))
        self.assertFalse(verify_skill.flag_declared_via_helper(
            self.cli, [self.source / "root.go"], "postcode"))

    def test_help_invocation_skips_required_positional_args(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newBuildCmd())
}
func newBuildCmd() *cobra.Command {
 return &cobra.Command{Use: "build <cart-id>", Args: cobra.ExactArgs(1)}
}
''')
        skill = self.cli / "SKILL.md"
        skill.write_text("```bash\ndemo-pp-cli build --help\ndemo-pp-cli build\n```\n", encoding="utf-8")
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_positional_args(self.cli, [skill], "demo-pp-cli", report)
        # Only the bare invocation is wrong; Cobra handles --help before Args.
        self.assertEqual(len(report.findings), 1)
        self.assertEqual(report.findings[0].evidence, "SKILL.md: (none)")

    def test_help_disabled_by_value_still_checks_positional_args(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newBuildCmd())
}
func newBuildCmd() *cobra.Command {
 return &cobra.Command{Use: "build <cart-id>", Args: cobra.ExactArgs(1)}
}
''')
        skill = self.cli / "SKILL.md"
        skill.write_text(
            "```bash\n"
            "demo-pp-cli build --help=true\n"
            "demo-pp-cli build --help=false\n"
            "demo-pp-cli build --help --help=0\n"
            "demo-pp-cli build --help=false --help\n"
            "```\n",
            encoding="utf-8",
        )
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_positional_args(self.cli, [skill], "demo-pp-cli", report)
        # Enabled help skips the check; disabled help does not, and the last
        # occurrence decides.
        self.assertEqual(len(report.findings), 2)
        self.assertTrue(all(f.severity == "error" for f in report.findings))

    def test_nested_inline_group_keeps_its_children(self):
        self.write('''package cli
func Execute() { rootCmd.AddCommand(newParentCmd()) }
func newParentCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "parent"}
 group := &cobra.Command{Use: "group", Args: cobra.NoArgs()}
 leaf := &cobra.Command{Use: "leaf <id>", Args: cobra.ExactArgs(1)}
 orphan := &cobra.Command{Use: "orphan"}
 group.AddCommand(leaf, newBuiltCmd())
 leaf.AddCommand(group)
 other.AddCommand(orphan)
 cmd.AddCommand(group)
 return cmd
}
func newBuiltCmd() *cobra.Command { return &cobra.Command{Use: "built"} }
''')
        path, positional, _ = verify_skill._cli_invocation_from_tokens(
            ["parent", "group", "leaf", "42"], self.cli)
        self.assertEqual(path, ["parent", "group", "leaf"])
        self.assertEqual(positional, ["42"])
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["parent", "group", "built"], self.cli)[0], ["parent", "group", "built"])
        self.assertEqual(verify_skill.resolve_command_path(
            self.cli, ["parent", "group", "leaf"])[1], "leaf <id>")
        # An inline command only counts when it is reachable from the parent.
        self.assertIsNone(verify_skill.resolve_command_path(
            self.cli, ["parent", "orphan"])[0])

        skill = self.cli / "SKILL.md"
        skill.write_text("```bash\ndemo-pp-cli parent group leaf 42\n```\n", encoding="utf-8")
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_positional_args(self.cli, [skill], "demo-pp-cli", report)
        self.assertEqual(report.findings, [])

    def test_inline_child_flags_do_not_change_the_parent(self):
        self.write('''package cli
func Execute() { rootCmd.AddCommand(newBatchCmd()) }
func newBatchCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "batch <id>", Args: cobra.ExactArgs(1)}
 cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
 cmd.Flags().StringVar(&mode, "mode", "", "Mode")
 child := &cobra.Command{Use: "child <id>", Args: cobra.ExactArgs(1)}
 child.Flags().StringVar(&body, "json", "", "JSON body")
 child.Flags().BoolVar(&fast, "mode", false, "Fast mode")
 cmd.AddCommand(child)
 return cmd
}
''')
        # The parent keeps its own flag types.
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["batch", "--json", "42"], self.cli)[1], ["42"])
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["batch", "--mode", "fast", "42"], self.cli)[1], ["42"])
        # The inline child reads its own declarations from the shared body.
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["batch", "child", "--json", "{}", "42"], self.cli)[1], ["42"])
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["batch", "child", "--mode", "42"], self.cli)[1], ["42"])

    def test_flag_fallback_needs_a_real_child_edge_for_graph_parents(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newExportCmd())
 rootCmd.AddCommand(newMessagesCmd())
 rootCmd.AddCommand(newChatCmd())
}
func newExportCmd() *cobra.Command { return &cobra.Command{Use: "export <resource>"} }
func newChatCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "chat"}
 cmd.AddCommand(newPostMessageCmd())
 return cmd
}
func newPostMessageCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "post_message"}
 cmd.Flags().StringVar(&text, "text", "", "Text")
 return cmd
}
''')
        # Flag lookup is per file, so the unrelated command needs its own file.
        (self.source / "messages.go").write_text('''package cli
func newMessagesCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "messages"}
 cmd.Flags().StringVar(&interval, "interval", "", "Poll interval")
 return cmd
}
''', encoding="utf-8")
        skill = self.cli / "SKILL.md"
        skill.write_text(
            "```bash\n"
            "demo-pp-cli export messages --interval 5s\n"
            "demo-pp-cli chat post_message --text hello\n"
            "```\n",
            encoding="utf-8",
        )
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_flag_commands(self.cli, [skill], "demo-pp-cli", report)
        # The unrelated messages command cannot excuse --interval on export,
        # while a real snake_case child still supplies its own flag.
        self.assertEqual(len(report.findings), 1)
        self.assertEqual(report.findings[0].command, "demo-pp-cli export")
        self.assertIn("--interval", report.findings[0].detail)

    def test_flag_fallback_accepts_a_child_wired_outside_the_graph(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newExportCmd())
 rootCmd.AddCommand(newParkingCmd())
 if parent := findCommand(rootCmd, "parking"); parent != nil {
  parent.AddCommand(newNearbyCmd())
 }
}
func newExportCmd() *cobra.Command { return &cobra.Command{Use: "export <resource>"} }
func newParkingCmd() *cobra.Command { return &cobra.Command{Use: "parking"} }
''')
        (self.source / "nearby.go").write_text('''package cli
func newNearbyCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "nearby"}
 cmd.Flags().StringVar(&lat, "lat", "", "Latitude")
 return cmd
}
''', encoding="utf-8")
        skill = self.cli / "SKILL.md"
        skill.write_text("```bash\ndemo-pp-cli parking nearby --lat 1\n```\n", encoding="utf-8")
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_flag_commands(self.cli, [skill], "demo-pp-cli", report)
        self.assertEqual(report.findings, [])

    def test_flag_fallback_keeps_a_constructor_attached_in_two_places(self):
        self.write('''package cli
func Execute() {
 rootCmd := &cobra.Command{Use: "demo-pp-cli"}
 rootCmd.AddCommand(newExportCmd())
 rootCmd.AddCommand(newParkingCmd())
 rootCmd.AddCommand(newNearbyCmd())
 if parent := findCommand(rootCmd, "parking"); parent != nil {
  parent.AddCommand(newNearbyCmd())
 }
}
func newExportCmd() *cobra.Command { return &cobra.Command{Use: "export <resource>"} }
func newParkingCmd() *cobra.Command { return &cobra.Command{Use: "parking"} }
''')
        (self.source / "nearby.go").write_text('''package cli
func newNearbyCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "nearby"}
 cmd.Flags().StringVar(&lat, "lat", "", "Latitude")
 return cmd
}
''', encoding="utf-8")
        skill = self.cli / "SKILL.md"
        skill.write_text("```bash\ndemo-pp-cli parking nearby --lat 1\n```\n", encoding="utf-8")
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_flag_commands(self.cli, [skill], "demo-pp-cli", report)
        self.assertEqual(report.findings, [])

    def test_helper_flags_stay_on_the_command_that_received_them(self):
        self.write('''package cli
func Execute() { rootCmd.AddCommand(newSavedCmd()) }
func newSavedCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "saved"}
 add := &cobra.Command{Use: "add"}
 addCritFlags(add, &crit)
 cmd.AddCommand(add)
 return cmd
}
''')
        (self.source / "flags.go").write_text('''package cli
func addCritFlags(cmd *cobra.Command, crit *criteria) {
 cmd.Flags().StringVar(&crit.deal, "deal", "", "Deal")
}
''', encoding="utf-8")
        skill = self.cli / "SKILL.md"
        skill.write_text(
            "```bash\ndemo-pp-cli saved add --deal x\ndemo-pp-cli saved --deal x\n```\n",
            encoding="utf-8",
        )
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_flag_commands(self.cli, [skill], "demo-pp-cli", report)
        # Only the parent recipe is wrong; the inline child got the flag.
        self.assertEqual([f.command for f in report.findings], ["demo-pp-cli saved"])
        self.assertIn("--deal", report.findings[0].detail)

    def test_returned_alias_keeps_the_command_helpers_flags_and_children(self):
        self.write('''package cli
func Execute() { rootCmd.AddCommand(newTargetCmd()) }
func newTargetCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "target <id>", Args: cobra.ExactArgs(1)}
 addTargetFlags(cmd, &opts)
 cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
 leaf := &cobra.Command{Use: "leaf"}
 leaf.Flags().StringVar(&body, "json", "", "JSON body")
 result := cmd
 result.AddCommand(leaf)
 return result
}
''')
        (self.source / "flags.go").write_text('''package cli
func addTargetFlags(cmd *cobra.Command, opts *options) {
 cmd.Flags().StringVar(&opts.region, "region", "", "Region")
}
''', encoding="utf-8")
        skill = self.cli / "SKILL.md"
        skill.write_text("```bash\ndemo-pp-cli target --region us 42\n```\n", encoding="utf-8")
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_flag_commands(self.cli, [skill], "demo-pp-cli", report)
        self.assertEqual(report.findings, [])
        self.assertEqual(verify_skill._cli_invocation_from_tokens(
            ["target", "--json", "42"], self.cli)[1], ["42"])
        self.assertEqual(verify_skill.resolve_command_path(
            self.cli, ["target", "leaf"])[1], "leaf")

    def test_returned_wrapper_falls_back_to_the_file_scan(self):
        self.write('''package cli
func Execute() { rootCmd.AddCommand(newTargetCmd()) }
func newTargetCmd() *cobra.Command {
 cmd := &cobra.Command{Use: "target", RunE: func(c *cobra.Command, args []string) error {
  err := run()
  return err
 }}
 addTargetFlags(cmd, &opts)
 return wrap(cmd)
}
''')
        (self.source / "flags.go").write_text('''package cli
func addTargetFlags(cmd *cobra.Command, opts *options) {
 cmd.Flags().StringVar(&opts.region, "region", "", "Region")
}
''', encoding="utf-8")
        skill = self.cli / "SKILL.md"
        skill.write_text("```bash\ndemo-pp-cli target --region us\n```\n", encoding="utf-8")
        report = verify_skill.Report(cli_dir=str(self.cli), skill_path=str(skill))
        verify_skill.check_flag_commands(self.cli, [skill], "demo-pp-cli", report)
        self.assertEqual(report.findings, [])

    def test_hook_addcommand_attaches_every_argument(self):
        self.write('''package cli
func init() {
 registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
  root.AddCommand(newFirstCmd(flags), newSecondCmd(flags))
 })
}
func newFirstCmd(flags *rootFlags) *cobra.Command {
 return &cobra.Command{Use: "first"}
}
func newSecondCmd(flags *rootFlags) *cobra.Command {
 return &cobra.Command{Use: "second"}
}
''')
        self.assertEqual(
            verify_skill.find_root_children(self.cli), ["newFirstCmd", "newSecondCmd"])


if __name__ == "__main__":
    unittest.main()
