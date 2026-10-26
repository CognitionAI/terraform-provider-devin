package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func orgBlueprintConfig(orgName, contents string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_blueprint" "test" {
  org_id   = devin_organization.test.org_id
  contents = %q
}
`, providerConfig, orgName, contents)
}

// The org-wide blueprint's YAML round-trips through the presigned contents
// URL, updates in place, imports, and drifts when edited out of band.
func TestAccBlueprintResource_org(t *testing.T) {
	orgName := randomName("tf-acc-blueprint-org")
	var orgID, blueprintID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: orgBlueprintConfig(orgName, "initialize: |\n  echo one\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_blueprint.test", "blueprint_id"),
					resource.TestCheckResourceAttr("devin_blueprint.test", "type", "org"),
					resource.TestCheckNoResourceAttr("devin_blueprint.test", "source"),
					captureStateAttr("devin_blueprint.test", "org_id", &orgID),
					captureStateAttr("devin_blueprint.test", "blueprint_id", &blueprintID),
				),
			},
			{
				Config: orgBlueprintConfig(orgName, "initialize: |\n  echo two\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("devin_blueprint.test", plancheck.ResourceActionUpdate),
					},
				},
			},
			{
				ResourceName:                         "devin_blueprint.test",
				ImportState:                          true,
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_blueprint.test", "org_id", "blueprint_id"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "blueprint_id",
				// Contents are only read back once Terraform manages them.
				ImportStateVerifyIgnore: []string{"contents"},
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/snapshot-setup/blueprints/%s", orgID, blueprintID)
					status, _ := testAccAPIRequest(t, http.MethodPatch, path, map[string]any{"contents": "initialize: |\n  echo out-of-band\n"})
					if status != http.StatusOK {
						t.Fatalf("out-of-band blueprint update returned %d", status)
					}
				},
				Config:             orgBlueprintConfig(orgName, "initialize: |\n  echo two\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			path := fmt.Sprintf("/v3/organizations/%s/snapshot-setup/blueprints/%s", orgID, blueprintID)
			if status, _ := testAccAPIRequest(t, http.MethodGet, path, nil); status == http.StatusOK {
				return fmt.Errorf("blueprint %s still exists after destroy", blueprintID)
			}
			return nil
		},
	})
}

func repoBlueprintConfig(contents string) string {
	return fmt.Sprintf(`%s
resource "devin_blueprint" "repo" {
  org_id    = %q
  repo_name = "acme/api"
  contents  = %q
}
`, orgTokenProviderConfig(), os.Getenv("DEVIN_ORG_TOKEN_ORG_ID"), contents)
}

// checkBlueprintContents asserts the API serves want as the blueprint's YAML.
func checkBlueprintContents(t *testing.T, orgID string, blueprintID *string, want string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		path := fmt.Sprintf("/v3/organizations/%s/snapshot-setup/blueprints/%s/contents", orgID, *blueprintID)
		status, body := testAccAPIRequestWithToken(t, http.MethodGet, path, nil, os.Getenv("DEVIN_ORG_TOKEN"))
		if status != http.StatusOK {
			return fmt.Errorf("GET %s returned %d", path, status)
		}
		url, _ := body["url"].(string)
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if string(got) != want {
			return fmt.Errorf("API blueprint contents = %q, want %q", got, want)
		}
		return nil
	}
}

// A repo blueprint on a seeded fake repo of the org-token org: create,
// in-place update and import, cross-checked against the API.
func TestAccBlueprintResource_repo(t *testing.T) {
	name := "devin_blueprint.repo"
	orgID := os.Getenv("DEVIN_ORG_TOKEN_ORG_ID")
	var blueprintID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: repoBlueprintConfig("initialize: |\n  echo one\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "type", "repo"),
					resource.TestCheckResourceAttr(name, "repo_name", "acme/api"),
					resource.TestCheckResourceAttr(name, "source", "database"),
					captureStateAttr(name, "blueprint_id", &blueprintID),
					checkBlueprintContents(t, orgID, &blueprintID, "initialize: |\n  echo one\n"),
				),
			},
			{
				Config: repoBlueprintConfig("initialize: |\n  echo two\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: checkBlueprintContents(t, orgID, &blueprintID, "initialize: |\n  echo two\n"),
			},
			{
				ResourceName:                         name,
				ImportState:                          true,
				ImportStateIdFunc:                    importStateIDFromAttrs(name, "org_id", "blueprint_id"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "blueprint_id",
				ImportStateVerifyIgnore:              []string{"contents"},
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			path := fmt.Sprintf("/v3/organizations/%s/snapshot-setup/blueprints/%s", orgID, blueprintID)
			if status, _ := testAccAPIRequestWithToken(t, http.MethodGet, path, nil, os.Getenv("DEVIN_ORG_TOKEN")); status == http.StatusOK {
				return fmt.Errorf("blueprint %s still exists after destroy", blueprintID)
			}
			return nil
		},
	})
}

func enterpriseBlueprintConfig(contents string) string {
	return fmt.Sprintf(`%s
resource "devin_enterprise_blueprint" "test" {
  contents = %q
}
`, providerConfig, contents)
}

func TestAccEnterpriseBlueprintResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: enterpriseBlueprintConfig("initialize: |\n  echo one\n"),
				Check:  resource.TestCheckResourceAttrSet("devin_enterprise_blueprint.test", "blueprint_id"),
			},
			{
				Config: enterpriseBlueprintConfig("initialize: |\n  echo two\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("devin_enterprise_blueprint.test", plancheck.ResourceActionUpdate),
					},
				},
			},
			{
				ResourceName:                         "devin_enterprise_blueprint.test",
				ImportState:                          true,
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_enterprise_blueprint.test", "blueprint_id"),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "blueprint_id",
			},
		},
	})
}

func blueprintFileConfig(orgName, content string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_blueprint" "test" {
  org_id   = devin_organization.test.org_id
  contents = "initialize: |\n  echo hi\n"
}

resource "devin_blueprint_file" "test" {
  org_id         = devin_organization.test.org_id
  blueprint_id   = devin_blueprint.test.blueprint_id
  name           = "settings.xml"
  content_base64 = base64encode(%q)
}
`, providerConfig, orgName, content)
}

// The file uploads as multipart, is replaced when its contents change, and
// drops out of state when deleted out of band.
func TestAccBlueprintFileResource_org(t *testing.T) {
	orgName := randomName("tf-acc-blueprint-file")
	var orgID, blueprintID, fileID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: blueprintFileConfig(orgName, "<settings/>"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_blueprint_file.test", "env_var_name"),
					resource.TestCheckResourceAttr("devin_blueprint_file.test", "size_bytes", "11"),
					captureStateAttr("devin_blueprint_file.test", "org_id", &orgID),
					captureStateAttr("devin_blueprint_file.test", "blueprint_id", &blueprintID),
				),
			},
			{
				Config: blueprintFileConfig(orgName, "<settings>v2</settings>"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("devin_blueprint_file.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_blueprint_file.test", "size_bytes", "23"),
					captureStateAttr("devin_blueprint_file.test", "file_id", &fileID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/snapshot-setup/blueprints/%s/files/%s", orgID, blueprintID, fileID)
					if status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil); status >= 300 {
						t.Fatalf("out-of-band file delete returned %d", status)
					}
				},
				Config:             blueprintFileConfig(orgName, "<settings>v2</settings>"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
