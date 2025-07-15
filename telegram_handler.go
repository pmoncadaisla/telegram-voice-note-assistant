package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"

	"github.com/google/generative-ai-go/genai"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	tUploader "github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// processVoiceNote orchestrates the entire voice note processing workflow.
func processVoiceNote(ctx context.Context, client *telegram.Client, model *genai.GenerativeModel, cfg *Config, msg *tg.Message, peer tg.InputPeerClass) {
	api := client.API()
	d := downloader.NewDownloader()

	// 1. Download audio
	log.Println("Downloading audio...")
	doc, _ := msg.Media.(*tg.MessageMediaDocument)
	file, _ := doc.Document.AsNotEmpty()
	buf := new(bytes.Buffer)
	if _, err := d.Download(api, file.AsInputDocumentFileLocation()).Stream(ctx, buf); err != nil {
		log.Printf("Failed to download file: %+v", err)
		return
	}
	audioData := buf.Bytes()
	log.Println("Audio downloaded.")

	// 2. Transcribe and summarize with Gemini
	log.Println("Sending audio to Gemini for transcription and summary...")
	transcription, summary, err := transcribeAndSummarize(ctx, model, audioData)
	if err != nil {
		log.Printf("Failed to transcribe and summarize: %+v", err)
		return
	}

	// 3. Send summary to "Saved Messages"
	log.Println("Sending summary to 'Saved Messages'...")
	randomID, _ := rand.Int(rand.Reader, big.NewInt(int64(^uint64(0)>>1)))
	_, err = api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     &tg.InputPeerSelf{},
		Message:  fmt.Sprintf("📝 **Summary of your wife's audio:**\n\n%s", summary),
		RandomID: randomID.Int64(),
	})
	if err != nil {
		log.Printf("Failed to send summary message: %+v", err)
	} else {
		log.Println("Summary sent successfully!")
	}

	// 4. Generate affirmative reply with Gemini
	log.Println("Generating affirmative reply with Gemini...")
	affirmativeText, err := generateAffirmativeReply(ctx, model, transcription)
	if err != nil {
		log.Printf("Failed to generate affirmative reply: %+v", err)
		return
	}
	log.Printf("Generated text response: %s", affirmativeText)

	// 5. Generate voice response with ElevenLabs
	log.Println("Generating voice response with ElevenLabs...")
	voiceResponseData, err := generateVoiceResponse(ctx, cfg.ElevenLabsAPIKey, cfg.ElevenLabsVoiceID, affirmativeText)
	if err != nil {
		log.Printf("Failed to generate voice response: %+v", err)
		return
	}
	log.Println("Voice response generated successfully.")

	// 6. Send voice response to the original chat
	log.Println("Sending voice response...")
	uploader := tUploader.NewUploader(api)

	uploadedFile, err := uploader.Upload(ctx, tUploader.NewUpload("response.mp3", bytes.NewReader(voiceResponseData), int64(len(voiceResponseData))))
	if err != nil {
		log.Printf("Failed to upload voice response: %+v", err)
		return
	}

	responseRandomID, _ := rand.Int(rand.Reader, big.NewInt(int64(^uint64(0)>>1)))
	_, err = api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer:     peer,
		RandomID: responseRandomID.Int64(),
		Media: &tg.InputMediaUploadedDocument{
			File:     uploadedFile,
			MimeType: "audio/mpeg",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeAudio{Voice: true, Duration: 5}, // Duration is an estimate
			},
		},
	})
	if err != nil {
		log.Printf("Failed to send voice response: %+v", err)
	} else {
		log.Println("Voice response sent successfully!")
	}
}
