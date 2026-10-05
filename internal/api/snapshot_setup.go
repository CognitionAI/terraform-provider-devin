// Hand-written models for the v3beta1 snapshot-setup API. These endpoints are
// not yet included in the filtered openapi.json bundle.
package api

// BlueprintCreateRequest is the body for POST .../snapshot-setup/blueprints.
type BlueprintCreateRequest struct {
	Contents *string `json:"contents,omitempty"`
	RepoName *string `json:"repo_name,omitempty"`
}

// BlueprintUpdateRequest is the body for PATCH .../snapshot-setup/blueprints/{id}.
type BlueprintUpdateRequest struct {
	Contents *string `json:"contents,omitempty"`
	Position *int    `json:"position,omitempty"`
}

// BlueprintResponse is metadata returned for a blueprint (contents omitted).
type BlueprintResponse struct {
	BlueprintID string  `json:"blueprint_id"`
	Type        string  `json:"type"`
	RepoName    *string `json:"repo_name"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

// BlueprintListResponse wraps list blueprints.
type BlueprintListResponse struct {
	Data []BlueprintResponse `json:"data"`
}

// BlueprintContentsResponse is returned by GET .../blueprints/{id}/contents.
type BlueprintContentsResponse struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

// SnapshotBuildTriggerRequest is the body for POST .../snapshot-setup/builds.
type SnapshotBuildTriggerRequest struct{}

// SnapshotBuildResponse is a snapshot build record.
type SnapshotBuildResponse struct {
	BuildID            string  `json:"build_id"`
	Status             string  `json:"status"`
	Trigger            string  `json:"trigger"`
	Pinned             bool    `json:"pinned"`
	StartedAt          *int64  `json:"started_at"`
	CompletedAt        *int64  `json:"completed_at"`
	CreatedAt          int64   `json:"created_at"`
	UpdatedAt          int64   `json:"updated_at"`
	TriggeredByUserID  *string `json:"triggered_by_user_id"`
}
