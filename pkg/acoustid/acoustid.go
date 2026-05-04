package acoustid

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/audd"
)

const lookupURL = "https://api.acoustid.org/v2/lookup"

type Client struct {
	APIKey string
}

type fpcalcOutput struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

type acoustidResponse struct {
	Status  string           `json:"status"`
	Results []acoustidResult `json:"results"`
}

type acoustidResult struct {
	Score      float64            `json:"score"`
	Recordings []acoustidRecording `json:"recordings"`
}

type acoustidRecording struct {
	Title   string          `json:"title"`
	Artists []acoustidArtist `json:"artists"`
}

type acoustidArtist struct {
	Name string `json:"name"`
}

// Recognize fingerprints audioPath via fpcalc and queries the AcoustID API.
// Returns nil, nil when the song is not recognized (not an error).
// Returns *audd.Result so callers can treat it identically to audd.Client.Recognize.
func (c *Client) Recognize(audioPath string) (*audd.Result, error) {
	fp, err := fingerprint(audioPath)
	if err != nil {
		return nil, fmt.Errorf("acoustid: fingerprint: %w", err)
	}

	params := url.Values{}
	params.Set("client", c.APIKey)
	params.Set("duration", strconv.FormatFloat(fp.Duration, 'f', 2, 64))
	params.Set("fingerprint", fp.Fingerprint)
	params.Set("meta", "recordings")

	resp, err := http.Get(lookupURL + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("acoustid: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	var res acoustidResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("acoustid: decode response: %w", err)
	}

	if res.Status != "ok" {
		return nil, fmt.Errorf("acoustid: API returned status %q", res.Status)
	}

	// Results are returned highest-score first; pick the first one with usable data.
	for _, r := range res.Results {
		for _, rec := range r.Recordings {
			if rec.Title == "" || len(rec.Artists) == 0 || rec.Artists[0].Name == "" {
				continue
			}
			return &audd.Result{
				Artist: rec.Artists[0].Name,
				Title:  rec.Title,
			}, nil
		}
	}

	return nil, nil
}

func fingerprint(audioPath string) (*fpcalcOutput, error) {
	out, err := exec.Command("fpcalc", "-json", audioPath).Output()
	if err != nil {
		return nil, fmt.Errorf("fpcalc: %w", err)
	}
	var fp fpcalcOutput
	if err := json.Unmarshal(out, &fp); err != nil {
		return nil, fmt.Errorf("fpcalc parse: %w", err)
	}
	return &fp, nil
}
