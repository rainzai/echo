package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

// 1024 was too small to read model numbers
const maxSide = 1536

const visionPrompt = `Someone sent this photo or screenshot to a deal checker. Identify the product so its price can be looked up.
Answer with JSON only:
{"brand": str|null, "product": str|null, "model": str|null, "variant": str|null,
 "price": number|null, "currency": str|null, "claimed_old_price": number|null, "claimed_discount": str|null,
 "shop": str|null, "other_products": [str], "visible_text": [str], "confident": bool, "missing": str|null}
- Only use what you can see. Never guess a model number or price that isn't visible.
- model: only a model or article number you can read exactly, character by character, else null.
- variant: size, colour, flavour, and always the pack size when it's more than one, e.g. "6-pack", "5 x 12 g".
- other_products: other products that have their own price shown, or, when no prices are shown,
  are just as large as the main one. Leave out anything without a price that is behind, beside
  or cut off from the main product.
- visible_text: the important words printed on the product or screen.
- confident: true only if brand and product are clear enough to search for this exact item.
- If the photo is mainly of something else (a pet, a person, a room) and a product only appears in it,
  confident is false and missing asks whether that product is what they want checked.
- missing: what you'd need to be sure, e.g. "model number, usually on a sticker underneath".`

type Vision struct {
	key   string
	model string
	http  *http.Client
}

func NewVision(key, model string) *Vision {
	return &Vision{key: key, model: model, http: &http.Client{}}
}

type visionContent struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type visionMessage struct {
	Role    string          `json:"role"`
	Content []visionContent `json:"content"`
}

type visionRequest struct {
	Model       string          `json:"model"`
	Messages    []visionMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
	// thinking is slower and missed things in testing
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
}

// Look returns what the model read from the image, as JSON
func (v *Vision) Look(ctx context.Context, img []byte) (string, error) {
	img, err := shrink(img)
	if err != nil {
		return "", err
	}

	body := visionRequest{
		Model: v.model,
		Messages: []visionMessage{{Role: "user", Content: []visionContent{
			{Type: "text", Text: visionPrompt},
			{Type: "image_url", ImageURL: &imageURL{URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)}},
		}}},
		MaxTokens:          800,
		Temperature:        0.2,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}

	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	// the free tier is rate limited, so back off on 429
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://integrate.api.nvidia.com/v1/chat/completions", bytes.NewReader(b))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+v.key)

		res, err := v.http.Do(req)
		if err != nil {
			return "", err
		}
		if res.StatusCode == http.StatusTooManyRequests {
			res.Body.Close()
			if attempt == 2 {
				return "", errors.New("nemotron rate limited")
			}
			continue
		}
		if res.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
			res.Body.Close()
			return "", fmt.Errorf("nemotron %s: %s", res.Status, msg)
		}
		err = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if err != nil {
			return "", err
		}
		break
	}
	if len(out.Choices) == 0 {
		return "", errors.New("nemotron returned nothing")
	}

	text := strings.TrimSpace(out.Choices[0].Message.Content)
	text = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```")
	text = strings.TrimSpace(text)
	if !json.Valid([]byte(text)) {
		return "", fmt.Errorf("nemotron returned no json: %.200s", text)
	}
	return text, nil
}

func shrink(b []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w > maxSide || h > maxSide {
		if w > h {
			w, h = maxSide, h*maxSide/w
		} else {
			w, h = w*maxSide/h, maxSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
