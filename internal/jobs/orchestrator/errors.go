package orchestrator

import "errors"

var (
	// ErrJobAccessDenied is returned when the caller's user_id does not own the job.
	ErrJobAccessDenied = errors.New("orchestrator: job access denied")
	// ErrPreviewNotReady is returned when approve is requested but no preview/image URL is available yet.
	ErrPreviewNotReady = errors.New("orchestrator: image preview not ready")
	// ErrInvalidJobState is returned when the job status does not allow the requested operation.
	ErrInvalidJobState = errors.New("orchestrator: invalid job state for this operation")
	// ErrNoPlatforms is returned when neither submission nor approval specifies target platforms.
	ErrNoPlatforms = errors.New("orchestrator: no platforms specified")
)
