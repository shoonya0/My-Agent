package approval

import (
	"sync"

	"myAgent/api/approvalpb"

	"go.uber.org/zap"
)

// StreamManager maintains active gRPC streaming connections from api-gateway
// instances and broadcasts job update notifications to all connected clients.
type StreamManager struct {
	streams map[string]approvalpb.ApprovalService_SubscribeJobUpdatesServer
	mu      sync.RWMutex
	log     *zap.Logger
}

// NewStreamManager creates a new stream manager instance.
func NewStreamManager(log *zap.Logger) *StreamManager {
	return &StreamManager{
		streams: make(map[string]approvalpb.ApprovalService_SubscribeJobUpdatesServer),
		log:     log,
	}
}

// Register adds a new stream connection from an api-gateway instance.
func (sm *StreamManager) Register(nodeID string, stream approvalpb.ApprovalService_SubscribeJobUpdatesServer) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.streams[nodeID] = stream
	sm.log.Info("Registered new gateway stream",
		zap.String("node_id", nodeID),
		zap.Int("total_streams", len(sm.streams)),
	)
}

// Unregister removes a stream connection when it closes.
func (sm *StreamManager) Unregister(nodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.streams, nodeID)
	sm.log.Info("Unregistered gateway stream",
		zap.String("node_id", nodeID),
		zap.Int("remaining_streams", len(sm.streams)),
	)
}

// Broadcast sends a job update notification to all connected api-gateway instances.
// Failed sends are logged but do not block other sends.
func (sm *StreamManager) Broadcast(update *approvalpb.JobUpdateNotification) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.streams) == 0 {
		sm.log.Debug("No active gateway streams to broadcast to",
			zap.String("job_id", update.JobId),
		)
		return
	}

	successCount := 0
	for nodeID, stream := range sm.streams {
		if err := stream.Send(update); err != nil {
			sm.log.Error("Failed to send update to gateway",
				zap.Error(err),
				zap.String("node_id", nodeID),
				zap.String("job_id", update.JobId),
			)
			// Note: Connection cleanup happens in the stream goroutine
		} else {
			successCount++
		}
	}

	sm.log.Debug("Broadcasted job update",
		zap.String("job_id", update.JobId),
		zap.String("notification_type", update.NotificationType),
		zap.Int("total_streams", len(sm.streams)),
		zap.Int("successful_sends", successCount),
	)
}

// Count returns the number of active streams.
func (sm *StreamManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.streams)
}
