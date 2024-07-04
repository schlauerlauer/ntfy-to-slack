package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	version            = "v1.3 2024-07-04"
	upstreamNtfyServer = "ntfy.sh"
)

var (
	defaultNtfyDomain = upstreamNtfyServer
	ntfyDomain        *string
	ntfyTopic         *string
	ntfyAuth          *string
	slackWebhookUrl   *string
)

type ntfyMessage struct {
	Id      string
	Time    int64
	Event   string
	Topic   string
	Title   string
	Message string
}

type slackMessage struct {
	Text string `json:"text"`
}

func main() {
	if logLevel, ok := os.LookupEnv("LOG_LEVEL"); ok {
		switch logLevel {
		case "debug":
			slog.SetLogLoggerLevel(slog.LevelDebug)
		case "warn":
			slog.SetLogLoggerLevel(slog.LevelWarn)
		case "error":
			slog.SetLogLoggerLevel(slog.LevelError)
		default:
			slog.SetLogLoggerLevel(slog.LevelInfo)
		}
	} else {
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}

	var envNtfyDomain, ok = os.LookupEnv("NTFY_DOMAIN")
	if ok {
		defaultNtfyDomain = envNtfyDomain
	}
	envNtfyTopic, _ := os.LookupEnv("NTFY_TOPIC")
	envNtfyAuth, _ := os.LookupEnv("NTFY_AUTH")
	envSlackWebhookUrl, _ := os.LookupEnv("SLACK_WEBHOOK_URL")

	ntfyDomain = flag.String("ntfy-domain", defaultNtfyDomain, "Choose the ntfy server to interact with.\nDefaults to "+upstreamNtfyServer+" or the value of the NTFY_DOMAIN env var, if it is set")
	ntfyTopic = flag.String("ntfy-topic", envNtfyTopic, "Choose the ntfy topic to interact with\nDefaults to the value of the NTFY_TOPIC env var, if it is set")
	ntfyAuth = flag.String("ntfy-auth", envNtfyAuth, "Specify token for reserved topics")
	slackWebhookUrl = flag.String("slack-webhook", envSlackWebhookUrl, "Choose the slack webhook url to send messages to\nDefaults to the value of the SLACK_WEBHOOK_URL env var, if it is set")
	versionFlag := flag.Bool("v", false, "prints current ntfy-to-slack version")

	flag.Parse()

	if *versionFlag {
		println(version)
		os.Exit(0)
	}

	for {
		if err := waitForNtfyMessage(); err != nil {
			slog.Error("waitForNtfyMessage", "err", err)
		} else {
			slog.Info("connection closed, restarting")
		}
		time.Sleep(30 * time.Second)
	}
}

func waitForNtfyMessage() error {
	client := &http.Client{}
	req, err := http.NewRequest(
		http.MethodGet,
		"https://"+*ntfyDomain+"/"+*ntfyTopic+"/json",
		nil,
	)
	if err != nil {
		slog.Error("error getting ntfy response", "err", err)
		return err
	}
	if ntfyAuth != nil {
		req.Header.Add("Authorization", "Bearer "+*ntfyAuth)
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("error connecting to ntfy server", "err", err)
		return err
	} else if resp.StatusCode != http.StatusOK {
		slog.Error("invalid status code", "expected", http.StatusOK, "domain", *ntfyDomain, "statusCode", strconv.FormatInt(int64(resp.StatusCode), 10))
		return errors.New("invalid response code from ntfy")
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg ntfyMessage
		err := json.Unmarshal([]byte(scanner.Text()), &msg)
		if err != nil {
			slog.Error("error while processing ntfy message", "err", err, "text", scanner.Text())
			continue
		}

		switch msg.Event {
		case "open":
			slog.Info("subscription established", "domain", *ntfyDomain)
			continue
		case "keepalive":
			slog.Debug("keepalive")
			continue
		case "message":
			slog.Info("sending message", "title", msg.Title, "message", msg.Message)
			if msg.Title != "" {
				go sendToSlack(&slackMessage{
					Text: "**" + msg.Title + "**: " + msg.Message,
				})
			} else {
				go sendToSlack(&slackMessage{
					Text: msg.Message,
				})
			}
			continue
		default:
			slog.Warn("bad message received", "message", scanner.Text())
			continue
		}
	}

	return nil
}

func sendToSlack(webhook *slackMessage) error {
	if webhook == nil {
		return errors.New("webhook undefined")
	}

	jsonBytes, err := json.Marshal(webhook)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		*slackWebhookUrl,
		bytes.NewBuffer(jsonBytes),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			slog.Error("error closing response body", "err", err)
		}
	}(resp.Body)

	if body, err := io.ReadAll(resp.Body); err != nil {
		slog.Error("error parsing body", "err", err)
		return err
	} else {
		slog.Debug("slack response", "status", resp.StatusCode, "body", body)
	}

	if resp.StatusCode >= 400 {
		return errors.New("error status code " + strconv.FormatInt(int64(resp.StatusCode), 10))
	}

	return nil
}
