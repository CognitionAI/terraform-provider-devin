package provider

import (
	"context"
	"fmt"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &orgTagsResource{}
var _ resource.ResourceWithImportState = &orgTagsResource{}
var _ resource.ResourceWithValidateConfig = &orgTagsResource{}

type orgTagsResource struct {
	client *Client
}

type orgTagsModel struct {
	OrgID      types.String `tfsdk:"org_id"`
	Tags       types.Set    `tfsdk:"tags"`
	DefaultTag types.String `tfsdk:"default_tag"`
}

func NewOrgTagsResource() resource.Resource {
	return &orgTagsResource{}
}

func (r *orgTagsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_tags"
}

func (r *orgTagsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the allowed session tags (and optional default tag) for an organization. This is a " +
			"singleton: only one should be declared per organization, and applying it replaces the entire tag set. " +
			"Destroying the resource clears all tags. Requires the session tags feature to be enabled for the enterprise.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID whose allowed tags are managed.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.SetAttribute{
				Description: "The full set of allowed session tags for the organization.",
				ElementType: types.StringType,
				Required:    true,
			},
			"default_tag": schema.StringAttribute{
				Description: "Default tag applied to new sessions. Must be one of the allowed tags.",
				Optional:    true,
			},
		},
	}
}

func (r *orgTagsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *orgTagsResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config orgTagsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.DefaultTag.IsNull() || config.DefaultTag.IsUnknown() || config.Tags.IsUnknown() {
		return
	}
	for _, element := range config.Tags.Elements() {
		if element.IsUnknown() {
			return
		}
	}

	tags := setToStrings(ctx, config.Tags, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	defaultTag := config.DefaultTag.ValueString()
	for _, t := range tags {
		if t == defaultTag {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		path.Root("default_tag"),
		"default_tag is not in tags",
		fmt.Sprintf("%q must be one of the values in tags", defaultTag),
	)
}

// apply replaces the org's allowed tags and reconciles the default tag. The
// tags PUT clears a default that is no longer in the new set, so the default
// is always (re)applied afterwards. saveState persists the model right after
// the tags PUT so the resource is tracked even if the default-tag call fails.
func (r *orgTagsResource) apply(ctx context.Context, plan *orgTagsModel, diags *diag.Diagnostics, saveState func()) {
	tags := setToStrings(ctx, plan.Tags, diags)
	if diags.HasError() {
		return
	}

	orgID := plan.OrgID.ValueString()

	var tagsResult api.TagsResponse
	if err := r.client.Put(ctx, orgTagsPath(orgID), api.TagsCreateRequest{Tags: tags}, &tagsResult); err != nil {
		diags.AddError("Failed to set organization tags", err.Error())
		return
	}

	diags.Append(mapTagsToModel(ctx, tagsResult.Tags, plan)...)
	// The default tag has not been applied yet, so the intermediate state
	// records it as null; a failure below then surfaces as a diff on the
	// next plan instead of silently keeping the unapplied value.
	plannedDefault := plan.DefaultTag
	plan.DefaultTag = types.StringNull()
	saveState()
	plan.DefaultTag = plannedDefault
	if diags.HasError() {
		return
	}

	if plan.DefaultTag.IsNull() || plan.DefaultTag.IsUnknown() {
		if err := r.client.Delete(ctx, orgDefaultTagPath(orgID), nil); err != nil && !IsNotFound(err) {
			diags.AddError("Failed to clear organization default tag", err.Error())
			return
		}
	} else {
		body := api.DefaultTagRequest{Tag: plan.DefaultTag.ValueString()}
		if err := r.client.Put(ctx, orgDefaultTagPath(orgID), body, nil); err != nil {
			diags.AddError("Failed to set organization default tag", err.Error())
			return
		}
	}
}

func (r *orgTagsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgTagsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &plan, &resp.Diagnostics, func() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	})
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgTagsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgTagsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID := state.OrgID.ValueString()

	var tagsResult api.TagsResponse
	err := r.client.Get(ctx, orgTagsPath(orgID), &tagsResult)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read organization tags", err.Error())
		return
	}

	var defaultResult api.DefaultTagResponse
	if err := r.client.Get(ctx, orgDefaultTagPath(orgID), &defaultResult); err != nil {
		resp.Diagnostics.AddError("Failed to read organization default tag", err.Error())
		return
	}

	resp.Diagnostics.Append(mapTagsToModel(ctx, tagsResult.Tags, &state)...)
	state.DefaultTag = stringFromNullable(defaultResult.DefaultTag)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgTagsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgTagsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, &plan, &resp.Diagnostics, func() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	})
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgTagsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgTagsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Clearing the tag list also clears the default tag server-side.
	if err := r.client.Delete(ctx, orgTagsPath(state.OrgID.ValueString()), nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to clear organization tags", err.Error())
	}
}

func (r *orgTagsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("org_id"), req, resp)
}

func mapTagsToModel(ctx context.Context, tags []string, model *orgTagsModel) diag.Diagnostics {
	set, diags := types.SetValueFrom(ctx, types.StringType, tags)
	model.Tags = set
	return diags
}
