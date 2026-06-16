package provider

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// tagsProviderConfig configures the provider against the dedicated
// session-tags enterprise (the only seeded enterprise with the feature
// enabled; requiring tags on the main enterprise would break every other
// test that starts sessions or schedules).
func tagsProviderConfig() string {
	return fmt.Sprintf("provider \"devin\" {\n  token = %q\n}\n", os.Getenv("DEVIN_TAGS_TOKEN"))
}

func orgTagsConfig(orgName string, tags []string, defaultTag string) string {
	tagList := ""
	for i, tag := range tags {
		if i > 0 {
			tagList += ", "
		}
		tagList += fmt.Sprintf("%q", tag)
	}
	extra := ""
	if defaultTag != "" {
		extra = fmt.Sprintf("\n  default_tag = %q", defaultTag)
	}
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_org_tags" "test" {
  org_id = devin_organization.test.org_id
  tags   = [%s]%s
}
`, tagsProviderConfig(), orgName, tagList, extra)
}

// checkAPITags asserts the API reports the expected allowed tags and default
// tag ("" means no default) for the captured org.
func checkAPITags(t *testing.T, orgID *string, wantTags []string, wantDefault string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequestWithToken(t, http.MethodGet, fmt.Sprintf("/v3/enterprise/organizations/%s/tags", *orgID), nil, os.Getenv("DEVIN_TAGS_TOKEN"))
		if status != http.StatusOK {
			return fmt.Errorf("GET tags returned %d", status)
		}
		gotTags, _ := parsed["tags"].([]any)
		if len(gotTags) != len(wantTags) {
			return fmt.Errorf("tags = %v, want %v", gotTags, wantTags)
		}
		for i, want := range wantTags {
			if gotTags[i] != want {
				return fmt.Errorf("tags = %v, want %v", gotTags, wantTags)
			}
		}

		status, parsed = testAccAPIRequestWithToken(t, http.MethodGet, fmt.Sprintf("/v3/enterprise/organizations/%s/tags/default", *orgID), nil, os.Getenv("DEVIN_TAGS_TOKEN"))
		if status != http.StatusOK {
			return fmt.Errorf("GET default tag returned %d", status)
		}
		gotDefault, _ := parsed["default_tag"].(string)
		if gotDefault != wantDefault {
			return fmt.Errorf("default_tag = %q, want %q", gotDefault, wantDefault)
		}
		return nil
	}
}

// testAccTagsPreCheck skips the test when no token for a session-tags
// enabled enterprise is available.
func testAccTagsPreCheck(t *testing.T) {
	t.Helper()
	testAccPreCheck(t)
	if os.Getenv("DEVIN_TAGS_TOKEN") == "" {
		t.Skip("DEVIN_TAGS_TOKEN must be set for org tags tests (enterprise with required_tags_enabled)")
	}
}

// Replace the tag set, manage the default tag, and import by org ID. The API
// stores tags sorted, so configs use already-sorted tag lists.
func TestAccOrgTagsResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-tags-org")
	var orgID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccTagsPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: orgTagsConfig(orgName, []string{"bugfix", "feature"}, "feature"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_tags.test", "tags.#", "2"),
					resource.TestCheckResourceAttr("devin_org_tags.test", "default_tag", "feature"),
					captureStateAttr("devin_org_tags.test", "org_id", &orgID),
					checkAPITags(t, &orgID, []string{"bugfix", "feature"}, "feature"),
				),
			},
			{
				ResourceName:                         "devin_org_tags.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "org_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_org_tags.test", "org_id"),
			},
			{
				// Dropping the default's tag from the set and the default
				// itself: the provider clears the default server-side.
				Config: orgTagsConfig(orgName, []string{"bugfix", "maintenance"}, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_tags.test", "tags.#", "2"),
					resource.TestCheckNoResourceAttr("devin_org_tags.test", "default_tag"),
					checkAPITags(t, &orgID, []string{"bugfix", "maintenance"}, ""),
				),
			},
		},
	})
}
