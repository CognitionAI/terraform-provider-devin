package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// Query (list block) tests need Terraform 1.14+; they skip on the older CLI
// versions the other acceptance tests support.
var queryVersionChecks = []tfversion.TerraformVersionCheck{
	tfversion.SkipBelow(tfversion.Version1_14_0),
}

// Create an organization, then find it via `list "devin_organization"`.
func TestAccOrganizationListResource_query(t *testing.T) {
	orgName := randomName("tf-acc-list-org")
	var orgID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		TerraformVersionChecks:   queryVersionChecks,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}
`, providerConfig, orgName),
				Check: captureStateAttr("devin_organization.test", "org_id", &orgID),
			},
			{
				// The harness injects the provider block into query configs.
				Query: true,
				Config: `
list "devin_organization" "all" {
  provider = devin
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectIdentity("devin_organization.all", map[string]knownvalue.Check{
						"org_id": knownvalue.NotNull(),
					}),
					querycheck.ExpectLengthAtLeast("devin_organization.all", 1),
				},
			},
		},
	})
}

// Create a playbook in a fresh org, then find it via `list "devin_playbook"`
// scoped to that org. The org is provisioned out-of-band because the query
// step's config is rendered before any step runs, so it cannot reference
// values captured from earlier steps.
func TestAccPlaybookListResource_query(t *testing.T) {
	testAccSetupPreCheck(t)
	orgID := createTestOrg(t, randomName("tf-acc-list-pb-org"))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		TerraformVersionChecks:   queryVersionChecks,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`%s
resource "devin_playbook" "test" {
  org_id = %q
  title  = "List test playbook"
  body   = "Steps."
}
`, providerConfig, orgID),
			},
			{
				Query: true,
				Config: fmt.Sprintf(`
list "devin_playbook" "all" {
  provider = devin

  config {
    org_id = %q
  }
}
`, orgID),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLength("devin_playbook.all", 1),
					querycheck.ExpectIdentity("devin_playbook.all", map[string]knownvalue.Check{
						"org_id":      knownvalue.StringExact(orgID),
						"playbook_id": knownvalue.NotNull(),
					}),
				},
			},
		},
	})
}

// createTestOrg provisions an organization out-of-band and removes it when
// the test finishes.
func createTestOrg(t *testing.T, name string) string {
	t.Helper()
	status, parsed := testAccAPIRequest(t, http.MethodPost, "/v3/enterprise/organizations", map[string]any{"name": name})
	if status != http.StatusOK {
		t.Fatalf("creating test org returned %d: %v", status, parsed)
	}
	orgID, _ := parsed["org_id"].(string)
	if orgID == "" {
		t.Fatalf("creating test org returned no org_id: %v", parsed)
	}
	t.Cleanup(func() {
		status, parsed := testAccAPIRequest(t, http.MethodDelete, "/v3/enterprise/organizations/"+orgID, nil)
		if status != http.StatusOK && status != http.StatusNotFound {
			t.Errorf("cleaning up test org %s returned %d: %v", orgID, status, parsed)
		}
	})
	return orgID
}
