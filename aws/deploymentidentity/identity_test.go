package deploymentidentity

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	testProviderARN = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
	previewPolicy   = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["cloudformation:DescribeStacks"],"Resource":"arn:aws:cloudformation:us-east-1:123456789012:stack/example/abc"}]}`
	applyPolicy     = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["cloudformation:UpdateStack"],"Resource":"arn:aws:cloudformation:us-east-1:123456789012:stack/example/abc"}]}`
)

func validArgs() Args {
	return Args{
		RoleNamePrefix: "ferro-deploy", ProviderARN: testProviderARN, Audience: GitHubOIDCAudience,
		RepositoryOwner: "example-org", RepositoryName: "example-repo", Ref: "refs/heads/main", ApplyEnvironment: "production",
		PreviewPolicyJSON: previewPolicy, ApplyPolicyJSON: applyPolicy,
	}
}

func TestBuildSubjectRequiresExactRepositoryExecutionContext(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Args)
		want string
		bad  bool
	}{
		{name: "branch", want: "repo:example-org/example-repo:ref:refs/heads/main"},
		{name: "immutable branch", edit: func(a *Args) { a.OwnerID = "1001"; a.RepositoryID = "2002" }, want: "repo:example-org@1001/example-repo@2002:ref:refs/heads/main"},
		{name: "environment", edit: func(a *Args) { a.Ref = ""; a.Environment = "production:blue" }, want: "repo:example-org/example-repo:environment:production%3Ablue"},
		{name: "immutable environment", edit: func(a *Args) { a.OwnerID = "1001"; a.RepositoryID = "2002"; a.Ref = ""; a.Environment = "production" }, want: "repo:example-org@1001/example-repo@2002:environment:production"},
		{name: "wildcard repository", edit: func(a *Args) { a.RepositoryName = "*" }, bad: true},
		{name: "wildcard ref", edit: func(a *Args) { a.Ref = "refs/heads/release/*" }, bad: true},
		{name: "wildcard environment", edit: func(a *Args) { a.Ref = ""; a.Environment = "prod*" }, bad: true},
		{name: "both ref and environment", edit: func(a *Args) { a.Environment = "production" }, bad: true},
		{name: "neither ref nor environment", edit: func(a *Args) { a.Ref = "" }, bad: true},
		{name: "unpaired immutable IDs", edit: func(a *Args) { a.OwnerID = "1001" }, bad: true},
		{name: "wrong audience", edit: func(a *Args) { a.Audience = "https://github.com/example" }, bad: true},
		{name: "wrong provider", edit: func(a *Args) { a.ProviderARN = "arn:aws:iam::123456789012:oidc-provider/other.example" }, bad: true},
		{name: "wildcard permission resource", edit: func(a *Args) {
			a.ApplyPolicyJSON = strings.ReplaceAll(applyPolicy, "arn:aws:cloudformation:us-east-1:123456789012:stack/example/abc", "*")
		}, bad: true},
		{name: "wildcard action", edit: func(a *Args) {
			a.PreviewPolicyJSON = strings.ReplaceAll(previewPolicy, "cloudformation:DescribeStacks", "cloudformation:Describe*")
		}, bad: true},
		{name: "same preview and apply authority", edit: func(a *Args) { a.ApplyPolicyJSON = previewPolicy }, bad: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := validArgs()
			if tc.edit != nil {
				tc.edit(&args)
			}
			got, err := validateArgs("ferro-deploy", args)
			if tc.bad {
				if err == nil {
					t.Fatalf("expected validation failure, got subject %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("subject %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateResourceRejectsRawIAMRoleTrustAndPermissionBypass(t *testing.T) {
	args := validArgs()
	subject := buildSubject(args)
	expected := PolicyExpectation{ProviderARN: testProviderARN, Audience: GitHubOIDCAudience, Subject: subject}
	goodTrust, err := buildTrustPolicy(expected.ProviderARN, expected.Audience, expected.Subject)
	if err != nil {
		t.Fatal(err)
	}
	good := Resource{Type: awsRoleType, Properties: map[string]any{"assumeRolePolicy": goodTrust}}
	if violations := ValidateResource(good, expected); len(violations) != 0 {
		t.Fatalf("valid role rejected: %v", violations)
	}

	for _, tc := range []struct{ name, trust string }{
		{name: "wrong repository", trust: strings.ReplaceAll(goodTrust, "example-org/example-repo", "other-org/other-repo")},
		{name: "wrong audience", trust: strings.ReplaceAll(goodTrust, GitHubOIDCAudience, "other-audience")},
		{name: "wrong subject", trust: strings.ReplaceAll(goodTrust, "refs/heads/main", "refs/heads/other")},
		{name: "wildcard subject", trust: strings.ReplaceAll(goodTrust, "refs/heads/main", "refs/heads/*")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := Resource{Type: awsRoleType, Properties: map[string]any{"assumeRolePolicy": tc.trust}}
			if violations := ValidateResource(resource, expected); len(violations) == 0 {
				t.Fatal("unsafe raw role trust policy was accepted")
			}
		})
	}

	wide := `{"Version":"2012-10-17","Statement":{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}}`
	for _, resource := range []Resource{
		{Type: awsRoleType, Properties: map[string]any{"assumeRolePolicy": goodTrust, "inlinePolicies": []any{map[string]any{"name": "wide", "policy": wide}}}},
		{Type: awsRolePolicyType, Properties: map[string]any{"policy": wide}},
		{Type: awsPolicyType, Properties: map[string]any{"policy": wide}},
		{Type: awsRolePolicyAttachmentType, Properties: map[string]any{"role": "deploy", "policyArn": "arn:aws:iam::aws:policy/AdministratorAccess"}},
		{Type: awsRoleType, Properties: map[string]any{"assumeRolePolicy": goodTrust, "managedPolicyArns": []string{"arn:aws:iam::aws:policy/AdministratorAccess"}}},
	} {
		if violations := ValidateResource(resource, expected); len(violations) == 0 {
			t.Fatalf("unsafe raw permission resource %s was accepted", resource.Type)
		}
	}
}

func TestValidateResourceBindsExpectedProviderAndScope(t *testing.T) {
	args := validArgs()
	subject := buildSubject(args)
	trust, err := buildTrustPolicy(testProviderARN, GitHubOIDCAudience, subject)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		expected PolicyExpectation
		trust    string
	}{
		{name: "provider mismatch", expected: PolicyExpectation{ProviderARN: "arn:aws:iam::999999999999:oidc-provider/token.actions.githubusercontent.com", Audience: GitHubOIDCAudience, Subject: subject}, trust: trust},
		{name: "subject mismatch", expected: PolicyExpectation{ProviderARN: testProviderARN, Audience: GitHubOIDCAudience, Subject: "repo:other-org/other-repo:ref:refs/heads/main"}, trust: trust},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource := Resource{Type: awsRoleType, Properties: map[string]any{"assumeRolePolicy": tc.trust}}
			if violations := ValidateResource(resource, tc.expected); len(violations) == 0 {
				t.Fatal("mismatched expected trust accepted")
			}
		})
	}
}

type resourceMocks struct {
	mu        sync.Mutex
	resources []pulumi.MockResourceArgs
}

func (m *resourceMocks) Call(pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}
func (m *resourceMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, args)
	outputs := args.Inputs.Copy()
	if args.TypeToken == awsRoleType {
		outputs["arn"] = resource.NewStringProperty("arn:aws:iam::123456789012:role/" + args.Name)
	}
	return args.Name + "-id", outputs, nil
}

func TestComponentRegistersSeparateScopedPreviewAndApplyRoles(t *testing.T) {
	mocks := &resourceMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewGitHubActionsDeploymentIdentity(ctx, "deployment", validArgs())
		return err
	}, pulumi.WithMocks("deployment-identity", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	mocks.mu.Lock()
	defer mocks.mu.Unlock()
	roles, rolePolicies := 0, 0
	roleNames := map[string]bool{}
	componentURN := string(resource.NewURN("test", "deployment-identity", "", "ajent:aws:deploymentidentity:GitHubActionsDeploymentIdentity", "deployment"))
	for _, item := range mocks.resources {
		switch item.TypeToken {
		case awsRoleType:
			roles++
			if item.RegisterRPC == nil || item.RegisterRPC.GetParent() != componentURN || componentURN == "" {
				t.Fatalf("role %s is not parented by the deployment component", item.Name)
			}
			name := item.Inputs["name"].StringValue()
			roleNames[name] = true
			trust := item.Inputs["assumeRolePolicy"].StringValue()
			expected := PolicyExpectation{ProviderARN: testProviderARN, Audience: GitHubOIDCAudience, Subject: "repo:example-org/example-repo:ref:refs/heads/main"}
			if strings.HasSuffix(name, "-apply") {
				expected.Subject = "repo:example-org/example-repo:environment:production"
			}
			if violations := ValidateResource(Resource{Type: item.TypeToken, Properties: map[string]any{"assumeRolePolicy": trust}}, expected); len(violations) > 0 {
				t.Fatalf("generated role trust rejected: %v", violations)
			}
		case awsRolePolicyType:
			rolePolicies++
			if item.RegisterRPC == nil || item.RegisterRPC.GetParent() != componentURN || componentURN == "" {
				t.Fatalf("role policy %s is not parented by the deployment component", item.Name)
			}
			policy := item.Inputs["policy"].StringValue()
			if err := validatePermissionsPolicy(policy); err != nil {
				t.Fatalf("generated policy invalid: %v", err)
			}
		}
	}
	if roles != 2 || rolePolicies != 2 || !roleNames["ferro-deploy-preview"] || !roleNames["ferro-deploy-apply"] {
		t.Fatalf("resources: roles=%d policies=%d names=%v", roles, rolePolicies, roleNames)
	}
}

func TestDecodeDocumentRejectsTrailingData(t *testing.T) {
	if _, err := decodeDocument(`{"Version":"2012-10-17"} {}`); err == nil {
		t.Fatal("trailing JSON object accepted")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(`{"x":1}`), &obj); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeDocument(obj); err != nil {
		t.Fatal(err)
	}
}

func TestReviewedResourceWildcardExceptions(t *testing.T) {
	for _, tc := range []struct {
		name, action, condition string
		valid                   bool
	}{
		{"subnet discovery", "ec2:DescribeSubnets", `,"Condition":{"StringEquals":{"aws:RequestedRegion":"us-west-2"}}`, true},
		{"registry login", "ecr:GetAuthorizationToken", `,"Condition":{"StringEquals":{"aws:RequestedRegion":"us-west-2"}}`, true},
		{"missing region", "ec2:DescribeSubnets", "", false},
		{"wildcard region", "ec2:DescribeSubnets", `,"Condition":{"StringEquals":{"aws:RequestedRegion":"*"}}`, false},
		{"mutating grant", "ec2:DeleteSubnet", `,"Condition":{"StringEquals":{"aws:RequestedRegion":"us-west-2"}}`, false},
		{"wildcard action", "ec2:Describe*", `,"Condition":{"StringEquals":{"aws:RequestedRegion":"us-west-2"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{"Version":"2012-10-17","Statement":{"Effect":"Allow","Action":"` + tc.action + `","Resource":"*"` + tc.condition + `}}`
			if err := validatePermissionsPolicy(doc); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestComponentRejectsSharedExecutionContext(t *testing.T) {
	args := validArgs()
	args.ApplyEnvironment, args.ApplyRef = "", args.Ref
	mocks := &resourceMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := NewGitHubActionsDeploymentIdentity(ctx, "deployment", args)
		return err
	}, pulumi.WithMocks("deployment-identity", "test", mocks))
	if err == nil || !strings.Contains(err.Error(), "different exact execution contexts") {
		t.Fatalf("shared context accepted: %v", err)
	}
}

func TestDifferentPolicyJSONDoesNotProveDistinctAuthority(t *testing.T) {
	args := validArgs()
	args.ApplyPolicyJSON = strings.Replace(previewPolicy, `"Effect":"Allow"`, `"Sid":"DifferentPresentation","Effect":"Allow"`, 1)
	if _, err := validateArgs("deployment", args); err != nil {
		t.Fatal(err)
	}
}
