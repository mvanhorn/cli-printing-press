# Publish Turn Boundary

Publishing a printed CLI opens or updates a pull request in
`mvanhorn/printing-press-library`, a repository outside the current local CLI
workspace. An `AskUserQuestion` answer inside polish is not enough authority to
perform that cross-repo side effect in the same model turn, especially in Auto
Mode where the model may auto-select the recommended option.

When polish offers **Publish separately**, it must:

1. Treat the menu answer as intent to hand off, not permission to execute.
2. Print the exact `/printing-press-publish <cli-name> --from-polish` command
   for the user, preserving the post-publish retro offer for this handoff.
3. Stop after printing that command.

The publish action may run only after a fresh user-authored message explicitly
asks to publish or invokes `/printing-press-publish`. Do not chain from polish's
Publish Offer directly into `/printing-press-publish`, and do not open a public
library PR from an auto-resolved polish menu option.

For polish, this rule covers the **Publish separately** handoff. Other skills
should define their own fresh-user boundary when they perform cross-repo side
effects.
