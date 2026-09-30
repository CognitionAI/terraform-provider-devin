package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const duplicateKnowledgeFolderPathSummary = "Duplicate knowledge folder path"

var _ datasource.DataSource = &knowledgeFoldersDataSource{}

type knowledgeFoldersDataSource struct {
	client *Client
}

type knowledgeFolderModel struct {
	FolderID       types.String `tfsdk:"folder_id"`
	Name           types.String `tfsdk:"name"`
	ParentFolderID types.String `tfsdk:"parent_folder_id"`
	Path           types.String `tfsdk:"path"`
	NoteCount      types.Int64  `tfsdk:"note_count"`
}

type knowledgeFoldersDataSourceModel struct {
	OrgID           types.String           `tfsdk:"org_id"`
	FolderIDsByPath types.Map              `tfsdk:"folder_ids_by_path"`
	Folders         []knowledgeFolderModel `tfsdk:"folders"`
	RootNoteCount   types.Int64            `tfsdk:"root_note_count"`
}

func NewKnowledgeFoldersDataSource() datasource.DataSource {
	return &knowledgeFoldersDataSource{}
}

func (d *knowledgeFoldersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_knowledge_folders"
}

func (d *knowledgeFoldersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the knowledge folders in an organization.",
		Attributes: map[string]schema.Attribute{
			"org_id": schema.StringAttribute{
				Description: "Organization ID that owns these knowledge folders.",
				Required:    true,
			},
			"folder_ids_by_path": schema.MapAttribute{
				Description: "Knowledge folder IDs keyed by their path.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"folders": schema.ListNestedAttribute{
				Description: "Knowledge folders in the organization.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"folder_id": schema.StringAttribute{
							Description: "Knowledge folder ID.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Knowledge folder name.",
							Computed:    true,
						},
						"parent_folder_id": schema.StringAttribute{
							Description: "Parent knowledge folder ID, or null for a root folder.",
							Computed:    true,
						},
						"path": schema.StringAttribute{
							Description: "Knowledge folder path.",
							Computed:    true,
						},
						"note_count": schema.Int64Attribute{
							Description: "Number of knowledge notes in the folder.",
							Computed:    true,
						},
					},
				},
			},
			"root_note_count": schema.Int64Attribute{
				Description: "Number of knowledge notes in the organization root.",
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
	var config knowledgeFoldersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.FolderTreeResponse
	if err := d.client.Get(ctx, orgKnowledgeFoldersPath(config.OrgID.ValueString()), &result); err != nil {
		resp.Diagnostics.AddError("Failed to read knowledge folders", err.Error())
		return
	}

	state, diags := knowledgeFoldersStateFromResponse(ctx, config.OrgID, &result)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func sortedKnowledgeFolders(folders []api.FolderSummary) []api.FolderSummary {
	sorted := append([]api.FolderSummary(nil), folders...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].FolderID < sorted[j].FolderID
	})
	return sorted
}

func knowledgeFoldersStateFromResponse(ctx context.Context, orgID types.String, response *api.FolderTreeResponse) (knowledgeFoldersDataSourceModel, diag.Diagnostics) {
	folders := sortedKnowledgeFolders(response.Folders)
	folderIDsByPath := make(map[string]string, len(folders))
	stateFolders := make([]knowledgeFolderModel, 0, len(folders))
	for _, folder := range folders {
		if existingID, ok := folderIDsByPath[folder.Path]; ok {
			var diags diag.Diagnostics
			diags.AddError(
				duplicateKnowledgeFolderPathSummary,
				fmt.Sprintf(
					"path %q is used by folder IDs %q and %q",
					folder.Path,
					existingID,
					folder.FolderID,
				),
			)
			return knowledgeFoldersDataSourceModel{}, diags
		}

		folderIDsByPath[folder.Path] = folder.FolderID
		stateFolders = append(stateFolders, knowledgeFolderModel{
			FolderID:       types.StringValue(folder.FolderID),
			Name:           types.StringValue(folder.Name),
			ParentFolderID: stringFromNullable(folder.ParentFolderID),
			Path:           types.StringValue(folder.Path),
			NoteCount:      types.Int64Value(int64(folder.NoteCount)),
		})
	}

	folderIDsByPathValue, diags := types.MapValueFrom(ctx, types.StringType, folderIDsByPath)
	if diags.HasError() {
		return knowledgeFoldersDataSourceModel{}, diags
	}

	return knowledgeFoldersDataSourceModel{
		OrgID:           orgID,
		FolderIDsByPath: folderIDsByPathValue,
		Folders:         stateFolders,
		RootNoteCount:   types.Int64Value(int64(response.RootNoteCount)),
	}, nil
}
