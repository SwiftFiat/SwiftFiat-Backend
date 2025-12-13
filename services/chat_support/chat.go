package chatsupport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

const (
	MaxAttachmentSize = 500 * 1024 // 500 KB
	AllowedFileTypes  = "image/jpeg,image/png,image/webp"
)

var (
	ErrMessageNotFound     = errors.New("message not found")
	ErrInvalidAttachment   = errors.New("invalid attachment")
	ErrAttachmentTooLarge  = errors.New("attachment exceeds 500KB limit")
	ErrUnsupportedFileType = errors.New("unsupported file type")
)

type ChatService struct {
	store  *db.Store
	logger *logging.Logger
	aiSvc  *AIService
}

func NewChatService(
	store *db.Store,
	logger *logging.Logger,
	aiSvc *AIService,
) *ChatService {
	return &ChatService{
		store:  store,
		logger: logger,
		aiSvc:  aiSvc,
	}
}

// SendMessage sends a message in a ticket conversation
func (s *ChatService) SendMessage(ctx context.Context, params *SendMessageParams) (*ChatMessageResponse, error) {
	// Validate ticket exists
	ticket, err := s.store.GetTicketByID(ctx, params.TicketID)
	if err != nil {
		return nil, fmt.Errorf("ticket not found: %w", err)
	}

	// Create the message
	var metadata map[string]interface{}
	if params.SenderType == "ai" {
		// Store AI-specific metadata
		metadata = map[string]interface{}{
			"generated_at": time.Now(),
		}
	}

	metadataJSON, _ := json.Marshal(metadata)

	message, err := s.store.CreateChatMessage(ctx, db.CreateChatMessageParams{
		TicketID:    params.TicketID,
		SenderID:    params.SenderID,
		SenderType:  params.SenderType,
		MessageText: params.MessageText,
		Metadata:    pqtype.NullRawMessage{RawMessage: metadataJSON, Valid: true},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create message: %v", err))
		return nil, err
	}

	// Update first response time if this is the first agent response
	if params.SenderType == "admin" {
		err = s.store.UpdateTicketFirstResponse(ctx, params.TicketID)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to update first response time: %v", err))
		}

		// Update ticket status to in_progress if it's assigned
		if ticket.Status == "assigned" {
			_, err = s.store.UpdateTicketStatus(ctx, db.UpdateTicketStatusParams{
				ID:     params.TicketID,
				Status: "in_progress",
			})
			if err != nil {
				s.logger.Error(fmt.Sprintf("failed to update ticket status: %v", err))
			}
		}
	}

	// Process attachments
	var attachments []AttachmentResponse
	if len(params.Attachments) > 0 {
		for _, fileHeader := range params.Attachments {
			attachment, err := s.processAttachment(ctx, message.ID, fileHeader)
			if err != nil {
				s.logger.Error(fmt.Sprintf("failed to process attachment: %v", err))
				continue
			}
			attachments = append(attachments, *attachment)
		}
	}

	// Build response
	var aiConfidence *float64
	if message.AiConfidenceScore.Valid {
		score, err := strconv.ParseFloat(message.AiConfidenceScore.String, 64)
		if err == nil {
			aiConfidence = &score
		}
	}

	response := &ChatMessageResponse{
		ID:                message.ID,
		TicketID:          message.TicketID,
		SenderID:          message.SenderID,
		SenderType:        message.SenderType,
		MessageText:       message.MessageText,
		AIConfidenceScore: aiConfidence,
		Metadata:          metadata,
		Attachments:       attachments,
		IsEdited:          message.IsEdited,
		CreatedAt:         message.CreatedAt,
	}

	return response, nil
}

// GetConversationHistory retrieves all messages for a ticket
func (s *ChatService) GetConversationHistory(ctx context.Context, ticketID int64) ([]ChatMessageResponse, error) {
	messages, err := s.store.ListChatMessagesByTicket(ctx, ticketID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get messages: %v", err))
		return nil, err
	}

	var responses []ChatMessageResponse
	for _, msg := range messages {
		// Get attachments for this message
		attachments, err := s.store.ListAttachmentsByMessage(ctx, msg.ID)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to get attachments: %v", err))
			attachments = []db.Attachment{}
		}

		var attachmentResponses []AttachmentResponse
		for _, att := range attachments {
			attachmentResponses = append(attachmentResponses, AttachmentResponse{
				ID:        att.ID,
				FileURL:   att.FileUrl,
				FileName:  att.FileName,
				FileSize:  att.FileSize,
				MimeType:  att.MimeType,
				Type:      att.Type,
				CreatedAt: att.CreatedAt,
			})
		}

		var metadata map[string]interface{}
		if len(msg.Metadata.RawMessage) > 0 {
			json.Unmarshal(msg.Metadata.RawMessage, &metadata)
		}

		var aiConfidence *float64
		if msg.AiConfidenceScore.Valid {
			score, err := strconv.ParseFloat(msg.AiConfidenceScore.String, 64)
			if err == nil {
				aiConfidence = &score
			}
		}

		responses = append(responses, ChatMessageResponse{
			ID:                msg.ID,
			TicketID:          msg.TicketID,
			SenderID:          msg.SenderID,
			SenderType:        msg.SenderType,
			MessageText:       msg.MessageText,
			AIConfidenceScore: aiConfidence,
			Metadata:          metadata,
			Attachments:       attachmentResponses,
			IsEdited:          msg.IsEdited,
			CreatedAt:         msg.CreatedAt,
			SenderFirstName:   msg.FirstName.String,
			SenderLastName:    msg.LastName.String,
		})
	}

	return responses, nil
}

// UpdateMessage allows editing a message
func (s *ChatService) UpdateMessage(ctx context.Context, messageID int64, newText string) (*db.ChatMessage, error) {
	message, err := s.store.UpdateChatMessage(ctx, db.UpdateChatMessageParams{
		ID:          messageID,
		MessageText: newText,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update message: %v", err))
		return nil, err
	}
	return &message, nil
}

// processAttachment validates and stores an attachment
func (s *ChatService) processAttachment(ctx context.Context, messageID int64, fileHeader *multipart.FileHeader) (*AttachmentResponse, error) {
	// Validate file size
	if fileHeader.Size > MaxAttachmentSize {
		return nil, ErrAttachmentTooLarge
	}

	// Validate file type
	contentType := fileHeader.Header.Get("Content-Type")
	if !strings.Contains(AllowedFileTypes, contentType) {
		return nil, ErrUnsupportedFileType
	}

	// Open the file
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Read file content
	// _fileBytes, err := io.ReadAll(file)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to read file: %w", err)
	// }

	// Generate unique filename
	ext := filepath.Ext(fileHeader.Filename)
	uniqueFilename := fmt.Sprintf("%s%s", uuid.New().String(), ext)

	// In production, upload to S3 or similar storage
	// For now, we'll store the path
	fileURL := fmt.Sprintf("/api/v1/chat/attachments/%s", uniqueFilename)

	// TODO: Implement actual file upload to S3/cloud storage
	// For now, we're just storing metadata

	// Create attachment record
	attachment, err := s.store.CreateAttachment(ctx, db.CreateAttachmentParams{
		MessageID: messageID,
		FileUrl:   fileURL,
		FileName:  fileHeader.Filename,
		FileSize:  int32(fileHeader.Size),
		MimeType:  contentType,
		Type:      "image",
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to create attachment: %v", err))
		return nil, err
	}

	return &AttachmentResponse{
		ID:        attachment.ID,
		FileURL:   attachment.FileUrl,
		FileName:  attachment.FileName,
		FileSize:  attachment.FileSize,
		MimeType:  attachment.MimeType,
		Type:      attachment.Type,
		CreatedAt: attachment.CreatedAt,
	}, nil
}

// GetLastMessage retrieves the last message in a ticket
func (s *ChatService) GetLastMessage(ctx context.Context, ticketID int64) (*db.ChatMessage, error) {
	message, err := s.store.GetLastMessageByTicket(ctx, ticketID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrMessageNotFound
		}
		s.logger.Error(fmt.Sprintf("failed to get last message: %v", err))
		return nil, err
	}
	return &message, nil
}

// GetMessageCount returns the number of messages in a ticket
func (s *ChatService) GetMessageCount(ctx context.Context, ticketID int64) (int64, error) {
	count, err := s.store.CountMessagesByTicket(ctx, ticketID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to count messages: %v", err))
		return 0, err
	}
	return count, nil
}