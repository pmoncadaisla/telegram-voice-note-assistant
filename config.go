package main

import (
	"os"
	"strconv"

	"github.com/go-faster/errors"
)

// Config stores all the necessary environment variables.
type Config struct {
	AppID             int
	AppHash           string
	TargetUserID      int64
	GeminiAPIKey      string
	GeminiModel       string // Optional, if not set it will be prompted
	ElevenLabsAPIKey  string
	ElevenLabsVoiceID string
}

// loadConfig loads the necessary environment variables.
func loadConfig() (Config, error) {
	appIDStr := os.Getenv("APP_ID")
	if appIDStr == "" {
		return Config{}, errors.New("APP_ID environment variable not set")
	}
	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		return Config{}, errors.Wrap(err, "invalid APP_ID")
	}

	targetUserIDStr := os.Getenv("TARGET_USER_ID")
	if targetUserIDStr == "" {
		return Config{}, errors.New("TARGET_USER_ID environment variable not set")
	}
	targetUserID, err := strconv.ParseInt(targetUserIDStr, 10, 64)
	if err != nil {
		return Config{}, errors.Wrap(err, "invalid TARGET_USER_ID")
	}
	geminiModel := os.Getenv("GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = "models/gemini-2.5-flash" // Default model
	}

	cfg := Config{
		AppID:             appID,
		AppHash:           os.Getenv("APP_HASH"),
		TargetUserID:      targetUserID,
		GeminiModel:       geminiModel,
		GeminiAPIKey:      os.Getenv("GEMINI_API_KEY"),
		ElevenLabsAPIKey:  os.Getenv("ELEVENLABS_API_KEY"),
		ElevenLabsVoiceID: os.Getenv("ELEVENLABS_VOICE_ID"),
	}

	if cfg.AppHash == "" || cfg.GeminiAPIKey == "" || cfg.ElevenLabsAPIKey == "" || cfg.ElevenLabsVoiceID == "" {
		return Config{}, errors.New("one or more required environment variables are not set (APP_HASH, GEMINI_API_KEY, ELEVENLABS_API_KEY, ELEVENLABS_VOICE_ID)")
	}

	return cfg, nil
}
