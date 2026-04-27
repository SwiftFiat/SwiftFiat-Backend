package coindesk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/google/uuid"
)

const (
	CoinDeskNewsAPI = "https://data-api.coindesk.com/news/v1/article/list"
	PollInterval    = 3 * time.Hour // Check for new articles every 3 hours
	CacheTTL        = 10 * time.Minute
)

// NewsArticle represents a crypto news article
type NewsArticle struct {
	ID          int64     `json:"id"`
	GUID        string    `json:"guid"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle"`
	Body        string    `json:"body"`
	Authors     string    `json:"authors"`
	URL         string    `json:"url"`
	ImageURL    string    `json:"image_url"`
	PublishedOn int64     `json:"published_on"`
	Sentiment   string    `json:"sentiment"`
	Keywords    string    `json:"keywords"`
	SourceName  string    `json:"source_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// CoinDeskNewsResponse represents the API response
type CoinDeskNewsResponse struct {
	Data []struct {
		ID          int64  `json:"ID"`
		GUID        string `json:"GUID"`
		Title       string `json:"TITLE"`
		Subtitle    string `json:"SUBTITLE"`
		Body        string `json:"BODY"`
		Authors     string `json:"AUTHORS"`
		URL         string `json:"URL"`
		ImageURL    string `json:"IMAGE_URL"`
		PublishedOn int64  `json:"PUBLISHED_ON"`
		Sentiment   string `json:"SENTIMENT"`
		Keywords    string `json:"KEYWORDS"`
		SourceData  struct {
			Name string `json:"NAME"`
		} `json:"SOURCE_DATA"`
	} `json:"Data"`
	Err struct {
		Message string `json:"message"`
	} `json:"Err"`
}

// MarketInsightsService handles crypto news and notifications
type MarketInsightsService struct {
	logger              *logging.Logger
	pushNotification    *service.PushNotificationService
	userService         *user_service.UserService
	httpClient          *http.Client
	cache               []*NewsArticle
	cacheMux            sync.RWMutex
	lastCacheUpdate     time.Time
	notifiedArticles    map[int64]bool // Track which articles we've sent notifications for
	notifiedMux         sync.RWMutex
	backgroundCtx       context.Context
	backgroundCancel    context.CancelFunc
	notificationEnabled bool
}

// NewMarketInsightsService creates a new market insights service
func NewMarketInsightsService(
	logger *logging.Logger,
	pushNotification *service.PushNotificationService,
	userService *user_service.UserService,
) *MarketInsightsService {
	ctx, cancel := context.WithCancel(context.Background())

	s := &MarketInsightsService{
		logger:           logger,
		pushNotification: pushNotification,
		userService:      userService,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		cache:               make([]*NewsArticle, 0),
		notifiedArticles:    make(map[int64]bool),
		backgroundCtx:       ctx,
		backgroundCancel:    cancel,
		notificationEnabled: true,
	}

	// Start background monitoring
	go s.startBackgroundMonitoring()

	return s
}

// GetNews fetches latest crypto news
func (s *MarketInsightsService) GetNews(ctx context.Context, limit int) ([]*NewsArticle, error) {
	// Check cache first
	s.cacheMux.RLock()
	cacheValid := time.Since(s.lastCacheUpdate) < CacheTTL
	if cacheValid && len(s.cache) > 0 {
		articles := s.cache
		s.cacheMux.RUnlock()

		// Return up to limit
		if limit > 0 && limit < len(articles) {
			return articles[:limit], nil
		}
		return articles, nil
	}
	s.cacheMux.RUnlock()

	// Fetch fresh data
	return s.fetchAndCacheNews(ctx, limit)
}

// fetchAndCacheNews fetches news from CoinDesk API
func (s *MarketInsightsService) fetchAndCacheNews(ctx context.Context, limit int) ([]*NewsArticle, error) {
	if limit == 0 {
		limit = 10
	}

	url := fmt.Sprintf("%s?lang=EN&limit=%d", CoinDeskNewsAPI, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch news: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp CoinDeskNewsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if apiResp.Err.Message != "" {
		return nil, fmt.Errorf("API error: %s", apiResp.Err.Message)
	}

	// Parse articles
	articles := make([]*NewsArticle, 0, len(apiResp.Data))
	for _, data := range apiResp.Data {
		article := &NewsArticle{
			ID:          data.ID,
			GUID:        data.GUID,
			Title:       data.Title,
			Subtitle:    data.Subtitle,
			Body:        data.Body,
			Authors:     data.Authors,
			URL:         data.URL,
			ImageURL:    data.ImageURL,
			PublishedOn: data.PublishedOn,
			Sentiment:   data.Sentiment,
			Keywords:    data.Keywords,
			SourceName:  data.SourceData.Name,
			CreatedAt:   time.Unix(data.PublishedOn, 0),
		}
		articles = append(articles, article)
	}

	// Update cache
	s.cacheMux.Lock()
	s.cache = articles
	s.lastCacheUpdate = time.Now()
	s.cacheMux.Unlock()

	s.logger.Info(fmt.Sprintf("Fetched %d news articles from CoinDesk", len(articles)))

	return articles, nil
}

// startBackgroundMonitoring checks for new articles and sends notifications
func (s *MarketInsightsService) startBackgroundMonitoring() {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	s.logger.Info("Market insights background monitoring started")

	// Do an initial fetch
	ctx := context.Background()
	s.checkForNewArticles(ctx)

	for {
		select {
		case <-s.backgroundCtx.Done():
			s.logger.Info("Market insights background monitoring stopped")
			return
		case <-ticker.C:
			s.checkForNewArticles(ctx)
		}
	}
}

// checkForNewArticles fetches latest articles and notifies users of new ones
func (s *MarketInsightsService) checkForNewArticles(ctx context.Context) {
	articles, err := s.fetchAndCacheNews(ctx, 10)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Error fetching news: %v", err))
		return
	}

	if !s.notificationEnabled {
		return
	}

	// Find new articles we haven't notified about
	newArticles := make([]*NewsArticle, 0)

	s.notifiedMux.RLock()
	for _, article := range articles {
		if !s.notifiedArticles[article.ID] {
			newArticles = append(newArticles, article)
		}
	}
	s.notifiedMux.RUnlock()

	if len(newArticles) == 0 {
		return
	}

	s.logger.Info(fmt.Sprintf("Found %d new articles to notify users about", len(newArticles)))

	// Only send notification for the most recent article to avoid spam
	if len(newArticles) > 0 {
		latestArticle := newArticles[0]
		go s.notifyUsersOfArticle(ctx, latestArticle)

		// Mark this article as notified
		s.notifiedMux.Lock()
		s.notifiedArticles[latestArticle.ID] = true

		// Clean up old entries to prevent memory leak
		if len(s.notifiedArticles) > 1000 {
			// Keep only the most recent 500
			newMap := make(map[int64]bool)
			count := 0
			for _, article := range articles {
				if count >= 500 {
					break
				}
				newMap[article.ID] = true
				count++
			}
			s.notifiedArticles = newMap
		}
		s.notifiedMux.Unlock()
	}
}

// notifyUsersOfArticle sends push notifications to all active users
func (s *MarketInsightsService) notifyUsersOfArticle(ctx context.Context, article *NewsArticle) {
	// Get all tokens for active users in one go
	tokens, err := s.userService.ListActiveUserTokens(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get active user tokens for notifications: %v", err))
		return
	}

	if len(tokens) == 0 {
		return
	}

	// Group tokens by UserID
	userTokensMap := make(map[uuid.UUID][]db.UserToken)
	for _, t := range tokens {
		userTokensMap[t.UserID] = append(userTokensMap[t.UserID], t)
	}

	title := "📰 Crypto News"
	message := article.Title
	if len(message) > 150 {
		message = message[:147] + "..."
	}

	// Use a worker pool to send notifications
	const numWorkers = 20
	taskChan := make(chan []db.UserToken, len(userTokensMap))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for userTokens := range taskChan {
				err := s.sendNewsNotification(ctx, userTokens, title, message)
				if err != nil {
					// Error is already logged inside sendNewsNotification
				}
			}
		}()
	}

	// Feed tasks to workers
	for _, userTokens := range userTokensMap {
		taskChan <- userTokens
	}
	close(taskChan)
	wg.Wait()

	s.logger.Info(fmt.Sprintf("Sent news notification to %d users: %s", len(userTokensMap), article.Title))
}

// sendNewsNotification sends a push notification to a user using provided tokens
func (s *MarketInsightsService) sendNewsNotification(ctx context.Context, tokens []db.UserToken, title, message string) error {
	var fcmToken, expoToken string
	for _, token := range tokens {
		switch service.PushProvider(token.Provider) {
		case service.PushProviderFCM:
			fcmToken = token.Token
		case service.PushProviderExpo:
			expoToken = token.Token
		}
	}

	if fcmToken == "" && expoToken == "" {
		return nil
	}

	badge := 1

	if fcmToken != "" {
		err := s.pushNotification.SendPush(ctx, &service.PushNotificationInfo{
			UserID:         tokens[0].UserID,
			Title:          title,
			Message:        message,
			Provider:       service.PushProviderFCM,
			UserFCMToken:   fcmToken,
			Badge:          badge,
			AnalyticsLabel: "crypto_news",
		})
		if err != nil {
			s.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if expoToken != "" {
		err := s.pushNotification.SendPush(ctx, &service.PushNotificationInfo{
			UserID:        tokens[0].UserID,
			Title:         title,
			Message:       message,
			Provider:      service.PushProviderExpo,
			UserExpoToken: expoToken,
		})
		if err != nil {
			s.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}

	return nil
}

// EnableNotifications enables/disables push notifications
func (s *MarketInsightsService) EnableNotifications(enabled bool) {
	s.notificationEnabled = enabled
	s.logger.Info(fmt.Sprintf("Notifications %s", map[bool]string{true: "enabled", false: "disabled"}[enabled]))
}

// Shutdown gracefully stops the service
func (s *MarketInsightsService) Shutdown() {
	s.logger.Info("Shutting down market insights service")
	s.backgroundCancel()
}
