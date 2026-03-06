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

// We inline the HTML so the Go binary is 100% self-contained (Pxxl-friendly!)
const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AI Scraper Vision</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 800px; margin: 40px auto; padding: 20px; background: #1a1a1a; color: #fff; }
        input { width: 70%; padding: 10px; font-size: 16px; border-radius: 4px; border: 1px solid #444; background: #333; color: white; }
        button { padding: 10px 20px; font-size: 16px; cursor: pointer; background: #007bff; color: white; border: none; border-radius: 4px; }
        #output { margin-top: 20px; padding: 20px; background: #000; border: 1px solid #333; min-height: 200px; white-space: pre-wrap; font-family: monospace; line-height: 1.5; }
    </style>
</head>
<body>
    <h2>What does AI see?</h2>
    <input type="text" id="urlInput" placeholder="https://dev.to" value="https://dev.to">
    <button onclick="analyze()">Analyze</button>
    <div id="output">Waiting for URL...</div>

    <script>
        function analyze() {
            const url = document.getElementById('urlInput').value;
            const output = document.getElementById('output');
            output.innerText = "Fetching and analyzing...\n\n";

            const source = new EventSource('/analyze?url=' + encodeURIComponent(url));
            
            source.onmessage = function(event) {
                output.innerText += event.data.replace(/<br>/g, "\n") + " "; 
            };
            
            source.onerror = function() {
                source.close(); 
            };
        }
    </script>
</body>
</html>`

func main() {
	// Serve the inlined HTML directly
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, indexHTML)
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
	w.Header().Set("X-Accel-Buffering", "no")
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

	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Fprintf(w, "data: Error parsing HTML\n\n")
		flusher.Flush()
		return
	}
	cleanText := extractText(doc)

	if len(cleanText) > 10000 {
		cleanText = cleanText[:10000]
	}

	// 3. Call OpenRouter
	aiResponse := callOpenRouter(cleanText)

	// 4. Stream the response
	words := strings.Split(aiResponse, " ")
	for _, word := range words {
		safeWord := strings.ReplaceAll(word, "\n", "<br>")
		fmt.Fprintf(w, "data: %s\n\n", safeWord)
		flusher.Flush()
		time.Sleep(50 * time.Millisecond) 
	}
}

// extractText recursively grabs text, ignoring scripts and styles
func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data) + " "
	}
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript" || n.Data == "nav" || n.Data == "footer") {
		return ""
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += extractText(c)
	}
	return text
}

// callOpenRouter calls the OpenRouter REST API
func callOpenRouter(text string) string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	url := "https://openrouter.ai/api/v1/chat/completions"

	prompt := "You are an AI scraper analyzing a website's raw DOM text. Summarize what this page is about, who it is for, and point out what is missing or unclear (e.g., if it's a blank React SPA). Be honest, brief, and direct. Here is the text: " + text

	reqBody := map[string]interface{}{
		"model": "google/gemini-2.5-flash", 
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

	defer func() { recover() }() 
	
	choices := result["choices"].([]interface{})
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	return message["content"].(string)
}
