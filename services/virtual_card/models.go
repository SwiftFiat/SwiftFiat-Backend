package virtualcard

import (
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bridgecards"
)

var (
	ErrCardNotFound          = fmt.Errorf("card not found")
	ErrInsufficientFunds     = fmt.Errorf("insufficient funds in wallet")
	ErrCardLimitExceeded     = fmt.Errorf("card limit exceeded")
	ErrPlanLimitExceeded     = fmt.Errorf("plan card limit exceeded")
	ErrInvalidCardPlan       = fmt.Errorf("invalid card plan")
	ErrCardAlreadyTerminated = fmt.Errorf("card already terminated")
	ErrSpendingLimitExceeded = fmt.Errorf("spending limit exceeded")
)

// ============================================================================
// CARD CREATION
// ============================================================================

type CreateCardResult struct {
	Card              *db.VirtualCard
	FundingRecord     *db.CardFundingHistory
	BillingRecord     *db.CardBillingHistory
	BridgeCardDetails *bridgecards.CreateCardResponse
}
