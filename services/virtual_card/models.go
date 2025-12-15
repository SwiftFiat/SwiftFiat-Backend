package virtualcard

import (
	"fmt"
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

// 'active', 'frozen', 'terminated', 'inactive'
type VirtualCardStatus string

const (
	VirtualCardStatusActive     VirtualCardStatus = "active"
	VirtualCardStatusFrozen     VirtualCardStatus = "frozen"
	VirtualCardStatusTerminated VirtualCardStatus = "terminated"
	VirtualCardStatusInactive   VirtualCardStatus = "inactive"
)

// 'pending', 'successful', 'failed', 'reversed'
type CardFundingStatus string

const (
	CardFundingStatusPending    CardFundingStatus = "pending"
	CardFundingStatusSuccessful CardFundingStatus = "successful"
	CardFundingStatusFailed     CardFundingStatus = "failed"
	CardFundingStatusReversed   CardFundingStatus = "reversed"
)

// 'pending', 'successful', 'declined', 'reversed'
type CardTransactionStatus string

const (
	CardTransactionStatusPending    CardTransactionStatus = "pending"
	CardTransactionStatusSuccessful CardTransactionStatus = "successful"
	CardTransactionStatusDeclined   CardTransactionStatus = "declined"
	CardTransactionStatusReversed   CardTransactionStatus = "reversed"
)

// 'active', 'cancelled', 'failed', 'paused'
type CardSubscriptionStatus string

const (
	CardSubscriptionStatusActive    CardSubscriptionStatus = "active"
	CardSubscriptionStatusCancelled CardSubscriptionStatus = "cancelled"
	CardSubscriptionStatusFailed    CardSubscriptionStatus = "failed"
	CardSubscriptionStatusPaused    CardSubscriptionStatus = "paused"
)

// 'pending', 'completed', 'failed', 'waived'
type CardBillingHistoryStatus string

const (
	CardBillingHistoryStatusPending    CardBillingHistoryStatus = "pending"
	CardBillingHistoryStatusSuccessful CardBillingHistoryStatus = "successful"
	CardBillingHistoryStatusFailed     CardBillingHistoryStatus = "failed"
	CardBillingHistoryStatusWaived     CardBillingHistoryStatus = "waived"
)
