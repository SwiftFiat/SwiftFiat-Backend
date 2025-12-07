package audit

import (
	"context"
	"encoding/json"
	"net"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/gin-gonic/gin"
)

// EventCategory represents high-level categorization of audit events
type EventCategory string

const (
	CategoryAuthentication EventCategory = "authentication"
	CategoryAuthorization  EventCategory = "authorization"
	CategoryAccount        EventCategory = "account"
	CategoryTransaction    EventCategory = "transaction"
	CategoryKYC            EventCategory = "kyc"
	CategoryCard           EventCategory = "card"
	CategorySecurity       EventCategory = "security"
	CategoryCompliance     EventCategory = "compliance"
	CategorySystem         EventCategory = "system"
	CategoryVaultSavings   EventCategory = "vault_savings"
	CategoryUserManagement EventCategory = "user_management"
	CategoryCrypto         EventCategory = "crypto"
	CategoryRateManager    EventCategory = "rate_manager"
)

// Severity represents the importance level of an audit event
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Action represents the operation performed
type Action string

const (
	ActionCreate  Action = "create"
	ActionRead    Action = "read"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionExecute Action = "execute"
	ActionView    Action = "view"
	ActionExport  Action = "export"
)

// Common event types as constants for type safety
const (
	// User management events
	EventUserTagUpdated            = "user.tag.updated"
	EventUserPushTokenUpdated      = "user.push_token.updated"
	EventUserPhoneNumberUpdated    = "user.phone_number.updated"
	EventUserNameUpdated           = "user.name.updated"
	EventUserDeleted               = "user.deleted"
	EventUserStatusUpdated         = "user.status.updated"
	EventBankAccountAdded          = "user.bank_account.added"
	EventDefaultBankAccountUpdated = "user.bank_account.default_updated"
	EventBankAccountDeleted        = "user.bank_account.deleted"
	EventReferralTracked           = "user.referral.tracked"
	EventReferralWithdrawalRequest = "user.referral.withdrawal_requested"

	// Authentication events
	EventUserLogin              = "user.login"
	EventUserLogout             = "user.logout"
	EventUserLogoutAllDevices   = "user.logout.all_devices"
	EventUserRegistered         = "user.registered"
	EventUserDeactivated        = "user.deactivated"
	EventUserReactivated        = "user.reactivated"
	EventPasswordChanged        = "user.password.changed"
	EventPasscodeChanged        = "user.passcode.changed"
	EventPasscodeCreated        = "user.passcode.created"
	EventPinCreated             = "user.pin.created"
	EventPinChanged             = "user.pin.changed"
	EventPasswordResetRequested = "user.password.reset_requested"
	EventPasswordResetCompleted = "user.password.reset_completed"
	Event2FAEnabled             = "user.2fa.enabled"
	Event2FADisabled            = "user.2fa.disabled"
	Event2FAVerified            = "user.2fa.verified"

	// Account events
	EventAccountCreated     = "account.created"
	EventAccountUpdated     = "account.updated"
	EventAccountDeleted     = "account.deleted"
	EventAccountSuspended   = "account.suspended"
	EventAccountReactivated = "account.reactivated"
	EventEmailVerified      = "account.email.verified"
	EventPhoneVerified      = "account.phone.verified"

	// Transaction events
	EventTransactionCreated     = "transaction.created"
	EventTransactionCompleted   = "transaction.completed"
	EventTransactionFailed      = "transaction.failed"
	EventTransactionRefunded    = "transaction.refunded"
	EventTransactionCancelled   = "transaction.cancelled"
	EventTransactionFeeCreated  = "transaction.fee.created"
	EventWalletSwapCreated      = "wallet.swap.created"
	EventAirtimePurchase        = "airtime.purchase"
	EventDataPurchase           = "data.purchase"
	EventTVSubscriptionPurchase = "tv.subscription.purchase"
	EventElectricityPurchase    = "electiricity.purchase"

	// KYC events
	EventKYCSubmitted   = "kyc.submitted"
	EventKYCApproved    = "kyc.approved"
	EventKYCRejected    = "kyc.rejected"
	EventKYCDocUploaded = "kyc.document.uploaded"

	// Card events
	EventCardCreated    = "card.created"
	EventCardActivated  = "card.activated"
	EventCardFrozen     = "card.frozen"
	EventCardUnfrozen   = "card.unfrozen"
	EventCardTerminated = "card.terminated"
	EventCardFunded     = "card.funded"

	EventSchedulerTriggered = "scheduler.triggered"

	// Security events
	EventSuspiciousActivity   = "security.suspicious_activity"
	EventRateLimitExceeded    = "security.rate_limit_exceeded"
	EventUnauthorizedAccess   = "security.unauthorized_access"
	EventMultipleFailedLogins = "security.multiple_failed_logins"

	// vault savings events
	EventVaultCreated              = "vault.created"
	EventVaultUpdated              = "vault.updated"
	EventVaultDeleted              = "vault.deleted"
	EventSavingsDeposited          = "savings.deposited"
	EventRecurringRuleUpdated      = "savings.recurring_rule.updated"
	EventSavingsWithdrawn          = "savings.withdrawn"
	EventYieldGenerated            = "yield.generated"
	EventYieldsProcessed           = "yields.processed"
	EventYieldConfigCreated        = "yield.config.created"
	EventYieldConfigUpdated        = "yield.config.updated"
	EventYieldConfigDeleted        = "yield.config.deleted"
	EventYieldConfigDeactivated    = "yield.config.deactivated"
	EventYieldConfigActivated      = "yield.config.activated"
	EventInterestPaid              = "interest.paid"
	EventVaultSuspended            = "vault.suspended"
	EventVaultReactivated          = "vault.reactivated"
	EventVaultTransferIn           = "vault.transfer_in"
	EventVaultTransferOut          = "vault.transfer_out"
	EventVaultAutoInvestSet        = "vault.auto_invest.set"
	EventVaultRecurringRulePaused  = "vault.recurring_rule.paused"
	EventVaultRecurringRuleResumed = "vault.recurring_rule.resumed"

	// Transfer events
	EventWalletTransferCreated = "wallet.transfer.created"
	EventFiatTransferCreated   = "fiat.transfer.created"

	// Crypto events
	EventCreateStaticWallet = "cryptomus.wallet.created"
	EventCreateQrCode       = "qrcode.created"
	EventDeleteQrCode       = "qrcode.deleted"
	EventCreateConversionRule = "smart-convert.rule.created"
	EventPauseConversionRule = "smart-convert.rule.paused"
	EventResumeConversionRule = "smart-convert.rule.resumed"
	EventDeleteConversionRule = "smart-convert.rule.deleted"
	EventManualConversion = "smart-convert.manual"

	// Reward events
	EventCreateRewardConfig = "rewards.config.created"
	EventUpdateRewardConfig = "rewards.config.updated"
	EventDeleteRewardConfig = "rewards.config.deleted"
	EventActivateRewardConfig = "rewards.config.activated"
	EventDeactivateRewardConfig = "rewards.config.deactivated"
)

// LogEntry represents the input for creating an audit log
type LogEntry struct {
	EventCategory EventCategory          `json:"event_category"`
	EventType     string                 `json:"event_type"`
	Severity      Severity               `json:"severity"`
	ActorID       *int64                 `json:"actor_id,omitempty"`
	ActorType     string                 `json:"actor_type"`
	ActorEmail    *string                `json:"actor_email,omitempty"`
	EntityType    string                 `json:"entity_type"`
	EntityID      string                 `json:"entity_id"`
	IPAddress     net.IP                 `json:"ip_address,omitempty"`
	UserAgent     string                 `json:"user_agent,omitempty"`
	RequestID     string                 `json:"request_id,omitempty"`
	Action        Action                 `json:"action"`
	Description   string                 `json:"description"`
	OldValues     map[string]interface{} `json:"old_values,omitempty"`
	NewValues     map[string]interface{} `json:"new_values,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Success       bool                   `json:"success"`
	ErrorMessage  *string                `json:"error_message,omitempty"`
}

// SearchFilters for querying audit logs
type SearchFilters struct {
	EventCategory *EventCategory `json:"event_category,omitempty"`
	EventType     *string        `json:"event_type,omitempty"`
	Severity      *Severity      `json:"severity,omitempty"`
	ActorID       *int64         `json:"actor_id,omitempty"`
	EntityType    *string        `json:"entity_type,omitempty"`
	EntityID      *string        `json:"entity_id,omitempty"`
	IPAddress     *string        `json:"ip_address,omitempty"`
	StartDate     time.Time      `json:"start_date"`
	EndDate       time.Time      `json:"end_date"`
	Limit         int32          `json:"limit"`
	Offset        int32          `json:"offset"`
}

// LogResponse represents an audit log entry returned from queries
type LogResponse struct {
	ID            int64          `json:"id"`
	EventCategory EventCategory  `json:"event_category"`
	EventType     string         `json:"event_type"`
	Severity      Severity       `json:"severity"`
	ActorID       *int64         `json:"actor_id,omitempty"`
	ActorType     string         `json:"actor_type"`
	ActorEmail    *string        `json:"actor_email,omitempty"`
	EntityType    string         `json:"entity_type"`
	EntityID      string         `json:"entity_id"`
	IPAddress     *string        `json:"ip_address,omitempty"`
	UserAgent     *string        `json:"user_agent,omitempty"`
	RequestID     *string        `json:"request_id,omitempty"`
	Action        Action         `json:"action"`
	Description   string         `json:"description"`
	OldValues     map[string]any `json:"old_values,omitempty"`
	NewValues     map[string]any `json:"new_values,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Success       bool           `json:"success"`
	ErrorMessage  *string        `json:"error_message,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// AuditStats represents aggregated statistics
type AuditStats struct {
	TotalEvents      int64     `json:"total_events"`
	UniqueActors     int64     `json:"unique_actors"`
	UniqueEntities   int64     `json:"unique_entities"`
	SuccessfulEvents int64     `json:"successful_events"`
	FailedEvents     int64     `json:"failed_events"`
	CriticalEvents   int64     `json:"critical_events"`
	ErrorEvents      int64     `json:"error_events"`
	WarningEvents    int64     `json:"warning_events"`
	StartDate        time.Time `json:"start_date"`
	EndDate          time.Time `json:"end_date"`
}

// CategoryCount for event category statistics
type CategoryCount struct {
	Category EventCategory `json:"category"`
	Count    int64         `json:"count"`
}

// SeverityCount for severity statistics
type SeverityCount struct {
	Severity Severity `json:"severity"`
	Count    int64    `json:"count"`
}

// SuspiciousActivity represents potentially malicious behavior
type SuspiciousActivity struct {
	ActorID    *int64    `json:"actor_id,omitempty"`
	ActorEmail *string   `json:"actor_email,omitempty"`
	IPAddress  *string   `json:"ip_address,omitempty"`
	EventCount int64     `json:"event_count"`
	LastEvent  time.Time `json:"last_event"`
}

// EntityActivity represents recent activity for an entity
type EntityActivity struct {
	EntityType    string    `json:"entity_type"`
	EntityID      string    `json:"entity_id"`
	ActivityCount int64     `json:"activity_count"`
	LastActivity  time.Time `json:"last_activity"`
	UniqueActors  int64     `json:"unique_actors"`
}

// IPActivity represents activity from a specific IP address
type IPActivity struct {
	IPAddress    string    `json:"ip_address"`
	UniqueUsers  int64     `json:"unique_users"`
	TotalEvents  int64     `json:"total_events"`
	FailedEvents int64     `json:"failed_events"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

// NewAuthenticationLog creates a log entry for authentication events
func NewAuthenticationLog(c *gin.Context, eventType string, actorID *int64, email *string, ActorTypeUser string, success bool, errMsg *string) *LogEntry {
	severity := SeverityInfo
	if !success {
		severity = SeverityWarning
	}

	return &LogEntry{
		EventCategory: CategoryAuthentication,
		EventType:     eventType,
		Severity:      severity,
		ActorID:       actorID,
		ActorType:     ActorTypeUser,
		ActorEmail:    email,
		EntityType:    "user",
		EntityID:      "",
		Action:        ActionExecute,
		Success:       success,
		ErrorMessage:  errMsg,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
	}
}

func NewLog(c *gin.Context, eventType, entityID, entityType, desc string, actorID *int64, email *string, ActorTypeUser string, success bool, errMsg *string) *LogEntry {
	severity := SeverityInfo
	if !success {
		severity = SeverityWarning
	}

	return &LogEntry{
		EventCategory: CategoryCrypto,
		EventType:     eventType,
		Severity:      severity,
		ActorID:       actorID,
		ActorType:     ActorTypeUser,
		ActorEmail:    email,
		EntityType:    entityType,
		EntityID:      entityID,
		Action:        ActionCreate,
		Success:       success,
		ErrorMessage:  errMsg,
		Description:   desc,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
	}
}

// NewTransactionLog creates a log entry for transaction events
func NewTransactionLog(c *gin.Context, eventType, transactionID, userRole string, actorID int64, amount float64, currency string, success bool) *LogEntry {
	return &LogEntry{
		EventCategory: CategoryTransaction,
		EventType:     eventType,
		Severity:      SeverityInfo,
		ActorID:       &actorID,
		ActorType:     userRole,
		EntityType:    "transaction",
		EntityID:      transactionID,
		Action:        ActionCreate,
		Success:       success,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
		Metadata: map[string]any{
			"amount":   amount,
			"currency": currency,
		},
	}
}

func NewVaultLog(c *gin.Context, eventType, entityType, entityID, userRole string, actorID *int64, severity Severity) *LogEntry {
	return &LogEntry{
		EventCategory: CategoryVaultSavings,
		EventType:     eventType,
		Severity:      severity,
		ActorID:       actorID,
		ActorType:     userRole,
		EntityType:    "vault_savings",
		EntityID:      entityID,
		Action:        ActionExecute,
		Success:       true,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
	}
}

func NewUserLog(c *gin.Context, eventType, entityID, userRole, desc string, actorID *int64, severity Severity, action Action, success bool) *LogEntry {
	return &LogEntry{
		EventCategory: CategoryUserManagement,
		EventType:     eventType,
		Severity:      severity,
		ActorID:       actorID,
		ActorType:     userRole,
		EntityType:    "user",
		EntityID:      entityID,
		Action:        action,
		Success:       success,
		Description:   desc,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
	}
}

// NewSecurityLog creates a log entry for security events
func NewSecurityLog(c *gin.Context, eventType, entityType, entityID string, actorID *int64, severity Severity) *LogEntry {
	return &LogEntry{
		EventCategory: CategorySecurity,
		EventType:     eventType,
		Severity:      severity,
		ActorID:       actorID,
		ActorType:     "system",
		EntityType:    entityType,
		EntityID:      entityID,
		Action:        ActionExecute,
		Success:       false,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
	}
}

func NewReferralLog(c *gin.Context, eventType, entityType, entityID, userRole string, actorID *int64, severity Severity) *LogEntry {
	return &LogEntry{
		EventCategory: CategoryUserManagement,
		EventType:     eventType,
		Severity:      severity,
		ActorID:       actorID,
		ActorType:     userRole,
		EntityType:    entityType,
		EntityID:      entityID,
		Action:        ActionExecute,
		Success:       true,
		IPAddress:     net.ParseIP(c.ClientIP()),
		UserAgent:     c.Request.UserAgent(),
	}
}

func ToLogResponse(log db.AuditLog) *LogResponse {
	var actorID *int64
	if log.ActorID.Valid {
		actorID = &log.ActorID.Int64
	}

	var actorEmail *string
	if log.ActorEmail.Valid {
		actorEmail = &log.ActorEmail.String
	}

	var ipAddress *string
	if log.IpAddress.Valid {
		s := log.IpAddress.IPNet.String()
		ipAddress = &s
	}

	var userAgent *string
	if log.UserAgent.Valid {
		userAgent = &log.UserAgent.String
	}

	var requestID *string
	if log.RequestID.Valid {
		requestID = &log.RequestID.String
	}

	var errorMessage *string
	if log.ErrorMessage.Valid {
		errorMessage = &log.ErrorMessage.String
	}

	var oldValues map[string]any
	if len(log.OldValues.RawMessage) > 0 {
		_ = json.Unmarshal(log.OldValues.RawMessage, &oldValues)
	}

	var newValues map[string]any
	if len(log.NewValues.RawMessage) > 0 {
		_ = json.Unmarshal(log.NewValues.RawMessage, &newValues)
	}

	var metadata map[string]any
	if len(log.Metadata.RawMessage) > 0 {
		_ = json.Unmarshal(log.Metadata.RawMessage, &metadata)
	}

	return &LogResponse{
		ID:            int64(log.ID),
		EventCategory: EventCategory(log.EventCategory),
		EventType:     log.EventType,
		Severity:      Severity(log.Severity),
		ActorID:       actorID,
		ActorType:     log.ActorType,
		ActorEmail:    actorEmail,
		EntityType:    log.EntityType,
		EntityID:      log.EntityID,
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
		RequestID:     requestID,
		Action:        Action(log.Action),
		Description:   log.Description,
		OldValues:     oldValues,
		NewValues:     newValues,
		Metadata:      metadata,
		Success:       log.Success,
		ErrorMessage:  errorMessage,
		CreatedAt:     log.CreatedAt,
	}
}

func ToLogResponses(logs []db.AuditLog) []LogResponse {
	responses := make([]LogResponse, len(logs))
	for i, log := range logs {
		responses[i] = *ToLogResponse(log)
	}
	return responses
}

// Context helpers for request enrichment
type contextKey string

const (
	contextKeyRequestID contextKey = "request_id"
	contextKeyUserID    contextKey = "user_id"
	contextKeyUserEmail contextKey = "user_email"
	contextKeyIPAddress contextKey = "ip_address"
	contextKeyUserAgent contextKey = "user_agent"
)

// EnrichFromContext extracts audit information from context
func EnrichFromContext(ctx context.Context, entry *LogEntry) {
	if requestID, ok := ctx.Value(contextKeyRequestID).(string); ok {
		entry.RequestID = requestID
	}

	if userID, ok := ctx.Value(contextKeyUserID).(int64); ok {
		entry.ActorID = &userID
	}

	if email, ok := ctx.Value(contextKeyUserEmail).(string); ok {
		entry.ActorEmail = &email
	}

	if ipStr, ok := ctx.Value(contextKeyIPAddress).(string); ok {
		if ip := net.ParseIP(ipStr); ip != nil {
			entry.IPAddress = ip
		}
	}

	if ua, ok := ctx.Value(contextKeyUserAgent).(string); ok {
		entry.UserAgent = ua
	}
}

// Context builders
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, contextKeyRequestID, requestID)
}

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, contextKeyUserID, userID)
}

func WithUserEmail(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, contextKeyUserEmail, email)
}

func WithIPAddress(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, contextKeyIPAddress, ip)
}

func WithUserAgent(ctx context.Context, ua string) context.Context {
	return context.WithValue(ctx, contextKeyUserAgent, ua)
}
