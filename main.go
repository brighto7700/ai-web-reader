package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/html"
)

func main() {
	// Serve the frontend UI
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Handle the scraping and AI analysis
	http.HandleFunc("/analyze", analyzeHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println("Server running on port " + port)
	http.ListenAndServe(":"+port, nil)
}

func analyzeHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Set headers for Server-Sent Events (SSE)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		return
	}

	// 2. Scrape the URL
	resp, err := http.Get(targetURL)
	if err != nil {
		fmt.Fprintf(w, "data: Error fetching URL\n\n")
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	// Parse the HTML and extract clean text
	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Fprintf(w, "data: Error parsing HTML\n\n")
		flusher.Flush()
		return
	}
	cleanText := extractText(doc)

	// Trim text so we don't exceed AI token limits (approx 10,000 chars for safety)
	if len(cleanText) > 10000 {
		cleanText = cleanText[:10000]
	}

	// 3. Call OpenRouter (Using Gemini)
	aiResponse := callOpenRouter(cleanText)

	// 4. Stream the response back to the UI word-by-word (Simulated stream)
	words := strings.Split(aiResponse, " ")
	for _, word := range words {
		// Replace newlines with a token so the SSE format doesn't break
		safeWord := strings.ReplaceAll(word, "\n", "<br>")
		fmt.Fprintf(w, "data: %s\n\n", safeWord)
		flusher.Flush()
		time.Sleep(50 * time.Millisecond) // Creates the live "typing" effect
	}
}

// extractText recursively grabs text, ignoring scripts, styles, and empty elements
func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data) + " "
	}
	// Ignore noisy tags that bots usually skip
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript" || n.Data == "nav" || n.Data == "footer") {
		return ""
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += extractText(c)
	}
	return text
}

// callOpenRouter calls the OpenRouter REST API using a free Gemini model
func callOpenRouter(text string) string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	url := "https://openrouter.ai/api/v1/chat/completions"

	prompt := "You are an AI scraper analyzing a website's raw DOM text. Summarize what this page is about, who it is for, and point out what is missing or unclear (e.g., if it's a blank React SPA). Be honest, brief, and direct. Here is the text: " + text

	reqBody := map[string]interface{}{
		"model": "google/gemini-2.5-flash", // Using Gemini via OpenRouter
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "Error creating request."
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/yourusername/what-ai-sees")
	req.Header.Set("X-Title", "AI Web Scraper Vision")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "Error calling AI API."
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)

	// Safely extract the text from the OpenAI-style JSON format
	defer func() { recover() }() 
	
	choices := result["choices"].([]interface{})
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	return message["content"].(string)
}
