package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func enterpriseKnowledgeNoteConfig(name, body, trigger string) string {
	return fmt.Sprintf(`%s
resource "devin_enterprise_knowledge_note" "test" {
  name    = %q
  body    = %q
  trigger = %q
}
`, providerConfig, name, body, trigger)
}

// Create, validate against the API, import by note ID, then update every
// mutable field in place.
func TestAccEnterpriseKnowledgeNoteResource_basic(t *testing.T) {
	var noteID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: enterpriseKnowledgeNoteConfig("Initial name", "Initial body", "When testing the provider"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_enterprise_knowledge_note.test", "note_id"),
					resource.TestCheckResourceAttr("devin_enterprise_knowledge_note.test", "name", "Initial name"),
					resource.TestCheckResourceAttr("devin_enterprise_knowledge_note.test", "body", "Initial body"),
					resource.TestCheckResourceAttr("devin_enterprise_knowledge_note.test", "trigger", "When testing the provider"),
					checkAPIFieldMatchesState("devin_enterprise_knowledge_note.test", "/v3/enterprise/knowledge/notes/{note_id}", "name", "name"),
					checkAPIFieldMatchesState("devin_enterprise_knowledge_note.test", "/v3/enterprise/knowledge/notes/{note_id}", "body", "body"),
					captureStateAttr("devin_enterprise_knowledge_note.test", "note_id", &noteID),
				),
			},
			{
				ResourceName:                         "devin_enterprise_knowledge_note.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "note_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_enterprise_knowledge_note.test", "note_id"),
			},
			{
				Config: enterpriseKnowledgeNoteConfig("Updated name", "Updated body", "When re-testing the provider"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_enterprise_knowledge_note.test", "name", "Updated name"),
					resource.TestCheckResourceAttr("devin_enterprise_knowledge_note.test", "body", "Updated body"),
					resource.TestCheckResourceAttrPtr("devin_enterprise_knowledge_note.test", "note_id", &noteID),
					checkAPIFieldMatchesState("devin_enterprise_knowledge_note.test", "/v3/enterprise/knowledge/notes/{note_id}", "name", "name"),
				),
			},
		},
	})
}

// An enterprise note deleted out-of-band must plan a recreate, not error.
func TestAccEnterpriseKnowledgeNoteResource_outOfBandDelete(t *testing.T) {
	var noteID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: enterpriseKnowledgeNoteConfig("OOB delete", "body", "When testing"),
				Check:  captureStateAttr("devin_enterprise_knowledge_note.test", "note_id", &noteID),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/enterprise/knowledge/notes/%s", noteID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band enterprise knowledge note delete returned %d", status)
					}
				},
				Config:             enterpriseKnowledgeNoteConfig("OOB delete", "body", "When testing"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
