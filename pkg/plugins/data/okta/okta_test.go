package okta_test

import (
	"context"
	_ "embed"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jarcoal/httpmock"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

type Responder struct {
	Method    string
	URL       string
	Responder httpmock.Responder
}

var (
	//go:embed testdata/users.json
	users []byte
	//go:embed testdata/groups.json
	groups []byte
	//go:embed testdata/group00g8c2qsz3rt2VcB85d7users.json
	group00g8c2qsz3rt2VcB85d7Users []byte
	//go:embed testdata/roles.json
	roles []byte
	//go:embed testdata/apps.json
	apps []byte
	//go:embed testdata/access_token.json
	accessToken []byte

	expectedUsers = []any{
		map[string]any{
			"_links": map[string]any{
				"self": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/users/00u8c2qt45hW8VYPO5d7",
				},
			},
			"activated": nil,
			"created":   "2023-02-14T20:45:11Z",
			"credentials": map[string]any{
				"emails": []any{
					map[string]any{
						"status": string("VERIFIED"),
						"type":   string("PRIMARY"),
						"value":  string("sergey@styra.com"),
					},
				},
				"password": map[string]any{},
				"provider": map[string]any{
					"name": "OKTA",
					"type": "OKTA",
				},
			},
			"id":              "00u8c2qt45hW8VYPO5d7",
			"lastLogin":       "2023-02-14T23:53:18Z",
			"lastUpdated":     "2023-02-14T23:53:13Z",
			"passwordChanged": "2023-02-14T23:53:13Z",
			"profile": map[string]any{
				"email":       "sergey@styra.com",
				"firstName":   "Sergey",
				"lastName":    "Styra",
				"login":       "sergey@styra.com",
				"mobilePhone": nil,
				"secondEmail": nil,
			},
			"status":        "ACTIVE",
			"statusChanged": "2023-02-14T23:53:13Z",
			"type": map[string]any{
				"id": "oty8c2qszdAhbwavE5d7",
			},
		},
	}
	expectedGroups = []any{
		map[string]any{
			"_links": map[string]any{
				"apps": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/groups/00g8c2qsz3rt2VcB85d7/apps",
				},
				"logo": []any{
					map[string]any{
						"href": "https://ok12static.oktacdn.com/assets/img/logos/groups/odyssey/okta-medium.1a5ebe44c4244fb796c235d86b47e3bb.png",
						"name": "medium",
						"type": "image/png",
					},
					map[string]any{
						"href": "https://ok12static.oktacdn.com/assets/img/logos/groups/odyssey/okta-large.d9cfbd8a00a4feac1aa5612ba02e99c0.png",
						"name": "large",
						"type": "image/png",
					},
				},
				"users": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/groups/00g8c2qsz3rt2VcB85d7/users",
				},
			},
			"created":               "2023-02-14T20:45:07Z",
			"id":                    "00g8c2qsz3rt2VcB85d7",
			"lastMembershipUpdated": "2023-02-14T20:45:11Z",
			"lastUpdated":           "2023-02-14T20:45:07Z",
			"objectClass": []any{
				"okta:user_group",
			},
			"profile": map[string]any{
				"description": "All users in your organization",
				"name":        "Everyone",
			},
			"type": "BUILT_IN",
		},
	}
	expectedGroupMembers = map[string]any{
		"00g8c2qsz3rt2VcB85d7": []any{
			map[string]any{
				"_links": map[string]any{
					"self": map[string]any{
						"href": "https://dev-09267642.okta.com/api/v1/users/00u8c2qt45hW8VYPO5d7",
					},
				},
				"activated": nil,
				"created":   "2023-02-14T20:45:11Z",
				"credentials": map[string]any{
					"emails": []any{
						map[string]any{
							"status": "VERIFIED",
							"type":   "PRIMARY",
							"value":  "sergey@styra.com",
						},
					},
					"password": map[string]any{},
					"provider": map[string]any{
						"name": "OKTA",
						"type": "OKTA",
					},
				},
				"id":              "00u8c2qt45hW8VYPO5d7",
				"lastLogin":       "2023-02-14T23:53:18Z",
				"lastUpdated":     "2023-02-14T23:53:13Z",
				"passwordChanged": "2023-02-14T23:53:13Z",
				"profile": map[string]any{
					"email":       "sergey@styra.com",
					"firstName":   "Sergey",
					"lastName":    "Styra",
					"login":       "sergey@styra.com",
					"mobilePhone": nil,
					"secondEmail": nil,
				},
				"status":        "ACTIVE",
				"statusChanged": "2023-02-14T23:53:13Z",
				"type": map[string]any{
					"id": "oty8c2qszdAhbwavE5d7",
				},
			},
		},
	}
	expectedRoles = []any{
		map[string]any{
			"_links": map[string]any{
				"permissions": map[string]any{
					"href": "https://dev-09267642-admin.okta.com/api/v1/iam/roles/cr08ciat6nBKUPtfG5d7/permissions",
				},
				"self": map[string]any{
					"href": "https://dev-09267642-admin.okta.com/api/v1/iam/roles/cr08ciat6nBKUPtfG5d7",
				},
			},
			"created":     "2023-02-15T18:56:19Z",
			"description": "for testing purpose with users.read permissions",
			"id":          "cr08ciat6nBKUPtfG5d7",
			"label":       "read-users",
			"lastUpdated": "2023-02-15T18:56:19Z",
			"permissions": nil,
		},
		map[string]any{
			"_links": map[string]any{
				"permissions": map[string]any{
					"href": "https://dev-09267642-admin.okta.com/api/v1/iam/roles/cr08cicu3ym2mDPWM5d7/permissions",
				},
				"self": map[string]any{
					"href": "https://dev-09267642-admin.okta.com/api/v1/iam/roles/cr08cicu3ym2mDPWM5d7",
				},
			},
			"created":     "2023-02-15T18:57:01Z",
			"description": "for testing purpose with groups.read permissions",
			"id":          "cr08cicu3ym2mDPWM5d7",
			"label":       "read-groups",
			"lastUpdated": "2023-02-15T18:57:01Z",
			"permissions": nil,
		},
	}
	expectedApps = []any{
		map[string]any{
			"_links": map[string]any{
				"accessPolicy": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgwgX1FhsHU5d7",
				},
				"appLinks": []any{
					map[string]any{
						"href": "https://dev-09267642.okta.com/home/saasure/0oa8c2qsynf2o2rp75d7/2",
						"name": "admin",
						"type": "text/html",
					},
				},
				"deactivate": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qsynf2o2rp75d7/lifecycle/deactivate",
				},
				"groups": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qsynf2o2rp75d7/groups",
				},
				"logo": []any{
					map[string]any{
						"href": "https://ok12static.oktacdn.com/assets/img/logos/okta_admin_app.da3325676d57eaf566cb786dd0c7a819.png",
						"name": "medium",
						"type": "image/png",
					},
				},
				"policies": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"PUT",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qsynf2o2rp75d7/policies",
				},
				"profileEnrollment": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxvl8fCoYO5d7",
				},
				"uploadLogo": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"POST",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qsynf2o2rp75d7/logo",
				},
				"users": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qsynf2o2rp75d7/users",
				},
			},
			"accessibility": map[string]any{
				"selfService": false,
			},
			"created": "2023-02-14T20:45:06Z",
			"credentials": map[string]any{
				"signing": map[string]any{
					"kid": "Z_KshFi815TvPEOFAcKPSsR2gQAHRALctRPJM3f95Tg",
				},
				"userNameTemplate": map[string]any{
					"template": "${source.login}",
					"type":     "BUILT_IN",
				},
			},
			"features":    []any{},
			"id":          "0oa8c2qsynf2o2rp75d7",
			"label":       "Okta Admin Console",
			"lastUpdated": "2023-02-14T20:45:09Z",
			"name":        "saasure",
			"settings": map[string]any{
				"app": map[string]any{},
				"notifications": map[string]any{
					"vpn": map[string]any{
						"network": map[string]any{
							"connection": "DISABLED",
						},
					},
				},
			},
			"signOnMode": "OPENID_CONNECT",
			"status":     "ACTIVE",
			"visibility": map[string]any{
				"appLinks": map[string]any{
					"admin": true,
				},
				"autoSubmitToolbar": false,
				"hide": map[string]any{
					"iOS": false,
					"web": false,
				},
			},
		},
		map[string]any{
			"_links": map[string]any{
				"accessPolicy": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxtgiXCFrw5d7",
				},
				"appLinks": []any{},
				"deactivate": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt1jx3I7twX5d7/lifecycle/deactivate",
				},
				"groups": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt1jx3I7twX5d7/groups",
				},
				"logo": []any{
					map[string]any{
						"href": "https://ok12static.oktacdn.com/assets/img/logos/okta-logo-browser-plugin.1db9f55776407dfc548a5d6985ff280a.svg",
						"name": "medium",
						"type": "image/png",
					},
				},
				"policies": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"PUT",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt1jx3I7twX5d7/policies",
				},
				"profileEnrollment": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxvl8fCoYO5d7",
				},
				"uploadLogo": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"POST",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt1jx3I7twX5d7/logo",
				},
				"users": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt1jx3I7twX5d7/users",
				},
			},
			"accessibility": map[string]any{
				"selfService": false,
			},
			"created": "2023-02-14T20:45:09Z",
			"credentials": map[string]any{
				"signing": map[string]any{
					"kid": "Z_KshFi815TvPEOFAcKPSsR2gQAHRALctRPJM3f95Tg",
				},
				"userNameTemplate": map[string]any{
					"template": "${source.login}",
					"type":     "BUILT_IN",
				},
			},
			"features":    []any{},
			"id":          "0oa8c2qt1jx3I7twX5d7",
			"label":       "Okta Browser Plugin",
			"lastUpdated": "2023-02-14T20:45:09Z",
			"name":        "okta_browser_plugin",
			"settings": map[string]any{
				"app": map[string]any{},
				"notifications": map[string]any{
					"vpn": map[string]any{
						"network": map[string]any{
							"connection": "DISABLED",
						},
					},
				},
			},
			"signOnMode": "OPENID_CONNECT",
			"status":     "ACTIVE",
			"visibility": map[string]any{
				"appLinks":          map[string]any{},
				"autoSubmitToolbar": false,
				"hide": map[string]any{
					"iOS": false,
					"web": false,
				},
			},
		},
		map[string]any{
			"_links": map[string]any{
				"accessPolicy": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxtgiXCFrw5d7",
				},
				"appLinks": []any{},
				"deactivate": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt3jKnnAnev5d7/lifecycle/deactivate",
				},
				"groups": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt3jKnnAnev5d7/groups",
				},
				"logo": []any{
					map[string]any{
						"href": "https://ok12static.oktacdn.com/assets/img/logos/okta-logo-end-user-dashboard.fc6d8fdbcb8cb4c933d009e71456cec6.svg",
						"name": "medium",
						"type": "image/png",
					},
				},
				"policies": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"PUT",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt3jKnnAnev5d7/policies",
				},
				"profileEnrollment": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxvl8fCoYO5d7",
				},
				"uploadLogo": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"POST",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt3jKnnAnev5d7/logo",
				},
				"users": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c2qt3jKnnAnev5d7/users",
				},
			},
			"accessibility": map[string]any{
				"selfService": false,
			},
			"created": "2023-02-14T20:45:10Z",
			"credentials": map[string]any{
				"signing": map[string]any{
					"kid": "Z_KshFi815TvPEOFAcKPSsR2gQAHRALctRPJM3f95Tg",
				},
				"userNameTemplate": map[string]any{
					"template": "${source.login}",
					"type":     "BUILT_IN",
				},
			},
			"features":    []any{},
			"id":          "0oa8c2qt3jKnnAnev5d7",
			"label":       "Okta Dashboard",
			"lastUpdated": "2023-02-14T20:45:10Z",
			"name":        "okta_enduser",
			"settings": map[string]any{
				"app": map[string]any{},
				"notifications": map[string]any{
					"vpn": map[string]any{
						"network": map[string]any{
							"connection": "DISABLED",
						},
					},
				},
			},
			"signOnMode": "OPENID_CONNECT",
			"status":     "ACTIVE",
			"visibility": map[string]any{
				"appLinks":          map[string]any{},
				"autoSubmitToolbar": false,
				"hide": map[string]any{
					"iOS": false,
					"web": false,
				},
			},
		},
		map[string]any{
			"_links": map[string]any{
				"accessPolicy": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxroCog5VS5d7",
				},
				"appLinks": []any{
					map[string]any{
						"href": "https://dev-09267642.okta.com/home/oidc_client/0oa8c98a57zeflTwg5d7/aln177a159h7Zf52X0g8",
						"name": "oidc_client_link",
						"type": "text/html",
					},
				},
				"clientCredentials": []any{
					map[string]any{
						"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c98a57zeflTwg5d7/credentials/jwks",
						"name": "jwks",
					},
				},
				"deactivate": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c98a57zeflTwg5d7/lifecycle/deactivate",
				},
				"groups": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c98a57zeflTwg5d7/groups",
				},
				"logo": []any{
					map[string]any{
						"href": "https://ok12static.oktacdn.com/assets/img/logos/default.6770228fb0dab49a1695ef440a5279bb.png",
						"name": "medium",
						"type": "image/png",
					},
				},
				"policies": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"PUT",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c98a57zeflTwg5d7/policies",
				},
				"profileEnrollment": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/policies/rst8c5cgxvl8fCoYO5d7",
				},
				"uploadLogo": map[string]any{
					"hints": map[string]any{
						"allow": []any{
							"POST",
						},
					},
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c98a57zeflTwg5d7/logo",
				},
				"users": map[string]any{
					"href": "https://dev-09267642.okta.com/api/v1/apps/0oa8c98a57zeflTwg5d7/users",
				},
			},
			"accessibility": map[string]any{
				"selfService": false,
			},
			"created": "2023-02-15T07:25:01Z",
			"credentials": map[string]any{
				"oauthClient": map[string]any{
					"autoKeyRotation":            true,
					"client_id":                  "0oa8c98a57zeflTwg5d7",
					"pkce_required":              false,
					"token_endpoint_auth_method": "private_key_jwt",
				},
				"signing": map[string]any{
					"kid": "Z_KshFi815TvPEOFAcKPSsR2gQAHRALctRPJM3f95Tg",
				},
				"userNameTemplate": map[string]any{
					"template": "${source.login}",
					"type":     "BUILT_IN"},
			},
			"features":    []any{},
			"id":          "0oa8c98a57zeflTwg5d7",
			"label":       "My API Services App Private Key",
			"lastUpdated": "2023-02-15T07:27:04Z",
			"name":        "oidc_client",
			"settings": map[string]any{
				"app":   map[string]any{},
				"notes": map[string]any{},
				"notifications": map[string]any{
					"vpn": map[string]any{
						"network": map[string]any{
							"connection": "DISABLED",
						},
					},
				},
				"oauthClient": map[string]any{
					"application_type": "service",
					"consent_method":   "REQUIRED",
					"grant_types": []any{
						"client_credentials",
					},
					"idp_initiated_login": map[string]any{
						"default_scope": []any{},
						"mode":          "DISABLED",
					},
					"issuer_mode": "DYNAMIC",
					"jwks": map[string]any{
						"keys": []any{
							map[string]any{
								"e":      "AQAB",
								"id":     "pks8c98z2gb0LXZQq5d7",
								"kid":    "4S6FQbaNJ8mjkxwrrGi2IZQqZLf-HBb-sTc1WoCpPgQ",
								"kty":    "RSA",
								"n":      "z1C2HzixXcykg0E16cZm4iaX5WNq2t8XKNMm7TXU1ewhcPPFaOqaGLUdi4XaoM-oXIwOsnS2F0Nf-tAc_F9NQ22Xl-2PaW2x3QYz4fPSIj8pi0BdpNuuk1By1okAzuE0PW_3BzGCO7Doc16Y73OthW1b8Nm0RpNJ6YKN0pO4reScukaxPexfT7f8FNJM0s-8vrgXwn-hMI911V9SIBA3NcWcDX3AOHWi9AoYlVfbm2Bfqngi8Z_XYXxjgaBODUxnl-8b5HXFuGByMO7JL9NKN24VM8ReNrL2oPPwf6pOL7M1Zp6qUYJPXHvbpNxatc03wHRDRaFhKeHpjTLuzthTSw",
								"status": "ACTIVE",
							},
						},
					},
					"redirect_uris": []any{},
					"response_types": []any{
						"token",
					},
					"wildcard_redirect": "DISABLED",
				},
			},
			"signOnMode": string("OPENID_CONNECT"),
			"status":     string("ACTIVE"),
			"visibility": map[string]any{
				"appLinks":          map[string]any{"oidc_client_link": bool(true)},
				"autoLaunch":        bool(false),
				"autoSubmitToolbar": bool(false),
				"hide":              map[string]any{"iOS": bool(true), "web": bool(true)},
			},
		},
	}
)

func JSONResponder(data []byte) httpmock.Responder {
	resp := httpmock.NewBytesResponse(http.StatusOK, data)
	resp.Header.Set("Content-Type", "application/json")
	return httpmock.ResponderFromResponse(resp)
}

func TestOKTAData(t *testing.T) {
	for _, tt := range []struct {
		name         string
		config       string
		expectedData map[string]any
		responders   []Responder
	}{
		{
			name: "token",
			config: `
plugins:
  data:
    okta.placeholder:
      type: okta
      url: https://okta.local
      token: test-token
      users: true
      groups: true
      roles: true
      apps: true
`,
			responders: []Responder{
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/users",
					Responder: JSONResponder(users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups",
					Responder: JSONResponder(groups),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups/00g8c2qsz3rt2VcB85d7/users",
					Responder: JSONResponder(group00g8c2qsz3rt2VcB85d7Users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/iam/roles",
					Responder: JSONResponder(roles),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/apps",
					Responder: JSONResponder(apps),
				},
			},
			expectedData: map[string]any{
				"users":         expectedUsers,
				"groups":        expectedGroups,
				"group-members": expectedGroupMembers,
				"roles":         expectedRoles,
				"apps":          expectedApps,
			},
		},
		{
			name: "private_key_old_format",
			config: `
plugins:
  data:
    okta.placeholder:
      type: okta
      url: https://okta.local
      client_id: test-client-id
      private_key: testdata/private_key_old.txt
      private_key_id: test-private-key-id
      users: true
      groups: true
      roles: true
      apps: true
`,
			responders: []Responder{
				{
					Method:    http.MethodPost,
					URL:       `=~https://okta.local/oauth2/v1/token\?client_assertion=.+&client_assertion_type=urn%3Aietf%3Aparams%3Aoauth%3Aclient-assertion-type%3Ajwt-bearer&grant_type=client_credentials&scope=okta.users.read\+okta.groups.read\+okta.roles.read\+okta.apps.read`,
					Responder: JSONResponder(accessToken),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/users",
					Responder: JSONResponder(users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups",
					Responder: JSONResponder(groups),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups/00g8c2qsz3rt2VcB85d7/users",
					Responder: JSONResponder(group00g8c2qsz3rt2VcB85d7Users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/iam/roles",
					Responder: JSONResponder(roles),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/apps",
					Responder: JSONResponder(apps),
				},
			},
			expectedData: map[string]any{
				"users":         expectedUsers,
				"groups":        expectedGroups,
				"group-members": expectedGroupMembers,
				"roles":         expectedRoles,
				"apps":          expectedApps,
			},
		},
		{
			name: "private_key_new_format",
			config: `
plugins:
  data:
    okta.placeholder:
      type: okta
      url: https://okta.local
      client_id: test-client-id
      private_key: testdata/private_key_new.txt
      private_key_id: test-private-key-id
      users: true
      groups: true
      roles: true
      apps: true
`,
			responders: []Responder{
				{
					Method:    http.MethodPost,
					URL:       `=~https://okta.local/oauth2/v1/token\?client_assertion=.+&client_assertion_type=urn%3Aietf%3Aparams%3Aoauth%3Aclient-assertion-type%3Ajwt-bearer&grant_type=client_credentials&scope=okta.users.read\+okta.groups.read\+okta.roles.read\+okta.apps.read`,
					Responder: JSONResponder(accessToken),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/users",
					Responder: JSONResponder(users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups",
					Responder: JSONResponder(groups),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups/00g8c2qsz3rt2VcB85d7/users",
					Responder: JSONResponder(group00g8c2qsz3rt2VcB85d7Users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/iam/roles",
					Responder: JSONResponder(roles),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/apps",
					Responder: JSONResponder(apps),
				},
			},
			expectedData: map[string]any{
				"users":         expectedUsers,
				"groups":        expectedGroups,
				"group-members": expectedGroupMembers,
				"roles":         expectedRoles,
				"apps":          expectedApps,
			},
		},
		{
			name: "client_secret",
			config: `
plugins:
  data:
    okta.placeholder:
      type: okta
      url: https://okta.local
      client_id: test-client-id
      client_secret: test-client-secret
      users: true
      groups: true
      roles: true
      apps: true
`,
			responders: []Responder{
				{
					Method:    http.MethodPost,
					URL:       `https://okta.local/oauth2/v1/token`,
					Responder: JSONResponder(accessToken),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/users",
					Responder: JSONResponder(users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups",
					Responder: JSONResponder(groups),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/groups/00g8c2qsz3rt2VcB85d7/users",
					Responder: JSONResponder(group00g8c2qsz3rt2VcB85d7Users),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/iam/roles",
					Responder: JSONResponder(roles),
				},
				{
					Method:    http.MethodGet,
					URL:       "https://okta.local/api/v1/apps",
					Responder: JSONResponder(apps),
				},
			},
			expectedData: map[string]any{
				"users":         expectedUsers,
				"groups":        expectedGroups,
				"group-members": expectedGroupMembers,
				"roles":         expectedRoles,
				"apps":          expectedApps,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("github.com/patrickmn/go-cache.(*janitor).Run"))

			httpmock.Activate()
			defer httpmock.DeactivateAndReset()
			for _, r := range tt.responders {
				httpmock.RegisterResponder(r.Method, r.URL, r.Responder)
			}

			ctx := context.Background()
			store := inmem.New()
			mgr := pluginMgr(t, store, tt.config)

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer mgr.Stop(ctx)

			waitForStorePath(ctx, t, store, "/okta/placeholder")
			act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/okta/placeholder"))
			if err != nil {
				t.Fatalf("read back data: %v", err)
			}
			if diff := cmp.Diff(tt.expectedData, act); diff != "" {
				t.Errorf("data value mismatch, diff:\n%s", diff)
			}
		})
	}
}

func TestOKTAOwned(t *testing.T) {
	config := `
plugins:
  data:
    okta.placeholder:
      type: okta
      url: https://okta.local
      token: test-token
      users: true
      groups: true
      roles: true
      apps: true
`
	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("github.com/patrickmn/go-cache.(*janitor).Run"))

	ctx := context.Background()
	store := inmem.New()
	mgr := pluginMgr(t, store, config)

	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/okta/placeholder"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/okta/placeholder" is owned by plugin "okta"` {
		t.Errorf("owned check failed, got %v", err)
	}
}

func pluginMgr(t *testing.T, store storage.Store, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	opts := []func(*plugins.Manager){
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{data.Name: data.Factory()}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}

func waitForStorePath(ctx context.Context, t *testing.T, store storage.Store, path string) {
	t.Helper()
	if err := util.WaitFunc(func() bool {
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath(path))
		if err != nil {
			if storage.IsNotFound(err) {
				return false
			}
			t.Fatalf("read back data: %v", err)
		}
		if cmp.Diff(map[string]any{}, act) == "" { // empty obj
			return false
		}
		return true
	}, 200*time.Millisecond, 10*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}
