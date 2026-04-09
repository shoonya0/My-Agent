// Package events provides utilities for Kafka event publishing.
//
// This package consolidates duplicate event publishing logic that was
// previously scattered across multiple workers. It provides:
// - Standardized failure event publishing
// - OpenTelemetry trace context propagation
// - Event builder functions
//
// Usage Example:
//
//	import "myAgent/pkg/events"
//
//	func handleError(ctx context.Context, jobID, userID string, err error) {
//	    if publishErr := events.PublishJobFailed(
//	        ctx, producer, jobID, userID, "my-service", err.Error(), log,
//	    ); publishErr != nil {
//	        log.Error("Failed to publish error event", zap.Error(publishErr))
//	    }
//	}
//
// Benefits:
//
// - Eliminates ~60 lines of duplicate code across 3 workers
// - Ensures consistent trace propagation across events
// - Centralizes event schema management
// - Simplifies worker error handling
package events
