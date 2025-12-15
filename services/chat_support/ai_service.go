package chatsupport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

const (
	ConfidenceThreshold = 0.55
	OpenAIBaseURL       = "https://api.openai.com/v1"
	OpenAIModel         = "gpt-4o-mini"
	MaxContextMessages  = 10
)

// Lower threshold = more AI responses, less escalation
// Higher threshold = more escalation, better quality

// Recommended values:
// 0.50 - Aggressive AI (more automation)
// 0.55 - Balanced (recommended)
// 0.70 - Conservative (more human touch)

type AIService struct {
	store      *db.Store
	logger     *logging.Logger
	config     *utils.Config
	httpClient *http.Client
}

func NewAIService(
	store *db.Store,
	logger *logging.Logger,
	config *utils.Config,
) *AIService {
	return &AIService{
		store:      store,
		logger:     logger,
		config:     config,
		httpClient: &http.Client{},
	}
}

// QueryAI processes a user query with AI and RAG
func (s *AIService) QueryAI(ctx context.Context, req *AIQueryRequest) (*AIQueryResponse, error) {
	// Step 1: Retrieve relevant FAQ documents using RAG
	faqSources, err := s.retrieveRelevantFAQs(ctx, req.Message)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to retrieve FAQs: %v", err))
		// Continue without FAQs rather than failing completely
		faqSources = []FAQSource{}
	}

	// Step 2: Build context for AI
	systemPrompt := s.buildSystemPrompt(faqSources)
	messages := s.buildMessages(systemPrompt, req.Message, req.ConversationContext)

	// Step 3: Call OpenAI API
	aiResponse, err := s.callOpenAI(ctx, messages)
	if err != nil {
		s.logger.Error(fmt.Sprintf("OpenAI API error: %v", err))
		return nil, ErrAIServiceUnavailable
	}

	// Step 4: Calculate confidence score
	confidenceScore := s.calculateConfidence(aiResponse, faqSources, req.Message)

	// Step 5: Determine if human escalation is needed
	humanRequired, escalationReason := s.shouldEscalate(
		confidenceScore,
		req.Message,
		faqSources,
	)

	return &AIQueryResponse{
		Answer:           aiResponse,
		ConfidenceScore:  confidenceScore,
		HumanRequired:    humanRequired,
		EscalationReason: escalationReason,
		FAQSources:       faqSources,
		Metadata: map[string]interface{}{
			"model":     OpenAIModel,
			"faq_count": len(faqSources),
		},
	}, nil
}

// retrieveRelevantFAQs uses vector similarity search to find relevant FAQ documents
func (s *AIService) retrieveRelevantFAQs(ctx context.Context, query string) ([]FAQSource, error) {
	// In production, this would use a vector database (Pinecone, Weaviate, etc.)
	// For now, we'll do a simple text search
	searchPattern := fmt.Sprintf("%%%s%%", query)

	faqs, err := s.store.SearchFAQDocuments(ctx, db.SearchFAQDocumentsParams{
		Title: searchPattern,
		Limit: 5,
	})
	if err != nil {
		return nil, err
	}

	var sources []FAQSource
	for _, faq := range faqs {
		// Create snippet (first 200 characters)
		snippet := faq.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}

		sources = append(sources, FAQSource{
			ID:       faq.ID,
			Title:    faq.Title,
			Snippet:  snippet,
			Category: faq.Category.String,
		})

		// Increment view count
		_ = s.store.IncrementFAQViewCount(ctx, faq.ID)
	}

	return sources, nil
}

// buildSystemPrompt creates the system prompt with FAQ context
func (s *AIService) buildSystemPrompt(faqSources []FAQSource) string {
	var prompt strings.Builder
	prompt.WriteString(`You are a helpful customer support assistant for SwiftFiat, a financial services platform. 
Your goal is to provide accurate, helpful, and friendly responses to customer inquiries.

Guidelines:
1. Be professional, friendly, and empathetic
2. Provide clear and concise answers
3. Use the FAQ knowledge base when available
4. If you're unsure or the question is complex, acknowledge it
5. For account-specific issues, payments, or verification, recommend contacting human support
6. Never make up information - only use provided context

`)

	if len(faqSources) > 0 {
		prompt.WriteString("\n\nRelevant FAQ Articles:\n")
		for i, source := range faqSources {
			fmt.Fprintf(&prompt, "\n%d. %s\n%s\n", i+1, source.Title, source.Snippet)
		}
	}

	return prompt.String()
}

// buildMessages constructs the message array for OpenAI
func (s *AIService) buildMessages(systemPrompt, userMessage string, context []ConversationMessage) []ConversationMessage {
	messages := []ConversationMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
	}

	// Add conversation context (limit to last N messages)
	contextLimit := MaxContextMessages
	if len(context) > contextLimit {
		context = context[len(context)-contextLimit:]
	}
	messages = append(messages, context...)

	// Add current user message
	messages = append(messages, ConversationMessage{
		Role:    "user",
		Content: userMessage,
	})

	return messages
}

// callOpenAI makes the API call to OpenAI
func (s *AIService) callOpenAI(ctx context.Context, messages []ConversationMessage) (string, error) {
	reqBody := OpenAIRequest{
		Model:       OpenAIModel,
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   500,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		OpenAIBaseURL+"/chat/completions",
		bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.OpenAIAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return "", err
	}

	if len(openAIResp.Choices) == 0 {
		return "", errors.New("no response from OpenAI")
	}

	return openAIResp.Choices[0].Message.Content, nil
}

// calculateConfidence determines confidence score based on various factors
func (s *AIService) calculateConfidence(response string, faqSources []FAQSource, query string) float64 {
	confidence := 0.5 // Base confidence

	// Increase confidence if FAQ sources were found
	if len(faqSources) > 0 {
		confidence += 0.2
	}

	// Increase confidence if response is substantial
	if len(response) > 100 {
		confidence += 0.1
	}

	// Decrease confidence if response contains uncertainty phrases
	uncertaintyPhrases := []string{
		"i'm not sure",
		"i don't know",
		"unclear",
		"might be",
		"possibly",
		"perhaps",
	}

	responseLower := strings.ToLower(response)
	for _, phrase := range uncertaintyPhrases {
		if strings.Contains(responseLower, phrase) {
			confidence -= 0.2
			break
		}
	}

	// Clamp between 0 and 1
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	return confidence

}

// shouldEscalate determines if the query should be escalated to human support
func (s *AIService) shouldEscalate(confidence float64, query string, faqSources []FAQSource) (bool, string) {
	// Check confidence threshold
	if confidence < ConfidenceThreshold {
		return true, "ai_low_confidence"
	}

	// Check for explicit human request
	humanRequestPhrases := []string{
		"speak to human",
		"talk to human",
		"human agent",
		"real person",
		"customer service",
		"representative",
	}
	queryLower := strings.ToLower(query)
	for _, phrase := range humanRequestPhrases {
		if strings.Contains(queryLower, phrase) {
			return true, "user_request"
		}
	}

	// Check for out-of-scope topics
	outOfScopeKeywords := []string{
		"payment issue",
		"transaction failed",
		"account locked",
		"verification problem",
		"kyc issue",
		"withdrawal problem",
		"deposit problem",
		"can't access",
		"suspended",
		"complaint",
		// Todo: add others
	}

	for _, keyword := range outOfScopeKeywords {
		if strings.Contains(queryLower, keyword) {
			return true, "out_of_scope"
		}
	}

	// Check for frustration/urgency
	frustrationKeywords := []string{
		"urgent",
		"emergency",
		"immediately",
		"frustrated",
		"angry",
		"unacceptable",
	}

	for _, keyword := range frustrationKeywords {
		if strings.Contains(queryLower, keyword) {
			return true, "complex_query"
		}
	}

	return false, ""
}

// MarkFAQHelpful increments the helpful count for an FAQ
func (s *AIService) MarkFAQHelpful(ctx context.Context, faqID int64) error {
	return s.store.IncrementFAQHelpfulCount(ctx, faqID)
}
