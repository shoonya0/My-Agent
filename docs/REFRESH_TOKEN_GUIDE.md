# Refresh Token Implementation Guide

## Overview

The refresh token feature has been implemented following the microservice architecture pattern where **api-gateway** handles HTTP requests and forwards them to **auth-service** via gRPC.

## Architecture

```
Client (HTTP)
    ↓
api-gateway (Port 8090)
    ↓ gRPC
auth-service (Port 9190)
    ↓
MySQL + Redis
```

## Token Lifecycle

### Access Token
- **Lifetime**: 15 minutes
- **Purpose**: Authenticate API requests
- **Usage**: `Authorization: Bearer <access_token>` header
- **Claim**: `token_use` is `"access"` (do not send refresh tokens as Bearer access tokens)

### Refresh Token
- **Lifetime**: 7 days
- **Purpose**: Obtain new access tokens without re-authentication
- **Usage**: Send only to `POST /api/refresh` in the JSON body
- **Claim**: `token_use` is `"refresh"`
- **Single-use (rotation)**: Each successful refresh blacklists the refresh JTI you presented and returns a **new** refresh token. Reusing the old refresh JWT returns **401**.

## API Endpoints

### POST /api/refresh

Exchanges a valid refresh token for a new token pair.

**Request:**
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Response (200 OK):**
```json
{
  "success": true,
  "message": "Token refreshed successfully",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "expires_in": 900,
    "token_type": "Bearer"
  }
}
```

**Error Responses:**

- **401 Unauthorized**: Invalid or expired refresh token
  ```json
  {
    "success": false,
    "error": {
      "code": "UNAUTHORIZED",
      "message": "Invalid or expired refresh token"
    }
  }
  ```

- **401 Unauthorized**: Refresh token has been revoked
  ```json
  {
    "success": false,
    "error": {
      "code": "UNAUTHORIZED",
      "message": "Invalid or expired refresh token"
    }
  }
  ```

### POST /api/logout

Ends the session for the **access** token in `Authorization` (required). Optionally revokes the **refresh** token in the JSON body so it cannot be exchanged again.

**Headers**

- `Authorization: Bearer <access_token>` — required

**Body (optional, recommended)**

```json
{
  "refresh_token": "<current refresh token>"
}
```

**Example**

```bash
curl -X POST http://localhost:8090/api/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"
```

If `refresh_token` is omitted, only the access token JTI is blacklisted; always send both when your client stores a refresh token.

## Implementation Details

### Components Modified

1. **api/proto/auth.proto**
   - Added `RefreshToken` RPC to `AuthService`
   - Added `RefreshTokenRequest` message

2. **internal/auth/service.go**
   - Added `RefreshToken(ctx, refreshToken)` method
   - Validates refresh token JWT
   - Checks Redis blacklist for revoked tokens
   - Loads user from database
   - Issues new token pair

3. **internal/auth/handler.go**
   - Added `RefreshToken` gRPC handler
   - Maps service errors to gRPC status codes

4. **internal/auth/repository.go**
   - Added `GetUserByID(ctx, userID)` method

5. **internal/apigateway/auth_handlers.go**
   - Added `RefreshToken` HTTP handler
   - Forwards request to auth-service via gRPC
   - Maps gRPC errors to HTTP status codes

6. **internal/apigateway/gateway.go**
   - Registered `POST /api/refresh` route

## Security Features

### Token Validation
1. **JWT Signature Verification**: Ensures token hasn't been tampered with
2. **Expiry Check**: Validates token hasn't expired
3. **`token_use`**: Access tokens (`access`) must not be sent to `/api/refresh`; refresh tokens (`refresh`) must not be validated as access tokens on `ValidateToken`
4. **Blacklist Check**: Revoked JTIs (logout or post-refresh rotation) are stored in Redis with TTL
5. **User Existence**: Confirms user still exists in database before issuing new tokens

### Refresh Token Rotation (Single-Use)
- Each successful `/api/refresh` **blacklists the refresh token JTI you just used**, then issues a new access + refresh pair
- A second request with the same refresh JWT returns **401** (reuse detection)
- Store the latest refresh token from every refresh response

### Logout and Revocation
- **Bearer access token** is always revoked on logout
- Optional **`refresh_token` in the JSON body** revokes that refresh JTI as well (recommended)

### Rate Limiting
- Public `/api/refresh` endpoint is rate-limited (30 req/min per IP)
- Prevents brute-force attacks on stolen refresh tokens

## Usage Examples

### 1. Login and Store Tokens

```bash
# Login
curl -X POST http://localhost:8090/api/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123"
  }'

# Response
{
  "success": true,
  "message": "Login successful",
  "data": {
    "access_token": "eyJhbGc...",
    "refresh_token": "eyJhbGc...",
    "expires_in": 900,
    "token_type": "Bearer"
  }
}

# Store both tokens securely
ACCESS_TOKEN="eyJhbGc..."
REFRESH_TOKEN="eyJhbGc..."
```

### 2. Use Access Token for API Requests

```bash
# Make authenticated request
curl -X GET http://localhost:8090/api/v1/me \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

### 3. Refresh When Access Token Expires

```bash
# When you get 401 Unauthorized, refresh the token
curl -X POST http://localhost:8090/api/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\": \"$REFRESH_TOKEN\"}"

# Update stored tokens with new values
```

### 4. Logout (Both Tokens)

```bash
curl -X POST http://localhost:8090/api/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}"
```

### 5. Client-Side Token Management (JavaScript Example)

```javascript
class TokenManager {
  constructor() {
    this.accessToken = localStorage.getItem('access_token');
    this.refreshToken = localStorage.getItem('refresh_token');
    this.tokenExpiry = localStorage.getItem('token_expiry');
  }

  async makeRequest(url, options = {}) {
    // Check if token is expired or about to expire (1 min buffer)
    if (this.isTokenExpired()) {
      await this.refreshAccessToken();
    }

    // Add auth header
    options.headers = {
      ...options.headers,
      'Authorization': `Bearer ${this.accessToken}`
    };

    const response = await fetch(url, options);

    // If 401, try to refresh once
    if (response.status === 401) {
      await this.refreshAccessToken();
      options.headers['Authorization'] = `Bearer ${this.accessToken}`;
      return fetch(url, options);
    }

    return response;
  }

  isTokenExpired() {
    if (!this.tokenExpiry) return true;
    const now = Math.floor(Date.now() / 1000);
    return now >= (parseInt(this.tokenExpiry) - 60); // 1 min buffer
  }

  async refreshAccessToken() {
    const response = await fetch('http://localhost:8090/api/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: this.refreshToken })
    });

    if (!response.ok) {
      // Refresh failed, redirect to login
      window.location.href = '/login';
      throw new Error('Token refresh failed');
    }

    const data = await response.json();
    this.accessToken = data.data.access_token;
    this.refreshToken = data.data.refresh_token;
    this.tokenExpiry = Math.floor(Date.now() / 1000) + data.data.expires_in;

    localStorage.setItem('access_token', this.accessToken);
    localStorage.setItem('refresh_token', this.refreshToken);
    localStorage.setItem('token_expiry', this.tokenExpiry);
  }
}

// Usage
const api = new TokenManager();
const response = await api.makeRequest('http://localhost:8090/api/v1/jobs');
```

## Best Practices

### Storage
- **Browser**: Use `httpOnly` cookies (requires server-side changes) or `localStorage` with XSS protection
- **Mobile Apps**: Use secure storage (Keychain on iOS, Keystore on Android)
- **Never** store tokens in:
  - URL parameters
  - Local files
  - Unencrypted storage

### Token Rotation Strategy
- Always replace stored `refresh_token` with the value returned by the latest `/api/refresh` response
- Optional further hardening: token families, device binding, sliding expiration (not all are implemented in this codebase)

### Monitoring
- Log all refresh token usage
- Alert on suspicious patterns:
  - Multiple refreshes from different IPs
  - Rapid refresh attempts
  - Refresh after logout

## Testing

See `test_refresh_token.sh` for automated testing script.

### Manual Testing

```bash
# 1. Login
curl -X POST http://localhost:8090/api/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123"}' \
  | jq -r '.data.refresh_token' > refresh_token.txt

# 2. Wait for access token to expire (15 minutes) or use immediately

# 3. Refresh token
curl -X POST http://localhost:8090/api/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$(cat refresh_token.txt)\"}"

# 4. Logout — send access (bearer) + refresh (body)
curl -X POST http://localhost:8090/api/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$(cat refresh_token.txt)\"}"

# 5. Try to refresh after logout (should fail with 401)
curl -X POST http://localhost:8090/api/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$(cat refresh_token.txt)\"}"
```

## Troubleshooting

### "Invalid or expired refresh token"
- Check if refresh token has expired (7 days)
- Verify user hasn't been deleted
- Check if token was revoked (logout)

### "Token refresh failed" (500 error)
- Check auth-service logs
- Verify database connection
- Verify Redis connection
- Check JWT_SECRET matches across services

### Rate Limited
- Reduce refresh frequency
- Implement exponential backoff
- Check rate limit configuration in gateway

## Future Enhancements

1. **Token Families**: Track token lineage for security beyond single-token rotation
3. **Device Binding**: Bind refresh tokens to specific devices
4. **Refresh Token Revocation List**: Separate blacklist for refresh tokens
5. **OAuth2 Compliance**: Full RFC 6749 implementation
