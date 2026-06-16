package provider

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func playbookConfig(orgName, title, body, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_playbook" "test" {
  org_id = devin_organization.test.org_id
  title  = %q
  body   = %q
%s}
`, providerConfig, orgName, title, body, extra)
}

// Create without the optional macro, validate against the API, import with the
// composite ID, update fields in place, set the macro, then clear it again.
func TestAccPlaybookResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-pb-org")
	var playbookID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: playbookConfig(orgName, "Initial title", "Initial body", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_playbook.test", "playbook_id"),
					resource.TestCheckResourceAttr("devin_playbook.test", "title", "Initial title"),
					resource.TestCheckResourceAttr("devin_playbook.test", "body", "Initial body"),
					resource.TestCheckNoResourceAttr("devin_playbook.test", "macro"),
					resource.TestCheckResourceAttrPair("devin_playbook.test", "org_id", "devin_organization.test", "org_id"),
					checkAPIFieldMatchesState("devin_playbook.test", "/v3/organizations/{org_id}/playbooks/{playbook_id}", "title", "title"),
					checkAPIFieldMatchesState("devin_playbook.test", "/v3/organizations/{org_id}/playbooks/{playbook_id}", "body", "body"),
					captureStateAttr("devin_playbook.test", "playbook_id", &playbookID),
				),
			},
			{
				ResourceName:                         "devin_playbook.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "playbook_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_playbook.test", "org_id", "playbook_id"),
			},
			{
				// In-place update of title/body and setting a macro.
				Config: playbookConfig(orgName, "Updated title", "Updated body", "  macro  = \"!tf_acc_macro\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_playbook.test", "title", "Updated title"),
					resource.TestCheckResourceAttr("devin_playbook.test", "body", "Updated body"),
					resource.TestCheckResourceAttr("devin_playbook.test", "macro", "!tf_acc_macro"),
					resource.TestCheckResourceAttrPtr("devin_playbook.test", "playbook_id", &playbookID),
					checkAPIFieldMatchesState("devin_playbook.test", "/v3/organizations/{org_id}/playbooks/{playbook_id}", "macro", "macro"),
				),
			},
			{
				// Removing the macro from config must clear it on the API side.
				Config: playbookConfig(orgName, "Updated title", "Updated body", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_playbook.test", "macro"),
					resource.TestCheckResourceAttrPtr("devin_playbook.test", "playbook_id", &playbookID),
				),
			},
		},
	})
}

// The API rejects macros that don't start with "!".
func TestAccPlaybookResource_invalidMacro(t *testing.T) {
	orgName := randomName("tf-acc-pb-badmacro")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      playbookConfig(orgName, "Bad macro", "body", "  macro  = \"not-a-macro\"\n"),
				ExpectError: regexp.MustCompile(`(?i)macro`),
			},
		},
	})
}

// Malformed composite import IDs must be rejected.
func TestAccPlaybookResource_importInvalidID(t *testing.T) {
	orgName := randomName("tf-acc-pb-import")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: playbookConfig(orgName, "Import test", "body", ""),
			},
			{
				ResourceName:  "devin_playbook.test",
				ImportState:   true,
				ImportStateId: "missing-a-slash",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
	})
}

// A playbook deleted out-of-band must plan a recreate, not error.
func TestAccPlaybookResource_outOfBandDelete(t *testing.T) {
	orgName := randomName("tf-acc-pb-oob")
	var orgID, playbookID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: playbookConfig(orgName, "OOB delete", "body", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_playbook.test", "org_id", &orgID),
					captureStateAttr("devin_playbook.test", "playbook_id", &playbookID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/playbooks/%s", orgID, playbookID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band playbook delete returned %d", status)
					}
				},
				Config:             playbookConfig(orgName, "OOB delete", "body", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Deleting the parent org out-of-band must make refresh treat the org and its
// child resources as gone (planning a recreate of everything) instead of
// failing the plan.
func TestAccPlaybookResource_orgDeletedOutOfBand(t *testing.T) {
	orgName := randomName("tf-acc-pb-orggone")
	var orgID string

	config := fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_playbook" "test" {
  org_id = devin_organization.test.org_id
  title  = "Org deleted out-of-band"
  body   = "body"
}

resource "devin_knowledge_note" "test" {
  org_id  = devin_organization.test.org_id
  name    = "Org deleted out-of-band"
  body    = "body"
  trigger = "When testing the Terraform provider"
}
`, providerConfig, orgName)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  captureStateAttr("devin_organization.test", "org_id", &orgID),
			},
			{
				PreConfig: func() {
					status, _ := testAccAPIRequest(t, http.MethodDelete, "/v3/enterprise/organizations/"+orgID, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band org delete returned %d", status)
					}
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// An org-scoped token can manage playbooks in its own org.
func TestAccPlaybookResource_orgScopedTokenOwnOrg(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`%s
resource "devin_playbook" "own_org" {
  org_id = %q
  title  = "Org token playbook"
  body   = "Managed with an org-scoped token"
}
`, orgTokenProviderConfig(), os.Getenv("DEVIN_ORG_TOKEN_ORG_ID")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_playbook.own_org", "playbook_id"),
					resource.TestCheckResourceAttr("devin_playbook.own_org", "org_id", os.Getenv("DEVIN_ORG_TOKEN_ORG_ID")),
				),
			},
			{
				ResourceName:                         "devin_playbook.own_org",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "playbook_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_playbook.own_org", "org_id", "playbook_id"),
			},
		},
	})
}

// An org-scoped token must not be able to manage playbooks in an org that
// belongs to a different account. The enterprise org is created out-of-band
// with the enterprise token; the Terraform config only uses the org token.
// (Two provider configurations of the same in-process test provider cannot be
// mixed in one config: they share a single server, so the last-configured
// client would be used for every resource.)
func TestAccPlaybookResource_orgScopedTokenWrongOrg(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	testAccOrgTokenPreCheck(t)

	status, org := testAccAPIRequest(t, http.MethodPost, "/v3/enterprise/organizations", map[string]any{
		"name": randomName("tf-acc-pb-crossorg"),
	})
	if status != http.StatusOK {
		t.Fatalf("creating enterprise org returned %d", status)
	}
	entOrgID, _ := org["org_id"].(string)
	if entOrgID == "" {
		t.Fatal("enterprise org create response missing org_id")
	}
	t.Cleanup(func() {
		status, _ := testAccAPIRequest(t, http.MethodDelete, "/v3/enterprise/organizations/"+entOrgID, nil)
		if status != http.StatusOK {
			t.Logf("WARNING: cleanup DELETE for org %s returned %d", entOrgID, status)
		}
	})

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`%s
resource "devin_playbook" "cross_org" {
  org_id = %q
  title  = "Should not be created"
  body   = "Cross-account access must fail"
}
`, orgTokenProviderConfig(), entOrgID),
				ExpectError: regexp.MustCompile(`(?i)403|forbidden|unauthorized`),
			},
		},
	})
}
