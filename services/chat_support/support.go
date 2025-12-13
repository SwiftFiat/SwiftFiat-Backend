package chatsupport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
)

type SupportAdminService struct {
	store  *db.Store
	logger *logging.Logger
}

func NewSupportAdminService(
	store *db.Store,
	logger *logging.Logger,
) *SupportAdminService {
	return &SupportAdminService{
		store:  store,
		logger: logger,
	}
}

// CreateSupportAdmin creates a support admin profile for a user
func (s *SupportAdminService) CreateSupportAdmin(ctx context.Context, params *CreateSupportAdminParams) (*db.SupportAdmin, error) {
	// Check if admin already exists
	_, err := s.store.GetSupportAdminByUserID(ctx, params.UserID)
	if err == nil {
		return nil, ErrAdminAlreadyExists
	}
	if err != sql.ErrNoRows {
		s.logger.Error(fmt.Sprintf("failed to check existing admin: %v", err))
		return nil, err
	}

	// Create support admin
	admin, err := s.store.CreateSupportAdmin(ctx, db.CreateSupportAdminParams{
		UserID:               params.UserID,
		Status:               "offline",
		MaxConcurrentTickets: params.MaxConcurrentTickets,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create support admin: %v", err))
		return nil, err
	}

	return &admin, nil
}

// GetSupportAdminByUserID retrieves a support admin by user ID
func (s *SupportAdminService) GetSupportAdminByUserID(ctx context.Context, userID int64) (*db.SupportAdmin, error) {
	admin, err := s.store.GetSupportAdminByUserID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAdminNotFound
		}
		s.logger.Error(fmt.Sprintf("failed to get support admin: %v", err))
		return nil, err
	}
	return &admin, nil
}

// GetSupportAdminByID retrieves a support admin by ID
func (s *SupportAdminService) GetSupportAdminByID(ctx context.Context, adminID int64) (*db.SupportAdmin, error) {
	admin, err := s.store.GetSupportAdminByID(ctx, adminID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAdminNotFound
		}
		s.logger.Error(fmt.Sprintf("failed to get support admin: %v", err))
		return nil, err
	}
	return &admin, nil
}

// UpdateAdminStatus updates the status of a support admin
func (s *SupportAdminService) UpdateAdminStatus(ctx context.Context, adminID int64, status string) (*db.SupportAdmin, error) {
	// Validate status
	validStatuses := map[string]bool{
		"online":  true,
		"offline": true,
		"busy":    true,
	}

	if !validStatuses[status] {
		return nil, ErrInvalidStatus
	}

	admin, err := s.store.UpdateSupportAdminStatus(ctx, db.UpdateSupportAdminStatusParams{
		ID:     adminID,
		Status: status,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update admin status: %v", err))
		return nil, err
	}

	return &admin, nil
}

// SetAdminOnline sets an admin's status to online
func (s *SupportAdminService) SetAdminOnline(ctx context.Context, userID int64) error {
	admin, err := s.GetSupportAdminByUserID(ctx, userID)
	if err != nil {
		return err
	}

	_, err = s.UpdateAdminStatus(ctx, admin.ID, "online")
	return err
}

// SetAdminOffline sets an admin's status to offline
func (s *SupportAdminService) SetAdminOffline(ctx context.Context, userID int64) error {
	admin, err := s.GetSupportAdminByUserID(ctx, userID)
	if err != nil {
		return err
	}

	_, err = s.UpdateAdminStatus(ctx, admin.ID, "offline")
	return err
}

// ListAvailableAdmins returns admins available to take tickets
func (s *SupportAdminService) ListAvailableAdmins(ctx context.Context, limit int32) ([]db.SupportAdmin, error) {
	admins, err := s.store.ListAvailableSupportAdmins(ctx, limit)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to list available admins: %v", err))
		return nil, err
	}
	return admins, nil
}

// ListAllAdmins returns all support admins with user details
func (s *SupportAdminService) ListAllAdmins(ctx context.Context) ([]ListAllSupportAdminsRowResponse, error) {
	admins, err := s.store.ListAllSupportAdmins(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to list all admins: %v", err))
		return nil, err
	}
	var response []ListAllSupportAdminsRowResponse
	for _, admin := range admins {
		response = append(response, mapListAllAdminRowToResponse(admin))
	}
	return response, nil
}

// GetAdminWorkload retrieves workload information for all admins
func (s *SupportAdminService) GetAdminWorkload(ctx context.Context) ([]GetAdminWorkloadRowResponse, error) {
	workload, err := s.store.GetAdminWorkload(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get admin workload: %v", err))
		return nil, err
	}
	var response []GetAdminWorkloadRowResponse
	for _, admin := range workload {
		response = append(response, mapGetAdminWorkloadRowToResponse(admin))
	}
	return response, nil
}

// IncrementTicketCount increments an admin's active ticket count
func (s *SupportAdminService) IncrementTicketCount(ctx context.Context, adminID int64) error {
	admin, err := s.GetSupportAdminByID(ctx, adminID)
	if err != nil {
		return err
	}

	// Check if at capacity
	if admin.ActiveTicketCount >= admin.MaxConcurrentTickets {
		return ErrMaxConcurrentExceeded
	}

	err = s.store.IncrementActiveTicketCount(ctx, adminID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to increment ticket count: %v", err))
		return err
	}

	// Update status to busy if at capacity
	if admin.ActiveTicketCount+1 >= admin.MaxConcurrentTickets {
		_, err = s.UpdateAdminStatus(ctx, adminID, "busy")
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to update status to busy: %v", err))
		}
	}

	return nil
}

// DecrementTicketCount decrements an admin's active ticket count
func (s *SupportAdminService) DecrementTicketCount(ctx context.Context, adminID int64) error {
	admin, err := s.GetSupportAdminByID(ctx, adminID)
	if err != nil {
		return err
	}

	err = s.store.DecrementActiveTicketCount(ctx, adminID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to decrement ticket count: %v", err))
		return err
	}

	// Update status back to online if was at capacity
	if admin.ActiveTicketCount == admin.MaxConcurrentTickets && admin.Status == "busy" {
		_, err = s.UpdateAdminStatus(ctx, adminID, "online")
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to update status to online: %v", err))
		}
	}

	return nil
}

type ListAllSupportAdminsRowResponse struct {
	ID                   int64     `json:"id"`
	UserID               int64     `json:"user_id"`
	Status               string    `json:"status"`
	ActiveTicketCount    int32     `json:"active_ticket_count"`
	MaxConcurrentTickets int32     `json:"max_concurrent_tickets"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	FirstName            *string   `json:"first_name"`
	LastName             *string   `json:"last_name"`
	Email                string    `json:"email"`
}

func mapListAllAdminRowToResponse(row db.ListAllSupportAdminsRow) ListAllSupportAdminsRowResponse {
	return ListAllSupportAdminsRowResponse{
		ID:                   row.ID,
		UserID:               row.UserID,
		Status:               row.Status,
		ActiveTicketCount:    row.ActiveTicketCount,
		MaxConcurrentTickets: row.MaxConcurrentTickets,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		FirstName:            &row.FirstName.String,
		LastName:             &row.LastName.String,
		Email:                row.Email,
	}
}

type GetAdminWorkloadRowResponse struct {
	ID                int64   `json:"id"`
	UserID            int64   `json:"user_id"`
	Status            string  `json:"status"`
	ActiveTicketCount int32   `json:"active_ticket_count"`
	FirstName         *string `json:"first_name"`
	LastName          *string `json:"last_name"`
	Email             string  `json:"email"`
	TotalTickets      int64   `json:"total_tickets"`
}

func mapGetAdminWorkloadRowToResponse(row db.GetAdminWorkloadRow) GetAdminWorkloadRowResponse {
	return GetAdminWorkloadRowResponse{
		ID:                row.ID,
		UserID:            row.UserID,
		Status:            row.Status,
		ActiveTicketCount: row.ActiveTicketCount,
		FirstName:         &row.FirstName.String,
		LastName:          &row.LastName.String,
		Email:             row.Email,
		TotalTickets:      row.TotalTickets,
	}
}

type AgentMetricResponse struct {
	ID                        int64          `json:"id"`
	SupportAdminID            int64          `json:"support_admin_id"`
	TicketsHandled            int32          `json:"tickets_handled"`
	TicketsResolved           int32          `json:"tickets_resolved"`
	AverageResolutionTime     *int32  `json:"average_resolution_time"`
	AverageResponseTime       *int32 `json:"average_response_time"`
	CustomerSatisfactionScore *string`json:"customer_satisfaction_score"`
	Date                      time.Time      `json:"date"`
	CreatedAt                 time.Time      `json:"created_at"`
}


func MapAgentMetricToResponse(raw db.AgentMetric) AgentMetricResponse {
	return AgentMetricResponse{
		ID:                        raw.ID,
		SupportAdminID:            raw.SupportAdminID,
		TicketsHandled:            raw.TicketsHandled,
		TicketsResolved:           raw.TicketsResolved,
		AverageResolutionTime:     &raw.AverageResolutionTime.Int32,
		AverageResponseTime:       &raw.AverageResponseTime.Int32,
		CustomerSatisfactionScore: &raw.CustomerSatisfactionScore.String,
		Date:                      raw.Date,
		CreatedAt:                 raw.CreatedAt,
	}
}

