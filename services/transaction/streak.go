package transaction

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// StreakUpdater interface defines methods for updating streaks
// This allows loose coupling between transaction and streak services
type StreakUpdater interface {
	UpdateStreakOnTransaction(ctx context.Context, userID uuid.UUID, transactionID uuid.UUID, transactionType string) error
}

// SetStreakUpdater sets the streak updater for the transaction service
// Call this during service initialization to enable streak integration
func (s *TransactionService) SetStreakUpdater(updater StreakUpdater) {
	s.streakUpdater = updater
	s.logger.Info("Streak updater integrated with transaction service")
}

// updateStreakAfterTransaction updates user's streak after successful transaction
// This is called internally after transaction commit
func (s *TransactionService) updateStreakAfterTransaction(
	ctx context.Context,
	userID uuid.UUID,
	transactionID uuid.UUID,
	transactionType TransactionType,
) {
	if s.streakUpdater == nil {
		s.logger.Warn("Streak updater not configured, skipping streak update")
		return
	}

	// Update streak asynchronously to not block the transaction response
	go func() {
		if err := s.streakUpdater.UpdateStreakOnTransaction(
			ctx,
			userID,
			transactionID,
			string(transactionType),
		); err != nil {
			s.logger.Error(fmt.Sprintf(
				"Failed to update streak for user %d after %s transaction: %v",
				userID,
				transactionType,
				err,
			))
		} else {
			s.logger.Info(fmt.Sprintf(
				"Successfully updated streak for user %d after %s transaction",
				userID,
				transactionType,
			))
		}
	}()
}
