package chatsupport

import (
	"errors"
	"mime/multipart"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTicketNotFound        = errors.New("ticket not found")
	ErrTicketAlreadyAssigned = errors.New("ticket already assigned")
	ErrNoAvailableAgents     = errors.New("no available agents")
	ErrInvalidTicketStatus   = errors.New("invalid ticket status")

	ErrAIServiceUnavailable = errors.New("AI service unavailable")
	ErrInsufficientContext  = errors.New("insufficient context for AI response")

	ErrAdminNotFound         = errors.New("support admin not found")
	ErrAdminAlreadyExists    = errors.New("support admin profile already exists")
	ErrInvalidStatus         = errors.New("invalid status")
	ErrMaxConcurrentExceeded = errors.New("maximum concurrent tickets exceeded")
)

type CreateTicketParams struct {
	UserID           uuid.UUID
	EscalationReason string
	Priority         string
	Category         string
}

type SendMessageParams struct {
	TicketID    int64
	SenderID    uuid.UUID
	SenderType  string // 'user', 'admin', 'ai', 'system'
	MessageText string
	Attachments []*multipart.FileHeader
}

type ChatMessageResponse struct {
	ID                int64                  `json:"id"`
	TicketID          int64                  `json:"ticket_id"`
	SenderID          uuid.UUID                  `json:"sender_id"`
	SenderType        string                 `json:"sender_type"`
	MessageText       string                 `json:"message_text"`
	AIConfidenceScore *float64               `json:"ai_confidence_score,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	Attachments       []AttachmentResponse   `json:"attachments"`
	IsEdited          bool                   `json:"is_edited"`
	CreatedAt         time.Time              `json:"created_at"`
	SenderFirstName   string                 `json:"sender_first_name,omitempty"`
	SenderLastName    string                 `json:"sender_last_name,omitempty"`
}

type AttachmentResponse struct {
	ID        int64     `json:"id"`
	FileURL   string    `json:"file_url"`
	FileName  string    `json:"file_name"`
	FileSize  int32     `json:"file_size"`
	MimeType  string    `json:"mime_type"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type AIQueryRequest struct {
	Message             string                `json:"message"`
	ConversationContext []ConversationMessage `json:"conversation_context"`
	UserID              uuid.UUID                 `json:"user_id"`
}

type ConversationMessage struct {
	Role    string `json:"role"` // 'user', 'assistant'
	Content string `json:"content"`
}

type AIQueryResponse struct {
	Answer           string                 `json:"answer"`
	ConfidenceScore  float64                `json:"confidence_score"`
	HumanRequired    bool                   `json:"human_required"`
	EscalationReason string                 `json:"escalation_reason,omitempty"`
	FAQSources       []FAQSource            `json:"faq_sources,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

type FAQSource struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Snippet  string `json:"snippet"`
	Category string `json:"category"`
}

type OpenAIRequest struct {
	Model               string                `json:"model"`
	Messages            []ConversationMessage `json:"messages"`
	Temperature         float64               `json:"temperature"`
	MaxCompletionTokens int                   `json:"max_completion_tokens,omitempty"`
}

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type CreateSupportAdminParams struct {
	UserID               uuid.UUID
	MaxConcurrentTickets int32
}
