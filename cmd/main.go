package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	ffmpeg "github.com/u2takey/ffmpeg-go"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	botToken   = assertEnv("TELEGRAM_BOT_TOKEN")
	groqAPIKey = assertEnv("GROQ_API_KEY")
)

func main() {
	bot, err := tgbotapi.NewBotAPI(botToken)
	handlerErr("NewBotAPI", err)

	for update := range bot.GetUpdatesChan(tgbotapi.NewUpdate(0)) {
		if update.Message == nil || update.Message.Voice == nil {
			continue
		}

		go handleAudioUpdate(bot, update)
	}
}

func handleAudioUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	file, err := bot.GetFile(tgbotapi.FileConfig{
		FileID: update.Message.Voice.FileID,
	})
	handlerErr("GetFile", err)

	fileLink := file.Link(bot.Token)
	log.Printf("file link: %s", fileLink)

	fileRes, err := http.DefaultClient.Get(fileLink)
	handlerErr("http.Get(fileLink)", err)

	convertedFile, err := convertFile(fileRes.Body)
	handlerErr("convertFile", err)
	defer convertedFile.Close()

	// audioText, err := getAudioText(convertedFile)
	audioText, err := getAudioTranslation(convertedFile)
	handlerErr("upload audio", err)

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, audioText)
	_, err = bot.Send(msg)
	handlerErr("bot.Send", err)
}

func getAudioText(file io.ReadCloser) (t string, err error) {
	values := map[string]io.Reader{
		"model":           strings.NewReader("distil-whisper-large-v3-en"),
		"response_format": strings.NewReader("json"),
		"language":        strings.NewReader("en"),
		"temperature":     strings.NewReader("0"),
		"file":            file,
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}

		if _, ok := r.(io.ReadCloser); ok {
			if fw, err = w.CreateFormFile(key, "file.ogg"); err != nil {
				return
			}
		} else {
			if fw, err = w.CreateFormField(key); err != nil {
				return
			}
		}
		if _, err = io.Copy(fw, r); err != nil {
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/audio/transcriptions", &b)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "bearer "+groqAPIKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}

	var body bytes.Buffer
	_, err = body.ReadFrom(res.Body)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status (%d): %s", res.StatusCode, body.String())
	}

	var r struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body.Bytes(), &r); err != nil {
		return "", err
	}

	return r.Text, nil
}

func getAudioTranslation(file io.ReadCloser) (t string, err error) {
	values := map[string]io.Reader{
		// TODO: handle prompt as options
		// "prompt":     strings.NewReader(""),
		"model":           strings.NewReader("whisper-large-v3"),
		"response_format": strings.NewReader("json"),
		"language":        strings.NewReader("pt"),
		"temperature":     strings.NewReader("0"),
		"file":            file,
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}

		if _, ok := r.(io.ReadCloser); ok {
			if fw, err = w.CreateFormFile(key, "file.ogg"); err != nil {
				return
			}
		} else {
			if fw, err = w.CreateFormField(key); err != nil {
				return
			}
		}
		if _, err = io.Copy(fw, r); err != nil {
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/audio/transcriptions", &b)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "bearer "+groqAPIKey)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}

	var body bytes.Buffer
	_, err = body.ReadFrom(res.Body)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status (%d): %s", res.StatusCode, body.String())
	}

	var r struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body.Bytes(), &r); err != nil {
		return "", err
	}

	return r.Text, nil
}

func convertFile(input io.ReadCloser) (io.ReadCloser, error) {
	tmpInputFile, err := os.CreateTemp("", "input*.oga")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp input file: %v", err)
	}
	defer os.Remove(tmpInputFile.Name())
	defer input.Close()

	_, err = io.Copy(tmpInputFile, input)
	if err != nil {
		return nil, fmt.Errorf("failed to write input to temp file: %v", err)
	}
	tmpInputFile.Close()

	var outputBuffer bytes.Buffer
	err = ffmpeg.Input(tmpInputFile.Name()).
		Output("pipe:", ffmpeg.KwArgs{"ar": "16000", "ac": "1", "f": "ogg"}).
		WithOutput(&outputBuffer, os.Stderr).
		Run()

	if err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %v", err)
	}

	return io.NopCloser(bytes.NewReader(outputBuffer.Bytes())), nil
}

func assertEnv(key string) string {
	if os.Getenv(key) == "" {
		log.Fatalf("env %s not set", key)
	}
	return os.Getenv(key)
}

func handlerErr(prefix string, err error) {
	if err != nil {
		log.Fatalf("[%s] %v", prefix, err)
	}
}
