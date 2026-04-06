# Messages Package

The `messages` package provides a centralized, standardized approach to handling API responses, error messages, and success messages across the MyAgent application. It ensures consistent, user-friendly communication with clients and simplifies error handling.

## Features

- **Standardized Response Format**: All API responses follow a consistent structure
- **User-Friendly Error Messages**: Transforms technical errors (like "EOF") into clear, actionable messages
- **Error Codes**: Provides machine-readable error codes for client-side handling
- **Automatic Error Parsing**: Intelligent parsing of Gin validation errors
- **Field-Specific Detection**: Automatically identifies which field(s) caused validation errors
- **Batch Validation**: Reports multiple field errors at once

## Response Structure

All responses follow this standardized format:

```json
{
  "success": true,
  "message": "User registered successfully",
  "data": {
    "access_token": "...",
    "refresh_token": "...",
    "expires_in": 3600,
    "token_type": "Bearer"
  }
}
```

Error responses include detailed error information with field detection:

```json
{
  "success": false,
  "error": {
    "code": "MISSING_FIELD",
    "message": "Password is required",
    "field": "password"
  }
}
```

Multiple field errors are reported together:

```json
{
  "success": false,
  "error": {
    "code": "INVALID_INPUT",
    "message": "Multiple validation errors occurred",
    "fields": {
      "email": "Email address format is invalid",
      "password": "Password is required"
    }
  }
}
```

## Usage

### 1. Handling Binding Errors with Field Detection (Recommended)

Use `ParseBindingErrorWithFields` to automatically detect which field(s) are empty or invalid:

```go
if err := c.ShouldBindJSON(&req); err != nil {
    resp := messages.ParseBindingErrorWithFields(err)
    c.JSON(http.StatusBadRequest, resp)
    return
}
```

This automatically:
- Detects which field(s) caused the error
- Returns field name(s) in the response
- Handles multiple validation errors at once
- Converts technical errors like "EOF" into user-friendly messages

**Response Example:**
```json
{
  "success": false,
  "error": {
    "code": "MISSING_FIELD",
    "message": "Password is required",
    "field": "password"
  }
}
```

### 1b. Simple Error Parsing (Legacy)

For backward compatibility, the simple version is still available:

```go
if err := c.ShouldBindJSON(&req); err != nil {
    code, message := messages.ParseBindingError(err)
    c.JSON(http.StatusBadRequest, messages.NewErrorResponse(code, message))
    return
}
```

### 2. Success Responses

```go
// Return success with data
resp := &model.TokenResponse{
    AccessToken: "...",
    RefreshToken: "...",
    ExpiresIn: 3600,
    TokenType: "Bearer",
}

c.JSON(http.StatusCreated, messages.NewSuccessResponse(
    messages.MsgRegistrationSuccess,
    resp,
))
```

### 3. Error Responses

```go
// Simple error
c.JSON(http.StatusNotFound, messages.NewErrorResponse(
    messages.ErrCodeNotFound,
    messages.MsgJobNotFound,
))

// Error with additional details
c.JSON(http.StatusBadRequest, messages.NewErrorResponseWithDetails(
    messages.ErrCodeInvalidInput,
    "Invalid image format",
    "Supported formats: PNG, JPEG, WebP",
))
```

## Available Error Codes

### Validation Errors
- `INVALID_INPUT` - General input validation failure
- `MISSING_FIELD` - Required field is missing
- `INVALID_EMAIL` - Email format is invalid
- `PASSWORD_TOO_SHORT` - Password doesn't meet minimum length
- `INVALID_FILE_FORMAT` - File format not supported
- `FILE_TOO_LARGE` - File exceeds size limit
- `INVALID_JSON` - JSON parsing failed
- `EMPTY_REQUEST_BODY` - Request body is empty

### Authentication Errors
- `UNAUTHORIZED` - User not authorized
- `INVALID_CREDENTIALS` - Login credentials invalid
- `TOKEN_EXPIRED` - JWT token expired
- `TOKEN_INVALID` - JWT token is malformed
- `TOKEN_MISSING` - JWT token not provided

### Resource Errors
- `NOT_FOUND` - Resource not found
- `ALREADY_EXISTS` - Resource already exists (e.g., email taken)
- `CONFLICT` - Resource conflict

### Server Errors
- `INTERNAL_SERVER_ERROR` - Generic server error
- `SERVICE_UNAVAILABLE` - Service temporarily unavailable
- `DATABASE_ERROR` - Database operation failed

## Pre-defined Messages

The package includes pre-defined messages for common scenarios:

### Validation Messages
- `MsgInvalidRequestBody`
- `MsgEmptyRequestBody`
- `MsgEmailRequired`
- `MsgPasswordRequired`
- `MsgDisplayNameRequired`
- `MsgInvalidEmail`
- `MsgPasswordTooShort`
- `MsgImageRequired`
- `MsgImageTooLarge`
- `MsgInvalidImageFormat`

### Authentication Messages
- `MsgInvalidCredentials`
- `MsgEmailAlreadyRegistered`
- `MsgTokenExpired`
- `MsgTokenInvalid`
- `MsgTokenMissing`
- `MsgUnauthorized`

### Success Messages
- `MsgRegistrationSuccess`
- `MsgLoginSuccess`
- `MsgLogoutSuccess`
- `MsgJobSubmitted`
- `MsgJobApproved`
- `MsgJobRejected`
- `MsgPlatformConnected`
- `MsgPlatformDisconnected`

## Examples

### Example 1: User Registration with Empty Body

**Request:**
```bash
curl -X POST http://localhost:8080/api/register \
  -H "Content-Type: application/json" \
  -d ''
```

**Response (Before):**
```json
{
  "error": "EOF"
}
```

**Response (After):**
```json
{
  "success": false,
  "error": {
    "code": "EMPTY_REQUEST_BODY",
    "message": "Request body cannot be empty"
  }
}
```

### Example 2: Missing Password Field

**Request:**
```bash
curl -X POST http://localhost:8080/api/register \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "display_name": "John"}'
```

**Response:**
```json
{
  "success": false,
  "error": {
    "code": "MISSING_FIELD",
    "message": "Password is required"
  }
}
```

### Example 3: Successful Registration

**Request:**
```bash
curl -X POST http://localhost:8080/api/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "secure123",
    "display_name": "John Doe"
  }'
```

**Response:**
```json
{
  "success": true,
  "message": "User registered successfully",
  "data": {
    "access_token": "eyJhbGc...",
    "refresh_token": "eyJhbGc...",
    "expires_in": 3600,
    "token_type": "Bearer"
  }
}
```

## Client-Side Integration

With standardized error codes and field detection, clients can implement intelligent error handling:

```javascript
async function registerUser(email, password, displayName) {
  try {
    const response = await fetch('/api/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password, display_name: displayName })
    });
    
    const data = await response.json();
    
    if (data.success) {
      // Handle success
      console.log(data.message);
      return data.data;
    } else {
      const error = data.error;
      
      // Handle single field error
      if (error.field) {
        highlightField(error.field, error.message);
        showError(error.message);
        return;
      }
      
      // Handle multiple field errors
      if (error.fields) {
        Object.entries(error.fields).forEach(([field, message]) => {
          highlightField(field, message);
        });
        showError('Please fix the highlighted fields');
        return;
      }
      
      // Handle error based on error code
      switch (error.code) {
        case 'EMPTY_REQUEST_BODY':
          showError('Please fill in all fields');
          break;
        case 'INVALID_EMAIL':
          showError('Please enter a valid email address');
          break;
        case 'PASSWORD_TOO_SHORT':
          showError('Password must be at least 8 characters');
          break;
        case 'ALREADY_EXISTS':
          showError('This email is already registered');
          break;
        default:
          showError(error.message);
      }
    }
  } catch (err) {
    showError('Network error. Please try again.');
  }
}

function highlightField(fieldName, message) {
  const input = document.querySelector(`input[name="${fieldName}"]`);
  if (input) {
    input.classList.add('error');
    const errorSpan = input.nextElementSibling;
    if (errorSpan && errorSpan.classList.contains('error-message')) {
      errorSpan.textContent = message;
    }
  }
}
```

## Best Practices

1. **Always use ParseBindingError**: When handling `ShouldBindJSON` or `ShouldBind` errors, always use `ParseBindingError` to convert technical errors into user-friendly messages.

2. **Use pre-defined messages**: Use the pre-defined message constants instead of hardcoding strings. This ensures consistency across the application.

3. **Add details when helpful**: Use `NewErrorResponseWithDetails` when additional context would help the user understand or fix the error.

4. **Be specific with error codes**: Choose the most specific error code available. This helps clients implement targeted error handling.

5. **Include success messages**: Even for successful operations, include a clear success message. This improves user experience and helps with debugging.

## Extending the Package

To add new error codes or messages:

1. Add the error code constant to the `const` block
2. Add the corresponding message constant
3. Update this README with the new additions
4. Consider adding parsing logic to `ParseBindingError` if applicable

## Migration Guide

To migrate existing code to use this package:

1. Add `"myAgent/pkg/messages"` to imports
2. Replace raw error responses with `messages.NewErrorResponse()`
3. Replace success responses with `messages.NewSuccessResponse()`
4. Use `ParseBindingError` for all binding errors
5. Test all endpoints to ensure proper error handling
