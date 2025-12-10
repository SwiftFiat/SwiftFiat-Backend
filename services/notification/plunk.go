package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type Plunk struct {
	HttpClient *http.Client
	Config     *utils.Config
	Redis      *redis.RedisService
}

type EmailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type TemplatedEmailRequest struct {
	To         string         `json:"to"`
	TemplateID string         `json:"template"`
	Data       map[string]any `json:"data"`
}

type TrackingEvent struct {
	EmailID   string `json:"email_id"`
	Event     string `json:"event"`
	TargetURL string `json:"target_url,omitempty"`
}

func NewPlunkService(config *utils.Config) *Plunk {
	return &Plunk{
		Config:     config,
		HttpClient: &http.Client{},
	}
}

func (s *Plunk) makeRequest(method, endpoint string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, s.Config.PlunkBaseUrl+endpoint, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.Config.PlunkSecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, errors.New(string(respBody))
	}

	return respBody, nil
}

func (s *Plunk) SendEmail(to, subject, body string) error {
	email := EmailRequest{
		To:      to,
		Subject: subject,
		Body:    body,
	}

	_, err := s.makeRequest("POST", "/send", email)
	return err
}

func (s *Plunk) SendTemplatedEmail(to, templateID string, data map[string]any) error {
	email := TemplatedEmailRequest{
		To:         to,
		TemplateID: templateID,
		Data:       data,
	}

	_, err := s.makeRequest("POST", "/send", email)
	return err
}

func (s *Plunk) TrackAction(email, event string, data map[string]any) error {
	_, err := s.makeRequest("POST", "/track", map[string]any{
		"event":      event,
		"email":      email,
		"subscribed": true,
		"data":       data,
	})
	return err
}

// Helper function to send failed login alert
func (s *Plunk) SendFailedLoginAlert(dbUser *db.User, failedCount int, clientIP string) error {
	tplData := map[string]any{
		"FirstName": dbUser.FirstName.String,
		"Attempts":  failedCount,
		"IP":        clientIP,
		"Time":      time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":      time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/failed_login_alert.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render failed login template: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: s.HttpClient,
	}

	subject := "SwiftFiat - Multiple Failed Login Attempts Detected"
	if err := emailService.SendEmail(dbUser.Email, subject, body); err != nil {
		return fmt.Errorf("failed to render failed login template: %v", err)
	}
	return nil
}

// Helper function to send new device alert
func (s *Plunk) SendNewDeviceAlert(dbUser *db.User, device struct{ IP, UserAgent string }) error {
	tplData := map[string]any{
		"FirstName": dbUser.FirstName.String,
		"LoginTime": time.Now().Format("02 Jan 2006 15:04 MST"),
		"IP":        device.IP,
		"UserAgent": device.UserAgent,
		"Location":  utils.GetLocationFromIP(device.IP),
		"Year":      time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/new_device_login.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render new device login template: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: s.HttpClient,
	}

	subject := "SwiftFiat - New Device Login Alert"
	if err := emailService.SendEmail(dbUser.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send new device login alert: %v", err)
	}
	return nil
}

// Helper function to send admin OTP
func (s *Plunk) SendAdminOTP(dbUser *db.User, email, otp string) error {
	tplData := map[string]any{
		"Name": dbUser.FirstName.String,
		"OTP":  otp,
	}

	body, err := utils.RenderEmailTemplate("templates/otp_template_designed.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render admin OTP template: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: s.HttpClient,
	}

	subject := "SwiftFiat - Admin Login OTP"
	if err := emailService.SendEmail(email, subject, body); err != nil {
		return fmt.Errorf("failed to send admin OTP email: %v", err)
	}
	return nil
}

// sendVerificationEmail sends the email verification OTP
func (s *Plunk) SendVerificationEmail(user *db.User, email, verificationCode string) error {
	tplData := map[string]any{
		"Name": user.FirstName.String,
		"OTP":  verificationCode,
	}

	body, err := utils.RenderEmailTemplate("templates/otp_template_designed.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render verification template: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: s.HttpClient,
	}

	subject := "SwiftFiat - Verify your email"
	if err := emailService.SendEmail(email, subject, body); err != nil {
		logging.NewLogger().Error(fmt.Sprintf("failed to send verification email to %s: %v", email, err))

		// Store failed email attempt for potential retry or admin notification
		bgCtx := context.Background()
		retryKey := fmt.Sprintf("failed_verification_email:%d", user.ID)
		if setErr := s.Redis.Set(bgCtx, retryKey, email, 24*time.Hour); setErr != nil {
			return fmt.Errorf("failed to store failed email attempt: %v", setErr)
		}
	}
	return nil
}

func (s *Plunk) SendAdminRegistrationEmail(user *db.User, twoFASecret, twoFAQRCode, twoFASetupURL string) error {
	tplData := map[string]any{
		"FirstName":     user.FirstName.String,
		"Email":         user.Email,
		"Role":          user.Role,
		"TwoFASetupURL": twoFASetupURL,
		"Year":          time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/admin_registration.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render admin registration email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat Admin Registration - Complete Your 2FA Setup"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send admin registration email: %v", err)
	}

	return nil
}

// ==========================================================
// Vault Savings
// ==========================================================
func (s *Plunk) SendGoalCreatedEmail(ctx context.Context, user *db.User, vaultName, currency, target_amount string) error {
	tplData := map[string]any{
		"FirstName":    user.FirstName.String,
		"VaultName":    vaultName,
		"Currency":     currency,
		"TargetAmount": target_amount,
		"DashboardUrl": "",
		"CreatedDate":  time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":         time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/goal_created.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault goal created email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Goal Created"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault goal created email: %v", err)
	}

	return nil
}

func (s *Plunk) SendGoalCompletedEmail(ctx context.Context, user *db.User, name, goalAmount, currency, daysToComplete string) error {
	tplData := map[string]any{
		"FirstName":      user.FirstName.String,
		"VaultName":      name,
		"Currency":       currency,
		"GoalAmount":     goalAmount,
		"CompletedDate":  time.Now().Format("02 Jan 2006 15:04 MST"),
		"DaysToComplete": daysToComplete,
		"Year":           time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/goal_completed.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault goal completed email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Goal Completed"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault goal completed email: %v", err)
	}

	return nil
}

func (s *Plunk) SendDepositSuccessEmail(ctx context.Context, user *db.User, name, amount, currency, newBalance, reference string) error {
	tplData := map[string]any{
		"FirstName":       user.FirstName.String,
		"VaultName":       name,
		"Currency":        currency,
		"Amount":          amount,
		"NewBalance":      newBalance,
		"Reference":       reference,
		"TransactionTime": time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":            time.Now().Year(),
		"Source":          "Wallet",
	}

	body, err := utils.RenderEmailTemplate("templates/goal_deposit_success.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault goal deposit success email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Goal Deposit Success"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault goal deposit success email: %v", err)
	}

	return nil
}

func (s *Plunk) SendWithdrawal2FARequiredEmail(ctx context.Context, user *db.User, txID, reference, amount, currency, initiatedTime, destination string) error {
	tplData := map[string]any{
		"FirstName":     user.FirstName.String,
		"TransactionID": txID,
		"Amount":        amount,
		"Currency":      currency,
		"InitiatedTime": initiatedTime,
		"Reference":     reference,
		"Destination":   destination,
		"Year":          time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/vault_withdrawal_2fa_required.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault withdrawal 2FA required email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Withdrawal 2FA Required"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault withdrawal 2FA required email: %v", err)
	}

	return nil
}

func (s *Plunk) SendWithdrawalSuccessEmail(ctx context.Context, user *db.User, name, amount, currency, reference string) error {
	tplData := map[string]any{
		"FirstName":     user.FirstName.String,
		"VaultName":     name,
		"Currency":      currency,
		"Amount":        amount,
		"Reference":     reference,
		"CompletedTime": time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":          time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/vault_withdrawal_success.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault withdrawal success email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Withdrawal Success"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault withdrawal success email: %v", err)
	}

	return nil
}

func (s *Plunk) SendRecurringDepositFailedEmail(ctx context.Context, user *db.User, name, amount, currency, reason string, scheduledDate time.Time) error {
	tplData := map[string]any{
		"FirstName":     user.FirstName.String,
		"VaultName":     name,
		"Currency":      currency,
		"Amount":        amount,
		"Reason":        reason,
		"ScheduledDate": scheduledDate.Format("02 Jan 2006 15:04 MST"),
		"Year":          time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/vault_recurring_deposit_failed.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault recurring deposit failed email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Recurring Deposit Failed"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault recurring deposit failed email: %v", err)
	}

	return nil
}

func (s *Plunk) SendRecurringDepositSuccessEmail(ctx context.Context, user *db.User, name, amount, currency, reference string, date time.Time) error {
	tplData := map[string]any{
		"FirstName":     user.FirstName.String,
		"VaultName":     name,
		"Currency":      currency,
		"Amount":        amount,
		"Reference":     reference,
		"DepositDate":   date.Format("02 Jan 2006 15:04 MST"),
		"CompletedTime": time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":          time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/vault_recurring_deposit_success.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault recurring deposit success email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Recurring Deposit Success"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault recurring deposit success email: %v", err)
	}

	return nil
}

func (s *Plunk) SendYieldCredited(ctx context.Context, user *db.User, name, amount, currency, balance, reference string) error {
	tplData := map[string]any{
		"FirstName": user.FirstName.String,
		"VaultName": name,
		"Currency":  currency,
		"Amount":    amount,
		"Balance":   balance,
		"Reference": reference,
		"Date":      time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":      time.Now().Year(),
	}

	body, err := utils.RenderEmailTemplate("templates/vault_yield_credited.html", tplData)
	if err != nil {
		return fmt.Errorf("failed to render vault yield credited email: %v", err)
	}

	emailService := Plunk{
		Config:     s.Config,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	subject := "SwiftFiat - Yield Credited"
	if err := emailService.SendEmail(user.Email, subject, body); err != nil {
		return fmt.Errorf("failed to send vault yield credited email: %v", err)
	}

	return nil
}
