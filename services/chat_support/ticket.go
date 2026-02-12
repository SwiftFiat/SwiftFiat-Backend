package chatsupport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
)

type TicketService struct {
	store           *db.Store
	logger          *logging.Logger
	notificationSvc *service.Notification
	plunkSvc        *service.Plunk
}

func NewTicketService(
	store *db.Store,
	logger *logging.Logger,
	notificationSvc *service.Notification,
	plunkSvc *service.Plunk,
) *TicketService {
	return &TicketService{
		store:           store,
		logger:          logger,
		notificationSvc: notificationSvc,
		plunkSvc:        plunkSvc,
	}
}

// CreateTicket creates a new support ticket
func (s *TicketService) CreateTicket(ctx context.Context, params *CreateTicketParams) (*db.Ticket, error) {
	ticket, err := s.store.CreateTicket(ctx, db.CreateTicketParams{
		UserID: params.UserID,
		Status: "open",
		EscalationReason: sql.NullString{
			String: params.EscalationReason,
			Valid:  params.EscalationReason != "",
		},
		Priority: params.Priority,
		Category: sql.NullString{
			String: params.Category,
			Valid:  params.Category != "",
		},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create ticket: %v", err))
		return nil, err
	}

	// Send notifications to available admins
	go s.notifyAdminsNewTicket(ctx, &ticket)

	return &ticket, nil
}

// AssignTicket assigns a ticket to a support admin
func (s *TicketService) AssignTicket(ctx context.Context, ticketID, adminID, assignedBy int64) (*db.Ticket, error) {
	// Get ticket to check current state
	ticket, err := s.store.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, ErrTicketNotFound
	}

	if ticket.AssignedTo.Valid {
		return nil, ErrTicketAlreadyAssigned
	}

	// Assign ticket
	updatedTicket, err := s.store.AssignTicket(ctx, db.AssignTicketParams{
		ID:         ticketID,
		AssignedTo: sql.NullInt64{Int64: adminID, Valid: true},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to assign ticket: %v", err))
		return nil, err
	}

	// Create assignment history
	_, err = s.store.CreateTicketAssignmentHistory(ctx, db.CreateTicketAssignmentHistoryParams{
		TicketID:   ticketID,
		AssignedTo: sql.NullInt64{Int64: adminID, Valid: true},
		AssignedBy: assignedBy,
		Reason: sql.NullString{
			String: "Ticket assigned by admin",
			Valid:  true,
		},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create assignment history: %v", err))
	}

	// Increment admin's active ticket count
	err = s.store.IncrementActiveTicketCount(ctx, adminID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to increment active ticket count: %v", err))
	}

	// Notify the assigned admin
	go s.notifyAgentAssignment(ctx, &updatedTicket, adminID)

	// Notify the user
	go s.notifyUserAssignment(ctx, &updatedTicket)

	return &updatedTicket, nil
}

// ClaimTicket allows an agent to claim an unassigned ticket
func (s *TicketService) ClaimTicket(ctx context.Context, ticketID, adminID int64) (*db.Ticket, error) {
	ticket, err := s.store.ClaimTicket(ctx, db.ClaimTicketParams{
		ID:         ticketID,
		AssignedTo: sql.NullInt64{Int64: adminID, Valid: true},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to claim ticket: %v", err))
		return nil, err
	}

	// Create assignment history
	_, err = s.store.CreateTicketAssignmentHistory(ctx, db.CreateTicketAssignmentHistoryParams{
		TicketID:   ticketID,
		AssignedTo: sql.NullInt64{Int64: adminID, Valid: true},
		AssignedBy: adminID,
		Reason: sql.NullString{
			String: "Ticket claimed by agent",
			Valid:  true,
		},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create assignment history: %v", err))
	}

	// Increment admin's active ticket count
	err = s.store.IncrementActiveTicketCount(ctx, adminID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to increment active ticket count: %v", err))
	}

	// Notify the user
	go s.notifyUserAssignment(ctx, &ticket)

	return &ticket, nil
}

// ReassignTicket reassigns a ticket from one agent to another
func (s *TicketService) ReassignTicket(ctx context.Context, ticketID, newAdminID, reassignedBy int64) (*db.Ticket, error) {
	ticket, err := s.store.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, ErrTicketNotFound
	}

	oldAdminID := ticket.AssignedTo

	// Update assignment
	updatedTicket, err := s.store.AssignTicket(ctx, db.AssignTicketParams{
		ID:         ticketID,
		AssignedTo: sql.NullInt64{Int64: newAdminID, Valid: true},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to reassign ticket: %v", err))
		return nil, err
	}

	_, err = s.store.CreateTicketAssignmentHistory(ctx, db.CreateTicketAssignmentHistoryParams{
		TicketID:     ticketID,
		AssignedFrom: oldAdminID,
		AssignedTo:   sql.NullInt64{Int64: newAdminID, Valid: true},
		AssignedBy:   reassignedBy,
		Reason: sql.NullString{
			String: "Ticket reassigned",
			Valid:  true,
		},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create assignment history: %v", err))
	}

	// Update ticket counts
	if oldAdminID.Valid {
		err = s.store.DecrementActiveTicketCount(ctx, oldAdminID.Int64)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to decrement old admin ticket count: %v", err))
		}
	}

	err = s.store.IncrementActiveTicketCount(ctx, newAdminID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to increment new admin ticket count: %v", err))
	}

	return &updatedTicket, nil
}

// UpdateTicketStatus updates the status of a ticket
func (s *TicketService) UpdateTicketStatus(ctx context.Context, ticketID int64, status string) (*db.Ticket, error) {
	validStatuses := map[string]bool{
		"open":        true,
		"assigned":    true,
		"in_progress": true,
		"resolved":    true,
		"closed":      true,
	}

	if !validStatuses[status] {
		return nil, ErrInvalidTicketStatus
	}

	ticket, err := s.store.UpdateTicketStatus(ctx, db.UpdateTicketStatusParams{
		ID:     ticketID,
		Status: status,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update ticket status: %v", err))
		return nil, err
	}

	// If ticket is resolved or closed, decrement active ticket count
	if status == "resolved" || status == "closed" {
		if ticket.AssignedTo.Valid {
			err = s.store.DecrementActiveTicketCount(ctx, ticket.AssignedTo.Int64)
			if err != nil {
				s.logger.Error(fmt.Sprintf("failed to decrement active ticket count: %v", err))
			}
		}

		// Notify user of resolution
		go s.notifyUserResolution(ctx, &ticket)
	}

	return &ticket, nil
}

// ResolveTicket marks a ticket as resolved
func (s *TicketService) ResolveTicket(ctx context.Context, ticketID int64) (*db.Ticket, error) {
	ticket, err := s.store.ResolveTicket(ctx, ticketID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to resolve ticket: %v", err))
		return nil, err
	}

	// Decrement active ticket count
	if ticket.AssignedTo.Valid {
		err = s.store.DecrementActiveTicketCount(ctx, ticket.AssignedTo.Int64)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to decrement active ticket count: %v", err))
		}
	}

	// Update agent metrics
	go s.updateAgentMetrics(ctx, ticket.AssignedTo.Int64, ticketID)

	// Notify user
	go s.notifyUserResolution(ctx, &ticket)

	return &ticket, nil
}

// AutoAssignTicket automatically assigns a ticket to an available agent
func (s *TicketService) AutoAssignTicket(ctx context.Context, ticketID int64) (*db.Ticket, error) {
	// Get available agents
	agents, err := s.store.ListAvailableSupportAdmins(ctx, 1)
	if err != nil || len(agents) == 0 {
		s.logger.Warn("no available agents for auto-assignment")
		return nil, ErrNoAvailableAgents
	}

	// Assign to the agent with lowest workload
	return s.AssignTicket(ctx, ticketID, agents[0].ID, 0) // 0 indicates auto-assignment
}

// GetTicketsByUser retrieves all tickets for a user
func (s *TicketService) GetTicketsByUser(ctx context.Context, userID int64, limit, offset int32) ([]db.Ticket, error) {
	tickets, err := s.store.ListTicketsByUser(ctx, db.ListTicketsByUserParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get tickets by user: %v", err))
		return nil, err
	}
	return tickets, nil
}

// GetTicketsByStatus retrieves tickets by status
func (s *TicketService) GetTicketsByStatus(ctx context.Context, status string, limit, offset int32) ([]db.ListTicketsByStatusRow, error) {
	tickets, err := s.store.ListTicketsByStatus(ctx, db.ListTicketsByStatusParams{
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get tickets by status: %v", err))
		return nil, err
	}
	return tickets, nil
}

// GetAllTickets retrieves all tickets with pagination
func (s *TicketService) GetAllTickets(ctx context.Context, limit, offset int32) ([]db.ListAllTicketsRow, error) {
	tickets, err := s.store.ListAllTickets(ctx, db.ListAllTicketsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get all tickets: %v", err))
		return nil, err
	}
	return tickets, nil
}

// GetTicketsByAssignedAdmin retrieves tickets assigned to a specific admin
func (s *TicketService) GetTicketsByAssignedAdmin(ctx context.Context, adminID int64, limit, offset int32) ([]db.ListTicketsByAssignedAdminRow, error) {
	tickets, err := s.store.ListTicketsByAssignedAdmin(ctx, db.ListTicketsByAssignedAdminParams{
		AssignedTo: sql.NullInt64{Int64: adminID, Valid: true},
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get tickets by admin: %v", err))
		return nil, err
	}
	return tickets, nil
}

// GetUnassignedTickets retrieves all unassigned tickets
func (s *TicketService) GetUnassignedTickets(ctx context.Context, limit, offset int32) ([]db.ListUnassignedTicketsRow, error) {
	tickets, err := s.store.ListUnassignedTickets(ctx, db.ListUnassignedTicketsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get unassigned tickets: %v", err))
		return nil, err
	}
	return tickets, nil
}

// GetTicketStatistics retrieves ticket statistics for a given time period
func (s *TicketService) GetTicketStatistics(ctx context.Context, since time.Time) (*db.GetTicketStatisticsRow, error) {
	stats, err := s.store.GetTicketStatistics(ctx, since)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get ticket statistics: %v", err))
		return nil, err
	}
	return &stats, nil
}

func (s *TicketService) notifyAdminsNewTicket(ctx context.Context, ticket *db.Ticket) {
	// Get all online admins
	admins, err := s.store.ListAvailableSupportAdmins(ctx, 10)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get available admins: %v", err))
		return
	}

	user, err := s.store.GetUserByID(ctx, ticket.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get user: %v", err))
		return
	}

	// Send in-app notifications to all available admins
	for _, admin := range admins {
		adminUser, err := s.store.GetUserByID(ctx, admin.UserID)
		if err != nil {
			continue
		}

		_, err = s.notificationSvc.CreateWithRecipients(
			ctx,
			nil,
			"New Support Ticket",
			fmt.Sprintf("New ticket #%d from %s %s", ticket.ID, user.FirstName.String, user.LastName.String),
			"system",
			[]int64{adminUser.ID},
		)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to create notification: %v", err))
		}

		// Send email notification
		err = s.plunkSvc.SendEmail(
			adminUser.Email,
			"New Support Ticket Assigned",
			fmt.Sprintf("A new support ticket #%d has been created and requires assignment.", ticket.ID),
		)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to send email: %v", err))
		}
	}
}

func (s *TicketService) notifyAgentAssignment(ctx context.Context, ticket *db.Ticket, adminID int64) {
	admin, err := s.store.GetSupportAdminByID(ctx, adminID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get admin: %v", err))
		return
	}

	adminUser, err := s.store.GetUserByID(ctx, admin.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get admin user: %v", err))
		return
	}

	_, err = s.notificationSvc.CreateWithRecipients(
		ctx,
		nil,
		"Ticket Assigned",
		fmt.Sprintf("Ticket #%d has been assigned to you", ticket.ID),
		"system",
		[]int64{adminUser.ID},
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create notification: %v", err))
	}
}

func (s *TicketService) notifyUserAssignment(ctx context.Context, ticket *db.Ticket) {
	_, err := s.notificationSvc.CreateWithRecipients(
		ctx,
		nil,
		"Support Agent Assigned",
		"A support agent has joined your conversation and will assist you shortly.",
		"system",
		[]int64{ticket.UserID},
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create notification: %v", err))
	}
}

func (s *TicketService) notifyUserResolution(ctx context.Context, ticket *db.Ticket) {
	_, err := s.notificationSvc.CreateWithRecipients(
		ctx,
		nil,
		"Ticket Resolved",
		fmt.Sprintf("Your support ticket #%d has been resolved.", ticket.ID),
		"system",
		[]int64{ticket.UserID},
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create notification: %v", err))
	}
}

func (s *TicketService) updateAgentMetrics(ctx context.Context, adminID, ticketID int64) {
	// Calculate resolution time and other metrics
	ticket, err := s.store.GetTicketByID(ctx, ticketID)
	if err != nil {
		return
	}

	if !ticket.ResolvedAt.Valid {
		return
	}

	resolutionTime := int32(ticket.ResolvedAt.Time.Sub(ticket.CreatedAt).Seconds())

	_, err = s.store.UpsertAgentMetrics(ctx, db.UpsertAgentMetricsParams{
		SupportAdminID:        adminID,
		TicketsHandled:        1,
		TicketsResolved:       1,
		AverageResolutionTime: sql.NullInt32{Int32: resolutionTime, Valid: true},
		Date:                  time.Now().Truncate(24 * time.Hour),
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update agent metrics: %v", err))
	}
}
