package authcmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type promptResult struct {
	value string
	err   error
}

type fakePrompter struct {
	available bool
	results   []promptResult
	prompts   []string
}

func (p *fakePrompter) Available() bool { return p.available }

func (p *fakePrompter) ReadLine(prompt string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	if len(p.results) == 0 {
		return "", errors.New("unexpected prompt")
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result.value, result.err
}

func TestResolveFullyInteractiveLogin(t *testing.T) {
	prompter := &fakePrompter{available: true, results: []promptResult{
		{value: " https://prod.example.com/ "},
		{value: " admin "},
		{value: " temporary-password "},
		{value: ""},
		{value: ""},
	}}

	got, err := Resolve(ResolveOptions{Prompter: prompter})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := Input{
		Endpoint: "https://prod.example.com",
		Username: "admin",
		Password: " temporary-password ",
		Fleet:    "prod.example.com",
		Context:  "prod.example.com-admin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
	wantPrompts := []string{
		"endpoint: ",
		"username: ",
		"password: ",
		"fleet [prod.example.com]: ",
		"context [prod.example.com-admin]: ",
	}
	if !reflect.DeepEqual(prompter.prompts, wantPrompts) {
		t.Fatalf("prompts = %#v, want %#v", prompter.prompts, wantPrompts)
	}
}

func TestResolveUsesExplicitValuesAndCustomNames(t *testing.T) {
	prompter := &fakePrompter{available: true, results: []promptResult{
		{value: "temporary-password"},
		{value: " local "},
		{value: " local-admin "},
	}}
	got, err := Resolve(ResolveOptions{
		Input:    Input{Endpoint: " https://ks.example.com/// ", Username: " admin "},
		Prompter: prompter,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := Input{Endpoint: "https://ks.example.com", Username: "admin", Password: "temporary-password", Fleet: "local", Context: "local-admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
	wantPrompts := []string{"password: ", "fleet [ks.example.com]: ", "context [local-admin]: "}
	if !reflect.DeepEqual(prompter.prompts, wantPrompts) {
		t.Fatalf("prompts = %#v, want %#v", prompter.prompts, wantPrompts)
	}
}

func TestResolveCompleteCredentialsDeriveNamesWithoutPrompts(t *testing.T) {
	prompter := &fakePrompter{available: true}
	got, err := Resolve(ResolveOptions{
		Input: Input{
			Endpoint: " https://ks.example.com/ ",
			Username: " admin ",
			Password: " secret ",
		},
		Prompter: prompter,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := Input{Endpoint: "https://ks.example.com", Username: "admin", Password: " secret ", Fleet: "ks.example.com", Context: "ks.example.com-admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
	if len(prompter.prompts) != 0 {
		t.Fatalf("prompts = %#v, want none", prompter.prompts)
	}
}

func TestResolveMissingUsernameAndPasswordContinuesGuidedFlow(t *testing.T) {
	prompter := &fakePrompter{available: true, results: []promptResult{
		{value: " admin "},
		{value: "temporary-password"},
		{value: ""},
		{value: ""},
	}}
	got, err := Resolve(ResolveOptions{
		Input:    Input{Endpoint: "https://ks.example.com"},
		Prompter: prompter,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := Input{
		Endpoint: "https://ks.example.com",
		Username: "admin",
		Password: "temporary-password",
		Fleet:    "ks.example.com",
		Context:  "ks.example.com-admin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
	wantPrompts := []string{
		"username: ",
		"password: ",
		"fleet [ks.example.com]: ",
		"context [ks.example.com-admin]: ",
	}
	if !reflect.DeepEqual(prompter.prompts, wantPrompts) {
		t.Fatalf("prompts = %#v, want %#v", prompter.prompts, wantPrompts)
	}
}

func TestResolveNonInteractiveRequirementsAndDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  Input
		err   string
	}{
		{name: "endpoint", err: "error: endpoint is required"},
		{name: "username", input: Input{Endpoint: "https://ks.example.com"}, err: "error: --username is required"},
		{name: "password", input: Input{Endpoint: "https://ks.example.com", Username: "admin"}, err: "error: --password is required"},
		{
			name:  "derive optional names",
			input: Input{Endpoint: "https://ks.example.com/", Username: "admin", Password: "secret"},
			want:  Input{Endpoint: "https://ks.example.com", Username: "admin", Password: "secret", Fleet: "ks.example.com", Context: "ks.example.com-admin"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prompter := &fakePrompter{available: false}
			got, err := Resolve(ResolveOptions{Input: test.input, Prompter: prompter})
			if test.err != "" {
				if err == nil || err.Error() != test.err {
					t.Fatalf("Resolve() error = %v, want %q", err, test.err)
				}
			} else if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Resolve() = %#v, %v; want %#v, nil", got, err, test.want)
			}
			if len(prompter.prompts) != 0 {
				t.Fatalf("prompts = %#v, want none", prompter.prompts)
			}
		})
	}
}

func TestResolveRejectsEmptyRequiredPromptOnce(t *testing.T) {
	prompter := &fakePrompter{available: true, results: []promptResult{{value: "  "}}}
	_, err := Resolve(ResolveOptions{Prompter: prompter})
	if err == nil || err.Error() != "error: endpoint is required" {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(prompter.prompts) != 1 {
		t.Fatalf("prompts = %#v, want one endpoint prompt", prompter.prompts)
	}
}

func TestResolveReportsPromptReadField(t *testing.T) {
	prompter := &fakePrompter{available: true, results: []promptResult{{err: errors.New("read failed")}}}
	_, err := Resolve(ResolveOptions{Prompter: prompter})
	if err == nil || !strings.Contains(err.Error(), "error: read endpoint: read failed") {
		t.Fatalf("Resolve() error = %v", err)
	}
}
