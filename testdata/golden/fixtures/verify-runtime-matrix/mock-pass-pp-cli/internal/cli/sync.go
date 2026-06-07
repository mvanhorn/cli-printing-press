// Package cli models a generated CLI's command layout for the verify
// data-pipeline gate. The mock-pass-pp-cli fixture implements its actual
// command handling in cmd/mock-pass-pp-cli/main.go, but the verify gate's
// sync-capability detection (hasRegisteredCommandFileWithPrefix, via
// runDataPipelineTest's cliHasSyncCommand) probes internal/cli for a
// sync-prefixed command file the way a real generated CLI is structured.
// This stub makes the fixture representative so the data-pipeline gate runs
// the binary's real sync handler and exercises the sync->sql->rows PASS path
// end-to-end, rather than being skipped as "no sync command".
package cli
