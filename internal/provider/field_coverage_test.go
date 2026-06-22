package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestFieldCoverage guards against v3 API request/response fields silently
// going unexposed by the provider.
//
// For every resource and data-source file it diffs the json fields of the
// generated api.*Request/*Response structs the file consumes against the
// tfsdk fields the file's models actually wire up. Any API field that is
// neither exposed nor listed in ignoredFields below fails the test, forcing a
// deliberate choice — wire the field, or document why it is intentionally
// omitted.
//
// ignoredFields is keyed by resource/data-source file name; the value lists
// API json field names intentionally not surfaced as their own tfsdk
// attribute in that file, each with the reason. Categories:
//   - pagination envelope (end_cursor/has_next_page/items/total) handled by
//     the list machinery, not a per-row attribute;
//   - server-managed read-only metadata (created_at/by, updated_at/by,
//     access_type, …) not modeled as config;
//   - fields exposed under a different tfsdk name (rename);
//   - request fields the endpoint rejects for this scope;
//   - bulk-request wrappers (the provider issues single-item requests).
var ignoredFields = map[string]map[string]string{
	"enterprise_knowledge_note_resource.go": {
		"folder_id":   "rejected by enterprise notes endpoint (400); folders are org-level only",
		"org_id":      "enterprise notes are account-scoped; org_id is read-only/derived",
		"access_type": "server-managed read-only metadata",
		"folder_path": "server-derived read-only metadata",
		"macro":       "server-derived read-only metadata",
		"created_at":  "server-managed read-only metadata",
		"updated_at":  "server-managed read-only metadata",
	},
	"knowledge_note_resource.go": {
		"access_type": "server-managed read-only metadata",
		"folder_path": "server-derived read-only metadata",
		"macro":       "server-derived read-only metadata",
		"created_at":  "server-managed read-only metadata",
		"updated_at":  "server-managed read-only metadata",
	},
	"playbook_resource.go": {
		"access_type": "server-managed read-only metadata",
		"created_at":  "server-managed read-only metadata",
		"created_by":  "server-managed read-only metadata",
		"updated_at":  "server-managed read-only metadata",
		"updated_by":  "server-managed read-only metadata",
	},
	"enterprise_playbook_resource.go": {
		"org_id":      "enterprise playbooks are account-scoped; org_id is read-only/derived",
		"access_type": "server-managed read-only metadata",
		"created_at":  "server-managed read-only metadata",
		"created_by":  "server-managed read-only metadata",
		"updated_at":  "server-managed read-only metadata",
		"updated_by":  "server-managed read-only metadata",
	},
	"organization_resource.go": {
		"created_at": "server-managed read-only metadata",
		"updated_at": "server-managed read-only metadata",
	},
	"secret_resource.go": {
		"secret_type":   "exposed via the 'type' attribute (SecretCreateRequest uses 'type')",
		"access_type":   "server-managed read-only metadata",
		"created_at":    "server-managed read-only metadata",
		"created_by":    "server-managed read-only metadata",
		"updated_at":    "server-managed read-only metadata",
		"updated_by":    "server-managed read-only metadata",
		"end_cursor":    paginationReason,
		"has_next_page": paginationReason,
		"items":         paginationReason,
		"total":         paginationReason,
	},
	"git_permission_resource.go": {
		"permissions":   "bulk-create wrapper; the provider creates one permission per resource",
		"created_at":    "server-managed read-only metadata",
		"end_cursor":    paginationReason,
		"has_next_page": paginationReason,
		"items":         paginationReason,
		"total":         paginationReason,
	},
	"idp_group_resource.go": {
		"idp_group_name":  "exposed via the 'name' attribute",
		"idp_group_names": "bulk-create wrapper; exposed via the 'name' attribute",
		"end_cursor":      paginationReason,
		"has_next_page":   paginationReason,
		"items":           paginationReason,
		"total":           paginationReason,
	},
	"org_tags_resource.go": {
		"tag": "exposed via the 'default_tag' attribute",
	},
	"schedule_resource.go": {
		// create_as_user_id / run_as_user_id are impersonation inputs gated by
		// the ImpersonateOrgSessions permission and have no read-back in
		// ScheduleResponse, so they are intentionally not exposed as config.
		"create_as_user_id":    "impersonation input (ImpersonateOrgSessions); write-only, no read-back",
		"run_as_user_id":       "impersonation input (ImpersonateOrgSessions); write-only, no read-back",
		"playbook":             "embedded playbook summary; exposed via the 'playbook_id' attribute",
		"consecutive_failures": "server-managed runtime status",
		"created_at":           "server-managed read-only metadata",
		"created_by":           "server-managed read-only metadata",
		"last_edited_by":       "server-managed read-only metadata",
		"last_error_at":        "server-managed runtime status",
		"last_error_message":   "server-managed runtime status",
		"last_executed_at":     "server-managed runtime status",
		"scheduled_session_id": "server-managed runtime status",
		"updated_at":           "server-managed read-only metadata",
	},
	// Data sources that only consume a PaginatedResponse envelope; rows are
	// flattened into a nested attribute keyed off the items element type.
	"git_connections_data_source.go": paginationOnly,
	"idp_groups_data_source.go":      paginationOnly,
	"organizations_data_source.go":   paginationOnly,
}

const paginationReason = "pagination envelope handled by the list machinery"

var paginationOnly = map[string]string{
	"end_cursor":    paginationReason,
	"has_next_page": paginationReason,
	"items":         paginationReason,
	"total":         paginationReason,
}

func structFieldsByTag(file *ast.File, tagKey string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fields := map[string]bool{}
			for _, f := range st.Fields.List {
				if f.Tag == nil {
					continue
				}
				tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
				v := tag.Get(tagKey)
				if v == "" {
					continue
				}
				name := strings.Split(v, ",")[0]
				if name == "" || name == "-" {
					continue
				}
				fields[name] = true
			}
			if len(fields) > 0 {
				out[ts.Name.Name] = fields
			}
		}
	}
	return out
}

func referencedAPITypes(file *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == "api" {
			out[sel.Sel.Name] = true
		}
		return true
	})
	return out
}

func TestFieldCoverage(t *testing.T) {
	fset := token.NewFileSet()

	apiFile, err := parser.ParseFile(fset, filepath.Join("..", "api", "models.gen.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse api models: %v", err)
	}
	apiStructs := structFieldsByTag(apiFile, "json")

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	for _, fp := range files {
		base := filepath.Base(fp)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		if !strings.HasSuffix(base, "_resource.go") && !strings.HasSuffix(base, "_data_source.go") {
			continue
		}

		f, err := parser.ParseFile(fset, fp, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", base, err)
		}

		tfsdkStructs := structFieldsByTag(f, "tfsdk")
		tfsdkUnion := map[string]bool{}
		for _, fields := range tfsdkStructs {
			for k := range fields {
				tfsdkUnion[k] = true
			}
		}

		var models []string
		for ref := range referencedAPITypes(f) {
			if _, ok := apiStructs[ref]; !ok {
				continue
			}
			if strings.HasSuffix(ref, "Request") || strings.HasSuffix(ref, "Response") {
				models = append(models, ref)
			}
		}
		sort.Strings(models)
		if len(models) == 0 {
			continue
		}

		ignored := ignoredFields[base]
		for _, model := range models {
			var unexposed []string
			for field := range apiStructs[model] {
				if tfsdkUnion[field] {
					continue
				}
				if _, ok := ignored[field]; ok {
					continue
				}
				unexposed = append(unexposed, field)
			}
			sort.Strings(unexposed)
			for _, field := range unexposed {
				t.Errorf("%s: API field %s.%s is not exposed as a tfsdk attribute and is not in ignoredFields. "+
					"Wire it into the resource, or add it to ignoredFields[%q] with a reason.",
					base, model, field, base)
			}
		}
	}
}
