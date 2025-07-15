package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/google/generative-ai-go/genai"
)

// transcribeAndSummarize sends the audio data to Gemini for transcription and summary.
func transcribeAndSummarize(ctx context.Context, model *genai.GenerativeModel, audioData []byte) (string, string, error) {
	prompt := "You are an assistant receiving voice notes. First, transcribe the voice note from my wife. Second, generate a concise summary in Spanish with the most important points. Return ONLY the transcription and the summary, separated by '---'. For example: 'Transcription: [text] --- Summary: [text]'"
	resp, err := model.GenerateContent(ctx, genai.Blob{MIMEType: "audio/ogg", Data: audioData}, genai.Text(prompt))
	if err != nil {
		return "", "", errors.Wrap(err, "failed to generate content with Gemini")
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", "", errors.New("Gemini returned no content")
	}
	content := resp.Candidates[0].Content.Parts[0].(genai.Text)
	parts := strings.SplitN(string(content), "---", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected response format from Gemini: %s", content)
	}
	transcription := strings.TrimSpace(strings.TrimPrefix(parts[0], "Transcripción:"))
	summary := strings.TrimSpace(strings.TrimPrefix(parts[1], "Resumen:"))

	return transcription, summary, nil
}

// generateAffirmativeReply uses Gemini to create a loving response.
func generateAffirmativeReply(ctx context.Context, model *genai.GenerativeModel, transcription string) (string, error) {
	prompt := fmt.Sprintf("Act as if you were my husband. My wife, whom I call 'Bey' or 'Amor', sent me the following message: '%s'. Generate a short (max 20 words), friendly, and loving response, always agreeing with her and showing support. Use a close and affectionate tone. Vary between 'Bey' and 'Amor' to call her.", transcription)
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", errors.Wrap(err, "failed to call Gemini for the response")
	}
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		return string(resp.Candidates[0].Content.Parts[0].(genai.Text)), nil
	}
	return "", errors.New("Gemini did not generate a text response")
}

// generateVoiceResponse converts text to audio using ElevenLabs.
func generateVoiceResponse(ctx context.Context, apiKey, voiceID, text string) ([]byte, error) {
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voiceID)
	payload := map[string]interface{}{
		"text":     text,
		"model_id": "eleven_multilingual_v2",
		"voice_settings": map[string]interface{}{
			"stability":         0.5,
			"similarity_boost":  0.75,
			"style":             0.1,
			"use_speaker_boost": true,
		},
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode ElevenLabs payload")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ElevenLabs request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send ElevenLabs request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ElevenLabs error: status %d, body: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
