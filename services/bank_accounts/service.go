package bankaccounts

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
)

type BankAccountService struct {
	store           *db.Store
	logger          *logging.Logger
	providerService *providers.ProviderService
}

func NewBankAccountService(
	store *db.Store,
	logger *logging.Logger,
	providerService *providers.ProviderService,
) *BankAccountService {
	return &BankAccountService{
		store:           store,
		logger:          logger,
		providerService: providerService,
	}
}

// ============================================================
// BANK ACCOUNT MANAGEMENT
// ============================================================

// CreateBankAccount creates and verifies a new bank account
func (s *BankAccountService) CreateBankAccount(ctx context.Context, userID int64, req *CreateBankAccountRequest) (*BankAccountResponse, error) {
	s.logger.Info(fmt.Sprintf("Creating bank account for user %d", userID))

	// Get Paystack provider
	provider, exists := s.providerService.GetProvider(providers.Paystack)
	if !exists {
		return nil, fmt.Errorf("paystack provider not available")
	}

	fiatProvider, ok := provider.(*fiat.NombaProvider)
	if !ok {
		return nil, fmt.Errorf("invalid paystack provider")
	}

	// Verify account with Paystack
	accountInfo, err := fiatProvider.ResolveAccount(req.AccountNumber, req.BankCode)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to verify account: %v", err))
		return nil, fmt.Errorf("failed to verify bank account: %w", err)
	}

	// Create bank account in database
	params := db.CreateBankAccountParams{
		UserID:        userID,
		AccountName:   accountInfo.AccountName,
		AccountNumber: req.AccountNumber,
		BankCode:      req.BankCode,
		BankName:      req.BankName,
		AccountType:   sql.NullString{String: s.stringPtrOrEmpty(req.AccountType), Valid: req.AccountType != nil},
		Currency:      "NGN",
		Label:         sql.NullString{String: s.stringPtrOrEmpty(req.Label), Valid: req.Label != nil},
		Description:   sql.NullString{String: s.stringPtrOrEmpty(req.Description), Valid: req.Description != nil},
	}

	bankAccount, err := s.store.CreateBankAccount(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create bank account: %w", err)
	}

	// Mark as verified since we successfully resolved it
	bankAccount, err = s.store.VerifyBankAccount(ctx, db.VerifyBankAccountParams{
		ID:                    bankAccount.ID,
		VerificationMethod:    sql.NullString{String: "Paystack", Valid: true},
		VerificationReference: sql.NullString{String: fmt.Sprintf("%d", accountInfo.BankID), Valid: true},
	})

	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to mark account as verified: %v", err))
	}

	// If this is user's first bank account, make it default
	existingAccounts, _ := s.store.GetBankAccountsByUser(ctx, userID)
	if len(existingAccounts) == 1 {
		s.SetDefaultBankAccount(ctx, bankAccount.ID, userID)
	}

	return s.toBankAccountResponse(&bankAccount), nil

}

// GetDefaultBankAccount retrieves user's default bank account
func (s *BankAccountService) GetDefaultBankAccount(ctx context.Context, userID int64) (*BankAccountResponse, error) {
	account, err := s.store.GetDefaultBankAccount(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, utils.ErrBankAccountNotFound
		}
		return nil, fmt.Errorf("failed to fetch default bank account: %w", err)
	}

	return s.toBankAccountResponse(&account), nil
}

// SetDefaultBankAccount sets a bank account as the default
func (s *BankAccountService) SetDefaultBankAccount(ctx context.Context, accountID uuid.UUID, userID int64) error {
	// First, clear all default flags for this user
	err := s.store.ClearDefaultBankAccounts(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to clear defaults: %w", err)
	}

	// Set the new default
	_, err = s.store.SetDefaultBankAccount(ctx, db.SetDefaultBankAccountParams{
		ID:     accountID,
		UserID: userID,
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return utils.ErrBankAccountNotFound
		}
		return fmt.Errorf("failed to set default: %w", err)
	}

	return nil
}

// DeleteBankAccount soft deletes a bank account
func (s *BankAccountService) DeleteBankAccount(ctx context.Context, accountID uuid.UUID, userID int64) error {
	// Check if account exists and belongs to user
	account, err := s.store.GetBankAccount(ctx, accountID)
	if err != nil {
		if err == sql.ErrNoRows {
			return utils.ErrBankAccountNotFound
		}
		return fmt.Errorf("failed to fetch account: %w", err)
	}

	if account.UserID != userID {
		return fmt.Errorf("unauthorized")
	}

	// Don't allow deleting the last bank account if it's linked to active QR codes
	// Check for active QR codes using this account
	qrCodes, err := s.store.GetQRCodesByUser(ctx, userID)
	if err == nil && len(qrCodes) > 0 {
		// Check if any are active
		for _, qr := range qrCodes {
			if qr.Status == "active" && qr.LinkedBankAccountID.Valid && qr.LinkedBankAccountID.UUID == account.ID {
				return fmt.Errorf("cannot delete bank account: it is linked to active QR codes")
			}
		}
	}

	// Soft delete
	_, err = s.store.DeleteBankAccount(ctx, db.DeleteBankAccountParams{
		ID:     accountID,
		UserID: userID,
	})

	if err != nil {
		return fmt.Errorf("failed to delete bank account: %w", err)
	}

	// If this was the default account, set another as default
	if account.IsDefault {
		accounts, _ := s.store.GetBankAccountsByUser(ctx, userID)
		if len(accounts) > 0 {
			s.SetDefaultBankAccount(ctx, accounts[0].ID, userID)
		}
	}
	return nil
}

// VerifyBankAccount manually verifies a bank account (admin function)
func (s *BankAccountService) VerifyBankAccount(ctx context.Context, accountID uuid.UUID, method, reference string) error {
	_, err := s.store.VerifyBankAccount(ctx, db.VerifyBankAccountParams{
		ID:                    accountID,
		VerificationMethod:    sql.NullString{String: method, Valid: true},
		VerificationReference: sql.NullString{String: reference, Valid: true},
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return utils.ErrBankAccountNotFound
		}
		return fmt.Errorf("failed to verify account: %w", err)
	}

	return nil
}

// UpdateBankAccountStatus updates the status of a bank account
func (s *BankAccountService) UpdateBankAccountStatus(ctx context.Context, accountID uuid.UUID, status string, isActive bool) error {
	_, err := s.store.UpdateBankAccountStatus(ctx, db.UpdateBankAccountStatusParams{
		ID:       accountID,
		Status:   status,
		IsActive: isActive,
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return utils.ErrBankAccountNotFound
		}
		return fmt.Errorf("failed to update bank account status: %w", err)
	}

	return nil
}
func (s *BankAccountService) GetAllBankAccounts(ctx context.Context) ([]*BankAccountResponse, error) {
	accounts, err := s.store.GetAllBankAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bank accounts: %w", err)
	}

	var responses []*BankAccountResponse
	for _, account := range accounts {
		responses = append(responses, s.toBankAccountResponse(&account))
	}

	return responses, nil
}


// GetBankAccounts retrieves all bank accounts for a user
func (s *BankAccountService) GetBankAccounts(ctx context.Context, userID int64) ([]*BankAccountResponse, error) {
	accounts, err := s.store.GetBankAccountsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bank accounts: %w", err)
	}

	var responses []*BankAccountResponse
	for _, account := range accounts {
		responses = append(responses, s.toBankAccountResponse(&account))
	}

	return responses, nil
}

// toBankAccountResponse converts db model to response
func (s *BankAccountService) toBankAccountResponse(account *db.BankAccount) *BankAccountResponse {
	// Get bank logo using the bank code
	bankLogo := fiat.GetBankLogoByCode(account.BankCode)

	return &BankAccountResponse{
		ID:            account.ID,
		AccountName:   account.AccountName,
		AccountNumber: account.AccountNumber,
		BankCode:      account.BankCode,
		BankName:      account.BankName,
		BankLogo:      bankLogo,
		AccountType:   s.nullStringToPtr(account.AccountType),
		Currency:      account.Currency,
		IsVerified:    account.IsVerified,
		IsDefault:     account.IsDefault,
		Status:        account.Status,
		Label:         s.nullStringToPtr(account.Label),
		CreatedAt:     account.CreatedAt,
	}
}

func (s *BankAccountService) stringPtrOrEmpty(str *string) string {
	if str == nil {
		return ""
	}
	return *str
}

func (s *BankAccountService) nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}
