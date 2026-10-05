package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func fetchBlueprintContents(ctx context.Context, client *Client, orgID, blueprintID string) (string, error) {
	var presigned api.BlueprintContentsResponse
	if err := client.Get(ctx, orgBlueprintContentsPath(orgID, blueprintID), &presigned); err != nil {
		return "", err
	}
	if presigned.URL == "" {
		return "", fmt.Errorf("empty presigned URL for blueprint contents")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, presigned.URL, nil)
	if err != nil {
		return "", fmt.Errorf("creating contents request: %w", err)
	}
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching blueprint contents: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading blueprint contents: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("blueprint contents download returned %d", resp.StatusCode)
	}
	return string(body), nil
}

func mapBlueprintResponseToModel(resp *api.BlueprintResponse, contents string, model *blueprintModel, diags *diag.Diagnostics) {
	model.BlueprintID = types.StringValue(resp.BlueprintID)
	model.Type = types.StringValue(resp.Type)
	model.RepoName = stringFromOptionalPtr(resp.RepoName)
	model.CreatedAt = types.Int64Value(resp.CreatedAt)
	model.UpdatedAt = types.Int64Value(resp.UpdatedAt)
	if contents != "" {
		model.Contents = types.StringValue(contents)
	}
}

func stringFromOptionalPtr(value *string) types.String {
	if value == nil || *value == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func stringPtrFrom(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := value.ValueString()
	return &v
}
