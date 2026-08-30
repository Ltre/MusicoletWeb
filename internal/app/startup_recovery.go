package app

import (
	"context"
	"fmt"
)

// RecoverStartup repairs durable cross-store work before the HTTP server starts.
// Import journals run first because their Git refs may already have advanced
// while SQLite still contains the previous Working State. Running generic Git
// reconciliation first could incorrectly audit that temporary mismatch.
func (s *Service) RecoverStartup(ctx context.Context) error {
	if err := s.RecoverCommitJournals(ctx); err != nil {
		return fmt.Errorf("recover import journals: %w", err)
	}
	if err := s.ReconcileGit(ctx); err != nil {
		return fmt.Errorf("reconcile Git audit: %w", err)
	}
	return nil
}
