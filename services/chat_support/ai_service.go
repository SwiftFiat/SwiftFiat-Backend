package chatsupport

import (
	"bytes"
	"context"
	"database/sql"
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
	OpenAIModel         = "gpt-3.5-turbo"
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
	if err != nil || aiResponse == "" {
		s.logger.Error(fmt.Sprintf("OpenAI API error or empty response: %v", err))
		// Fallback to mock response
		aiResponse = "I'm sorry, but I'm having trouble connecting to my knowledge base right now. Please try rephrasing your question or contact support for immediate assistance."
		// If we have FAQ sources, use the FAQ content instead
		if len(faqSources) > 0 {
			aiResponse = fmt.Sprintf("Based on our FAQ '%s': %s", faqSources[0].Title, faqSources[0].Snippet)
		}
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
	// Keywords to match
	keywords := []string{
		"virtual", 
		"card", 
		"vault", 
		"savings", 
		"conversion", 
		"ramp", 
		"airtime", 
		"bill", 
		"payment", 
		"transfer", 
		"withdraw", 
		"deposit", 
		"account", 
		"verification", 
		"kyc", 
		"limits", 
		"fees", 
		"security", 
		"support", 
		"app", 
		"transaction", 
		"balance", 
		"gift", 
		"subscription", 
		"crypto", 
		"fiat", 
		"currency", 
		"exchange", 
		"top-up",
		"referrals",
		"crypto",
		"rewards",
		"qr",
		"totp",
		"streaks",
	}

	// Split query into words
	words := strings.Fields(strings.ToLower(query))

	// Find matching keywords in the query
	var matchingKeywords []string
	for _, word := range words {
		for _, keyword := range keywords {
			if strings.Contains(word, keyword) || strings.Contains(keyword, word) {
				matchingKeywords = append(matchingKeywords, keyword)
			}
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var uniqueKeywords []string
	for _, k := range matchingKeywords {
		if !seen[k] {
			seen[k] = true
			uniqueKeywords = append(uniqueKeywords, k)
		}
	}

	if len(uniqueKeywords) == 0 {
		return []FAQSource{}, nil
	}

	// Search for FAQs that contain any of the matching keywords in title
	var allFaqs []db.FaqDocument
	for _, keyword := range uniqueKeywords {
		faqs, err := s.store.SearchFAQDocuments(ctx, db.SearchFAQDocumentsParams{
			Column1: sql.NullString{String: "%" + keyword + "%", Valid: true},
			Limit:   10, // higher limit to get more
		})
		if err != nil {
			s.logger.Error(fmt.Sprintf("error searching for keyword %s: %v", keyword, err))
			continue
		}
		allFaqs = append(allFaqs, faqs...)
	}

	// Remove duplicates
	seenFaq := make(map[int64]bool)
	var uniqueFaqs []db.FaqDocument
	for _, faq := range allFaqs {
		if !seenFaq[faq.ID] {
			seenFaq[faq.ID] = true
			uniqueFaqs = append(uniqueFaqs, faq)
		}
	}

	// Limit to 5
	if len(uniqueFaqs) > 5 {
		uniqueFaqs = uniqueFaqs[:5]
	}

	s.logger.Info(fmt.Sprintf("Searching FAQs for query: %s, found %d FAQs", query, len(uniqueFaqs)))

	var sources []FAQSource
	for _, faq := range uniqueFaqs {
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
	// Check if API key is configured
	if s.config.OpenAIAPIKey == "" {
		s.logger.Warn("OpenAI API key not configured - using fallback response")
		return "", errors.New("api_key_not_configured")
	}

	// Log the API key length for debugging (don't log the actual key)
	s.logger.Info(fmt.Sprintf("Calling OpenAI with model: %s, API key length: %d, messages count: %d",
		OpenAIModel, len(s.config.OpenAIAPIKey), len(messages)))

	reqBody := OpenAIRequest{
		Model:               OpenAIModel,
		Messages:            messages,
		Temperature:         1,
		MaxCompletionTokens: 500,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to marshal request body: %v", err))
		return "", err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		OpenAIBaseURL+"/chat/completions",
		bytes.NewBuffer(jsonBody),
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create HTTP request: %v", err))
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.OpenAIAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error(fmt.Sprintf("HTTP request failed: %v", err))
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		s.logger.Error(fmt.Sprintf("OpenAI API error: status=%d, response=%s", resp.StatusCode, string(body)))
		return "", fmt.Errorf("openai_api_error: %d - %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to decode OpenAI response: %v, body: %s", err, string(body)))
		return "", err
	}

	if len(openAIResp.Choices) == 0 {
		s.logger.Error("No choices in OpenAI response")
		return "", errors.New("no response from OpenAI")
	}

	content := openAIResp.Choices[0].Message.Content
	preview := content
	if len(preview) > 50 {
		preview = preview[:50]
	}
	s.logger.Info(fmt.Sprintf("Successfully got OpenAI response: %s", preview))
	return content, nil
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
