package provider

import (
	"context"
	"encoding/base64"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &blueprintFileResource{}

type blueprintFileResource struct {
	client *Client
}

type blueprintFileModel struct {
	OrgID         types.String `tfsdk:"org_id"`
	BlueprintID   types.String `tfsdk:"blueprint_id"`
	Name          types.String `tfsdk:"name"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	FileID        types.String `tfsdk:"file_id"`
	EnvVarName    types.String `tfsdk:"env_var_name"`
	SizeBytes     types.Int64  `tfsdk:"size_bytes"`
}

func NewBlueprintFileResource() resource.Resource {
	return &blueprintFileResource{}
}

func (r *blueprintFileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blueprint_file"
}

func (r *blueprintFileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "Uploads a file attached to a snapshot setup blueprint. Blueprint steps read it through the " +
			"environment variable in env_var_name. Files can't be edited in place, so any change replaces the file. " +
			"Devin doesn't return file contents, so only a deleted file is detected as drift.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description:   "Organization of an org-wide or repository blueprint. Omit for the enterprise blueprint.",
				Optional:      true,
				PlanModifiers: replace,
			},
			"blueprint_id": schema.StringAttribute{
				Description:   "Blueprint the file is attached to.",
				Required:      true,
				PlanModifiers: replace,
			},
			"name": schema.StringAttribute{
				Description:   "File name.",
				Required:      true,
				PlanModifiers: replace,
			},
			"content_base64": schema.StringAttribute{
				Description:   "Base64-encoded file contents, e.g. filebase64(\"${path.module}/files/settings.xml\").",
				Required:      true,
				Sensitive:     true,
				PlanModifiers: replace,
			},
			"file_id": schema.StringAttribute{
				Description:   "File ID (assigned by Devin).",
				Computed:      true,
				PlanModifiers: computed,
			},
			"env_var_name": schema.StringAttribute{
				Description:   "Environment variable holding the file's path during blueprint steps.",
				Computed:      true,
				PlanModifiers: computed,
			},
			"size_bytes": schema.Int64Attribute{
				Description: "File size in bytes.",
				Computed:    true,
			},
		},
	}
}

func (r *blueprintFileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *blueprintFileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan blueprintFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	content, err := base64.StdEncoding.DecodeString(plan.ContentBase64.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid content_base64", err.Error())
		return
	}

	query := url.Values{}
	query.Set("name", plan.Name.ValueString())
	path := blueprintFilesPath(plan.OrgID.ValueString(), plan.BlueprintID.ValueString()) + "?" + query.Encode()
	var result api.BlueprintFile
	if err := r.client.PostFile(ctx, path, plan.Name.ValueString(), content, &result); err != nil {
		resp.Diagnostics.AddError("Failed to upload blueprint file", err.Error())
		return
	}
	applyBlueprintFile(&plan, &result)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blueprintFileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blueprintFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var list api.BlueprintFileList
	err := r.client.Get(ctx, blueprintFilesPath(state.OrgID.ValueString(), state.BlueprintID.ValueString()), &list)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read blueprint files", err.Error())
		return
	}
	for i := range list.Data {
		if list.Data[i].FileID == state.FileID.ValueString() {
			applyBlueprintFile(&state, &list.Data[i])
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update is never called: every configurable attribute requires replacement.
func (r *blueprintFileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan blueprintFileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blueprintFileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state blueprintFileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	path := blueprintFilesPath(state.OrgID.ValueString(), state.BlueprintID.ValueString()) + "/" + url.PathEscape(state.FileID.ValueString())
	if err := r.client.Delete(ctx, path, nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete blueprint file", err.Error())
	}
}

func applyBlueprintFile(model *blueprintFileModel, file *api.BlueprintFile) {
	model.FileID = types.StringValue(file.FileID)
	model.EnvVarName = types.StringValue(file.EnvVarName)
	model.SizeBytes = types.Int64Value(int64(file.SizeBytes))
}
