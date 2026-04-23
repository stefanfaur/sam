package tools

import "context"

// RTKClient is the subset of *rtk.Client that Bash/Read tools depend on.
// Declared here so tests can inject fakes without importing the rtk package.
type RTKClient interface {
	Enabled() bool
	Rewrite(ctx context.Context, cmd string) (string, bool, error)
	Read(ctx context.Context, path string) ([]byte, error)
}
