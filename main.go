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

// Single file solution (Pxxl-friendly)
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
    <input type="text" id="urlInput" placeholder="https://example.com" value="https://example.com">
    <button onclick="analyze()">Analyze</button>
    <div id="output">Waiting for URL...</div>

    <script>
        async function analyze() {
            const url = document.getElementById('urlInput').value;
            const output = document.getElementById('output');
            output.innerText = "Analyzing site content... (this takes ~10s)\n\n";

            try {
                const response = await fetch('/analyze?url=' + encodeURIComponent(url));
                if (!response.ok) {
                    const errText = await response.text();
                    output.innerText = "Server Error: " + errText;
                    return;
                }

                const text = await response.text();
                output.innerText = "";
                
                // Typing effect
                const words = text.split(" ");
                let i = 0;
                const interval = setInterval(() => {
                    if (i < words.length) {
                        output.innerText += words[i] + " ";
                        i++;
                    } else {
                        clearInterval(interval);
                    }
                }, 40);
            } catch (err) {
                output.innerText = "Network Error: Check connection.";
            }
        }
    </script>
</body>
</html>`

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, indexHTML)
	})

	http.HandleFunc("/analyze", analyzeHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println("Server running on port " + port)
	http.ListenAndServe(":"+port, nil)
}

func analyzeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		fmt.Fprintf(w, "Request Error: %s", err.Error())
		return
	}
	// Disguise as browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(w, "Scrape Error: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Fprint(w, "Parse Error.")
		return
	}
	
	cleanText := extractText(doc)
	
	// Aggressive trim to keep it free and fast
	if len(cleanText) > 1200 {
		cleanText = cleanText[:1200]
	}

	aiResponse := callOpenRouter(cleanText)
	fmt.Fprint(w, aiResponse)
}

func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data) + " "
	}
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "nav" || n.Data == "footer") {
		return ""
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += extractText(c)
	}
	return text
}

func callOpenRouter(text string) string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "API ERROR: Set OPENROUTER_API_KEY in Pxxl settings."
	}

	url := "https://openrouter.ai/api/v1/chat/completions"
	prompt := "Analyze this raw website text. Summarize the purpose and note if it looks like a blank JS/React app. Text: " + text

	reqBody := map[string]interface{}{
		"model": "google/gemma-3-27b-it:free", 
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 250, // CRITICAL: Keeps the request "cheap" enough for free tier
	}
	
	jsonBody, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// 15s timeout for AI response
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "AI API Error: " + err.Error()
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)

	// Catch OpenRouter error messages
	if errMap, hasErr := result["error"].(map[string]interface{}); hasErr {
		return "OpenRouter Says: " + fmt.Sprint(errMap["message"])
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "Error: No response from AI."
	}
	
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	return message["content"].(string)
}
