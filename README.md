# Telegram Voice Note Assistant

> **Disclaimer:** This is a proof-of-concept project created for fun to explore the capabilities of modern AI APIs. It is not intended for production use and should be used with care.

This is a personal Go-based Telegram bot designed to automate responses to voice notes from a specific person. When a new voice message is received from the target user, the bot transcribes it, generates a summary for you, and sends a custom, AI-generated voice reply back to the sender.

This project uses Google's Gemini API for all language-based tasks (transcription, summary, and response generation) and the ElevenLabs API for realistic text-to-speech conversion with a cloned voice.

## ✨ Features

- **Automatic Voice Note Processing**: Listens for new voice messages from a predefined Telegram user.
- **AI-Powered Transcription & Summary**: Uses the Gemini API to accurately transcribe the audio and create a concise summary.
- **Contextual & Loving Replies**: Generates a short, affirmative, and affectionate text response in the persona of a husband, always agreeing with the sender.
- **Realistic Voice Generation**: Converts the generated text reply into a natural-sounding voice message using a specific cloned voice from ElevenLabs.
- **Private Summaries**: Sends the summary of the voice note to your "Saved Messages" in Telegram for your private review.
- **Secure Configuration**: All sensitive keys and IDs are managed via environment variables.

## 🚀 Getting Started

Follow these steps to get the bot up and running on your own machine.

### Prerequisites

- **Go**: Version 1.21 or later.
- **Telegram Account**: You need an active Telegram account.
- **Telegram API Credentials**: Get your `APP_ID` and `APP_HASH` from [my.telegram.org](https://my.telegram.org).
- **Google Gemini API Key**: Get your key from [Google AI Studio](https://aistudio.google.com/app/apikey).
- **ElevenLabs API Key & Voice ID**: Get your API key and the ID of your cloned voice from your [ElevenLabs account](https://elevenlabs.io/).

### 1. Clone the Repository

```bash
git clone <your-repository-url>
cd telegram-gemini-go
```

### 2. Set Up Environment Variables

The bot is configured using environment variables. You can export them directly in your shell or create a script to load them.

```sh
# Telegram API credentials
export APP_ID="YOUR_TELEGRAM_APP_ID"
export APP_HASH="YOUR_TELEGRAM_APP_HASH"

# Phone number associated with your Telegram account
export PHONE_NUMBER="+1234567890"

# The Telegram User ID of the person you want to respond to
export TARGET_USER_ID="THEIR_TELEGRAM_USER_ID"

# API Keys and Voice ID
export GEMINI_API_KEY="YOUR_GEMINI_API_KEY"
export GEMINI_MODEL="models/gemini-2.5-flash"
export ELEVENLABS_API_KEY="YOUR_ELEVENLABS_API_KEY"
export ELEVENLABS_VOICE_ID="YOUR_ELEVENLABS_VOICE_ID"
```

**How to find the `TARGET_USER_ID`**: You can find a user's ID by forwarding one of their messages to a bot like `@userinfobot`.

### 3. Run the Application

Once the environment variables are set, you can run the bot directly. The first time you run it, you may be prompted to enter your phone number, 2FA password, and a verification code in the terminal to log in to Telegram.

```bash
go run .
```

The bot is now running and will process voice notes as they arrive.

## 🛠️ Development & Building

### Code Structure

The codebase is organized into several files for clarity and maintainability:

- `main.go`: Contains the main application loop, client setup, and dispatcher logic.
- `config.go`: Handles loading and validation of environment variables.
- `telegram_handler.go`: Contains the core logic for processing a voice note (`processVoiceNote`).
- `ai_services.go`: Manages all interactions with external APIs (Gemini and ElevenLabs).
- `auth.go`: Implements the terminal-based authentication flow for Telegram.

### Dependencies

To ensure all dependencies are correct, you can run:

```bash
go mod tidy
```

### Building from Source

To compile the application into a single executable binary:

```bash
go build -o telegram-gemini-bot
```

You can then run the bot using `./telegram-gemini-bot`.

## 📄 License

This project is licensed under the MIT License. See the `LICENSE` file for details.
