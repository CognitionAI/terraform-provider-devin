package provider

import (
	"context"
	"testing"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

func TestKnowledgeFoldersStateFromResponse(t *testing.T) {
	ctx := context.Background()
	parentID := nullable.NewNullableWithValue("folder-parent")
	response := api.FolderTreeResponse{
		Folders: []api.FolderSummary{
			{
				FolderID:       "folder-backend",
				Name:           "Backend",
				NoteCount:      3,
				ParentFolderID: parentID,
				Path:           "Engineering/Backend",
			},
			{
				FolderID:  "folder-engineering",
				Name:      "Engineering",
				NoteCount: 5,
				Path:      "Engineering",
			},
		},
		RootNoteCount: 7,
	}

	state, diags := knowledgeFoldersStateFromResponse(ctx, types.StringValue("org-123"), &response)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if got := state.OrgID.ValueString(); got != "org-123" {
		t.Fatalf("expected org-123, got %q", got)
	}
	if state.FolderIDsByPath.IsNull() {
		t.Fatal("expected folder_ids_by_path to be non-null")
	}
	elements := state.FolderIDsByPath.Elements()
	expectedIDs := map[string]string{
		"Engineering":         "folder-engineering",
		"Engineering/Backend": "folder-backend",
	}
	if len(elements) != len(expectedIDs) {
		t.Fatalf("expected %d map entries, got %d", len(expectedIDs), len(elements))
	}
	for path, expectedID := range expectedIDs {
		value, ok := elements[path].(types.String)
		if !ok {
			t.Fatalf("expected string map value for %q, got %T", path, elements[path])
		}
		if got := value.ValueString(); got != expectedID {
			t.Errorf("folder_ids_by_path[%q] = %q, want %q", path, got, expectedID)
		}
	}

	if got := len(state.Folders); got != 2 {
		t.Fatalf("expected 2 folders, got %d", got)
	}
	if got := state.Folders[0].Path.ValueString(); got != "Engineering" {
		t.Errorf("first folder path = %q, want Engineering", got)
	}
	if got := state.Folders[1].Path.ValueString(); got != "Engineering/Backend" {
		t.Errorf("second folder path = %q, want Engineering/Backend", got)
	}
	if got := state.Folders[1].FolderID.ValueString(); got != "folder-backend" {
		t.Errorf("second folder ID = %q, want folder-backend", got)
	}
	if got := state.Folders[0].ParentFolderID; !got.IsNull() {
		t.Errorf("root folder parent ID = %v, want null", got)
	}
	if got := state.Folders[1].ParentFolderID.ValueString(); got != "folder-parent" {
		t.Errorf("nested folder parent ID = %q, want folder-parent", got)
	}
	if got := state.RootNoteCount.ValueInt64(); got != 7 {
		t.Errorf("root note count = %d, want 7", got)
	}
}

func TestSortedKnowledgeFoldersSortsByPathThenFolderID(t *testing.T) {
	folders := sortedKnowledgeFolders([]api.FolderSummary{
		{FolderID: "z", Path: "same"},
		{FolderID: "after", Path: "z-after"},
		{FolderID: "a", Path: "same"},
		{FolderID: "before", Path: "a-before"},
	})

	expected := []struct {
		path     string
		folderID string
	}{
		{path: "a-before", folderID: "before"},
		{path: "same", folderID: "a"},
		{path: "same", folderID: "z"},
		{path: "z-after", folderID: "after"},
	}
	for i, want := range expected {
		if got := folders[i]; got.Path != want.path || got.FolderID != want.folderID {
			t.Errorf("sorted folder %d = (%q, %q), want (%q, %q)", i, got.Path, got.FolderID, want.path, want.folderID)
		}
	}
}

func TestKnowledgeFoldersStateFromResponseRejectsDuplicatePath(t *testing.T) {
	response := api.FolderTreeResponse{
		Folders: []api.FolderSummary{
			{FolderID: "z", Name: "Z", Path: "same"},
			{FolderID: "a", Name: "A", Path: "same"},
			{FolderID: "b", Name: "B", Path: "before"},
		},
	}

	state, diagnostics := knowledgeFoldersStateFromResponse(context.Background(), types.StringValue("org"), &response)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(diagnostics))
	}
	if got, want := diagnostics[0].Summary(), "Duplicate knowledge folder path"; got != want {
		t.Fatalf("diagnostic summary = %q, want %q", got, want)
	}
	if got, want := diagnostics[0].Detail(), `path "same" is used by folder IDs "a" and "z"`; got != want {
		t.Fatalf("diagnostic detail = %q, want %q", got, want)
	}
	if !state.FolderIDsByPath.IsNull() {
		t.Fatal("expected duplicate path to prevent state mapping")
	}
}

func TestOrgKnowledgeFoldersPathEscapesOrgID(t *testing.T) {
	if got, want := orgKnowledgeFoldersPath("org/team"), "/v3/organizations/org%2Fteam/knowledge/folders"; got != want {
		t.Fatalf("orgKnowledgeFoldersPath = %q, want %q", got, want)
	}
}

func TestKnowledgeFoldersStateFromEmptyResponsePreservesCollections(t *testing.T) {
	state, diags := knowledgeFoldersStateFromResponse(
		context.Background(),
		types.StringValue("org"),
		&api.FolderTreeResponse{},
	)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if state.FolderIDsByPath.IsNull() {
		t.Fatal("expected empty folder_ids_by_path to be non-null")
	}
	if state.Folders == nil {
		t.Fatal("expected empty folders to be non-nil")
	}
}
