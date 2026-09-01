package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &knowledgeFoldersDataSource{}

type knowledgeFoldersDataSource struct {
	client *Client
}

type knowledgeFolderSummaryModel struct {
	FolderID       types.String `tfsdk:"folder_id"`
	Name           types.String `tfsdk:"name"`
	Path           types.String `tfsdk:"path"`
	ParentFolderID types.String `tfsdk:"parent_folder_id"`
	NoteCount      types.Int64  `tfsdk:"note_count"`
}

type knowledgeFoldersDataSourceModel struct {
	OrgID         types.String                  `tfsdk:"org_id"`
	Folders       []knowledgeFolderSummaryModel `tfsdk:"folders"`
	RootNoteCount types.Int64                   `tfsdk:"root_note_count"`
}

func NewKnowledgeFoldersDataSource() datasource.DataSource {
	return &knowledgeFoldersDataSource{}
}

func (d *knowledgeFoldersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_knowledge_folders"
}

func (d *knowledgeFoldersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists an organization's knowledge folders, e.g. to look up a folder_id by name or path " +
			"and place a devin_knowledge_note in it. Devin has no API for creating, renaming, or deleting " +
			"knowledge folders, so folders are created in the Devin UI and referenced from Terraform.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID whose knowledge folders are listed.",
				Required:    true,
			},
			"folders": schema.ListNestedAttribute{
				Description: "Knowledge folders in the organization, as a flat list of the full folder tree.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: knowledgeFolderAttributes(),
				},
			},
			"root_note_count": schema.Int64Attribute{
				Description: "Number of knowledge notes at the root, i.e. not in any folder.",
				Computed:    true,
			},
		},
	}
}

func (d *knowledgeFoldersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *knowledgeFoldersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state knowledgeFoldersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The folder tree endpoint returns the whole tree in one response; there
	// is no pagination envelope to walk.
	var result api.FolderTreeResponse
	if err := d.client.Get(ctx, orgKnowledgeFoldersPath(state.OrgID.ValueString()), &result); err != nil {
		resp.Diagnostics.AddError("Failed to list knowledge folders", err.Error())
		return
	}

	state.Folders = flattenKnowledgeFolders(result.Folders)
	state.RootNoteCount = types.Int64Value(int64(result.RootNoteCount))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// knowledgeFolderAttributes is the per-folder attribute set shared by the org
// and enterprise folder data sources.
func knowledgeFolderAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"folder_id": schema.StringAttribute{
			Description: "Folder ID, as accepted by devin_knowledge_note.folder_id.",
			Computed:    true,
		},
		"name": schema.StringAttribute{
			Description: "Folder name.",
			Computed:    true,
		},
		"path": schema.StringAttribute{
			Description: "Full path of the folder within the tree, e.g. 'Backend/Conventions'.",
			Computed:    true,
		},
		"parent_folder_id": schema.StringAttribute{
			Description: "ID of the parent folder, or null for a top-level folder.",
			Computed:    true,
		},
		"note_count": schema.Int64Attribute{
			Description: "Number of knowledge notes directly in this folder.",
			Computed:    true,
		},
	}
}

func flattenKnowledgeFolders(folders []api.FolderSummary) []knowledgeFolderSummaryModel {
	out := make([]knowledgeFolderSummaryModel, 0, len(folders))
	for _, item := range folders {
		out = append(out, knowledgeFolderSummaryModel{
			FolderID:       types.StringValue(item.FolderID),
			Name:           types.StringValue(item.Name),
			Path:           types.StringValue(item.Path),
			ParentFolderID: stringFromNullable(item.ParentFolderID),
			NoteCount:      types.Int64Value(int64(item.NoteCount)),
		})
	}
	return out
}
