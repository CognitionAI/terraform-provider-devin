package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func enterprisePlaybookConfig(title, body string, macro string) string {
	extra := ""
	if macro != "" {
		extra = fmt.Sprintf("\n  macro = %q", macro)
	}
	return fmt.Sprintf(`%s
resource "devin_enterprise_playbook" "test" {
  title = %q
  body  = %q%s
}
`, providerConfig, title, body, extra)
}

// Create, validate against the API, import by playbook ID, update in place,
// then clear the macro by removing it from config.
func TestAccEnterprisePlaybookResource_basic(t *testing.T) {
	macro := "!" + randomName("tf-acc-ent-pb")
	var playbookID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: enterprisePlaybookConfig("Initial title", "Initial body", macro),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_enterprise_playbook.test", "playbook_id"),
					resource.TestCheckResourceAttr("devin_enterprise_playbook.test", "title", "Initial title"),
					resource.TestCheckResourceAttr("devin_enterprise_playbook.test", "body", "Initial body"),
					resource.TestCheckResourceAttr("devin_enterprise_playbook.test", "macro", macro),
					checkAPIFieldMatchesState("devin_enterprise_playbook.test", "/v3/enterprise/playbooks/{playbook_id}", "title", "title"),
					checkAPIFieldMatchesState("devin_enterprise_playbook.test", "/v3/enterprise/playbooks/{playbook_id}", "body", "body"),
					captureStateAttr("devin_enterprise_playbook.test", "playbook_id", &playbookID),
				),
			},
			{
				ResourceName:                         "devin_enterprise_playbook.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "playbook_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_enterprise_playbook.test", "playbook_id"),
			},
			{
				Config: enterprisePlaybookConfig("Updated title", "Updated body", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_enterprise_playbook.test", "title", "Updated title"),
					resource.TestCheckResourceAttr("devin_enterprise_playbook.test", "body", "Updated body"),
					resource.TestCheckNoResourceAttr("devin_enterprise_playbook.test", "macro"),
					resource.TestCheckResourceAttrPtr("devin_enterprise_playbook.test", "playbook_id", &playbookID),
					checkAPIFieldMatchesState("devin_enterprise_playbook.test", "/v3/enterprise/playbooks/{playbook_id}", "title", "title"),
				),
			},
		},
	})
}
