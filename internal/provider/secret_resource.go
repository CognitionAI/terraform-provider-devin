package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &secretResource{}
var _ resource.ResourceWithIdentity = &secretResource{}

type secretResource struct {
	client *Client
}

type secretModel struct {
	SecretID    types.String `tfsdk:"secret_id"`
	OrgID       types.String `tfsdk:"org_id"`
	Key         types.String `tfsdk:"key"`
	Value       types.String `tfsdk:"value"`
	Type        types.String `tfsdk:"type"`
	Note        types.String `tfsdk:"note"`
	IsSensitive types.Bool   `tfsdk:"is_sensitive"`
}

func NewSecretResource() resource.Resource {
	return &secretResource{}
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an org-level secret made available to Devin sessions. " +
			"The Devin API has no secret update endpoint, so any change forces a replacement. " +
			"The secret value is never returned by the API and cannot be imported.",
		Attributes: map[string]schema.Attribute{
			"secret_id": schema.StringAttribute{
				Description: "Secret ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns this secret.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key": schema.StringAttribute{
				Description: "Secret name, exposed to sessions as an environment variable. Must follow environment variable naming conventions.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Description: "Secret value. Stored encrypted by Devin and never returned by the API.",
				Required:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Description: "Secret type: key-value, cookie, or totp. Defaults to key-value.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("key-value"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("key-value", "cookie", "totp"),
				},
			},
			"note": schema.StringAttribute{
				Description: "Human-readable note describing the secret.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"is_sensitive": schema.BoolAttribute{
				Description: "Whether the value is hidden in the Devin UI. Defaults to true.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *secretResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = secretIdentitySchema()
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan secretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.SecretCreateRequest{
		Type:        api.SecretCreateRequestType(plan.Type.ValueString()),
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		IsSensitive: boolPtrFrom(plan.IsSensitive),
		Note:        optionalStringFrom(plan.Note),
	}

	var result api.SecretResponse
	err := r.client.Post(ctx, orgSecretsPath(plan.OrgID.ValueString()), body, &result)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create secret", err.Error())
		return
	}

	mapSecretResponseToModel(&result, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, secretIdentityModel{OrgID: plan.OrgID, SecretID: plan.SecretID}, &resp.Diagnostics)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is no GET-by-id endpoint for secrets; page through the org list.
	found, result, err := r.findSecret(ctx, state.OrgID.ValueString(), state.SecretID.ValueString())
	if IsNotFound(err) || (err == nil && !found) {
		setIdentity(ctx, resp.Identity, secretIdentityModel{OrgID: state.OrgID, SecretID: state.SecretID}, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read secret", err.Error())
		return
	}

	mapSecretResponseToModel(result, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, secretIdentityModel{OrgID: state.OrgID, SecretID: state.SecretID}, &resp.Diagnostics)
}

func (r *secretResource) findSecret(ctx context.Context, orgID, secretID string) (bool, *api.SecretResponse, error) {
	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}
		apiPath := orgSecretsPath(orgID) + "?" + query.Encode()

		var page api.PaginatedResponseSecretResponse
		if err := r.client.Get(ctx, apiPath, &page); err != nil {
			return false, nil, err
		}
		for i := range page.Items {
			if page.Items[i].SecretID == secretID {
				return true, &page.Items[i], nil
			}
		}
		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			return false, nil, nil
		}
		cursor = page.EndCursor.MustGet()
	}
}

func (r *secretResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All attributes require replacement; the API has no secret update endpoint.
	resp.Diagnostics.AddError(
		"Secrets cannot be updated in place",
		"All devin_secret attributes require replacement. This is a bug in the provider's plan modifiers.",
	)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The delete endpoint returns 403 (not 404) for a secret that no longer
	// exists, which is indistinguishable from a real authorization error. Check
	// existence via the list endpoint first so an out-of-band delete is a no-op
	// rather than a spurious failure.
	found, _, err := r.findSecret(ctx, state.OrgID.ValueString(), state.SecretID.ValueString())
	if IsNotFound(err) || (err == nil && !found) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete secret", err.Error())
		return
	}

	err = r.client.Delete(ctx, orgSecretPath(state.OrgID.ValueString(), state.SecretID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete secret", err.Error())
	}
}

func mapSecretResponseToModel(resp *api.SecretResponse, model *secretModel) {
	model.SecretID = types.StringValue(resp.SecretID)
	model.IsSensitive = types.BoolValue(resp.IsSensitive)
	model.Type = types.StringValue(string(resp.SecretType))
	if key := stringFromNullable(resp.Key); !key.IsNull() {
		model.Key = key
	}
	model.Note = stringFromNullable(resp.Note)
	// The API never returns the secret value; the value already in the model
	// (from plan or prior state) is preserved.
}
