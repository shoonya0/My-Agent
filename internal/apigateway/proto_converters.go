package apigateway

import (
	"myAgent/api/authpb"
	"myAgent/pkg/types"
)

// protoToTokenResponse converts protobuf TokenResponse to domain TokenResponse.
func protoToTokenResponse(pb *authpb.TokenResponse) *types.TokenResponse {
	return &types.TokenResponse{
		AccessToken:  pb.GetAccessToken(),
		RefreshToken: pb.GetRefreshToken(),
		ExpiresIn:    int(pb.GetExpiresIn()),
		TokenType:    pb.GetTokenType(),
	}
}
