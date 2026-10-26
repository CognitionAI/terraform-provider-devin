package provider

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// acme/web and acme/api are fake repos the local seed adds to the
// synthetic git connection and grants to the org-token org.
const accIndexedRepo = "acme/web"

func repositoryIndexingConfig(branches string) string {
	return fmt.Sprintf(`%s
resource "devin_repository_indexing" "test" {
  org_id          = %q
  repository_path = %q
  branch_names    = %s
}
`, orgTokenProviderConfig(), os.Getenv("DEVIN_ORG_TOKEN_ORG_ID"), accIndexedRepo, branches)
}

func checkSetLacks(resourceName, attr, value string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		for k, v := range rs.Primary.Attributes {
			if strings.HasPrefix(k, attr+".") && k != attr+".#" && v == value {
				return fmt.Errorf("%s.%s still contains %q", resourceName, attr, value)
			}
		}
		return nil
	}
}

// checkIndexedBranches asserts the API reports repo as indexed with exactly
// the given branches.
func checkIndexedBranches(t *testing.T, orgID, repo string, want ...string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, body := testAccAPIRequestWithToken(t, http.MethodGet,
			fmt.Sprintf("/v3/organizations/%s/repositories/indexing", orgID), nil, os.Getenv("DEVIN_ORG_TOKEN"))
		if status != http.StatusOK {
			return fmt.Errorf("listing indexed repositories returned %d", status)
		}
		items, _ := body["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["repository_path"] != repo {
				continue
			}
			if item["indexing_enabled"] != true {
				return fmt.Errorf("%s is not indexed", repo)
			}
			var got []string
			branches, _ := item["branches"].([]any)
			for _, b := range branches {
				got = append(got, fmt.Sprint(b))
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				return fmt.Errorf("API branches for %s = %v, want %v", repo, got, want)
			}
			return nil
		}
		return fmt.Errorf("%s not in the indexed repositories list", repo)
	}
}

// Indexing starts on create, adding a branch updates in place with the new
// computed branches, removing one deletes it server-side, and destroy turns
// indexing off.
func TestAccRepositoryIndexingResource_branches(t *testing.T) {
	orgID := os.Getenv("DEVIN_ORG_TOKEN_ORG_ID")
	name := "devin_repository_indexing.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: repositoryIndexingConfig(`["dev"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemAttr(name, "branches.*", "dev"),
					checkIndexedBranches(t, orgID, accIndexedRepo, "dev"),
				),
			},
			{
				Config: repositoryIndexingConfig(`["dev", "release"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemAttr(name, "branches.*", "dev"),
					resource.TestCheckTypeSetElemAttr(name, "branches.*", "release"),
					checkIndexedBranches(t, orgID, accIndexedRepo, "dev", "release"),
				),
			},
			{
				Config: repositoryIndexingConfig(`["release"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemAttr(name, "branches.*", "release"),
					checkSetLacks(name, "branches", "dev"),
					checkIndexedBranches(t, orgID, accIndexedRepo, "release"),
				),
			},
			{
				ResourceName:                         name,
				ImportState:                          true,
				ImportStateId:                        orgID + "/" + accIndexedRepo,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "repository_path",
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			status, body := testAccAPIRequestWithToken(t, http.MethodGet,
				fmt.Sprintf("/v3/organizations/%s/repositories/indexing", orgID), nil, os.Getenv("DEVIN_ORG_TOKEN"))
			if status != http.StatusOK {
				return fmt.Errorf("listing indexed repositories returned %d", status)
			}
			items, _ := body["items"].([]any)
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item["repository_path"] == accIndexedRepo && item["indexing_enabled"] == true {
					return fmt.Errorf("%s is still indexed after destroy", accIndexedRepo)
				}
			}
			return nil
		},
	})
}
