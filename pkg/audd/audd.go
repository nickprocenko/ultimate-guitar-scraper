package audd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

const endpoint = "https://api.audd.io/"

type Client struct {
	APIKey string
}

type Result struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Album  string `json:"album"`
}

type response struct {
	Status  string  `json:"status"`
	Result  *Result `json:"result"`
	Error   *apiErr `json:"error"`
}

type apiErr struct {
	ErrorCode    int    `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// Recognize sends the audio file at audioPath to AudD and returns the
// identified song, or an error if the song could not be recognized.
func (c *Client) Recognize(audioPath string) (*Result, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("audd: open audio file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if err := mw.WriteField("api_token", c.APIKey); err != nil {
		return nil, fmt.Errorf("audd: write api_token field: %w", err)
	}

	part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, fmt.Errorf("audd: create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("audd: copy audio data: %w", err)
	}
	mw.Close()

	req, err := http.NewRequest("POST", endpoint, &buf)
	if err != nil {
		return nil, fmt.Errorf("audd: create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audd: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	var res response
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("audd: decode response: %w", err)
	}

	if res.Status != "success" {
		if res.Error != nil {
			return nil, fmt.Errorf("audd: API error %d: %s", res.Error.ErrorCode, res.Error.ErrorMessage)
		}
		return nil, fmt.Errorf("audd: unrecognized response status %q", res.Status)
	}

	if res.Result == nil {
		return nil, nil
	}

	return res.Result, nil
}
