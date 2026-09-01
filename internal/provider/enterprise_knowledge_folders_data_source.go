package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &enterpriseKnowledgeFoldersDataSource{}

type enterpriseKnowledgeFoldersDataSource struct {
	client *Client
}

type enterpriseKnowledgeFoldersDataSourceModel struct {
	Folders       []knowledgeFolderSummaryModel `tfsdk:"folders"`
	RootNoteCount types.Int64                   `tfsdk:"root_note_count"`
}

func NewEnterpriseKnowledgeFoldersDataSource() datasource.DataSource {
	return &enterpriseKnowledgeFoldersDataSource{}
}

func (d *enterpriseKnowledgeFoldersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_knowledge_folders"
}

func (d *enterpriseKnowledgeFoldersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the enterprise's knowledge folders. Devin has no API for creating, renaming, or " +
			"deleting knowledge folders, so folders are created in the Devin UI and referenced from Terraform. " +
			"Note that devin_enterprise_knowledge_note does not accept a folder_id — the enterprise notes " +
			"endpoint rejects it — so this data source is for inspecting the enterprise tree; use " +
			"devin_knowledge_folders to place org-level notes.",
		Attributes: map[string]schema.Attribute{
			"folders": schema.ListNestedAttribute{
				Description: "Knowledge folders in the enterprise, as a flat list of the full folder tree.",
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

func (d *enterpriseKnowledgeFoldersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *enterpriseKnowledgeFoldersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state enterpriseKnowledgeFoldersDataSourceModel

	// The folder tree endpoint returns the whole tree in one response; there
	// is no pagination envelope to walk.
	var result api.FolderTreeResponse
	if err := d.client.Get(ctx, enterpriseKnowledgeFoldersPath, &result); err != nil {
		resp.Diagnostics.AddError("Failed to list enterprise knowledge folders", err.Error())
		return
	}

	state.Folders = flattenKnowledgeFolders(result.Folders)
	state.RootNoteCount = types.Int64Value(int64(result.RootNoteCount))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
