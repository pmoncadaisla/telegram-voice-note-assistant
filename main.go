package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/go-faster/errors"
	"github.com/google/generative-ai-go/genai"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
	"google.golang.org/api/option"
)

// Config almacena todas las variables de entorno necesarias.
type Config struct {
	AppID        int
	AppHash      string
	TargetUserID int64
	GeminiAPIKey string
}

func main() {
	// Es buena práctica ejecutar la lógica principal en una función que devuelva un error.
	if err := run(context.Background()); err != nil {
		log.Fatalf("Error: %+v", err)
	}
}

func run(ctx context.Context) error {
	// 1. CARGAR CONFIGURACIÓN DESDE VARIABLES DE ENTORNO
	// Es más seguro que tener las claves directamente en el código.
	// EJEMPLO DE CÓMO ESTABLECERLAS EN TU TERMINAL:
	// export APP_ID=12345
	// export APP_HASH=abcdef...
	// export TARGET_USER_ID=987654321
	// export GEMINI_API_KEY=AIzaSy...
	cfg, err := loadConfig()
	if err != nil {
		return errors.Wrap(err, "no se pudo cargar la configuración")
	}

	// 2. INICIALIZAR EL CLIENTE DE GEMINI
	geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
	if err != nil {
		return errors.Wrap(err, "error al crear el cliente de Gemini")
	}
	defer geminiClient.Close()
	geminiModel := geminiClient.GenerativeModel("models/gemini-2.5-flash") // Usamos Flash por velocidad y coste

	// 3. CONFIGURAR EL DISPATCHER DE TELEGRAM
	// El dispatcher se encarga de dirigir las actualizaciones (como nuevos mensajes)
	// al manejador correcto.
	dispatcher := tg.NewUpdateDispatcher()

	// 4. CONFIGURAR EL CLIENTE DE TELEGRAM (gotd)
	// Usamos un archivo de sesión para no tener que iniciar sesión cada vez.
	client := telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: "telegram-session.json"},
		UpdateHandler:  dispatcher,
	})

	// 5. REGISTRAR EL MANEJADOR DE MENSAJES
	// Aquí es donde ocurre la magia. Le decimos al dispatcher qué hacer
	// cuando llega un nuevo mensaje.
	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if !ok || msg.Out {
			// Ignorar si no es un mensaje de texto/media o si es enviado por nosotros.
			return nil
		}

		// Filtrar: ¿es un documento de tipo audio/ogg?
		doc, ok := msg.Media.(*tg.MessageMediaDocument)
		if !ok {
			return nil
		}
		document, ok := doc.Document.AsNotEmpty()
		if !ok || document.MimeType != "audio/ogg" {
			return nil
		}

		// Filtrar: ¿es del usuario que nos interesa?
		peer, ok := msg.PeerID.(*tg.PeerUser)
		if !ok || peer.UserID != cfg.TargetUserID {
			return nil
		}

		log.Println("Nota de voz recibida del usuario objetivo. Procesando en segundo plano...")

		// Procesar en una goroutine para no bloquear el manejador de eventos.
		// Esto permite al bot seguir recibiendo mensajes mientras procesa el audio.
		go processVoiceNote(context.Background(), client, geminiModel, msg)

		return nil
	})

	// 6. EJECUTAR EL CLIENTE
	return client.Run(ctx, func(ctx context.Context) error {
		// Realizar la autenticación si es necesario.
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return errors.Wrap(err, "fallo al obtener estado de autenticación")
		}

		if !status.Authorized {
			// Si no estamos autorizados, iniciar el flujo de autenticación en la terminal.
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
			codeBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return errors.Wrap(err, "fallo al leer el código de verificación")
			}
			fmt.Println() // Add a newline after reading the code
			verificationCode := string(codeBytes)

			if _, err := client.Auth().SignIn(ctx, phone, verificationCode, code.PhoneCodeHash); err != nil {
				if errors.Is(err, auth.ErrPasswordAuthNeeded) {
					fmt.Print("Introduce tu contraseña de doble factor (2FA): ")
					passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
					if err != nil {
						return errors.Wrap(err, "fallo al leer la contraseña 2FA")
					}
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
		// Esperar hasta que el contexto se cancele (ej. Ctrl+C)
		<-ctx.Done()
		return ctx.Err()
	})
}

// processVoiceNote se encarga de la lógica pesada: descargar, enviar a Gemini y responder.
func processVoiceNote(ctx context.Context, client *telegram.Client, model *genai.GenerativeModel, msg *tg.Message) {
	api := client.API()
	d := downloader.NewDownloader()

	// 1. Descargar el archivo de audio
	log.Println("Descargando audio...")
	doc, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		log.Println("Error: no se pudo obtener el media como documento.")
		return
	}
	file, ok := doc.Document.AsNotEmpty()
	if !ok {
		log.Println("Error: el documento está vacío.")
		return
	}

	buf := &strings.Builder{} // Usamos un buffer en memoria
	result, err := d.Download(api, file.AsInputDocumentFileLocation()).Stream(ctx, buf)
	if err != nil {
		log.Printf("Error al descargar el archivo: %+v", err)
		return
	}
	// Reemplazar la validación de `result.OK` con una verificación adecuada
	if result == nil {
		log.Println("Error: la descarga no fue exitosa.")
		return
	}

	// 2. Subir y procesar con Gemini
	log.Println("Enviando audio a Gemini...")
	prompt := "Eres un asistente que recibe notas de voz. Transcribe la nota de voz de mi mujer y luego genera un resumen conciso en español con los puntos más importantes. Da un resumen breve y claro, evitando detalles innecesarios. El mensaje de respuesta debe estar formateado para Telegram."

	// Cambiar `genai.Audio` por `genai.Content` para enviar datos genéricos al modelo
	resp, err := model.GenerateContent(ctx, genai.Blob{
		MIMEType: "audio/ogg",
		Data:     []byte(buf.String()),
	}, genai.Text(prompt))
	if err != nil {
		log.Printf("Error al generar contenido con Gemini: %+v", err)
		return
	}

	// 3. Extraer la respuesta y enviar el resumen a "Mensajes Guardados"
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		part := resp.Candidates[0].Content.Parts[0]
		if txt, ok := part.(genai.Text); ok {
			summary := string(txt)
			log.Println("Resumen generado. Enviando a 'Mensajes Guardados'...")

			randomID, err := rand.Int(rand.Reader, big.NewInt(int64(^uint64(0)>>1)))
			if err != nil {
				log.Printf("Error al generar randomID: %+v", err)
				return
			}

			_, err = api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
				Peer:     &tg.InputPeerSelf{},
				Message:  fmt.Sprintf("📝 **Resumen del audio de tu mujer:**\n\n%s", summary),
				RandomID: randomID.Int64(),
			})

			if err != nil {
				log.Printf("Error al enviar el mensaje de resumen: %+v", err)
			} else {
				log.Println("¡Resumen enviado con éxito!")
			}
		}
	}
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

	cfg := Config{
		AppID:        appID,
		AppHash:      os.Getenv("APP_HASH"),
		TargetUserID: targetUserID,
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
	}

	if cfg.AppHash == "" || cfg.GeminiAPIKey == "" {
		return Config{}, errors.New("las variables APP_HASH o GEMINI_API_KEY no están definidas")
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
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func (a termAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Introduce tu contraseña de doble factor (2FA): ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func (a termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Introduce el código de verificación: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func (a termAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("registro no implementado")
}

func (a termAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	fmt.Println("Debes aceptar los términos de servicio para continuar.")
	fmt.Println(tos.Text)
	return errors.New("términos de servicio no aceptados")
}
