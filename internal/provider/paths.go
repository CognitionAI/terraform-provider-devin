package provider

import "net/url"

const (
	enterpriseOrganizationsPath = "/v3/enterprise/organizations"
	organizationsPath           = "/v3/organizations"
)

func organizationPath(orgID string) string {
	return enterpriseOrganizationsPath + "/" + url.PathEscape(orgID)
}

func orgGitPermissionsPath(orgID string) string {
	return organizationPath(orgID) + "/git-providers/permissions"
}

func orgGitPermissionPath(orgID, gitPermissionID string) string {
	return orgGitPermissionsPath(orgID) + "/" + url.PathEscape(gitPermissionID)
}

func orgPlaybooksPath(orgID string) string {
	return organizationsPath + "/" + url.PathEscape(orgID) + "/playbooks"
}

func orgPlaybookPath(orgID, playbookID string) string {
	return orgPlaybooksPath(orgID) + "/" + url.PathEscape(playbookID)
}

func orgKnowledgeNotesPath(orgID string) string {
	return organizationsPath + "/" + url.PathEscape(orgID) + "/knowledge/notes"
}

func orgKnowledgeNotePath(orgID, noteID string) string {
	return orgKnowledgeNotesPath(orgID) + "/" + url.PathEscape(noteID)
}

func orgSecretsPath(orgID string) string {
	return organizationsPath + "/" + url.PathEscape(orgID) + "/secrets"
}

func orgSecretPath(orgID, secretID string) string {
	return orgSecretsPath(orgID) + "/" + url.PathEscape(secretID)
}

func orgSchedulesPath(orgID string) string {
	return organizationsPath + "/" + url.PathEscape(orgID) + "/schedules"
}

func orgSchedulePath(orgID, scheduleID string) string {
	return orgSchedulesPath(orgID) + "/" + url.PathEscape(scheduleID)
}

const (
	enterpriseGitConnectionsPath     = "/v3/enterprise/git-providers/connections"
	enterpriseRolesPath              = "/v3/enterprise/roles"
	enterpriseIPAccessListPath       = "/v3/enterprise/ip-access-list"
	enterpriseOrgGroupLimitsPath     = "/v3/enterprise/org-group-limits"
	enterpriseIdpGroupsPath          = "/v3/enterprise/idp-groups"
	enterpriseMemberIdpGroups        = "/v3/enterprise/members/idp-groups"
	enterprisePlaybooksPath          = "/v3/enterprise/playbooks"
	enterpriseKnowledgeNotesPath     = "/v3/enterprise/knowledge/notes"
	enterpriseMemberUsersPath        = "/v3/enterprise/members/users"
	enterpriseMemberServiceUsersPath = "/v3/enterprise/members/service-users"
)

func enterprisePlaybookPath(playbookID string) string {
	return enterprisePlaybooksPath + "/" + url.PathEscape(playbookID)
}

func enterpriseKnowledgeNotePath(noteID string) string {
	return enterpriseKnowledgeNotesPath + "/" + url.PathEscape(noteID)
}

func enterpriseMemberUserPath(userID string) string {
	return enterpriseMemberUsersPath + "/" + url.PathEscape(userID)
}

func enterpriseMemberServiceUserPath(serviceUserID string) string {
	return enterpriseMemberServiceUsersPath + "/" + url.PathEscape(serviceUserID)
}

func orgMemberUserPath(orgID, userID string) string {
	return organizationPath(orgID) + "/members/users/" + url.PathEscape(userID)
}

func orgMemberServiceUserPath(orgID, serviceUserID string) string {
	return organizationPath(orgID) + "/members/service-users/" + url.PathEscape(serviceUserID)
}

func orgTagsPath(orgID string) string {
	return organizationPath(orgID) + "/tags"
}

func orgDefaultTagPath(orgID string) string {
	return orgTagsPath(orgID) + "/default"
}

func enterpriseIdpGroupPath(name string) string {
	return enterpriseIdpGroupsPath + "/" + url.PathEscape(name)
}

func enterpriseMemberIdpGroupPath(name string) string {
	return enterpriseMemberIdpGroups + "/" + url.PathEscape(name)
}

func orgMemberIdpGroupsPath(orgID string) string {
	return organizationPath(orgID) + "/members/idp-groups"
}

func orgMemberIdpGroupPath(orgID, name string) string {
	return orgMemberIdpGroupsPath(orgID) + "/" + url.PathEscape(name)
}
