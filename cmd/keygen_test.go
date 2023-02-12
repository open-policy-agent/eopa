package cmd

import (
	"testing"

	"github.com/keygen-sh/keygen-go/v2"
	"gopkg.in/h2non/gock.v1"
)

func setupKeygen(expiry string, code string) {

	// Intercept Keygen's HTTP client
	gock.InterceptClient(keygen.HTTPClient)

	if testing.Verbose() {
		gock.Observe(gock.DumpRequest)
	}

	// Mock endpoints
	gock.New("https://api.keygen.sh").
		Get(`/v1/accounts/([^\/]+)/me`).
		Reply(200).
		SetHeader("Keygen-Signature", `keyid="1fddcec8-8dd3-4d8d-9b16-215cac0f9b52", algorithm="ed25519", signature="IiyYX1ah2HFzbcCx+3sv+KJpOppFdMRuZ7NWlnwZMKAf5khj9c4TO4z6fr62BqNXlyROOTxZinX8UpXHJHVyAw==", headers="(request-target) host date digest"`).
		SetHeader("Digest", "sha-256=d4uZ26hjiUNqopuSkYcYwg2aBuNtr4D1/9iDhlvf0H8=").
		SetHeader("Date", "Wed, 15 Jun 3022 18:52:14 GMT").
		BodyString(`{"data":{"id":"218810ed-2ac8-4c26-a725-a6da67500561","type":"licenses","attributes":{"name":"Demo License","key":"C1B6DE-39A6E3-DE1529-8559A0-4AF593-V3","expiry":null,"status":"ACTIVE","uses":0,"suspended":false,"scheme":null,"encrypted":false,"strict":false,"floating":false,"concurrent":false,"protected":true,"maxMachines":1,"maxProcesses":null,"maxCores":null,"maxUses":null,"requireHeartbeat":false,"requireCheckIn":false,"lastValidated":"2022-06-15T18:52:12.068Z","lastCheckIn":null,"nextCheckIn":null,"metadata":{"email":"user@example.com"},"created":"2020-09-14T21:18:08.990Z","updated":"2022-06-15T18:52:12.073Z"},"relationships":{"account":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52"},"data":{"type":"accounts","id":"1fddcec8-8dd3-4d8d-9b16-215cac0f9b52"}},"product":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/product"},"data":{"type":"products","id":"ef6e0993-70d6-42c4-a0e8-846cb2e3fa54"}},"policy":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/policy"},"data":{"type":"policies","id":"629307fb-331d-430b-978a-44d45d9de133"}},"group":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/group"},"data":null},"user":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/user"},"data":null},"machines":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/machines"},"meta":{"cores":0,"count":1}},"tokens":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/tokens"}},"entitlements":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/entitlements"}}},"links":{"self":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561"}}}`)

	gock.New("https://api.keygen.sh").
		Post(`/v1/accounts/([^\/]+)/licenses/([^\/]+)/actions/validate`).
		Reply(200).
		SetHeader("Keygen-Signature", `keyid="1fddcec8-8dd3-4d8d-9b16-215cac0f9b52", algorithm="ed25519", signature="z5zckjhvw88ZZQG3/TitNyDMjtWQajzwM6WPX4bQZnjvbfAqJthhCP5A6fubuYTJznow5FpsE5+zicJY+e6qCQ==", headers="(request-target) host date digest"`).
		SetHeader("Digest", "sha-256=Whz4/RQLcj8UMvLMumkbblZm3L8mvYR34kXwq5Cf6YQ=").
		SetHeader("Date", "Wed, 15 Jun 2022 18:52:16 GMT").
		BodyString(`{"data":{"id":"218810ed-2ac8-4c26-a725-a6da67500561","type":"licenses","attributes":{"name":"Demo License","key":"C1B6DE-39A6E3-DE1529-8559A0-4AF593-V3","expiry":` + expiry + `,"status":"ACTIVE","uses":0,"suspended":false,"scheme":null,"encrypted":false,"strict":false,"floating":false,"concurrent":false,"protected":true,"maxMachines":1,"maxProcesses":null,"maxCores":null,"maxUses":null,"requireHeartbeat":false,"requireCheckIn":false,"lastValidated":"2022-06-15T18:52:16.115Z","lastCheckIn":null,"nextCheckIn":null,"metadata":{"email":"user@example.com"},"created":"2020-09-14T21:18:08.990Z","updated":"2022-06-15T18:52:16.121Z"},"relationships":{"account":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52"},"data":{"type":"accounts","id":"1fddcec8-8dd3-4d8d-9b16-215cac0f9b52"}},"product":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/product"},"data":{"type":"products","id":"ef6e0993-70d6-42c4-a0e8-846cb2e3fa54"}},"policy":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/policy"},"data":{"type":"policies","id":"629307fb-331d-430b-978a-44d45d9de133"}},"group":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/group"},"data":null},"user":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/user"},"data":null},"machines":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/machines"},"meta":{"cores":0,"count":1}},"tokens":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/tokens"}},"entitlements":{"links":{"related":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561/entitlements"}}},"links":{"self":"/v1/accounts/1fddcec8-8dd3-4d8d-9b16-215cac0f9b52/licenses/218810ed-2ac8-4c26-a725-a6da67500561"}},"meta":{"ts":"2022-06-15T18:52:16.126Z","valid":true,"detail":"is valid","code":` + code + `}}`)

	gock.New("https://api.keygen.sh").
		Post(`/v1/accounts/([^\/]+)/machines`).
		Reply(200)

	gock.New("https://api.keygen.sh").
		Post(`/v1/accounts/([^\/]+)/machines//actions/ping`).
		Reply(200)

	gock.New("https://api.keygen.sh").
		Delete(`/v1/accounts/([^\/]+)/machines/([^\/]+)`).
		Reply(200)

	gock.New("https://api.keygenx.sh").
		Get(`/v1/me`).
		Reply(429).
		BodyString(`429 Too Many Requests`)
}

func TestKeygen(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()
	keygen.PublicKey = "" // don't validate the signature

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"NO_MACHINE\"")

	var result int
	license.ValidateLicense("", "", func(code int, err error) { result = code })
	if result != 0 {
		t.Fatalf("Invalid result %d", result)
	}
	license.ReleaseLicense()
}

func TestKeygenExpiry(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()
	keygen.PublicKey = "" // don't validate the signature

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("null", "\"NO_MACHINE\"")

	var result int
	license.ValidateLicense("", "", func(code int, err error) { result = code })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
}

func TestKeygenExpired(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()
	keygen.PublicKey = "" // don't validate the signature

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"EXPIRED\"")

	var result int
	license.ValidateLicense("", "", func(code int, err error) { result = code })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
}

func TestKeygenToken(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "")
	t.Setenv("STYRA_LOAD_LICENSE_TOKEN", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()
	keygen.PublicKey = "" // don't validate the signature

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"NOT_FOUND\"")

	var result int
	license.ValidateLicense("", "", func(code int, err error) { result = code })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
}

func TestKeygenSignature(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "")
	t.Setenv("STYRA_LOAD_LICENSE_TOKEN", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"NOT_FOUND\"")

	var result int
	license.ValidateLicense("", "", func(code int, err error) { result = code })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
}

func TestKeygenValid(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "")
	t.Setenv("STYRA_LOAD_LICENSE_TOKEN", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()
	keygen.PublicKey = "" // don't validate the signature

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"VALID\"")

	var result int
	license.ValidateLicense("", "", func(code int, err error) { result = code })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
}

func TestKeygenRateLimit(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "7F08EC-970E9D-B0214E-4CF0C7-354C97-V3")
	license := NewLicense()
	keygen.APIURL = "https://api.keygenx.sh" // simulate RateLimitExceeded

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"VALID\"")

	var result int
	var err error
	license.ValidateLicense("", "", func(code int, lerr error) { result, err = code, lerr })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}

	expected := "invalid license: rate limit has been exceeded"
	if err.Error() != expected {
		t.Fatalf("expected: %v, got %v", expected, err.Error())
	}
}

func TestKeygenOffline(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "key/7F08EC970E9D.B0214E4CF0C7354C97")
	license := NewLicense()
	keygen.APIURL = "https://api.keygenx.sh" // simulate RateLimitExceeded

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"VALID\"")

	var result int
	var err error
	expected := "off-line license verification failed: license key is not genuine"
	license.ValidateLicense("", "", func(code int, lerr error) { result, err = code, lerr })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
	if err.Error() != expected {
		t.Fatalf("expected: %v, got %v", expected, err.Error())
	}
}

func TestKeygenOfflineExpired(t *testing.T) {
	t.Setenv("STYRA_LOAD_LICENSE_KEY", "key/eyJhY2NvdW50Ijp7ImlkIjoiZGQwMTA1ZDEtOTU2NC00ZjU4LWFlMWMtOWRlZmRkMGJmZWE3In0sInByb2R1Y3QiOnsiaWQiOiJmN2RhNGFlNS03YmY1LTQ2ZjYtOTYzNC0wMjZiZWM1ZTg1OTkifSwicG9saWN5Ijp7ImlkIjoiZTVjYjZmMTgtZTVjOS00OTJjLTgyMmYtMDFiYzUxNjYxNmI2IiwiZHVyYXRpb24iOjI1OTIwMDB9LCJ1c2VyIjpudWxsLCJsaWNlbnNlIjp7ImlkIjoiYWJmNWMxYWItODYwYy00NzUxLTlhODItNTc5Mjk0OWIxNjFlIiwiY3JlYXRlZCI6IjIwMjMtMDItMTJUMTc6MzM6MjIuNzcxWiIsImV4cGlyeSI6IjIwMjMtMDItMDFUMDA6MDA6MDAuMDAwWiJ9fQ==.2NLHJjiAiXkO7HsBoQFrmXG32gC0ZH9SDxUEcacqqHPgvZq0RcczFV603XuJ7mzAtN5OEPa6XoETksjsBteqCQ==")
	license := NewLicense()
	keygen.APIURL = "https://api.keygenx.sh" // simulate RateLimitExceeded

	defer gock.Off()
	defer gock.RestoreClient(keygen.HTTPClient)

	setupKeygen("\"3023-09-14T21:18:08.990Z\"", "\"VALID\"")

	var result int
	var err error
	expected := "off-line license verification failed: license expired 2023-02-01 00:00:00 +0000 UTC"
	license.ValidateLicense("", "", func(code int, lerr error) { result, err = code, lerr })
	if result != 2 {
		t.Fatalf("Invalid result %d", result)
	}
	if err.Error() != expected {
		t.Fatalf("expected: %v, got %v", expected, err.Error())
	}
}
