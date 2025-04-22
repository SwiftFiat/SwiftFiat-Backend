// internal/service/activity_log_cleanup.go
package activitylogs

// import (
// 	"context"
// 	"time"

// )

// type ActivityLogCleanupService struct {
// 	repo     *repository.ActivityLogRepository
// 	interval time.Duration
// 	retention time.Duration
// }

// func NewActivityLogCleanupService(repo *repository.ActivityLogRepository) *ActivityLogCleanupService {
// 	return &ActivityLogCleanupService{
// 		repo:     repo,
// 		interval: 24 * time.Hour, // Run daily
// 		retention: 90 * 24 * time.Hour, // Keep logs for 90 days
// 	}
// }

// func (s *ActivityLogCleanupService) Start() {
// 	ticker := time.NewTicker(s.interval)
// 	defer ticker.Stop()

// 	for {
// 		<-ticker.C
// 		s.cleanup()
// 	}
// }

// func (s *ActivityLogCleanupService) cleanup() {
// 	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
// 	defer cancel()

// 	threshold := time.Now().Add(-s.retention)
// 	_, err := s.repo.DeleteBefore(ctx, threshold)
// 	if err != nil {
// 		// Log error
// 	}
// }