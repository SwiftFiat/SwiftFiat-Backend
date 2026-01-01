package rewards

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/security"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ============================================================================
// REWARD SERVICE
// ============================================================================
// This service handles all reward points operations:
// - Awarding points after successful bill payments
// - Redeeming points during bill payments
// - Managing reward configurations (admin)
// - Providing reward history and analytics
// ============================================================================

// RewardService handles reward points operations
type RewardService struct {
	store  *db.Store
	logger *logging.Logger
	notif  *service.PushNotificationService
	cache  *security.Cache
}

// NewRewardService creates a new reward service instance
func NewRewardService(
	store *db.Store,
	logger *logging.Logger,
	notif *service.PushNotificationService,
	cache *security.Cache,
) *RewardService {
	return &RewardService{
		store:  store,
		logger: logger,
		notif:  notif,
		cache:  cache,
	}
}

// ============================================================================
// EARN REWARD POINTS
// ============================================================================

// AwardRewardPointsParams represents parameters for awarding reward points
type AwardRewardPointsParams struct {
	UserID            int64
	TransactionID     uuid.UUID
	TransactionAmount string
	TransactionType   string // "bill_payment", "gift_card", etc.
	ServiceInfo       string // For notification message
}

// AwardRewardPoints automatically awards reward points after successful transaction
// This should be called after a bill payment is successfully completed
func (s *RewardService) AwardRewardPoints(ctx context.Context, params AwardRewardPointsParams) (*RewardTransactionResponse, error) {
	s.logger.Info("Awarding reward points", map[string]any{
		"user_id":        params.UserID,
		"transaction_id": params.TransactionID,
		"amount":         params.TransactionAmount,
	})

	// Get active reward configuration
	config, err := s.store.GetActiveRewardConfiguration(ctx, params.TransactionType)
	if err != nil {
		if err == sql.ErrNoRows {
			s.logger.Info("No active reward configuration found", map[string]interface{}{
				"transaction_type": params.TransactionType,
			})
			return nil, nil // No rewards configured, not an error
		}
		return nil, fmt.Errorf("failed to get reward configuration: %w", err)
	}

	// Check minimum transaction amount
	minTxAmount, err := decimal.NewFromString(config.MinTransactionAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid min_transaction_amount in config: %w", err)
	}

	txAmount, err := decimal.NewFromString(params.TransactionAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction amount in config: %w", err)
	}

	if txAmount.LessThan(minTxAmount) {
		s.logger.Info("Transaction amount below minimum for rewards", map[string]interface{}{
			"tx_amount":        params.TransactionAmount,
			"minimum_txAmount": minTxAmount,
		})
		return nil, nil // Below minimum, no rewards
	}

	// Calculate reward points
	pointsEarned := CalculateRewardPoints(txAmount.IntPart(), mustParseFloat(config.RewardRate))

	var maxPointPerTX float64
	if config.MaxPointsPerTransaction.Valid {
		maxPointPerTX, err = strconv.ParseFloat(config.MaxPointsPerTransaction.String, 64)
		if err != nil {
			return nil, err
		}
	}

	// Apply max points cap if configured
	if config.MaxPointsPerTransaction.Valid && float64(pointsEarned) > maxPointPerTX {
		pointsEarned = int64(maxPointPerTX)
	}
	// Skip if no points to award
	if pointsEarned <= 0 {
		return nil, nil
	}

	// Start transaction
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.store.WithTx(tx)

	// Award points using the optimized query
	description := fmt.Sprintf("Earned ₦%d reward points from %s payment of ₦%s",
		pointsEarned, params.TransactionType, params.TransactionAmount)

	rewardTx, err := qtx.AwardRewardPoints(ctx, db.AwardRewardPointsParams{
		UserID:                params.UserID,
		PointsAmount:          fmt.Sprintf("%d", pointsEarned),
		TransactionID:         uuid.NullUUID{UUID: params.TransactionID, Valid: true},
		SourceTransactionType: sql.NullString{String: params.TransactionType, Valid: true},
		TransactionAmount:     sql.NullString{String: params.TransactionAmount, Valid: true},
		RewardConfigID:        sql.NullInt64{Int64: config.ID, Valid: true},
		Description:           sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to award reward points: %w", err)
	}

	pointsEarnedToDecimal := decimal.NewFromInt(pointsEarned)

	amountUSD, err := utils.ConvertToUSD(ctx, pointsEarnedToDecimal, "NGN")
	if err != nil {
		return nil, fmt.Errorf("failed to convert to USD: %w", err)
	}

	ttx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          params.UserID,
		Amount:          pointsEarnedToDecimal.String(),
		Currency:        "NGN",
		AmountUsd:       amountUSD.String(),
		Type:            string(transaction.Rewards),
		TransactionFlow: string(transaction.InPlatform),
		Description:     sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	_, err = qtx.CreateRewardTransaction(ctx, db.CreateRewardTransactionParams{
		UserID:                params.UserID,
		PointsAmount:          fmt.Sprintf("+ %s", pointsEarnedToDecimal.String()),
		NairaValue:            pointsEarnedToDecimal.String(),
		TransactionID:         uuid.NullUUID{UUID: ttx.ID, Valid: true},
		SourceTransactionType: sql.NullString{String: params.TransactionType, Valid: true},
		TransactionAmount:     sql.NullString{String: params.TransactionAmount, Valid: true},
		RewardConfigID:        sql.NullInt64{Int64: config.ID, Valid: true},
		BalanceAfter:          rewardTx.BalanceAfter,
		Status:                string(transaction.Success),
		Description:           sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create reward transaction: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info("Reward points awarded successfully", map[string]interface{}{
		"user_id":       params.UserID,
		"points_earned": pointsEarned,
		"reward_tx_id":  rewardTx.ID,
		"balance_after": rewardTx.BalanceAfter,
	})

	// Clear user balance cache
	s.clearUserRewardCache(int32(params.UserID))

	// Send notification asynchronously
	go func() {
		message := FormatRewardMessage(RewardTransactionTypeEarned, pointsEarned, params.ServiceInfo)
		err := s.notif.SendRewardNotification(ctx, params.UserID, message, RewardTransactionTypeEarned, pointsEarned)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Error sending reward notification: %v", err))
		}
	}()

	return MapRewardTransactionToResponse(&rewardTx), nil
}

// ============================================================================
// REDEEM REWARD POINTS
// ============================================================================

// RedeemRewardPointsParams represents parameters for redeeming reward points
type RedeemRewardPointsParams struct {
	UserID             int64
	PointsToRedeem     string
	BillTransactionID  uuid.UUID
	OriginalBillAmount string
	ServiceType        string
	ServiceProvider    string
}

// RedeemRewardPoints redeems reward points and applies discount to bill payment
// This should be called before processing the actual bill payment
// Returns the final amount to pay after discount
func (s *RewardService) RedeemRewardPoints(ctx context.Context, params RedeemRewardPointsParams) (*RewardRedemptionResponse, float64, error) {
	s.logger.Info("Redeeming reward points", map[string]any{
		"user_id":     params.UserID,
		"points":      params.PointsToRedeem,
		"bill_amount": params.OriginalBillAmount,
	})

	// Get user's current balance
	balance, err := s.store.GetUserRewardBalance(ctx, params.UserID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get user reward balance: %w", err)
	}

	// Validate redemption
	if err := ValidateRewardRedemption(
		mustParseFloat(params.PointsToRedeem),
		mustParseFloat(balance.RewardBalance),
		mustParseFloat(params.OriginalBillAmount)); err != nil {
		return nil, 0, err
	}

	// Calculate final amount after discount
	discountAmount := CalculateDiscount(mustParseFloat(params.PointsToRedeem))
	finalAmount := mustParseFloat(params.OriginalBillAmount) - discountAmount

	// Start transaction
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.store.WithTx(tx)

	// Redeem points using optimized query
	description := fmt.Sprintf("Redeemed ₦%s reward points on %s payment",
		params.PointsToRedeem, params.ServiceType)

	rewardTx, err := qtx.RedeemRewardPointsSimple(ctx, db.RedeemRewardPointsSimpleParams{
		UserID:            params.UserID,
		PointsAmount:      params.PointsToRedeem,
		TransactionID:     uuid.NullUUID{UUID: params.BillTransactionID, Valid: true},
		TransactionAmount: sql.NullString{String: params.OriginalBillAmount, Valid: true},
		Description:       sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to redeem reward points: %w", err)
	}

	// Create detailed redemption record
	redemption, err := qtx.CreateRewardRedemption(ctx, db.CreateRewardRedemptionParams{
		RewardTransactionID:      rewardTx.ID,
		UserID:                   int32(params.UserID),
		BillPaymentTransactionID: params.BillTransactionID,
		PointsRedeemed:           params.PointsToRedeem,
		DiscountAmount:           fmt.Sprintf("%.2f", discountAmount),
		OriginalBillAmount:       params.OriginalBillAmount,
		FinalAmountPaid:          fmt.Sprintf("%.2f", finalAmount),
		ServiceType:              sql.NullString{String: params.ServiceType, Valid: true},
		ServiceProvider:          sql.NullString{String: params.ServiceProvider, Valid: true},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create redemption record: %w", err)
	}

	pointsToReddemDecimal, err := utils.ToDecimal(params.PointsToRedeem)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to convert points to decimal: %w", err)
	}

	amountUSD, err := utils.ConvertToUSD(ctx, pointsToReddemDecimal, "NGN")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to convert points to USD: %w", err)
	}

	// Create transaction
	ttx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          params.UserID,
		Amount:          params.PointsToRedeem,
		Currency:        "NGN",
		AmountUsd:       amountUSD.String(),
		Type:            string(transaction.Rewards),
		TransactionFlow: string(transaction.InPlatform),
		Description:     sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Create reward transaction
	_, err = qtx.CreateRewardTransaction(ctx, db.CreateRewardTransactionParams{
		UserID:                params.UserID,
		PointsAmount:          fmt.Sprintf("- %s", params.PointsToRedeem),
		NairaValue:            pointsToReddemDecimal.String(),
		SourceTransactionType: sql.NullString{String: "bill_payment", Valid: true},
		TransactionID:         uuid.NullUUID{UUID: ttx.ID, Valid: true},
		TransactionAmount:     sql.NullString{String: params.OriginalBillAmount, Valid: true},
		BalanceAfter:          rewardTx.BalanceAfter,
		Status:                string(transaction.Success),
		Description:           sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create reward transaction: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info("Reward points redeemed successfully", map[string]interface{}{
		"user_id":         params.UserID,
		"points_redeemed": params.PointsToRedeem,
		"discount_amount": discountAmount,
		"final_amount":    finalAmount,
		"balance_after":   rewardTx.BalanceAfter,
	})

	// Clear user balance cache
	s.clearUserRewardCache(int32(params.UserID))

	pointsToRedeem, err := strconv.Atoi(params.PointsToRedeem)
	if err != nil {
		return nil, 0, err
	}

	// Send notification asynchronously
	go func() {
		serviceInfo := fmt.Sprintf("%s payment", params.ServiceType)
		message := FormatRewardMessage(RewardTransactionTypeRedeemed, int64(pointsToRedeem), serviceInfo)
		s.notif.SendRewardNotification(ctx, params.UserID, message, RewardTransactionTypeRedeemed, int64(pointsToRedeem))
	}()

	return MapRewardRedemptionToResponse(&redemption), finalAmount, nil
}

// ============================================================================
// USER REWARD QUERIES
// ============================================================================

// GetUserRewardBalance returns user's current reward balance
func (s *RewardService) GetUserRewardBalance(ctx context.Context, userID int32) (*RewardBalanceResponse, error) {
	// Try cache first
	cacheKey := fmt.Sprintf("reward:balance:%d", userID)
	if cached, found := s.cache.Get(cacheKey); found {
		if balance, ok := cached.(*RewardBalanceResponse); ok {
			return balance, nil
		}
	}

	balance, err := s.store.GetUserRewardBalance(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to get reward balance: %w", err)
	}

	response := &RewardBalanceResponse{
		CurrentBalance:  balance.RewardBalance,
		TotalEarned:     balance.TotalRewardEarned,
		TotalRedeemed:   balance.TotalRewardRedeemed,
		AvailablePoints: balance.RewardBalance,
	}

	// Cache for 5 minutes
	s.cache.Insert(cacheKey, response)

	return response, nil
}

// GetUserRewardSummary returns comprehensive reward summary for user
func (s *RewardService) GetUserRewardSummary(ctx context.Context, userID int64) (*RewardSummaryResponse, error) {
	summary, err := s.store.GetUserRewardSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get reward summary: %w", err)
	}

	return &RewardSummaryResponse{
		UserID:                  int32(summary.UserID),
		CurrentBalance:          summary.CurrentBalance,
		TotalEarned:             summary.TotalEarned,
		TotalRedeemed:           summary.TotalRedeemed,
		TotalEarnTransactions:   summary.TotalEarnTransactions,
		TotalRedeemTransactions: summary.TotalRedeemTransactions,
		LastUpdated:             summary.LastUpdated,
	}, nil
}

// GetUserRewardHistory returns paginated reward transaction history
func (s *RewardService) GetUserRewardHistory(ctx context.Context, userID int32, filter RewardHistoryFilter) (*RewardHistoryResponse, error) {
	// Set defaults
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = DefaultPageSize
	}
	if filter.PageSize > MaxPageSize {
		filter.PageSize = MaxPageSize
	}

	offset := (filter.Page - 1) * filter.PageSize

	var transactions []db.RewardTransaction
	var totalCount int64
	var err error

	// Query based on filters
	if filter.Type != "" && filter.DateFrom != nil && filter.DateTo != nil {
		transactions, err = s.store.ListUserRewardTransactionsByTypeAndDateRange(ctx,
			db.ListUserRewardTransactionsByTypeAndDateRangeParams{
				UserID:          int64(userID),
				TransactionType: filter.Type,
				CreatedAt:       *filter.DateFrom,
				CreatedAt_2:     *filter.DateTo,
				Limit:           int32(filter.PageSize),
				Offset:          int32(offset),
			})
		totalCount, _ = s.store.CountUserRewardTransactionsByType(ctx,
			db.CountUserRewardTransactionsByTypeParams{
				UserID:          int64(userID),
				TransactionType: filter.Type,
			})
	} else if filter.Type != "" {
		transactions, err = s.store.ListUserRewardTransactionsByType(ctx,
			db.ListUserRewardTransactionsByTypeParams{
				UserID:          int64(userID),
				TransactionType: filter.Type,
				Limit:           int32(filter.PageSize),
				Offset:          int32(offset),
			})
		totalCount, _ = s.store.CountUserRewardTransactionsByType(ctx,
			db.CountUserRewardTransactionsByTypeParams{
				UserID:          int64(userID),
				TransactionType: filter.Type,
			})
	} else if filter.DateFrom != nil && filter.DateTo != nil {
		transactions, err = s.store.ListUserRewardTransactionsByDateRange(ctx,
			db.ListUserRewardTransactionsByDateRangeParams{
				UserID:      int64(userID),
				CreatedAt:   *filter.DateFrom,
				CreatedAt_2: *filter.DateTo,
				Limit:       int32(filter.PageSize),
				Offset:      int32(offset),
			})
		totalCount, _ = s.store.CountUserRewardTransactions(ctx, int64(userID))
	} else {
		transactions, err = s.store.ListUserRewardTransactions(ctx,
			db.ListUserRewardTransactionsParams{
				UserID: int64(userID),
				Limit:  int32(filter.PageSize),
				Offset: int32(offset),
			})
		totalCount, _ = s.store.CountUserRewardTransactions(ctx, int64(userID))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get reward history: %w", err)
	}

	// Convert to response format
	items := make([]RewardTransactionItem, len(transactions))
	for i, tx := range transactions {
		items[i] = RewardTransactionItem{
			ID:           fmt.Sprintf("%d", tx.ID),
			Type:         tx.TransactionType,
			Amount:       tx.PointsAmount,
			Description:  tx.Description.String,
			Status:       tx.Status,
			BalanceAfter: tx.BalanceAfter,
			Date:         tx.CreatedAt,
		}
		if tx.SourceTransactionType.Valid {
			items[i].SourceTransaction = tx.SourceTransactionType.String
		}
	}

	totalPages := int(totalCount / int64(filter.PageSize))
	if totalCount%int64(filter.PageSize) > 0 {
		totalPages++
	}

	return &RewardHistoryResponse{
		Transactions: items,
		Pagination: PaginationMetadata{
			Page:       filter.Page,
			PageSize:   filter.PageSize,
			TotalPages: totalPages,
			TotalCount: totalCount,
		},
	}, nil
}

// GetRecentRewardActivity returns recent reward activity for dashboard
func (s *RewardService) GetRecentRewardActivity(ctx context.Context, userID int32, limit int) ([]RewardTransactionItem, error) {
	if limit <= 0 {
		limit = 10
	}

	transactions, err := s.store.GetRecentRewardActivity(ctx, db.GetRecentRewardActivityParams{
		UserID: int64(userID),
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get recent activity: %w", err)
	}

	items := make([]RewardTransactionItem, len(transactions))
	for i, tx := range transactions {
		items[i] = RewardTransactionItem{
			ID:          fmt.Sprintf("%d", tx.ID),
			Type:        tx.TransactionType,
			Amount:      tx.PointsAmount,
			Description: tx.Description.String,
			Date:        tx.CreatedAt,
		}
	}

	return items, nil
}

// ============================================================================
// ADMIN: REWARD CONFIGURATION MANAGEMENT
// ============================================================================

// CreateRewardConfiguration creates a new reward configuration (admin only)
func (s *RewardService) CreateRewardConfiguration(ctx context.Context, req CreateRewardConfigRequest, adminID int32) (*RewardConfigurationResponse, error) {
	validFrom := time.Now()
	if req.ValidFrom != nil {
		validFrom = *req.ValidFrom
	}

	var maxPoints sql.NullString
	if req.MaxPointsPerTransaction != nil {
		maxPoints = sql.NullString{String: *req.MaxPointsPerTransaction, Valid: true}
	}

	rewardRate := strconv.FormatFloat(float64(req.RewardRate), 'f', -1, 64)

	config, err := s.store.CreateRewardConfiguration(ctx, db.CreateRewardConfigurationParams{
		ConfigName:              req.ConfigName,
		RewardRate:              rewardRate,
		TransactionType:         req.TransactionType,
		MinTransactionAmount:    req.MinTransactionAmount,
		MaxPointsPerTransaction: maxPoints,
		IsActive:                req.IsActive,
		ValidFrom:               validFrom,
		ValidUntil:              sql.NullTime{Time: req.ValidUntil, Valid: true},
		CreatedBy:               sql.NullInt32{Int32: adminID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create reward configuration: %w", err)
	}

	// Clear cache for active configurations
	s.cache.Delete("reward:config:active:" + req.TransactionType)

	return MapRewardConfigToResponse(&config), nil
}

// UpdateRewardConfiguration updates an existing reward configuration (admin only)
// UpdateRewardConfiguration updates an existing reward configuration (admin only)
func (s *RewardService) UpdateRewardConfiguration(ctx context.Context, configID int64, req UpdateRewardConfigRequest) (*RewardConfigurationResponse, error) {
    params := db.UpdateRewardConfigurationParams{
        ID: configID,
    }

    if req.ConfigName != "" {
        params.ConfigName = sql.NullString{String: req.ConfigName, Valid: true}
    }

    if req.RewardRate != "" {
        params.RewardRate = sql.NullString{String: req.RewardRate, Valid: true}
    }

    if req.TransactionType != "" {
        params.TransactionType = sql.NullString{String: req.TransactionType, Valid: true}
    }

    if req.MinTransactionAmount != "" {
        params.MinTransactionAmount = sql.NullString{String: req.MinTransactionAmount, Valid: true}
    }

    if req.MaxPointsPerTransaction != "" {
        params.MaxPointsPerTransaction = sql.NullString{String: req.MaxPointsPerTransaction, Valid: true}
    }

    if req.IsActive != nil {
        params.IsActive = sql.NullBool{Bool: *req.IsActive, Valid: true}
    }

    if !req.ValidFrom.IsZero() {
        params.ValidFrom = sql.NullTime{Time: req.ValidFrom, Valid: true}
    }

    if !req.ValidUntil.IsZero() {
        params.ValidUntil = sql.NullTime{Time: req.ValidUntil, Valid: true}
    }

    config, err := s.store.UpdateRewardConfiguration(ctx, params)
    if err != nil {
        return nil, fmt.Errorf("failed to update reward configuration: %w", err)
    }

    // Clear cache
    s.cache.Delete(fmt.Sprintf("reward:config:%d", configID))

    return MapRewardConfigToResponse(&config), nil
}

// ============================================================================
// ADMIN: REWARD ANALYTICS
// ============================================================================

// GetRewardStatistics returns overall reward system statistics (admin only)
func (s *RewardService) GetRewardStatistics(ctx context.Context) (*RewardStatisticsSummary, error) {
	stats, err := s.store.GetRewardStatisticsSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get reward statistics: %w", err)
	}

	totalPointsIssued, err := decimal.NewFromString(stats.TotalPointsIssued)
	if err != nil {
		return nil, err
	}

	totalPointsRedeemed, err := decimal.NewFromString(stats.TotalPointsRedeemed)
	if err != nil {
		return nil, err
	}

	redemptionRate := decimal.Zero
	// if stats.TotalPointsIssued > 0 {
	// 	redemptionRate = (stats.TotalPointsRedeemed / stats.TotalPointsIssued) * 100
	// }
	if totalPointsIssued.GreaterThan(decimal.Zero) {
		redemptionRate = totalPointsRedeemed.Div(totalPointsIssued).Mul(decimal.NewFromInt(100))
	}

	return &RewardStatisticsSummary{
		OutstandingLiability: stats.OutstandingLiability,
		TotalPointsIssued:    stats.TotalPointsIssued,
		TotalPointsRedeemed:  stats.TotalPointsRedeemed,
		UsersWithBalance:     stats.UsersWithBalance,
		TotalUsers:           stats.TotalUsers,
		RedemptionRate:       redemptionRate.String(),
	}, nil
}

// ============================================================================
// UTILITY METHODS
// ============================================================================

// clearUserRewardCache clears cached reward data for a user
func (s *RewardService) clearUserRewardCache(userID int32) {
	cacheKeys := []string{
		fmt.Sprintf("reward:balance:%d", userID),
		fmt.Sprintf("reward:summary:%d", userID),
	}
	for _, key := range cacheKeys {
		s.cache.Delete(key)
	}
}

// VerifyUserRewardBalance verifies user's balance matches transaction history
func (s *RewardService) VerifyUserRewardBalance(ctx context.Context, userID int64) (bool, error) {
	result, err := s.store.VerifyUserRewardBalance(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("failed to verify reward balance: %w", err)
	}

	// Check if there's any discrepancy
	return result.Discrepancy == 0, nil
}

type GetTopUsersByRewardsEarnedRow struct {
	ID                  int64  `json:"id"`
	FirstName           string `json:"first_name"`
	LastName            string `json:"last_name"`
	Email               string `json:"email"`
	RewardBalance       string `json:"reward_balance"`
	TotalRewardEarned   string `json:"total_reward_earned"`
	TotalRewardRedeemed string `json:"total_reward_redeemed"`
}

func MapGetTopUsersByRewardsEarnedRowToResponse(row *db.GetTopUsersByRewardsEarnedRow) *GetTopUsersByRewardsEarnedRow {
	return &GetTopUsersByRewardsEarnedRow{
		ID:                  row.ID,
		FirstName:           row.FirstName.String,
		LastName:            row.LastName.String,
		Email:               row.Email,
		RewardBalance:       row.RewardBalance,
		TotalRewardEarned:   row.TotalRewardEarned,
		TotalRewardRedeemed: row.TotalRewardRedeemed,
	}
}
