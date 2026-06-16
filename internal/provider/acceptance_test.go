package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Acceptance tests run against a real Devin API. They are gated by
// TF_ACC=1 and require:
//
//	DEVIN_API_URL  base URL of the API under test (must be a local instance)
//	DEVIN_TOKEN    enterprise-scoped service user token (cog_*)
//
// Optional, used by the org-scoped token tests (skipped when unset):
//
//	DEVIN_ORG_TOKEN         org-scoped service user token (cog_*)
//	DEVIN_ORG_TOKEN_ORG_ID  org_id the org-scoped token belongs to

const providerConfig = `
provider "devin" {}
`

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"devin": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	apiURL := os.Getenv("DEVIN_API_URL")
	if apiURL == "" {
		t.Fatal("DEVIN_API_URL must be set for acceptance tests")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		t.Fatalf("DEVIN_API_URL is not a valid URL: %v", err)
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
	default:
		t.Fatalf("refusing to run acceptance tests against non-local API %q; the tests create and delete organizations", apiURL)
	}
	if os.Getenv("DEVIN_TOKEN") == "" {
		t.Fatal("DEVIN_TOKEN must be set for acceptance tests")
	}
}

// testAccSetupPreCheck is for tests that perform API-driven setup before
// resource.Test runs: it skips (rather than fails) when acceptance tests are
// not enabled, then applies the usual environment checks.
func testAccSetupPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	testAccPreCheck(t)
}

func testAccOrgTokenPreCheck(t *testing.T) {
	t.Helper()
	testAccPreCheck(t)
	if os.Getenv("DEVIN_ORG_TOKEN") == "" || os.Getenv("DEVIN_ORG_TOKEN_ORG_ID") == "" {
		t.Skip("DEVIN_ORG_TOKEN and DEVIN_ORG_TOKEN_ORG_ID must be set for org-scoped token tests")
	}
}

// orgTokenProviderConfig returns a provider block configured with the
// org-scoped token instead of the default enterprise token from DEVIN_TOKEN.
func orgTokenProviderConfig() string {
	return fmt.Sprintf("provider \"devin\" {\n  token = %q\n}\n", os.Getenv("DEVIN_ORG_TOKEN"))
}

func randomName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// testAccAPIRequest performs a direct request against the API under test using
// the enterprise token, for cross-checking Terraform state and for out-of-band
// mutations.
func testAccAPIRequest(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	return testAccAPIRequestWithToken(t, method, path, body, os.Getenv("DEVIN_TOKEN"))
}

// testAccAPIRequestWithToken is testAccAPIRequest with an explicit bearer
// token, for tests that need a token other than the enterprise service-user
// token (e.g. the session-tags enterprise token).
func testAccAPIRequestWithToken(t *testing.T, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling request body: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	url := strings.TrimRight(os.Getenv("DEVIN_API_URL"), "/") + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	var parsed map[string]any
	if len(raw) > 0 {
		// Some endpoints return non-object payloads; ignore decode errors and
		// let callers rely on the status code in that case.
		_ = json.Unmarshal(raw, &parsed)
	}
	return resp.StatusCode, parsed
}

// stateAttr returns an attribute value from a resource in the Terraform state.
func stateAttr(s *terraform.State, resourceName, attr string) (string, error) {
	rs, ok := s.RootModule().Resources[resourceName]
	if !ok {
		return "", fmt.Errorf("resource %s not found in state", resourceName)
	}
	value, ok := rs.Primary.Attributes[attr]
	if !ok {
		return "", fmt.Errorf("attribute %s not set on %s", attr, resourceName)
	}
	return value, nil
}

// captureStateAttr stores an attribute value into target so later steps (e.g.
// PreConfig out-of-band mutations) can reference IDs created by earlier steps.
func captureStateAttr(resourceName, attr string, target *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		value, err := stateAttr(s, resourceName, attr)
		if err != nil {
			return err
		}
		*target = value
		return nil
	}
}

// importStateIDFromAttrs builds an import ID by joining the given state
// attributes with "/" (e.g. "org_id/playbook_id" composite IDs).
func importStateIDFromAttrs(resourceName string, attrs ...string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		parts := make([]string, 0, len(attrs))
		for _, attr := range attrs {
			value, err := stateAttr(s, resourceName, attr)
			if err != nil {
				return "", err
			}
			parts = append(parts, value)
		}
		return strings.Join(parts, "/"), nil
	}
}

// checkAPIField asserts that fetching apiPath (with {attr} placeholders
// substituted from state) returns a JSON object whose field matches the given
// state attribute, validating that what Terraform stored matches the API.
func checkAPIFieldMatchesState(resourceName, pathTemplate, apiField, stateAttrName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		attrs := rs.Primary.Attributes

		path := pathTemplate
		for key, value := range attrs {
			path = strings.ReplaceAll(path, "{"+key+"}", value)
		}
		if strings.Contains(path, "{") {
			return fmt.Errorf("unresolved placeholder in API path %q", path)
		}

		url := strings.TrimRight(os.Getenv("DEVIN_API_URL"), "/") + path
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+os.Getenv("DEVIN_TOKEN"))
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
		}
		var parsed map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return err
		}

		apiValue := ""
		if raw, ok := parsed[apiField]; ok && raw != nil {
			switch v := raw.(type) {
			case string:
				apiValue = v
			case float64:
				apiValue = strings.TrimSuffix(fmt.Sprintf("%f", v), ".000000")
			case bool:
				apiValue = fmt.Sprintf("%t", v)
			default:
				apiValue = fmt.Sprintf("%v", v)
			}
		}

		stateValue := attrs[stateAttrName]
		if apiValue != stateValue {
			return fmt.Errorf("API field %s = %q does not match state attribute %s = %q", apiField, apiValue, stateAttrName, stateValue)
		}
		return nil
	}
}
