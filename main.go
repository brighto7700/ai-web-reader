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
        async function analyze() {
            const url = document.getElementById('urlInput').value;
            const output = document.getElementById('output');
            
            // Tell the user we are working (since fetching takes a few seconds)
            output.innerText = "Fetching raw HTML and analyzing... (this usually takes 5-10 seconds)\n\n";

            try {
                // Standard fetch request - no SSE needed!
                const response = await fetch('/analyze?url=' + encodeURIComponent(url));
                const text = await response.text();
                
                // Clear the loading message
                output.innerText = "";
                
                // Simulated streaming effect (Frontend trick!)
                const words = text.split(" ");
                let i = 0;
                
                const interval = setInterval(() => {
                    if (i < words.length) {
                        output.innerText += words[i] + " ";
                        i++;
                    } else {
                        clearInterval(interval); // Stop typing when done
                    }
                }, 40); // 40ms per word = nice reading speed

            } catch (err) {
                output.innerText = "Error connecting to the server.";
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
	// Standard text response
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	resp, err := http.Get(targetURL)
	if err != nil {
		fmt.Fprintf(w, "Error fetching URL: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Fprintf(w, "Error parsing HTML data.")
		return
	}
	cleanText := extractText(doc)

	if len(cleanText) > 10000 {
		cleanText = cleanText[:10000]
	}

	// Call OpenRouter
	aiResponse := callOpenRouter(cleanText)

	// Send the entire response back at once (JavaScript will handle the typing effect)
	fmt.Fprint(w, aiResponse)
}

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

func callOpenRouter(text string) string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "API ERROR: OPENROUTER_API_KEY environment variable is missing on the server."
	}

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
	req.Header.Set("HTTP-Referer", "https://github.com/what-ai-sees")
	req.Header.Set("X-Title", "AI Web Scraper Vision")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "Error calling AI API: " + err.Error()
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)

	if errMap, hasErr := result["error"].(map[string]interface{}); hasErr {
		if msg, ok := errMap["message"].(string); ok {
			return "OpenRouter Error: " + msg
		}
	}

	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "Error: Unexpected empty response from OpenRouter."
	}
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	return message["content"].(string)
}
