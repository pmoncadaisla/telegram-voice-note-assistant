package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-faster/errors"
	"github.com/google/generative-ai-go/genai"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
	"google.golang.org/api/option"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("Error: %+v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return errors.Wrap(err, "could not load configuration")
	}

	geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
	if err != nil {
		return errors.Wrap(err, "failed to create Gemini client")
	}
	defer geminiClient.Close()
	geminiModel := geminiClient.GenerativeModel("models/gemini-1.5-flash")

	dispatcher := tg.NewUpdateDispatcher()
	client := telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: "telegram-session.json"},
		UpdateHandler:  dispatcher,
	})

	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg.Out {
			return nil
		}

		doc, ok := msg.Media.(*tg.MessageMediaDocument)
		if !ok {
			return nil
		}
		document, ok := doc.Document.AsNotEmpty()
		if !ok || document.MimeType != "audio/ogg" {
			return nil
		}

		peer, ok := msg.PeerID.(*tg.PeerUser)
		if !ok || peer.UserID != cfg.TargetUserID {
			return nil
		}

		user, ok := e.Users[peer.UserID]
		if !ok {
			log.Printf("Could not find user %d in entities", peer.UserID)
			return nil
		}
		inputPeer := user.AsInputPeer()

		log.Println("Voice note received from target user. Processing in the background...")
		go processVoiceNote(context.Background(), client, geminiModel, &cfg, msg, inputPeer)
		return nil
	})

	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to get authentication status")
		}

		if !status.Authorized {
			// If not authorized, start the authentication flow in the terminal.
			phone := os.Getenv("PHONE_NUMBER")
			codeRaw, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
			if err != nil {
				return errors.Wrap(err, "failed to send verification code")
			}
			code, ok := codeRaw.(*tg.AuthSentCode)
			if !ok {
				return errors.New("failed to convert verification code")
			}

			fmt.Print("Enter the verification code: ")
			codeBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return errors.Wrap(err, "failed to read verification code")
			}
			fmt.Println() // Add a newline after reading the code
			verificationCode := string(codeBytes)

			if _, err := client.Auth().SignIn(ctx, phone, verificationCode, code.PhoneCodeHash); err != nil {
				if errors.Is(err, auth.ErrPasswordAuthNeeded) {
					fmt.Print("Enter your 2FA password: ")
					passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
					if err != nil {
						return errors.Wrap(err, "failed to read 2FA password")
					}
					fmt.Println()
					password := string(passwordBytes)

					if _, err := client.Auth().Password(ctx, password); err != nil {
						return errors.Wrap(err, "failed to sign in with 2FA password")
					}
				} else {
					return errors.Wrap(err, "failed to sign in")
				}
			}
		}

		log.Println("Client connected and listening for messages...")
		// Wait until the context is canceled (e.g., Ctrl+C)
		<-ctx.Done()
		return ctx.Err()
	})
}
