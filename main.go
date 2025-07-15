package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/google/generative-ai-go/genai"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	tUploader "github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
	"google.golang.org/api/option"
)

// Config almacena todas las variables de entorno necesarias.
type Config struct {
	AppID             int
	AppHash           string
	TargetUserID      int64
	GeminiAPIKey      string
	GeminiModel       string
	ElevenLabsAPIKey  string
	ElevenLabsVoiceID string
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("Error: %+v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return errors.Wrap(err, "no se pudo cargar la configuración")
	}

	geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
	if err != nil {
		return errors.Wrap(err, "error al crear el cliente de Gemini")
	}
	defer geminiClient.Close()
	geminiModel := geminiClient.GenerativeModel(cfg.GeminiModel)

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
			log.Printf("No se pudo encontrar al usuario %d en las entidades", peer.UserID)
			return nil
		}
		inputPeer := user.AsInputPeer()

		log.Println("Nota de voz recibida del usuario objetivo. Procesando en segundo plano...")
		go processVoiceNote(context.Background(), client, geminiModel, &cfg, msg, inputPeer)
		return nil
	})

	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return errors.Wrap(err, "fallo al obtener estado de autenticación")
		}

		if !status.Authorized {
			phone := os.Getenv("PHONE_NUMBER")
			codeRaw, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
			if err != nil {
				return errors.Wrap(err, "fallo al enviar el código de verificación")
			}
			code, ok := codeRaw.(*tg.AuthSentCode)
			if !ok {
				return errors.New("fallo al convertir el código de verificación")
			}

			fmt.Print("Introduce el código de verificación: ")
			codeBytes, _ := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			verificationCode := string(codeBytes)

			if _, err := client.Auth().SignIn(ctx, phone, verificationCode, code.PhoneCodeHash); err != nil {
				if errors.Is(err, auth.ErrPasswordAuthNeeded) {
					fmt.Print("Introduce tu contraseña de doble factor (2FA): ")
					passwordBytes, _ := term.ReadPassword(int(os.Stdin.Fd()))
					fmt.Println()
					password := string(passwordBytes)
					if _, err := client.Auth().Password(ctx, password); err != nil {
						return errors.Wrap(err, "fallo en el inicio de sesión con contraseña 2FA")
					}
				} else {
					return errors.Wrap(err, "fallo en el inicio de sesión")
				}
			}
		}

		log.Println("Cliente conectado y escuchando mensajes...")
		log.Println("Usando modelo de Gemini:", cfg.GeminiModel)
		<-ctx.Done()
		return ctx.Err()
	})
}

// processVoiceNote orquesta todo el flujo de procesamiento de la nota de voz.
func processVoiceNote(ctx context.Context, client *telegram.Client, model *genai.GenerativeModel, cfg *Config, msg *tg.Message, peer tg.InputPeerClass) {
	api := client.API()
	d := downloader.NewDownloader()

	// 1. Descargar audio
	log.Println("Descargando audio...")
	doc, _ := msg.Media.(*tg.MessageMediaDocument)
	file, _ := doc.Document.AsNotEmpty()
	buf := new(bytes.Buffer)
	if _, err := d.Download(api, file.AsInputDocumentFileLocation()).Stream(ctx, buf); err != nil {
		log.Printf("Error al descargar el archivo: %+v", err)
		return
	}
	audioData := buf.Bytes()
	log.Println("Audio descargado.")

	// 2. Transcribir y resumir con Gemini
	log.Println("Enviando audio a Gemini para transcribir y resumir...")
	prompt := "Genera un resumen de la nota de voz que me manda mi mujer con puntos más importantes. Genera la respuesta formateado para Telegram. '"
	resp, err := model.GenerateContent(ctx, genai.Blob{MIMEType: "audio/ogg", Data: audioData}, genai.Text(prompt))
	if err != nil {
		log.Printf("Error al generar contenido con Gemini: %+v", err)
		return
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		log.Println("Error: Gemini no devolvió contenido.")
		return
	}
	content := resp.Candidates[0].Content.Parts[0].(genai.Text)
	parts := strings.SplitN(string(content), "---", 2)
	if len(parts) != 2 {
		log.Printf("Error: El formato de respuesta de Gemini es inesperado: %s", content)
		return
	}
	transcription := strings.TrimSpace(strings.TrimPrefix(parts[0], "Transcripción:"))
	summary := strings.TrimSpace(strings.TrimPrefix(parts[1], "Resumen:"))

	// 3. Enviar resumen a "Mensajes Guardados"
	log.Println("Enviando resumen a 'Mensajes Guardados'...")
	randomID, _ := rand.Int(rand.Reader, big.NewInt(int64(^uint64(0)>>1)))
	_, err = api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     &tg.InputPeerSelf{},
		Message:  fmt.Sprintf("📝 **Resumen del audio de tu mujer:** \n\n%s", summary),
		RandomID: randomID.Int64(),
	})
	if err != nil {
		log.Printf("Error al enviar el mensaje de resumen: %+v", err)
	} else {
		log.Println("¡Resumen enviado con éxito!")
	}

	// 4. Generar respuesta afirmativa con Gemini
	log.Println("Generando respuesta afirmativa con Gemini...")
	affirmativeText, err := generateAffirmativeReply(ctx, model, transcription)
	if err != nil {
		log.Printf("Error al generar la respuesta afirmativa: %+v", err)
		return
	}
	log.Printf("Respuesta de texto generada: %s", affirmativeText)

	// 5. Generar nota de voz con ElevenLabs
	log.Println("Generando nota de voz con ElevenLabs...")
	voiceResponseData, err := generateVoiceResponse(ctx, cfg.ElevenLabsAPIKey, cfg.ElevenLabsVoiceID, affirmativeText)
	if err != nil {
		log.Printf("Error al generar la nota de voz: %+v", err)
		return
	}
	log.Println("Nota de voz generada con éxito.")

	// 6. Enviar nota de voz de respuesta al chat original
	log.Println("Enviando nota de voz de respuesta...")
	uploader := tUploader.NewUploader(api)

	uploadedFile, err := uploader.Upload(ctx, tUploader.NewUpload("response.mp3", bytes.NewReader(voiceResponseData), int64(len(voiceResponseData))))
	if err != nil {
		log.Printf("Error al subir la nota de voz de respuesta: %+v", err)
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
				&tg.DocumentAttributeAudio{Voice: true, Duration: 5}, // Duración es una estimación
			},
		},
	})
	if err != nil {
		log.Printf("Error al enviar la nota de voz de respuesta: %+v", err)
	} else {
		log.Println("¡Nota de voz de respuesta enviada con éxito!")
	}
}

// generateAffirmativeReply usa Gemini para crear una respuesta cariñosa.
func generateAffirmativeReply(ctx context.Context, model *genai.GenerativeModel, transcription string) (string, error) {
	prompt := fmt.Sprintf("Actúa como si fueras mi marido. Mi mujer, a la que llamo 'Amor', me ha enviado el siguiente mensaje: '%s'. Genera una respuesta corta usando vocabulario de Español de Madrid (máximo 20 palabras), amigable y amorosa, siempre dándole la razón y mostrando acuerdo. Usa un tono cercano y cariñoso. Refiérete a ella como 'Bey' o 'Amor' pero elige solo una de las dos. No digas cocosas como 'Beso' o 'Te quiero' ya que no se las suelo decir nunca.", transcription)
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", errors.Wrap(err, "fallo en la llamada a Gemini para la respuesta")
	}
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		return string(resp.Candidates[0].Content.Parts[0].(genai.Text)), nil
	}
	return "", errors.New("Gemini no generó una respuesta de texto")
}

// generateVoiceResponse convierte texto a audio usando ElevenLabs.
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
		return nil, errors.Wrap(err, "error al codificar el payload de ElevenLabs")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, errors.Wrap(err, "error al crear la petición a ElevenLabs")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "error al enviar la petición a ElevenLabs")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error de ElevenLabs: status %d, body: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// loadConfig carga las variables de entorno necesarias.
func loadConfig() (Config, error) {
	appIDStr := os.Getenv("APP_ID")
	if appIDStr == "" {
		return Config{}, errors.New("la variable de entorno APP_ID no está definida")
	}
	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		return Config{}, errors.Wrap(err, "APP_ID inválido")
	}

	targetUserIDStr := os.Getenv("TARGET_USER_ID")
	if targetUserIDStr == "" {
		return Config{}, errors.New("la variable de entorno TARGET_USER_ID no está definida")
	}
	targetUserID, err := strconv.ParseInt(targetUserIDStr, 10, 64)
	if err != nil {
		return Config{}, errors.Wrap(err, "TARGET_USER_ID inválido")
	}
	geminiModel := os.Getenv("GEMINI_MODEL")
	if geminiModel == "" {
		return Config{}, errors.New("la variable de entorno GEMINI_MODEL no está definida")
	}

	cfg := Config{
		AppID:             appID,
		AppHash:           os.Getenv("APP_HASH"),
		TargetUserID:      targetUserID,
		GeminiAPIKey:      os.Getenv("GEMINI_API_KEY"),
		GeminiModel:       geminiModel,
		ElevenLabsAPIKey:  os.Getenv("ELEVENLABS_API_KEY"),
		ElevenLabsVoiceID: os.Getenv("ELEVENLABS_VOICE_ID"),
	}

	if cfg.AppHash == "" || cfg.GeminiAPIKey == "" || cfg.ElevenLabsAPIKey == "" || cfg.ElevenLabsVoiceID == "" {
		return Config{}, errors.New("una o más variables de entorno requeridas no están definidas (APP_HASH, GEMINI_API_KEY, ELEVENLABS_API_KEY, ELEVENLABS_VOICE_ID)")
	}

	return cfg, nil
}

// termAuth implementa la interfaz de autenticación para la terminal.
type termAuth struct {
	phone string
}

func (a termAuth) Phone(_ context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	fmt.Print("Introduce tu número de teléfono: ")
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), nil
}

func (a termAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Introduce tu contraseña de doble factor (2FA): ")
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), nil
}

func (a termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Introduce el código de verificación: ")
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), nil
}

func (a termAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("registro no implementado")
}

func (a termAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	fmt.Println("Debes aceptar los términos de servicio para continuar.")
	fmt.Println(tos.Text)
	return errors.New("términos de servicio no aceptados")
}
