package kafka

import (
	"context"
	"errors"
	"fmt"

	"github.com/redpanda-data/benthos/v4/public/service"

	"github.com/styrainc/enterprise-opa-private/internal/benthos/impl/aws/config"

	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

func notImportedAWSFn(c *service.ParsedConfig) (sasl.Mechanism, error) {
	return nil, errors.New("unable to configure AWS SASL as this binary does not import components/aws")
}

// AWSSASLFromConfigFn is populated with the child `aws` package when imported.
var AWSSASLFromConfigFn = notImportedAWSFn

// SASLFields returns the SASL config fields.
func SASLFields() *service.ConfigField {
	return service.NewObjectListField("sasl",
		service.NewStringAnnotatedEnumField("mechanism", map[string]string{
			"none":          "Disable sasl authentication",
			"PLAIN":         "Plain text authentication.",
			"OAUTHBEARER":   "OAuth Bearer based authentication.",
			"SCRAM-SHA-256": "SCRAM based authentication as specified in RFC5802.",
			"SCRAM-SHA-512": "SCRAM based authentication as specified in RFC5802.",
			"AWS_MSK_IAM":   "AWS IAM based authentication as specified by the 'aws-msk-iam-auth' java library.",
		}).
			Description("The SASL mechanism to use."),
		service.NewStringField("username").
			Description("A username to provide for PLAIN or SCRAM-* authentication.").
			Default(""),
		service.NewStringField("password").
			Description("A password to provide for PLAIN or SCRAM-* authentication.").
			Default("").Secret(),
		service.NewStringField("token").
			Description("The token to use for a single session's OAUTHBEARER authentication.").
			Default(""),
		service.NewStringMapField("extensions").
			Description("Key/value pairs to add to OAUTHBEARER authentication requests.").
			Optional(),
		service.NewObjectField("aws", config.SessionFields()...).
			Description("Contains AWS specific fields for when the `mechanism` is set to `AWS_MSK_IAM`.").
			Optional(),
	).
		Description("Specify one or more methods of SASL authentication. SASL is tried in order; if the broker supports the first mechanism, all connections will use that mechanism. If the first mechanism fails, the client will pick the first supported mechanism. If the broker does not support any client mechanisms, connections will fail.").
		Advanced().Optional().
		Example(
			[]any{
				map[string]any{
					"mechanism": "SCRAM-SHA-512",
					"username":  "foo",
					"password":  "bar",
				},
			},
		)
}

// SASLMechanismsFromConfig constructs a sasl.Mechanism slice from a parsed config.
func SASLMechanismsFromConfig(c *service.ParsedConfig) ([]sasl.Mechanism, error) {
	if !c.Contains("sasl") {
		return nil, nil
	}

	sList, err := c.FieldObjectList("sasl")
	if err != nil {
		return nil, err
	}

	var mechanisms []sasl.Mechanism
	var mechanism sasl.Mechanism
	for i, mConf := range sList {
		mechStr, err := mConf.FieldString("mechanism")
		if err == nil {
			switch mechStr {
			case "", "none":
				continue
			case "PLAIN":
				mechanism, err = plainSaslFromConfig(mConf)
				mechanisms = append(mechanisms, mechanism)
			case "OAUTHBEARER":
				mechanism, err = oauthSaslFromConfig(mConf)
				mechanisms = append(mechanisms, mechanism)
			case "SCRAM-SHA-256":
				mechanism, err = scram256SaslFromConfig(mConf)
				mechanisms = append(mechanisms, mechanism)
			case "SCRAM-SHA-512":
				mechanism, err = scram512SaslFromConfig(mConf)
				mechanisms = append(mechanisms, mechanism)
			case "AWS_MSK_IAM":
				mechanism, err = AWSSASLFromConfigFn(mConf)
				mechanisms = append(mechanisms, mechanism)
			default:
				err = fmt.Errorf("unknown mechanism: %v", mechStr)
			}
		}
		if err != nil {
			if len(sList) == 1 {
				return nil, err
			}
			return nil, fmt.Errorf("mechanism %v: %w", i, err)
		}
	}

	return mechanisms, nil
}

func plainSaslFromConfig(c *service.ParsedConfig) (sasl.Mechanism, error) {
	username, err := c.FieldString("username")
	if err != nil {
		return nil, err
	}
	password, err := c.FieldString("password")
	if err != nil {
		return nil, err
	}
	return plain.Plain(func(c context.Context) (plain.Auth, error) {
		return plain.Auth{
			User: username,
			Pass: password,
		}, nil
	}), nil
}

func oauthSaslFromConfig(c *service.ParsedConfig) (sasl.Mechanism, error) {
	token, err := c.FieldString("token")
	if err != nil {
		return nil, err
	}
	var extensions map[string]string
	if c.Contains("extensions") {
		if extensions, err = c.FieldStringMap("extensions"); err != nil {
			return nil, err
		}
	}
	return oauth.Oauth(func(c context.Context) (oauth.Auth, error) {
		return oauth.Auth{
			Token:      token,
			Extensions: extensions,
		}, nil
	}), nil
}

func scram256SaslFromConfig(c *service.ParsedConfig) (sasl.Mechanism, error) {
	username, err := c.FieldString("username")
	if err != nil {
		return nil, err
	}
	password, err := c.FieldString("password")
	if err != nil {
		return nil, err
	}
	return scram.Sha256(func(c context.Context) (scram.Auth, error) {
		return scram.Auth{
			User: username,
			Pass: password,
		}, nil
	}), nil
}

func scram512SaslFromConfig(c *service.ParsedConfig) (sasl.Mechanism, error) {
	username, err := c.FieldString("username")
	if err != nil {
		return nil, err
	}
	password, err := c.FieldString("password")
	if err != nil {
		return nil, err
	}
	return scram.Sha512(func(c context.Context) (scram.Auth, error) {
		return scram.Auth{
			User: username,
			Pass: password,
		}, nil
	}), nil
}

//------------------------------------------------------------------------------

// SASL specific error types.
var (
	ErrUnsupportedSASLMechanism = errors.New("unsupported SASL mechanism")
)
