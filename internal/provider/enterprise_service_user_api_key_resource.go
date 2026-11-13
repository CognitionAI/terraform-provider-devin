package provider

import (
	"context"
	"errors"
	"net/http"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &enterpriseServiceUserAPIKeyResource{}

type enterpriseServiceUserAPIKeyResource struct {
	client *Client
}

type enterpriseServiceUserAPIKeyModel struct {
	ServiceUserID types.String `tfsdk:"service_user_id"`
	APIKeyID      types.String `tfsdk:"api_key_id"`
	Name          types.String `tfsdk:"name"`
	ExpiresAt     types.Int64  `tfsdk:"expires_at"`
	CreatedAt     types.Int64  `tfsdk:"created_at"`
	Token         types.String `tfsdk:"token"`
}

func NewEnterpriseServiceUserAPIKeyResource() resource.Resource {
	return &enterpriseServiceUserAPIKeyResource{}
}

func (r *enterpriseServiceUserAPIKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_service_user_api_key"
}

func (r *enterpriseServiceUserAPIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an API key of an existing service user. The token is returned by the API only when " +
			"the key is created, so it is stored in Terraform state (as a sensitive value) and the resource cannot " +
			"be imported. API keys cannot be changed in place: any change creates a new key and revokes the old " +
			"one. Set `lifecycle { create_before_destroy = true }` to create the new key before the old one is " +
			"revoked. A key that was revoked outside Terraform or has expired is removed from state, so the next " +
			"apply creates a new one. Destroying this resource revokes the key.",
		Attributes: map[string]schema.Attribute{
			"service_user_id": schema.StringAttribute{
				Description: "ID of an existing service user.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"api_key_id": schema.StringAttribute{
				Description: "API key ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the API key.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"expires_at": schema.Int64Attribute{
				Description: "Expiration as a UNIX timestamp in seconds. Must be in the future when the key is " +
					"created. The key does not expire when omitted.",
				Optional: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"created_at": schema.Int64Attribute{
				Description: "Creation time as a UNIX timestamp in seconds.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"token": schema.StringAttribute{
				Description: "The API token. Only available from the run that created the key.",
				Computed:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *enterpriseServiceUserAPIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *enterpriseServiceUserAPIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseServiceUserAPIKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serviceUserID := plan.ServiceUserID.ValueString()
	body := api.CreateAPIKeyRequest{
		Name:      plan.Name.ValueString(),
		ExpiresAt: optionalIntFrom(plan.ExpiresAt),
	}
	var created api.APIKeyWithTokenResponse
	if err := r.client.Post(ctx, enterpriseServiceUserAPIKeysPath(serviceUserID), body, &created); err != nil {
		resp.Diagnostics.AddError("Failed to create service user API key", err.Error())
		return
	}

	plan.APIKeyID = types.StringValue(created.APIKeyID)
	plan.Name = types.StringValue(created.APIKeyName)
	plan.Token = types.StringValue(created.Token)
	plan.CreatedAt = types.Int64Unknown()
	// Save the one-time token before the follow-up read, which can fail.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The create response has no created_at; read it back from the list.
	key, err := r.findAPIKey(ctx, serviceUserID, created.APIKeyID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read service user API key after creating it", err.Error())
		return
	}
	if key == nil {
		resp.Diagnostics.AddError(
			"Failed to read service user API key after creating it",
			"The API key was created but is missing from the service user's API keys.",
		)
		return
	}
	mapAPIKeyResponseToModel(key, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *enterpriseServiceUserAPIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseServiceUserAPIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.findAPIKey(ctx, state.ServiceUserID.ValueString(), state.APIKeyID.ValueString())
	if IsNotFound(err) || (err == nil && (key == nil || !key.IsActive)) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read service user API key", err.Error())
		return
	}

	mapAPIKeyResponseToModel(key, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// findAPIKey looks the key up in the service user's key list (there is no
// GET-by-id endpoint, and the list is not paginated). It returns nil when the
// key is not in the list.
func (r *enterpriseServiceUserAPIKeyResource) findAPIKey(ctx context.Context, serviceUserID, apiKeyID string) (*api.APIKeyResponse, error) {
	var page api.PaginatedResponseAPIKeyResponse
	// Only active keys, so the response does not grow with every past rotation.
	if err := r.client.Get(ctx, enterpriseServiceUserAPIKeysPath(serviceUserID)+"?status="+string(api.Active), &page); err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].APIKeyID == apiKeyID {
			return &page.Items[i], nil
		}
	}
	return nil, nil
}

func (r *enterpriseServiceUserAPIKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Service user API keys cannot be updated in place",
		"All devin_enterprise_service_user_api_key attributes require replacement. This is a bug in the provider's plan modifiers.",
	)
}

func (r *enterpriseServiceUserAPIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterpriseServiceUserAPIKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, enterpriseServiceUserAPIKeyPath(state.ServiceUserID.ValueString(), state.APIKeyID.ValueString()), nil)
	// 409 means the key is already revoked.
	var apiErr *APIError
	if err == nil || IsNotFound(err) || (errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict) {
		return
	}
	resp.Diagnostics.AddError("Failed to revoke service user API key", err.Error())
}

func mapAPIKeyResponseToModel(resp *api.APIKeyResponse, model *enterpriseServiceUserAPIKeyModel) {
	model.APIKeyID = types.StringValue(resp.APIKeyID)
	model.Name = types.StringValue(resp.APIKeyName)
	model.ExpiresAt = int64FromNullable(resp.ExpiresAt)
	model.CreatedAt = types.Int64Value(int64(resp.CreatedAt))
}
