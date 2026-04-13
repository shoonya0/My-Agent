package types

import "context"

// Job Status Lifecycle constants
const (
	JobStatusPending          = "pending"
	JobStatusRefining         = "refining"
	JobStatusAwaitingApproval = "awaiting_approval"
	JobStatusRejected         = "rejected"
	JobStatusDistributing     = "distributing"
	JobStatusCompleted        = "completed"
	JobStatusFailed           = "failed"
)

// PlatformConnector defines the interface every social-platform connector
// must implement.
type PlatformConnector interface {
	Publish(ctx context.Context, req PostRequest) (*PublishResult, error)
	Validate(ctx context.Context) error
	Name() string
}
