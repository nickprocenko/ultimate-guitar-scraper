package acrcloud

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/audd"
)

type Client struct {
	Host         string
	AccessKey    string
	AccessSecret string
}

type acrResponse struct {
	Status struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"status"`
	Metadata *struct {
		Music []acrMusic `json:"music"`
	} `json:"metadata"`
}

type acrMusic struct {
	Title   string `json:"title"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

// Recognize sends audio to ACRCloud and returns the identified song.
// Returns nil, nil when the song is not recognized.
func (c *Client) Recognize(audioPath string) (*audd.Result, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("acrcloud: open: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("acrcloud: stat: %w", err)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("access_key", c.AccessKey)
	mw.WriteField("sample_bytes", strconv.FormatInt(fi.Size(), 10))
	mw.WriteField("timestamp", timestamp)
	mw.WriteField("signature", c.sign(timestamp))
	mw.WriteField("data_type", "audio")
	mw.WriteField("signature_version", "1")

	part, err := mw.CreateFormFile("sample", filepath.Base(audioPath))
	if err != nil {
		return nil, fmt.Errorf("acrcloud: create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("acrcloud: copy audio: %w", err)
	}
	mw.Close()

	url := fmt.Sprintf("https://%s/v1/identify", c.Host)
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return nil, fmt.Errorf("acrcloud: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acrcloud: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	var res acrResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("acrcloud: decode response: %w", err)
	}

	// Code 1001 = no result found
	if res.Status.Code == 1001 {
		return nil, nil
	}
	if res.Status.Code != 0 {
		return nil, fmt.Errorf("acrcloud: API error %d: %s", res.Status.Code, res.Status.Msg)
	}

	if res.Metadata == nil || len(res.Metadata.Music) == 0 {
		return nil, nil
	}

	m := res.Metadata.Music[0]
	artist := ""
	if len(m.Artists) > 0 {
		artist = m.Artists[0].Name
	}

	return &audd.Result{Artist: artist, Title: m.Title}, nil
}

func (c *Client) sign(timestamp string) string {
	str := fmt.Sprintf("POST\n/v1/identify\n%s\naudio\n1\n%s", c.AccessKey, timestamp)
	mac := hmac.New(sha1.New, []byte(c.AccessSecret))
	mac.Write([]byte(str))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
