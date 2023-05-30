package trial

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"

	"github.com/styrainc/load-private/pkg/tui"
)

type RunTrialArgs struct {
	Input   Input
	KeyOnly bool
	StdOut  io.Writer
	Client  Client
	RunForm tui.RunForm
}

func Run(args RunTrialArgs) error {
	err := interactiveFill(&args.Input, args.RunForm)
	if !args.KeyOnly {
		// unless the output is key only, always add some extra spacing after
		// the form runs. This looks better overall even when no interactive
		// input is needed.
		fmt.Fprintln(args.StdOut)
	}
	if err != nil {
		return err
	}
	result, err := args.Client.Create(args.Input)
	if err != nil {
		return fmt.Errorf("trial creation failed: %s", err)
	}
	result.Output(args.StdOut, args.KeyOnly)
	return nil
}

type Client struct {
	url string

	httpClient *http.Client
}

func NewClient(url string) Client {
	return Client{
		url:        url,
		httpClient: &http.Client{},
	}
}

func (tc *Client) Create(input Input) (Result, error) {
	var result Result

	body := new(bytes.Buffer)
	err := json.NewEncoder(body).Encode(input)
	if err != nil {
		return result, fmt.Errorf("could not encode body: %w", err)
	}
	requestURL, err := url.JoinPath(tc.url, "register", "trial")
	if err != nil {
		return result, fmt.Errorf("could not join URL path: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, requestURL, body)
	if err != nil {
		return result, fmt.Errorf("could not create http request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")

	response, err := tc.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("error making http request: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		responseBody, err := io.ReadAll(response.Body)
		defer response.Body.Close()

		if err != nil {
			responseBody = []byte(fmt.Sprintf("unable to read response body: %s", err))
		}

		return result, fmt.Errorf("%d: %s", response.StatusCode, responseBody)
	}

	err = json.NewDecoder(response.Body).Decode(&result)
	if err != nil {
		return result, fmt.Errorf("could not decode response: %w", err)
	}
	return result, nil
}

type Input struct {
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Email     string `json:"email,omitempty"`
	Company   string `json:"company,omitempty"`
	Country   string `json:"country,omitempty"`
	Duration  int    `json:"duration,omitempty"`
}

func interactiveFill(input *Input, runForm tui.RunForm) (err error) {
	fillFn := func(value string, prompt string, validator tui.InputValidator) (string, error) {
		if value != "" {
			return value, nil
		}
		element := tui.NewTextInput(prompt).WithValidation(validator)
		return runForm(element)
	}
	input.FirstName, err = fillFn(input.FirstName, "First Name:", validateNotEmpty)
	if err != nil {
		return err
	}
	input.LastName, err = fillFn(input.LastName, "Last Name:", validateNotEmpty)
	if err != nil {
		return err
	}
	input.Email, err = fillFn(input.Email, "Email:", validateEmail)
	if err != nil {
		return err
	}
	input.Company, err = fillFn(input.Company, "Company:", validateNotEmpty)
	if err != nil {
		return err
	}
	input.Country, err = fillFn(input.Country, "Country:", validateNotEmpty)
	if err != nil {
		return err
	}

	return nil
}

type Result struct {
	Key     string `json:"key"`
	Message string `json:"message"`
}

func (tr *Result) Output(w io.Writer, keyOnly bool) {
	if keyOnly {
		fmt.Fprintln(w, tr.Key)
	} else {
		fmt.Fprintln(w, tr.Message)
	}
}

func validateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	return err
}

func validateNotEmpty(value string) error {
	if value == "" {
		return errors.New("value is required")
	}
	return nil
}
