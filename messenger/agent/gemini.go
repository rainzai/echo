package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Gemini struct {
	key      string
	model    string
	thinking string
	http     *http.Client
}

func NewGemini(key, model, thinking string) *Gemini {
	return &Gemini{key: key, model: model, thinking: thinking, http: &http.Client{}}
}

type part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type functionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type tool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

type thinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel"`
}

type generationConfig struct {
	ThinkingConfig *thinkingConfig `json:"thinkingConfig,omitempty"`
}

type request struct {
	SystemInstruction content           `json:"systemInstruction"`
	Contents          []content         `json:"contents"`
	Tools             []tool            `json:"tools,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type response struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

func (g *Gemini) Generate(ctx context.Context, system string, tools []Tool, history []Message) (Message, error) {
	body := request{
		SystemInstruction: content{Parts: []part{{Text: system}}},
		Contents:          contents(history),
	}
	if len(tools) > 0 {
		var decls []functionDeclaration
		for _, t := range tools {
			decls = append(decls, functionDeclaration{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
		}
		body.Tools = []tool{{FunctionDeclarations: decls}}
	}
	if g.thinking != "" {
		body.GenerationConfig = &generationConfig{ThinkingConfig: &thinkingConfig{ThinkingLevel: g.thinking}}
	}

	b, err := json.Marshal(body)
	if err != nil {
		return Message{}, err
	}

	url := "https://generativelanguage.googleapis.com/v1beta/models/" + g.model + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.key)

	res, err := g.http.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return Message{}, fmt.Errorf("gemini %s: %s", res.Status, msg)
	}

	var out response
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return Message{}, err
	}
	if len(out.Candidates) == 0 {
		return Message{}, errors.New("gemini returned no candidates")
	}

	m := Message{Role: RoleEcho}
	var texts []string
	for _, p := range out.Candidates[0].Content.Parts {
		switch {
		case p.FunctionCall != nil && m.Call == nil:
			m.Call = &Call{Name: p.FunctionCall.Name, Args: p.FunctionCall.Args, Signature: p.ThoughtSignature}
		case p.Text != "" && !p.Thought:
			texts = append(texts, strings.TrimSpace(p.Text))
		}
	}
	m.Text = strings.Join(texts, " ")
	if m.Call == nil && m.Text == "" {
		return Message{}, errors.New("gemini returned no text")
	}
	return m, nil
}

// contents maps our history to gemini's roles, echo is "model"
func contents(history []Message) []content {
	out := make([]content, 0, len(history))
	for _, m := range history {
		switch {
		case m.Result != nil:
			out = append(out, content{Role: "user", Parts: []part{{
				FunctionResponse: &functionResponse{Name: m.Result.Name, Response: m.Result.Data},
			}}})
		case m.Role == RoleEcho:
			var parts []part
			if m.Text != "" {
				parts = append(parts, part{Text: m.Text})
			}
			if m.Call != nil {
				parts = append(parts, part{
					FunctionCall:     &functionCall{Name: m.Call.Name, Args: m.Call.Args},
					ThoughtSignature: m.Call.Signature,
				})
			}
			out = append(out, content{Role: "model", Parts: parts})
		default:
			out = append(out, content{Role: "user", Parts: []part{{Text: m.Text}}})
		}
	}
	return out
}
