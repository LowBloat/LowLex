// Package mt talks to machine translation services: Google Cloud Translation, DeepL,
// Microsoft Translator, LibreTranslate and any OpenAI-compatible chat completions API.
package mt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config is the provider configuration as saved by an admin. Unused fields stay empty.
type Config struct {
	APIKey      string   `json:"api_key,omitempty"`
	BaseURL     string   `json:"base_url,omitempty"`
	Region      string   `json:"region,omitempty"`
	Model       string   `json:"model,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

// Item is one string to translate. Key and Description give an AI provider context.
type Item struct {
	Text        string
	Key         string
	Description string
}

// Request translates Items from Source to Target (BCP 47 codes such as en, pt-BR).
type Request struct {
	Source  string
	Target  string
	Items   []Item
	Context string // project description, used by AI providers
}

type Translator interface {
	Translate(ctx context.Context, req Request) ([]string, error)
}

// Field describes one configuration input for the admin UI.
type Field struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
	Default  string `json:"default,omitempty"`
	Multi    bool   `json:"multiline,omitempty"`
}

type TypeInfo struct {
	Type   string  `json:"type"`
	Label  string  `json:"label"`
	AI     bool    `json:"ai"`
	Fields []Field `json:"fields"`
}

// Types lists the supported providers in the order the UI shows them.
var Types = []TypeInfo{
	{Type: "openai", Label: "AI (OpenAI-compatible)", AI: true, Fields: []Field{
		{Name: "base_url", Label: "Base URL", Required: true, Default: "https://api.openai.com/v1"},
		{Name: "api_key", Label: "API key", Secret: true},
		{Name: "model", Label: "Model", Required: true, Default: "gpt-4o-mini"},
		{Name: "prompt", Label: "Extra instructions (tone, glossary)", Multi: true},
	}},
	{Type: "google", Label: "Google Cloud Translation", Fields: []Field{
		{Name: "api_key", Label: "API key", Secret: true, Required: true},
	}},
	{Type: "deepl", Label: "DeepL", Fields: []Field{
		{Name: "api_key", Label: "Auth key", Secret: true, Required: true},
	}},
	{Type: "azure", Label: "Microsoft Translator", Fields: []Field{
		{Name: "api_key", Label: "Subscription key", Secret: true, Required: true},
		{Name: "region", Label: "Region (e.g. brazilsouth)"},
		{Name: "base_url", Label: "Endpoint", Default: "https://api.cognitive.microsofttranslator.com"},
	}},
	{Type: "libretranslate", Label: "LibreTranslate", Fields: []Field{
		{Name: "base_url", Label: "Server URL", Required: true, Default: "https://libretranslate.com"},
		{Name: "api_key", Label: "API key", Secret: true},
	}},
}

func TypeByName(t string) (TypeInfo, bool) {
	for _, ti := range Types {
		if ti.Type == t {
			return ti, true
		}
	}
	return TypeInfo{}, false
}

// New builds a translator for a provider type.
func New(typ string, c Config) (Translator, error) {
	switch typ {
	case "google":
		return google{c}, nil
	case "deepl":
		return deepl{c}, nil
	case "azure":
		return azure{c}, nil
	case "libretranslate":
		return libre{c}, nil
	case "openai":
		return openAI{c}, nil
	}
	return nil, fmt.Errorf("unknown provider type %q", typ)
}

// Translate splits the request into batches the provider accepts.
func Translate(ctx context.Context, t Translator, req Request) ([]string, error) {
	size := 50
	if _, ok := t.(openAI); ok {
		size = 25
	}
	out := make([]string, 0, len(req.Items))
	for start := 0; start < len(req.Items); start += size {
		end := min(start+size, len(req.Items))
		part := req
		part.Items = req.Items[start:end]
		res, err := t.Translate(ctx, part)
		if err != nil {
			return out, err
		}
		if len(res) != len(part.Items) {
			return out, fmt.Errorf("provider returned %d translations for %d texts", len(res), len(part.Items))
		}
		out = append(out, res...)
	}
	return out, nil
}

var client = &http.Client{Timeout: 120 * time.Second}

func postJSON(ctx context.Context, url string, headers map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return fmt.Errorf("%s: %s", resp.Status, msg)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	return nil
}

func texts(items []Item) []string {
	s := make([]string, len(items))
	for i, it := range items {
		s[i] = it.Text
	}
	return s
}

// base returns the primary subtag: "pt-BR" -> "pt".
func base(code string) string {
	code = strings.ReplaceAll(code, "_", "-")
	b, _, _ := strings.Cut(code, "-")
	return strings.ToLower(b)
}

func norm(code string) string { return strings.ToLower(strings.ReplaceAll(code, "_", "-")) }

// --- Google Cloud Translation (v2, API key) ---

type google struct{ c Config }

func googleCode(code string) string {
	switch n := norm(code); n {
	case "zh-cn", "zh-hans", "zh-sg", "zh":
		return "zh-CN"
	case "zh-tw", "zh-hant", "zh-hk":
		return "zh-TW"
	case "pt-pt":
		return "pt-PT"
	case "he":
		return "iw"
	default:
		return base(n)
	}
}

func (g google) Translate(ctx context.Context, r Request) ([]string, error) {
	var res struct {
		Data struct {
			Translations []struct {
				TranslatedText string `json:"translatedText"`
			} `json:"translations"`
		} `json:"data"`
	}
	body := map[string]any{"q": texts(r.Items), "target": googleCode(r.Target), "format": "text"}
	if r.Source != "" {
		body["source"] = googleCode(r.Source)
	}
	url := "https://translation.googleapis.com/language/translate/v2?key=" + g.c.APIKey
	if err := postJSON(ctx, url, nil, body, &res); err != nil {
		return nil, err
	}
	out := make([]string, len(res.Data.Translations))
	for i, t := range res.Data.Translations {
		out[i] = t.TranslatedText
	}
	return out, nil
}

// --- DeepL ---

type deepl struct{ c Config }

func deeplTarget(code string) string {
	switch n := norm(code); n {
	case "en":
		return "EN-US"
	case "pt":
		return "PT-BR"
	case "zh", "zh-cn", "zh-hans", "zh-sg":
		return "ZH-HANS"
	case "zh-tw", "zh-hant", "zh-hk":
		return "ZH-HANT"
	case "en-us", "en-gb", "pt-br", "pt-pt", "es-419":
		return strings.ToUpper(n)
	default:
		return strings.ToUpper(base(n))
	}
}

func (d deepl) Translate(ctx context.Context, r Request) ([]string, error) {
	host := "https://api.deepl.com"
	if strings.HasSuffix(d.c.APIKey, ":fx") {
		host = "https://api-free.deepl.com"
	}
	var res struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	body := map[string]any{"text": texts(r.Items), "target_lang": deeplTarget(r.Target)}
	if r.Source != "" {
		body["source_lang"] = strings.ToUpper(base(r.Source))
	}
	if err := postJSON(ctx, host+"/v2/translate", map[string]string{"Authorization": "DeepL-Auth-Key " + d.c.APIKey}, body, &res); err != nil {
		return nil, err
	}
	out := make([]string, len(res.Translations))
	for i, t := range res.Translations {
		out[i] = t.Text
	}
	return out, nil
}

// --- Microsoft Translator ---

type azure struct{ c Config }

func azureCode(code string) string {
	switch n := norm(code); n {
	case "zh", "zh-cn", "zh-hans", "zh-sg":
		return "zh-Hans"
	case "zh-tw", "zh-hant", "zh-hk":
		return "zh-Hant"
	case "pt-pt":
		return "pt-pt"
	case "fr-ca":
		return "fr-ca"
	default:
		return base(n)
	}
}

func (a azure) Translate(ctx context.Context, r Request) ([]string, error) {
	endpoint := strings.TrimRight(a.c.BaseURL, "/")
	if endpoint == "" {
		endpoint = "https://api.cognitive.microsofttranslator.com"
	}
	url := endpoint + "/translate?api-version=3.0&to=" + azureCode(r.Target)
	if r.Source != "" {
		url += "&from=" + azureCode(r.Source)
	}
	body := make([]map[string]string, len(r.Items))
	for i, it := range r.Items {
		body[i] = map[string]string{"Text": it.Text}
	}
	var res []struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	headers := map[string]string{"Ocp-Apim-Subscription-Key": a.c.APIKey, "Ocp-Apim-Subscription-Region": a.c.Region}
	if err := postJSON(ctx, url, headers, body, &res); err != nil {
		return nil, err
	}
	out := make([]string, len(res))
	for i, t := range res {
		if len(t.Translations) > 0 {
			out[i] = t.Translations[0].Text
		}
	}
	return out, nil
}

// --- LibreTranslate ---

type libre struct{ c Config }

func (l libre) Translate(ctx context.Context, r Request) ([]string, error) {
	src := "auto"
	if r.Source != "" {
		src = base(r.Source)
	}
	body := map[string]any{"q": texts(r.Items), "source": src, "target": base(r.Target), "format": "text"}
	if l.c.APIKey != "" {
		body["api_key"] = l.c.APIKey
	}
	var res struct {
		TranslatedText []string `json:"translatedText"`
	}
	if err := postJSON(ctx, strings.TrimRight(l.c.BaseURL, "/")+"/translate", nil, body, &res); err != nil {
		return nil, err
	}
	return res.TranslatedText, nil
}

// --- OpenAI-compatible chat completions (OpenAI, OpenRouter, Ollama, LM Studio, vLLM...) ---

type openAI struct{ c Config }

const aiSystemPrompt = `You are a professional software localization translator.
Translate each item from %s to %s for a software user interface.
Rules:
- Keep placeholders and markup exactly as they are: {name}, {{count}}, %%s, %%1$d, $t(key), <b>, </b>, <0>, \n.
- Keep ICU MessageFormat structure (plural, select, # and the keywords) and translate only the human text inside it.
- Match the tone, length and capitalisation style of the source. Do not add explanations.
- Use the key name and description only as context; never translate them.
Reply with JSON only: {"translations": ["..."]}, one string per item, same order and same count.`

func (o openAI) Translate(ctx context.Context, r Request) ([]string, error) {
	type item struct {
		Text    string `json:"text"`
		Key     string `json:"key,omitempty"`
		Context string `json:"context,omitempty"`
	}
	items := make([]item, len(r.Items))
	for i, it := range r.Items {
		items[i] = item{it.Text, it.Key, it.Description}
	}
	user, _ := json.Marshal(map[string]any{"items": items})

	system := fmt.Sprintf(aiSystemPrompt, langName(r.Source), langName(r.Target))
	if strings.TrimSpace(r.Context) != "" {
		system += "\nProduct context: " + strings.TrimSpace(r.Context)
	}
	if strings.TrimSpace(o.c.Prompt) != "" {
		system += "\nAdditional instructions: " + strings.TrimSpace(o.c.Prompt)
	}

	body := map[string]any{
		"model": o.c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": string(user)},
		},
	}
	if o.c.Temperature != nil {
		body["temperature"] = *o.c.Temperature
	}
	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	base := strings.TrimRight(o.c.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	headers := map[string]string{}
	if o.c.APIKey != "" {
		headers["Authorization"] = "Bearer " + o.c.APIKey
	}
	if err := postJSON(ctx, base+"/chat/completions", headers, body, &res); err != nil {
		return nil, err
	}
	if len(res.Choices) == 0 {
		return nil, errors.New("model returned no choices")
	}
	return parseAIList(res.Choices[0].Message.Content, len(r.Items))
}

// parseAIList pulls {"translations": [...]} out of a reply that may be wrapped in a code fence.
func parseAIList(content string, n int) ([]string, error) {
	s := strings.TrimSpace(content)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var obj struct {
		Translations []string `json:"translations"`
	}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return nil, fmt.Errorf("model reply is not the expected JSON: %w", err)
	}
	if len(obj.Translations) != n {
		return nil, fmt.Errorf("model returned %d translations for %d texts", len(obj.Translations), n)
	}
	return obj.Translations, nil
}

func langName(code string) string {
	if code == "" {
		return "the source language"
	}
	if n, ok := LanguageNames[code]; ok {
		return n + " (" + code + ")"
	}
	if n, ok := LanguageNames[base(code)]; ok {
		return n + " (" + code + ")"
	}
	return code
}
