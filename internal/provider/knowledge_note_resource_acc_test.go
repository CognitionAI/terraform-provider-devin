package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func knowledgeNoteConfig(orgName, name, body, trigger string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_knowledge_note" "test" {
  org_id  = devin_organization.test.org_id
  name    = %q
  body    = %q
  trigger = %q
}
`, providerConfig, orgName, name, body, trigger)
}

// Create, validate against the API, import with the composite ID, then update
// every mutable field in place.
func TestAccKnowledgeNoteResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-kn-org")
	var noteID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: knowledgeNoteConfig(orgName, "Initial name", "Initial body", "When testing the provider"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_knowledge_note.test", "note_id"),
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "name", "Initial name"),
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "body", "Initial body"),
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "trigger", "When testing the provider"),
					resource.TestCheckResourceAttrPair("devin_knowledge_note.test", "org_id", "devin_organization.test", "org_id"),
					checkAPIFieldMatchesState("devin_knowledge_note.test", "/v3/organizations/{org_id}/knowledge/notes/{note_id}", "name", "name"),
					checkAPIFieldMatchesState("devin_knowledge_note.test", "/v3/organizations/{org_id}/knowledge/notes/{note_id}", "body", "body"),
					checkAPIFieldMatchesState("devin_knowledge_note.test", "/v3/organizations/{org_id}/knowledge/notes/{note_id}", "trigger", "trigger"),
					captureStateAttr("devin_knowledge_note.test", "note_id", &noteID),
				),
			},
			{
				ResourceName:                         "devin_knowledge_note.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "note_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_knowledge_note.test", "org_id", "note_id"),
			},
			{
				Config: knowledgeNoteConfig(orgName, "Updated name", "Updated body", "When re-testing the provider"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "name", "Updated name"),
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "body", "Updated body"),
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "trigger", "When re-testing the provider"),
					resource.TestCheckResourceAttrPtr("devin_knowledge_note.test", "note_id", &noteID),
					checkAPIFieldMatchesState("devin_knowledge_note.test", "/v3/organizations/{org_id}/knowledge/notes/{note_id}", "name", "name"),
				),
			},
		},
	})
}

// Malformed composite import IDs must be rejected.
func TestAccKnowledgeNoteResource_importInvalidID(t *testing.T) {
	orgName := randomName("tf-acc-kn-import")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: knowledgeNoteConfig(orgName, "Import test", "body", "When testing"),
			},
			{
				ResourceName:  "devin_knowledge_note.test",
				ImportState:   true,
				ImportStateId: "missing-a-slash",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
	})
}

// A knowledge note deleted out-of-band must plan a recreate, not error.
func TestAccKnowledgeNoteResource_outOfBandDelete(t *testing.T) {
	orgName := randomName("tf-acc-kn-oob")
	var orgID, noteID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: knowledgeNoteConfig(orgName, "OOB delete", "body", "When testing"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_knowledge_note.test", "org_id", &orgID),
					captureStateAttr("devin_knowledge_note.test", "note_id", &noteID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/knowledge/notes/%s", orgID, noteID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band knowledge note delete returned %d", status)
					}
				},
				Config:             knowledgeNoteConfig(orgName, "OOB delete", "body", "When testing"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func knowledgeNoteConfigWithPin(orgName, name, body, trigger, pinnedRepo string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_knowledge_note" "test" {
  org_id      = devin_organization.test.org_id
  name        = %q
  body        = %q
  trigger     = %q
  pinned_repo = %q
}
`, providerConfig, orgName, name, body, trigger, pinnedRepo)
}

// pinned_repo is managed directly by the provider: setting it pins the note,
// removing it from config clears the pin on the API side.
func TestAccKnowledgeNoteResource_pinnedRepoLifecycle(t *testing.T) {
	orgName := randomName("tf-acc-kn-pin")
	var orgID, noteID string

	checkAPIPinnedRepo := func(want any) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			path := fmt.Sprintf("/v3/organizations/%s/knowledge/notes/%s", orgID, noteID)
			status, note := testAccAPIRequest(t, http.MethodGet, path, nil)
			if status != http.StatusOK {
				return fmt.Errorf("reading knowledge note returned %d", status)
			}
			if note["pinned_repo"] != want {
				return fmt.Errorf("pinned_repo = %v, want %v", note["pinned_repo"], want)
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: knowledgeNoteConfig(orgName, "Pinned note", "body", "When testing"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_knowledge_note.test", "org_id", &orgID),
					captureStateAttr("devin_knowledge_note.test", "note_id", &noteID),
					resource.TestCheckNoResourceAttr("devin_knowledge_note.test", "pinned_repo"),
				),
			},
			{
				Config: knowledgeNoteConfigWithPin(orgName, "Pinned note", "body", "When testing", "github.com/example/pinned-repo"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_knowledge_note.test", "pinned_repo", "github.com/example/pinned-repo"),
					resource.TestCheckResourceAttrPtr("devin_knowledge_note.test", "note_id", &noteID),
					checkAPIPinnedRepo("github.com/example/pinned-repo"),
				),
			},
			{
				Config: knowledgeNoteConfig(orgName, "Pinned note", "body", "When testing"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_knowledge_note.test", "pinned_repo"),
					checkAPIPinnedRepo(nil),
				),
			},
		},
	})
}
