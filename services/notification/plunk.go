package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type Plunk struct {
	HttpClient *http.Client
	Config     *utils.Config
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

	req.Header.Set("Authorization", "Bearer "+s.Config.PlunkApiKey)
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
