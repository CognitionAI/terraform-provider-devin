package provider

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// devin_git_permission CRUD requires a git connection; the CRUD tests skip
// unless the test environment provides one via DEVIN_GIT_CONNECTION_ID. The
// validation tests below always run since they never reach the API.

func gitPermissionConfig(attrs string) string {
	return fmt.Sprintf(`%s
resource "devin_git_permission" "test" {
  org_id            = "org-validation-only"
  git_connection_id = "git-connection-validation-only"
%s}
`, providerConfig, attrs)
}

func TestAccGitPermissionResource_requiresExactlyOnePath(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// No path attribute at all.
				Config:      gitPermissionConfig(""),
				ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured`),
			},
			{
				// Two mutually exclusive path attributes.
				Config:      gitPermissionConfig("  repo_path    = \"myorg/myrepo\"\n  group_prefix = \"myorg\"\n"),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
			},
		},
	})
}

func TestAccGitPermissionResource_rejectsTrailingSlashGroupPrefix(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      gitPermissionConfig("  group_prefix = \"myorg/\"\n"),
				ExpectError: regexp.MustCompile(`must not end with a slash`),
			},
		},
	})
}

// testAccGitConnectionPreCheck gates tests that need a real git connection
// behind DEVIN_GIT_CONNECTION_ID.
func testAccGitConnectionPreCheck(t *testing.T) {
	t.Helper()
	testAccPreCheck(t)
	if os.Getenv("DEVIN_GIT_CONNECTION_ID") == "" {
		t.Skip("DEVIN_GIT_CONNECTION_ID must be set for git permission CRUD tests")
	}
}

func gitPermissionCRUDConfig(orgName, attrs string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_git_permission" "test" {
  org_id            = devin_organization.test.org_id
  git_connection_id = %q
%s}
`, providerConfig, orgName, os.Getenv("DEVIN_GIT_CONNECTION_ID"), attrs)
}

// checkAPIGitPermission lists the org's git permissions out-of-band and
// verifies the row for the resource's git_permission_id has the expected
// field values (nil means the field must be null/absent).
func checkAPIGitPermission(t *testing.T, resourceName string, want map[string]any) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		orgID, err := stateAttr(s, resourceName, "org_id")
		if err != nil {
			return err
		}
		permID, err := stateAttr(s, resourceName, "git_permission_id")
		if err != nil {
			return err
		}

		status, body := testAccAPIRequest(t, http.MethodGet,
			fmt.Sprintf("/v3/enterprise/organizations/%s/git-providers/permissions?first=100", orgID), nil)
		if status != http.StatusOK {
			return fmt.Errorf("listing git permissions returned %d", status)
		}
		items, _ := body["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["git_permission_id"] != permID {
				continue
			}
			for field, expected := range want {
				if got := item[field]; got != expected {
					return fmt.Errorf("API permission field %s = %v, want %v", field, got, expected)
				}
			}
			return nil
		}
		return fmt.Errorf("git permission %s not found in API list for org %s", permID, orgID)
	}
}

// Full lifecycle against the real API: create a group_prefix permission,
// validate it against the API, import it with the composite ID, update
// read_only in place, then switch to a prefix_path permission (which forces
// a replace).
func TestAccGitPermissionResource_crud(t *testing.T) {
	orgName := randomName("tf-acc-gitperm-org")
	groupPrefix := randomName("tf-acc-group")
	prefixPath := "github.com/" + randomName("tf-acc-prefix")
	var permissionID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccGitConnectionPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: gitPermissionCRUDConfig(orgName, fmt.Sprintf("  group_prefix      = %q\n", groupPrefix)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_git_permission.test", "git_permission_id"),
					resource.TestCheckResourceAttr("devin_git_permission.test", "git_connection_id", os.Getenv("DEVIN_GIT_CONNECTION_ID")),
					resource.TestCheckResourceAttr("devin_git_permission.test", "group_prefix", groupPrefix),
					resource.TestCheckNoResourceAttr("devin_git_permission.test", "repo_path"),
					resource.TestCheckNoResourceAttr("devin_git_permission.test", "prefix_path"),
					resource.TestCheckResourceAttr("devin_git_permission.test", "read_only", "false"),
					resource.TestCheckResourceAttrPair("devin_git_permission.test", "org_id", "devin_organization.test", "org_id"),
					checkAPIGitPermission(t, "devin_git_permission.test", map[string]any{
						"group_prefix": groupPrefix,
						"repo_path":    nil,
						"prefix_path":  nil,
						"read_only":    false,
					}),
					captureStateAttr("devin_git_permission.test", "git_permission_id", &permissionID),
				),
			},
			{
				ResourceName:                         "devin_git_permission.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "git_permission_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_git_permission.test", "org_id", "git_permission_id"),
			},
			{
				// read_only is the only in-place-updatable attribute.
				Config: gitPermissionCRUDConfig(orgName, fmt.Sprintf("  group_prefix      = %q\n  read_only         = true\n", groupPrefix)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_git_permission.test", "read_only", "true"),
					resource.TestCheckResourceAttrPtr("devin_git_permission.test", "git_permission_id", &permissionID),
					checkAPIGitPermission(t, "devin_git_permission.test", map[string]any{
						"group_prefix": groupPrefix,
						"read_only":    true,
					}),
				),
			},
			{
				// Switching the path type forces a replace with a new ID.
				Config: gitPermissionCRUDConfig(orgName, fmt.Sprintf("  prefix_path       = %q\n", prefixPath)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_git_permission.test", "prefix_path", prefixPath),
					resource.TestCheckNoResourceAttr("devin_git_permission.test", "group_prefix"),
					resource.TestCheckResourceAttr("devin_git_permission.test", "read_only", "false"),
					func(s *terraform.State) error {
						newID, err := stateAttr(s, "devin_git_permission.test", "git_permission_id")
						if err != nil {
							return err
						}
						if newID == permissionID {
							return fmt.Errorf("expected a new git_permission_id after replace, still %s", newID)
						}
						return nil
					},
					checkAPIGitPermission(t, "devin_git_permission.test", map[string]any{
						"prefix_path":  prefixPath,
						"group_prefix": nil,
						"repo_path":    nil,
						"read_only":    false,
					}),
				),
			},
		},
	})
}

// A permission deleted out-of-band must plan a recreate, not error.
func TestAccGitPermissionResource_outOfBandDelete(t *testing.T) {
	orgName := randomName("tf-acc-gitperm-oob")
	groupPrefix := randomName("tf-acc-group-oob")
	var orgID, permissionID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccGitConnectionPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: gitPermissionCRUDConfig(orgName, fmt.Sprintf("  group_prefix      = %q\n", groupPrefix)),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_git_permission.test", "org_id", &orgID),
					captureStateAttr("devin_git_permission.test", "git_permission_id", &permissionID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/enterprise/organizations/%s/git-providers/permissions/%s", orgID, permissionID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band git permission delete returned %d", status)
					}
				},
				Config:             gitPermissionCRUDConfig(orgName, fmt.Sprintf("  group_prefix      = %q\n", groupPrefix)),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
