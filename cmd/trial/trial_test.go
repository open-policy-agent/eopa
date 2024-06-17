package trial

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"gopkg.in/h2non/gock.v1"

	"github.com/styrainc/enterprise-opa-private/pkg/tui"
)

const (
	testKey     string = "EB237F-F4B803-G4G270-84C1D1-85GD0C-V3"
	testMessage string = `Your 14-day trial has been activated!

License Key: EB237F-F4B803-G4G270-84C1D1-85GD0C-V3

To evaluate Enterprise OPA, export this key in your shell configuration file:

export EOPA_LICENSE_KEY=EB237F-F4B803-G4G270-84C1D1-85GD0C-V3`
)

var defaultInput = Input{
	FirstName: "Alice",
	LastName:  "Bob",
	Email:     "alice.bob@styra.com",
	Company:   "Styra",
	Country:   "United States",
}

func TestRunTrial(t *testing.T) {
	cases := []struct {
		name     string
		input    Input
		keyOnly  bool
		expected string
		runForm  tui.RunForm
	}{
		{
			name:     "basic message",
			input:    defaultInput,
			keyOnly:  false,
			expected: testMessage,
		},
		{
			name:     "key only",
			input:    defaultInput,
			keyOnly:  true,
			expected: testKey,
		},
		{
			name: "interactive name",
			input: Input{
				LastName: defaultInput.LastName,
				Email:    defaultInput.Email,
				Company:  defaultInput.Company,
				Country:  defaultInput.Country,
			},
			runForm:  func(tui.FormElement) (string, error) { return defaultInput.FirstName, nil },
			keyOnly:  false,
			expected: testMessage,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer gock.Off()
			gock.New("http://example.com/").
				Post("register/trial").
				MatchHeader("Content-Type", "application/json").
				JSON(defaultInput).
				Reply(200).
				JSON(map[string]string{
					"key":     testKey,
					"message": testMessage,
				})
			buf := new(bytes.Buffer)

			err := Run(RunTrialArgs{
				Input:   tc.input,
				KeyOnly: tc.keyOnly,
				StdOut:  buf,
				Client:  NewClient("http://example.com"),
				RunForm: tc.runForm,
			})
			if err != nil {
				t.Fatalf("could not run trial command: %v", err)
			}

			result, err := io.ReadAll(buf)
			if err != nil {
				t.Fatalf("could not read buffer: %v", err)
			}

			if !strings.Contains(string(result), tc.expected) {
				t.Errorf("message did not contain expected value\n\nReceived:\n%s\n\nShould have contained:\n%s\n", string(result), tc.expected)
			}
		})
	}
}

func TestTrialClientHttpError(t *testing.T) {
	expectedError := "trial creation failed: 400: request failed"

	defer gock.Off()
	gock.New("http://example.com/").
		Post("register/trial").
		Reply(400).
		BodyString("request failed")

	err := Run(RunTrialArgs{
		Input:  defaultInput,
		Client: NewClient("http://example.com"),
		StdOut: io.Discard,
	})
	if err == nil {
		t.Fatal("expected trail creation to fail, but it succeeded")
	}

	if err.Error() != expectedError {
		t.Errorf("Expected HTTP error message %q but got %q", expectedError, err.Error())
	}
}

func TestTrialInteractiveError(t *testing.T) {
	expectedError := errors.New("trial creation failed: 400: request failed")

	input := Input{
		FirstName: defaultInput.FirstName,
		LastName:  defaultInput.LastName,
		Company:   defaultInput.Company,
	}

	runFormFunc := func(tui.FormElement) (string, error) {
		return "", expectedError
	}

	err := Run(RunTrialArgs{
		Input:   input,
		Client:  NewClient("http://example.com"),
		RunForm: runFormFunc,
		StdOut:  io.Discard,
	})
	if err == nil {
		t.Fatal("expected trail creation to fail, but it succeeded")
	}

	if !errors.Is(err, expectedError) {
		t.Errorf("Expected error %#v but got %#v", expectedError, err)
	}
}
