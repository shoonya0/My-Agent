package approval

import (
	"myAgent/api/approvalpb"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// GRPCHandler implements the ApprovalService gRPC server interface.
type GRPCHandler struct {
	approvalpb.UnimplementedApprovalServiceServer
	streamManager *StreamManager
	log           *zap.Logger
}

// NewGRPCHandler constructs a new gRPC handler with the required dependencies.
func NewGRPCHandler(streamManager *StreamManager, log *zap.Logger) *GRPCHandler {
	return &GRPCHandler{
		streamManager: streamManager,
		log:           log,
	}
}

// SubscribeJobUpdates implements the streaming RPC that allows api-gateway
// instances to receive real-time job update notifications.
func (h *GRPCHandler) SubscribeJobUpdates(req *approvalpb.SubscribeRequest, stream approvalpb.ApprovalService_SubscribeJobUpdatesServer) error {
	nodeID := req.NodeId
	if nodeID == "" {
		nodeID = "unknown"
	}

	h.log.Info("Gateway subscribed to job updates stream",
		zap.String("node_id", nodeID),
	)

	h.streamManager.Register(nodeID, stream)
	defer h.streamManager.Unregister(nodeID)

	// Block until the stream context is cancelled (client disconnected or server shutdown)
	<-stream.Context().Done()

	h.log.Info("Gateway unsubscribed from job updates stream",
		zap.String("node_id", nodeID),
		zap.Error(stream.Context().Err()),
	)

	return stream.Context().Err()
}

// GRPCRegistrar returns a function that registers this handler with a gRPC server.
// This follows the pattern used by auth-service and orchestrator.
func (h *GRPCHandler) GRPCRegistrar() func(*grpc.Server) {
	return func(srv *grpc.Server) {
		approvalpb.RegisterApprovalServiceServer(srv, h)
	}
}
