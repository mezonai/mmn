package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type AlertmanagerWebhook struct {
	Status string  `json:"status"`
	Alerts []Alert `json:"alerts"`
}

type Alert struct {
	Annotations map[string]string `json:"annotations"`
	Labels      map[string]string `json:"labels"`
	Status      string            `json:"status"`
}

type MezonWebhook struct {
	Type    string       `json:"type"`
	Message MezonMessage `json:"message"`
}

type MezonMessage struct {
	Text     string         `json:"t"`
	Mentions []MezonMention `json:"mentions"`
}

type MezonMention struct {
	UserID string `json:"user_id"`
	E      int    `json:"e"`
}

type WebhookHandler struct {
	mezonWebhookURL string
	httpClient      *http.Client
}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{
		mezonWebhookURL: os.Getenv("MEZON_WEBHOOK_URL"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var alertWebhook AlertmanagerWebhook
	if err := json.Unmarshal(body, &alertWebhook); err != nil {
		log.Printf("Error parsing webhook JSON: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received webhook: status=%s, alerts=%d", alertWebhook.Status, len(alertWebhook.Alerts))

	success := h.processAlerts(&alertWebhook)

	response := map[string]string{"status": "success"}
	if !success {
		response["status"] = "error"
		w.WriteHeader(http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *WebhookHandler) processAlerts(webhook *AlertmanagerWebhook) bool {
	if len(webhook.Alerts) == 0 {
		log.Println("No alerts to process")
		return true
	}

	// Process each alert
	for _, alert := range webhook.Alerts {
		if err := h.sendToMezon(&alert); err != nil {
			log.Printf("Error sending alert to Mezon: %v", err)
			return false
		}
	}

	return true
}

func (h *WebhookHandler) sendToMezon(alert *Alert) error {
	message, exists := alert.Annotations["message"]
	if !exists {
		log.Println("No message found in alert annotations")
		return fmt.Errorf("no message in alert annotations")
	}

	mezonWebhook := MezonWebhook{
		Type: "hook",
		Message: MezonMessage{
			Text: message,
			Mentions: []MezonMention{
				{
					UserID: "1775731111020111321", //user_id of @here
					E:      5,
				},
			},
		},
	}

	jsonData, err := json.Marshal(mezonWebhook)
	if err != nil {
		return fmt.Errorf("error marshaling Mezon webhook: %v", err)
	}

	req, err := http.NewRequest("POST", h.mezonWebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request to Mezon: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("Successfully sent to Mezon: %d", resp.StatusCode)
	return nil
}

func main() {
	port := "8080"
	handler := NewWebhookHandler()

	http.Handle("/", handler)

	log.Printf("Webhook receiver starting on port %s", port)
	log.Printf("Mezon webhook URL: %s", handler.mezonWebhookURL)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
