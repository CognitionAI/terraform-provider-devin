package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &idpGroupResource{}
var _ resource.ResourceWithImportState = &idpGroupResource{}
var _ resource.ResourceWithIdentity = &idpGroupResource{}

type idpGroupResource struct {
	client *Client
}

type idpGroupModel struct {
	Name types.String `tfsdk:"name"`
}

func NewIdpGroupResource() resource.Resource {
	return &idpGroupResource{}
}

func (r *idpGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_idp_group"
}

func (r *idpGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Registers an IdP group name with the enterprise so roles can be mapped to it via " +
			"devin_enterprise_idp_group_role or devin_org_idp_group_role. The group has no role assignment on its own.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Description: "IdP group name as provided by the identity provider.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *idpGroupResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idpGroupIdentitySchema()
}

func (r *idpGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *idpGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan idpGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.IdpGroupsBulkCreateRequest{IdpGroupNames: []string{plan.Name.ValueString()}}
	var result []api.IdpGroupResponse
	if err := r.client.Post(ctx, enterpriseIdpGroupsPath, body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to register IdP group", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, idpGroupIdentityModel(plan), &resp.Diagnostics)
}

func (r *idpGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state idpGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.groupExists(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read IdP groups", err.Error())
		return
	}
	if !found {
		setIdentity(ctx, resp.Identity, idpGroupIdentityModel(state), &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, idpGroupIdentityModel(state), &resp.Diagnostics)
}

func (r *idpGroupResource) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
	// All attributes are RequiresReplace, so Update is never called.
}

func (r *idpGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state idpGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, enterpriseIdpGroupPath(state.Name.ValueString()), nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete IdP group", err.Error())
	}
}

func (r *idpGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("name"), path.Root("name"), req, resp)
}

func (r *idpGroupResource) groupExists(ctx context.Context, name string) (bool, error) {
	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseIdpGroupResponse
		if err := r.client.Get(ctx, enterpriseIdpGroupsPath+"?"+query.Encode(), &page); err != nil {
			return false, err
		}
		for _, item := range page.Items {
			if item.IdpGroupName == name {
				return true, nil
			}
		}
		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			return false, nil
		}
		cursor = page.EndCursor.MustGet()
	}
}
