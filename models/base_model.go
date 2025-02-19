package models

import "github.com/SwiftFiat/SwiftFiat-Backend/utils"

type SuccessResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Version string      `json:"version"`
}

type CustomResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Version string      `json:"version"`
}

type ErrorResponse struct {
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Errors  []string `json:"errors,omitempty"`
	Version string   `json:"version"`
}

func NewError(msg string) *ErrorResponse {
	return &ErrorResponse{
		Status:  "failed",
		Message: msg,
		Version: utils.REVISION,
	}
}

func NewSuccess(msg string, data interface{}) *SuccessResponse {
	return &SuccessResponse{
		Status:  "successful",
		Message: msg,
		Data:    data,
		Version: utils.REVISION,
	}
}

func NewCustomResponse(status string, msg string, data interface{}) *CustomResponse {
	return &CustomResponse{
		Status:  status,
		Message: msg,
		Data:    data,
		Version: utils.REVISION,
	}
}
