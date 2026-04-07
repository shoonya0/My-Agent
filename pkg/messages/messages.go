package messages

import (
	"errors"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Response represents a standardized API response structure.
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
}

// ErrorInfo provides detailed error information.
type ErrorInfo struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details string            `json:"details,omitempty"`
	Field   string            `json:"field,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Error codes for standardized error handling.
const (
	// Validation error codes
	ErrCodeInvalidInput      = "INVALID_INPUT"
	ErrCodeMissingField      = "MISSING_FIELD"
	ErrCodeInvalidEmail      = "INVALID_EMAIL"
	ErrCodePasswordTooShort  = "PASSWORD_TOO_SHORT"
	ErrCodeInvalidFileFormat = "INVALID_FILE_FORMAT"
	ErrCodeFileTooLarge      = "FILE_TOO_LARGE"
	ErrCodeInvalidJSON       = "INVALID_JSON"
	ErrCodeEmptyRequestBody  = "EMPTY_REQUEST_BODY"

	// Authentication error codes
	ErrCodeUnauthorized       = "UNAUTHORIZED"
	ErrCodeInvalidCredentials = "INVALID_CREDENTIALS"
	ErrCodeTokenExpired       = "TOKEN_EXPIRED"
	ErrCodeTokenInvalid       = "TOKEN_INVALID"
	ErrCodeTokenMissing       = "TOKEN_MISSING"

	// Resource error codes
	ErrCodeNotFound      = "NOT_FOUND"
	ErrCodeAlreadyExists = "ALREADY_EXISTS"
	ErrCodeConflict      = "CONFLICT"

	// Rate limiting error codes
	ErrCodeRateLimitExceeded = "RATE_LIMIT_EXCEEDED"

	// Server error codes
	ErrCodeInternalServer     = "INTERNAL_SERVER_ERROR"
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeDatabaseError      = "DATABASE_ERROR"

	// Job/Operation error codes
	ErrCodeJobNotFound     = "JOB_NOT_FOUND"
	ErrCodeJobFailed       = "JOB_FAILED"
	ErrCodeOperationFailed = "OPERATION_FAILED"
)

// Common error messages.
const (
	// Validation messages
	MsgInvalidRequestBody  = "Request body is invalid or malformed"
	MsgEmptyRequestBody    = "Request body cannot be empty"
	MsgEmailRequired       = "Email address is required"
	MsgPasswordRequired    = "Password is required"
	MsgDisplayNameRequired = "Display name is required"
	MsgInvalidEmail        = "Email address format is invalid"
	MsgPasswordTooShort    = "Password must be at least 8 characters long"
	MsgImageRequired       = "Image file is required"
	MsgImageTooLarge       = "Image file size must be under 20MB"
	MsgInvalidImageFormat  = "Image must be in PNG, JPEG, or WebP format"
	MsgPromptRequired      = "Prompt text is required"
	MsgPlatformsRequired   = "At least one platform must be selected"

	// Authentication messages
	MsgInvalidCredentials     = "Invalid email or password"
	MsgEmailAlreadyRegistered = "This email is already registered"
	MsgTokenExpired           = "Authentication token has expired"
	MsgTokenInvalid           = "Authentication token is invalid"
	MsgTokenMissing           = "Authentication token is required"
	MsgUnauthorized           = "You are not authorized to perform this action"

	// Resource messages
	MsgNotFound             = "The requested resource was not found"
	MsgJobNotFound          = "Job not found"
	MsgPlatformNotConnected = "Platform is not connected"

	// Operation messages
	MsgRegistrationFailed  = "User registration failed. Please try again"
	MsgLoginFailed         = "Login failed. Please try again"
	MsgLogoutFailed        = "Failed to log out. Please try again"
	MsgOAuthFailed         = "OAuth sign-in failed. Please try again"
	MsgInvalidOAuthState   = "Invalid or expired OAuth session"
	MsgOAuthNotConfigured  = "OAuth sign-in is not available for this provider"
	MsgJobSubmissionFailed = "Failed to submit job. Please try again"
	MsgJobApprovalFailed   = "Failed to approve job. Please try again"
	MsgJobRejectionFailed  = "Failed to reject job. Please try again"

	// Success messages
	MsgRegistrationSuccess  = "User registered successfully"
	MsgLoginSuccess         = "Login successful"
	MsgLogoutSuccess        = "Logged out successfully"
	MsgJobSubmitted         = "Job submitted successfully"
	MsgJobApproved          = "Job approved successfully"
	MsgJobRejected          = "Job rejected successfully"
	MsgPlatformConnected    = "Platform connected successfully"
	MsgPlatformDisconnected = "Platform disconnected successfully"

	// Server messages
	MsgInternalServerError = "An internal server error occurred. Please try again later"
	MsgServiceUnavailable  = "Service is temporarily unavailable. Please try again later"
	MsgDatabaseError       = "Database operation failed. Please try again"
)

// ErrorResponse creates a standardized error response.
func ErrorResponse(code, message string) Response {
	return Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}
}

// ErrorResponseWithDetails creates a standardized error response with additional details.
func ErrorResponseWithDetails(code, message, details string) Response {
	return Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
}

// ErrorResponseWithField creates a standardized error response with field information.
func ErrorResponseWithField(code, message, field string) Response {
	return Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Field:   field,
		},
	}
}

// ErrorResponseWithFields creates a standardized error response with multiple field errors.
func ErrorResponseWithFields(code, message string, fields map[string]string) Response {
	return Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Fields:  fields,
		},
	}
}

// SuccessResponse creates a standardized success response.
func SuccessResponse(message string, data interface{}) Response {
	return Response{
		Success: true,
		Message: message,
		Data:    data,
	}
}

// ParseBindingErrorWithFields interprets Gin binding errors and returns detailed field information.
func ParseBindingErrorWithFields(err error) Response {
	if err == nil {
		return Response{Success: true}
	}

	// Try to extract validator.ValidationErrors
	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		if len(validationErrs) == 1 {
			// Single field error
			fieldErr := validationErrs[0]
			code, message := parseValidationError(fieldErr)
			field := jsonFieldName(fieldErr.Field())
			return ErrorResponseWithField(code, message, field)
		} else if len(validationErrs) > 1 {
			// Multiple field errors
			fields := make(map[string]string)
			for _, fieldErr := range validationErrs {
				field := jsonFieldName(fieldErr.Field())
				_, message := parseValidationError(fieldErr)
				fields[field] = message
			}
			return ErrorResponseWithFields(
				ErrCodeInvalidInput,
				"Multiple validation errors occurred",
				fields,
			)
		}
	}

	// Handle non-validation errors
	errStr := err.Error()
	switch {
	case errStr == "EOF":
		return ErrorResponse(ErrCodeEmptyRequestBody, MsgEmptyRequestBody)
	case strings.Contains(errStr, "unexpected end of JSON input"):
		return ErrorResponse(ErrCodeInvalidJSON, MsgInvalidRequestBody)
	case strings.Contains(errStr, "invalid character"):
		return ErrorResponse(ErrCodeInvalidJSON, MsgInvalidRequestBody)
	default:
		return ErrorResponse(ErrCodeInvalidInput, MsgInvalidRequestBody)
	}
}

// parseValidationError converts a validator.FieldError to user-friendly code and message.
func parseValidationError(fieldErr validator.FieldError) (code, message string) {
	field := strings.ToLower(fieldErr.Field())
	tag := fieldErr.Tag()

	// Handle specific field + tag combinations
	switch {
	case field == "email" && tag == "required":
		return ErrCodeMissingField, MsgEmailRequired
	case field == "email" && tag == "email":
		return ErrCodeInvalidEmail, MsgInvalidEmail
	case field == "password" && tag == "required":
		return ErrCodeMissingField, MsgPasswordRequired
	case field == "password" && tag == "min":
		return ErrCodePasswordTooShort, MsgPasswordTooShort
	case field == "displayname" && tag == "required":
		return ErrCodeMissingField, MsgDisplayNameRequired
	case tag == "required":
		return ErrCodeMissingField, fieldErr.Field() + " is required"
	case tag == "email":
		return ErrCodeInvalidEmail, fieldErr.Field() + " must be a valid email address"
	case tag == "min":
		return ErrCodeInvalidInput, fieldErr.Field() + " is too short"
	case tag == "max":
		return ErrCodeInvalidInput, fieldErr.Field() + " is too long"
	default:
		return ErrCodeInvalidInput, fieldErr.Field() + " validation failed"
	}
}

// jsonFieldName converts struct field name to JSON field name (snake_case).
func jsonFieldName(field string) string {
	// Convert PascalCase to snake_case
	var result strings.Builder
	for i, r := range field {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
