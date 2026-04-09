package credentials

import (
	"context"
	"fmt"

	"myAgent/pkg/crypto"
	"myAgent/pkg/types"

	"github.com/google/uuid"
)

// Service manages encrypted platform credentials for users.
type Service interface {
	Connect(ctx context.Context, userID string, req types.ConnectPlatformRequest) (*types.PlatformCredentialResponse, error)
	List(ctx context.Context, userID string) (*types.ListCredentialsResponse, error)
	Get(ctx context.Context, userID, platform string) (*types.PlatformCredentialResponse, error)
	Update(ctx context.Context, userID, platform string, req types.UpdatePlatformRequest) (*types.PlatformCredentialResponse, error)
	Disconnect(ctx context.Context, userID, platform string) error
	GetDecryptedToken(ctx context.Context, userID, platform string) (token string, metadata map[string]string, err error)
}

type service struct {
	repo      Repository
	encryptor *crypto.Encryptor
}

// NewService returns a Service backed by the repository and AES-GCM.
func NewService(repo Repository, enc *crypto.Encryptor) Service {
	return &service{repo: repo, encryptor: enc}
}

func (s *service) Connect(ctx context.Context, userID string, req types.ConnectPlatformRequest) (*types.PlatformCredentialResponse, error) {
	encTok, err := s.encryptor.Encrypt(req.Token)
	if err != nil {
		return nil, fmt.Errorf("credentials: encrypt token: %w", err)
	}

	meta := cloneMeta(req.Metadata)
	cred := &types.PlatformCredential{
		ID:             uuid.New().String(),
		UserID:         userID,
		Platform:       req.Platform,
		AccessTokenEnc: encTok,
		Metadata:       meta,
	}

	if err := s.repo.Upsert(ctx, cred); err != nil {
		return nil, err
	}

	stored, err := s.repo.GetByUserAndPlatform(ctx, userID, req.Platform)
	if err != nil {
		return nil, err
	}
	return credentialToResponse(stored), nil
}

func (s *service) List(ctx context.Context, userID string) (*types.ListCredentialsResponse, error) {
	creds, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]types.PlatformCredentialResponse, 0, len(creds))
	for i := range creds {
		out = append(out, *credentialToResponse(&creds[i]))
	}
	return &types.ListCredentialsResponse{Credentials: out}, nil
}

func (s *service) Get(ctx context.Context, userID, platform string) (*types.PlatformCredentialResponse, error) {
	cred, err := s.repo.GetByUserAndPlatform(ctx, userID, platform)
	if err != nil {
		return nil, err
	}
	return credentialToResponse(cred), nil
}

func (s *service) Update(ctx context.Context, userID, platform string, req types.UpdatePlatformRequest) (*types.PlatformCredentialResponse, error) {
	existing, err := s.repo.GetByUserAndPlatform(ctx, userID, platform)
	if err != nil {
		return nil, err
	}

	encTok, err := s.encryptor.Encrypt(req.Token)
	if err != nil {
		return nil, fmt.Errorf("credentials: encrypt token: %w", err)
	}

	existing.AccessTokenEnc = encTok
	if req.Metadata != nil {
		existing.Metadata = cloneMeta(req.Metadata)
	}

	if err := s.repo.Upsert(ctx, existing); err != nil {
		return nil, err
	}

	stored, err := s.repo.GetByUserAndPlatform(ctx, userID, platform)
	if err != nil {
		return nil, err
	}
	return credentialToResponse(stored), nil
}

func (s *service) Disconnect(ctx context.Context, userID, platform string) error {
	return s.repo.Delete(ctx, userID, platform)
}

func (s *service) GetDecryptedToken(ctx context.Context, userID, platform string) (string, map[string]string, error) {
	cred, err := s.repo.GetByUserAndPlatform(ctx, userID, platform)
	if err != nil {
		return "", nil, err
	}

	token, err := s.encryptor.Decrypt(cred.AccessTokenEnc)
	if err != nil {
		return "", nil, fmt.Errorf("credentials: decrypt access token: %w", err)
	}

	meta := make(map[string]string)
	for k, v := range cred.Metadata {
		meta[k] = v
	}
	return token, meta, nil
}

func credentialToResponse(c *types.PlatformCredential) *types.PlatformCredentialResponse {
	var meta map[string]string
	if len(c.Metadata) > 0 {
		meta = cloneMeta(c.Metadata)
	}
	return &types.PlatformCredentialResponse{
		Platform:       c.Platform,
		Connected:      true,
		PlatformUserID: c.PlatformUserID,
		Metadata:       meta,
		ConnectedAt:    c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
}

func cloneMeta(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
