package deploymentidentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	awsRoleType                     = "aws:iam/role:Role"
	awsRolePolicyType               = "aws:iam/rolePolicy:RolePolicy"
	awsPolicyType                   = "aws:iam/policy:Policy"
	awsRolePolicyAttachmentType     = "aws:iam/rolePolicyAttachment:RolePolicyAttachment"
	awsPolicyAttachmentType         = "aws:iam/policyAttachment:PolicyAttachment"
	awsRolePoliciesExclusiveType    = "aws:iam/rolePoliciesExclusive:RolePoliciesExclusive"
	awsRoleAttachmentsExclusiveType = "aws:iam/rolePolicyAttachmentsExclusive:RolePolicyAttachmentsExclusive"
)

// PolicyExpectation binds validation to the exact identity this stack intends
// to create. A policy pack should configure these values explicitly.
type PolicyExpectation struct {
	ProviderARN string
	Audience    string
	Subject     string
}

// Resource is the small provider-neutral surface needed by the pure resource
// validator. Properties are the resolved Pulumi resource properties.
type Resource struct {
	Type       string
	Properties map[string]any
}

// ValidateResource checks IAM resources in a deployment-identity stack. It
// returns violations rather than mutating resources. Unknown resource types
// are ignored; attach/override resources that can bypass the role's explicit
// policy are rejected.
func ValidateResource(resource Resource, expected PolicyExpectation) []string {
	var violations []string
	if err := validateExpectation(expected); err != nil {
		return []string{"invalid deployment identity policy configuration"}
	}
	switch resource.Type {
	case awsRoleType:
		trust, err := decodeDocument(resource.Properties["assumeRolePolicy"])
		if err != nil || !validTrustPolicy(trust, expected) {
			violations = append(violations, "IAM role trust must match the exact configured GitHub OIDC provider, audience, and subject")
		}
		if raw, exists := resource.Properties["managedPolicyArns"]; exists {
			arns := stringList(raw)
			if arns == nil && raw != nil || len(arns) > 0 {
				violations = append(violations, "managed policy attachments bypass explicit deployment permission validation")
			}
		}
		for _, key := range []string{"inlinePolicies", "inlinePolicy"} {
			if value, ok := resource.Properties[key]; ok {
				policies, err := inlinePolicyDocuments(value)
				if err != nil {
					violations = append(violations, "IAM role inline policy properties are malformed")
					continue
				}
				for _, policy := range policies {
					if err := validatePermissionsDocument(policy); err != nil {
						violations = append(violations, "IAM role inline policy is broader than exact resource permissions")
					}
				}
			}
		}
	case awsRolePolicyType, awsPolicyType:
		policy, err := decodeDocument(resource.Properties["policy"])
		if err != nil || validatePermissionsDocument(policy) != nil {
			violations = append(violations, "IAM permission policy must use explicit actions and exact resource ARNs")
		}
	case awsRolePolicyAttachmentType, awsPolicyAttachmentType, awsRolePoliciesExclusiveType, awsRoleAttachmentsExclusiveType:
		violations = append(violations, "IAM policy attachment or exclusive-policy resource bypasses explicit deployment permission validation")
	}
	return violations
}

func validateExpectation(expected PolicyExpectation) error {
	if !awsOIDCProviderARN.MatchString(expected.ProviderARN) {
		return errors.New("invalid GitHub OIDC provider ARN")
	}
	if expected.Audience != GitHubOIDCAudience {
		return errors.New("invalid GitHub OIDC audience")
	}
	if !validExpectedSubject(expected.Subject) {
		return errors.New("invalid exact GitHub subject")
	}
	return nil
}

func validTrustPolicy(document map[string]any, expected PolicyExpectation) bool {
	if document["Version"] != "2012-10-17" || len(document) != 2 {
		return false
	}
	statements, ok := objectList(document["Statement"])
	if !ok || len(statements) != 1 {
		return false
	}
	statement := statements[0]
	if len(statement) != 4 && len(statement) != 5 {
		return false
	}
	if statement["Effect"] != "Allow" || statement["Action"] != "sts:AssumeRoleWithWebIdentity" {
		return false
	}
	principal, ok := object(statement["Principal"])
	if !ok || principal["Federated"] != expected.ProviderARN || len(principal) != 1 {
		return false
	}
	condition, ok := object(statement["Condition"])
	if !ok || len(condition) != 1 {
		return false
	}
	equals, ok := object(condition["StringEquals"])
	if !ok || len(equals) != 2 {
		return false
	}
	return equals[GitHubOIDCProviderHost+":aud"] == expected.Audience && equals[GitHubOIDCProviderHost+":sub"] == expected.Subject
}

func validExpectedSubject(subject string) bool {
	if !validExactValue(subject) || strings.ContainsAny(subject, "*?") || !strings.HasPrefix(subject, "repo:") {
		return false
	}
	parts := strings.SplitN(subject, ":", 3)
	if len(parts) != 3 {
		return false
	}
	repository := strings.Split(parts[1], "/")
	if len(repository) != 2 || !validSubjectName(repository[0]) || !validSubjectName(repository[1]) {
		return false
	}
	switch {
	case strings.HasPrefix(parts[2], "ref:"):
		return validGitHubRef(strings.TrimPrefix(parts[2], "ref:"))
	case strings.HasPrefix(parts[2], "environment:"):
		return validExactValue(strings.TrimPrefix(parts[2], "environment:"))
	default:
		return false
	}
}

func validSubjectName(value string) bool {
	parts := strings.Split(value, "@")
	if len(parts) == 1 {
		return validGitHubName(value)
	}
	return len(parts) == 2 && validGitHubName(parts[0]) && decimalID.MatchString(parts[1])
}

func validatePermissionsPolicy(input string) error {
	document, err := decodeDocument(input)
	if err != nil {
		return fmt.Errorf("must be a valid JSON IAM policy")
	}
	return validatePermissionsDocument(document)
}

func validatePermissionsDocument(document map[string]any) error {
	if document["Version"] != "2012-10-17" {
		return errors.New("IAM policy version must be 2012-10-17")
	}
	statements, ok := objectList(document["Statement"])
	if !ok || len(statements) == 0 {
		return errors.New("IAM policy must have at least one statement")
	}
	allowCount := 0
	for _, statement := range statements {
		effect, ok := statement["Effect"].(string)
		if !ok || effect != "Allow" && effect != "Deny" {
			return errors.New("IAM policy statement has invalid effect")
		}
		if effect == "Deny" {
			continue
		}
		allowCount++
		if _, exists := statement["Principal"]; exists {
			return errors.New("identity permission policies cannot declare a principal")
		}
		if _, exists := statement["NotAction"]; exists {
			return errors.New("NotAction is not allowed in deployment permission policy")
		}
		if _, exists := statement["NotResource"]; exists {
			return errors.New("NotResource is not allowed in deployment permission policy")
		}
		actions := stringList(statement["Action"])
		if len(actions) == 0 {
			return errors.New("Allow statement must name explicit actions")
		}
		for _, action := range actions {
			if action == "" || strings.ContainsAny(action, "*?") || !strings.Contains(action, ":") {
				return errors.New("Allow statement action must be exact")
			}
		}
		resources := stringList(statement["Resource"])
		if len(resources) == 0 {
			return errors.New("Allow statement must name exact resource ARNs")
		}
		for _, resource := range resources {
			if !validExactARN(resource) {
				return errors.New("Allow statement resource must be an exact ARN without wildcards")
			}
		}
	}
	if allowCount == 0 {
		return errors.New("IAM policy must have at least one Allow statement")
	}
	return nil
}

func validExactARN(value string) bool {
	if !validExactValue(value) || strings.ContainsAny(value, "*?") || !strings.HasPrefix(value, "arn:") {
		return false
	}
	parts := strings.SplitN(value, ":", 6)
	return len(parts) == 6 && parts[1] != "" && parts[2] != "" && parts[5] != ""
}

func canonicalJSON(input string) (string, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", errors.New("multiple JSON values")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeDocument(value any) (map[string]any, error) {
	var encoded []byte
	var err error
	switch typed := value.(type) {
	case string:
		encoded = []byte(typed)
	case json.RawMessage:
		encoded = []byte(typed)
	case []byte:
		encoded = typed
	default:
		encoded, err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("expected JSON object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("multiple JSON values")
	}
	return document, nil
}

func object(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func objectList(value any) ([]map[string]any, bool) {
	switch typed := value.(type) {
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			entry, ok := object(item)
			if !ok {
				return nil, false
			}
			result = append(result, entry)
		}
		return result, true
	case map[string]any:
		return []map[string]any{typed}, true
	default:
		return nil, false
	}
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				return nil
			}
			result = append(result, str)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return nil
	}
}

func inlinePolicyDocuments(value any) ([]map[string]any, error) {
	entries, ok := objectList(value)
	if !ok {
		return nil, errors.New("expected inline policy list")
	}
	policies := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		policy, err := decodeDocument(entry["policy"])
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, nil
}
