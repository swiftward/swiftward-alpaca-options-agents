// Package harness holds the clock. It decides WHEN a session runs and says why
// it woke it; it never decides what to trade - that is the session's job, and
// the hackathon's autonomy requirement rests on the difference.
package harness

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

// Run reads the declaration naming the sessions and their causes, then holds the
// clock until ctx ends.
//
// With no declaration there is nothing to wake and no cause to record, so this
// refuses to start rather than idling: a harness that runs while waking nobody
// is indistinguishable from a working one.
func Run(ctx context.Context, declarationPath string, log *zap.Logger) error {
	if declarationPath == "" {
		return fmt.Errorf("harness role needs DECLARATION: the file naming the sessions and when each one wakes")
	}
	raw, err := os.ReadFile(declarationPath)
	if err != nil {
		return fmt.Errorf("read declaration %s: %w", declarationPath, err)
	}
	log.Info("declaration read", zap.String("path", declarationPath), zap.Int("bytes", len(raw)))

	return fmt.Errorf("declaration format is not implemented yet: nothing can be scheduled from %s", declarationPath)
}
