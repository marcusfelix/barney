// Package manifest parses Barney trigger manifests, evaluates CEL filter
// expressions against normalized payloads, and renders prompt templates.
package manifest

import (
	"context"
	"fmt"
	"log"
	"strings"
	"text/template"

	"github.com/deploid/barney/internal/gitcmd"
	"github.com/google/cel-go/cel"
	"gopkg.in/yaml.v3"
)

// ManifestPath is the repo-relative location of the trigger manifest.
const ManifestPath = ".barney/manifest.yaml"

// Trigger is a single trigger rule from the manifest.
type Trigger struct {
	ID             string `yaml:"id"`
	Event          string `yaml:"event"`
	Filter         string `yaml:"filter"`
	Agent          string `yaml:"agent"`
	PromptTemplate string `yaml:"prompt_template"`
}

// Manifest is the parsed trigger manifest schema.
type Manifest struct {
	Version  string    `yaml:"version"`
	Triggers []Trigger `yaml:"triggers"`
}

// MatchedTrigger pairs a trigger with its rendered prompt after a successful
// filter evaluation.
type MatchedTrigger struct {
	Trigger Trigger
	Prompt  string
}

// LoadFromRef reads and parses .barney/manifest.yaml as committed at ref
// (e.g. "origin/main"), never from the working tree. Returns (nil, nil) when
// the manifest does not exist at ref.
//
// This matters for pull_request events: the workspace checks out the PR's
// own head so the agent can operate on it, but that head may belong to an
// untrusted fork. Reading the manifest from the trusted base ref instead of
// the checked-out tree means a fork can't ship its own triggers or prompts.
func LoadFromRef(ctx context.Context, repoDir, ref string) (*Manifest, error) {
	target := ref + ":" + ManifestPath
	if _, err := gitcmd.Run(ctx, repoDir, "cat-file", "-e", target); err != nil {
		return nil, nil
	}
	data, err := gitcmd.Run(ctx, repoDir, "show", target)
	if err != nil {
		return nil, fmt.Errorf("read manifest at %s: %w", target, err)
	}
	return Parse([]byte(data))
}

// Parse parses manifest YAML content.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Version == "" {
		m.Version = "v0"
	}
	for i := range m.Triggers {
		if m.Triggers[i].ID == "" {
			m.Triggers[i].ID = fmt.Sprintf("trigger-%d", i)
		}
		if m.Triggers[i].Agent == "" {
			m.Triggers[i].Agent = "opencode"
		}
	}
	return &m, nil
}

// triggerEventParts splits an event spec like "issues.opened" into its type
// and action. The action may be "*" to match any action.
func triggerEventParts(spec string) (eventType, action string, ok bool) {
	parts := strings.SplitN(spec, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	eventType = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	} else {
		action = "*"
	}
	return eventType, action, true
}

// MatchesEvent reports whether the trigger's event spec matches the incoming
// normalized event type and action (from payload.action).
func MatchesEvent(trigger Trigger, eventType, action string) bool {
	wantType, wantAction, ok := triggerEventParts(trigger.Event)
	if !ok {
		return false
	}
	if wantType != eventType {
		return false
	}
	if wantAction == "*" || wantAction == "" {
		return true
	}
	return wantAction == action
}

// payloadAction extracts payload.action if present.
func payloadAction(payload map[string]interface{}) string {
	if v, ok := payload["action"].(string); ok {
		return v
	}
	return ""
}

// EvaluateFilter runs a CEL expression against the payload map. The payload
// is exposed as the CEL variable "payload".
func EvaluateFilter(ctx context.Context, expression string, payload map[string]interface{}) (bool, error) {
	if strings.TrimSpace(expression) == "" {
		return true, nil
	}
	env, err := cel.NewEnv(
		cel.Variable("payload", cel.DynType),
	)
	if err != nil {
		return false, fmt.Errorf("cel env: %w", err)
	}
	ast, issues := env.Parse(expression)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("cel parse %q: %w", expression, issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("cel program %q: %w", expression, err)
	}
	out, _, err := prg.Eval(map[string]interface{}{"payload": payload})
	if err != nil {
		return false, fmt.Errorf("cel eval %q: %w", expression, err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel eval %q: expected bool, got %T", expression, out.Value())
	}
	return b, nil
}

// RenderPrompt renders the trigger's prompt_template with the payload exposed
// as ".payload" (plus ".eventType" and ".eventID" for convenience).
func RenderPrompt(trigger Trigger, payload map[string]interface{}, eventType, eventID string) (string, error) {
	if strings.TrimSpace(trigger.PromptTemplate) == "" {
		return "", fmt.Errorf("trigger %q has empty prompt_template", trigger.ID)
	}
	tmpl, err := template.New("prompt").Parse(trigger.PromptTemplate)
	if err != nil {
		return "", fmt.Errorf("parse prompt template for trigger %q: %w", trigger.ID, err)
	}
	data := map[string]interface{}{
		"payload":   payload,
		"eventType": eventType,
		"eventID":   eventID,
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render prompt template for trigger %q: %w", trigger.ID, err)
	}
	return buf.String(), nil
}

// Process finds all triggers matching the incoming event whose CEL filter
// passes, and returns them with rendered prompts. A trigger whose filter or
// prompt template fails to evaluate or render is skipped (logged); other
// triggers still run.
func Process(ctx context.Context, m *Manifest, eventType, eventID string, payload map[string]interface{}) []MatchedTrigger {
	var matched []MatchedTrigger
	action := payloadAction(payload)
	for _, trigger := range m.Triggers {
		if !MatchesEvent(trigger, eventType, action) {
			continue
		}
		ok, err := EvaluateFilter(ctx, trigger.Filter, payload)
		if err != nil {
			log.Printf("[manifest] trigger %q: skipping, filter evaluation failed: %v", trigger.ID, err)
			continue
		}
		if !ok {
			continue
		}
		prompt, err := RenderPrompt(trigger, payload, eventType, eventID)
		if err != nil {
			log.Printf("[manifest] trigger %q: skipping, prompt rendering failed: %v", trigger.ID, err)
			continue
		}
		matched = append(matched, MatchedTrigger{Trigger: trigger, Prompt: prompt})
	}
	return matched
}
