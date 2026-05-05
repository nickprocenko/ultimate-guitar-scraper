package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	_ "embed"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/acrcloud"
	"github.com/Pilfer/ultimate-guitar-scraper/pkg/acoustid"
	"github.com/Pilfer/ultimate-guitar-scraper/pkg/audd"
	"github.com/Pilfer/ultimate-guitar-scraper/pkg/ultimateguitar"
	"github.com/urfave/cli"
)

//go:embed web/index.html
var indexHTML []byte

// ServeCommand starts the web server.
var ServeCommand = cli.Command{
	Name:        "serve",
	Aliases:     []string{"s"},
	Usage:       "ug serve",
	Description: "Start the ChordFinder web server.\n\nEnv vars: ACOUSTID_API_KEY, AUDD_API_KEY, SUPABASE_URL, SUPABASE_KEY, PORT",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "port",
			Value: "8080",
			Usage: "Port to listen on (overridden by PORT env var)",
		},
		cli.StringFlag{
			Name:  "type",
			Value: "chords",
			Usage: "Preferred tab type: chords or tabs",
		},
	},
	Action: serveAction,
}

type appServer struct {
	acrClient      *acrcloud.Client
	acoustidClient *acoustid.Client
	auddClient     *audd.Client
	cache          *supabaseCache
	scraper        ultimateguitar.Scraper
	primaryType    ultimateguitar.TabType
	fallbackType   ultimateguitar.TabType
}

type identifyResponse struct {
	Detected *detectedSong              `json:"detected,omitempty"`
	Tab      *ultimateguitar.TabResult  `json:"tab,omitempty"`
	Error    string                     `json:"error,omitempty"`
	Cached   bool                       `json:"cached,omitempty"`
}

type detectedSong struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
}

func serveAction(c *cli.Context) {
	srv := &appServer{scraper: ultimateguitar.New()}

	// ffmpeg — warn but don't crash; identify will return an error if missing
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Println("Warning: ffmpeg not found — audio processing will fail")
	}

	// API keys — warn only; checked lazily in handleIdentify
	acrKey := os.Getenv("ACRCLOUD_ACCESS_KEY")
	acrSecret := os.Getenv("ACRCLOUD_ACCESS_SECRET")
	acrHost := os.Getenv("ACRCLOUD_HOST")
	acoustidKey := os.Getenv("ACOUSTID_API_KEY")
	auddKey := os.Getenv("AUDD_API_KEY")

	if acrKey != "" && acrSecret != "" && acrHost != "" {
		srv.acrClient = &acrcloud.Client{Host: acrHost, AccessKey: acrKey, AccessSecret: acrSecret}
		log.Println("ACRCloud enabled")
	} else if acrKey != "" {
		log.Println("Warning: ACRCloud needs ACRCLOUD_ACCESS_KEY, ACRCLOUD_ACCESS_SECRET and ACRCLOUD_HOST")
	}
	if acoustidKey != "" {
		if _, err := exec.LookPath("fpcalc"); err != nil {
			log.Println("Warning: fpcalc not found — AcoustID disabled")
		} else {
			srv.acoustidClient = &acoustid.Client{APIKey: acoustidKey}
		}
	}
	if auddKey != "" {
		srv.auddClient = &audd.Client{APIKey: auddKey}
	}
	if srv.acrClient == nil && srv.acoustidClient == nil && srv.auddClient == nil {
		log.Println("Warning: no API keys set — /api/identify will return no_keys_configured")
	}

	if supaURL, supaKey := os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"); supaURL != "" && supaKey != "" {
		srv.cache = &supabaseCache{URL: supaURL, APIKey: supaKey}
		log.Println("Supabase cache enabled")
	}

	preferChords := c.String("type") != "tabs"
	if preferChords {
		srv.primaryType = ultimateguitar.TabTypeChords
		srv.fallbackType = ultimateguitar.TabTypeTabs
	} else {
		srv.primaryType = ultimateguitar.TabTypeTabs
		srv.fallbackType = ultimateguitar.TabTypeChords
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = c.String("port")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/api/identify", srv.handleIdentify)

	log.Printf("ChordFinder listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func (srv *appServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func (srv *appServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (srv *appServer) handleIdentify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if srv.acrClient == nil && srv.acoustidClient == nil && srv.auddClient == nil {
		writeJSON(w, identifyResponse{Error: "no_keys_configured"})
		return
	}

	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, identifyResponse{Error: "invalid_request"})
		return
	}

	audioFile, _, err := r.FormFile("audio")
	if err != nil {
		writeJSON(w, identifyResponse{Error: "missing_audio"})
		return
	}
	defer audioFile.Close()

	// Save raw upload to temp file
	tmpRaw, err := os.CreateTemp("", "ug-raw-*")
	if err != nil {
		writeJSON(w, identifyResponse{Error: "internal_error"})
		return
	}
	defer os.Remove(tmpRaw.Name())
	if _, err := io.Copy(tmpRaw, audioFile); err != nil {
		tmpRaw.Close()
		writeJSON(w, identifyResponse{Error: "internal_error"})
		return
	}
	tmpRaw.Close()

	// Convert to WAV (handles WebM/Opus from Chrome, MP4 from Safari, OGG from Firefox)
	tmpWAV, err := os.CreateTemp("", "ug-web-*.wav")
	if err != nil {
		writeJSON(w, identifyResponse{Error: "internal_error"})
		return
	}
	tmpWAV.Close()
	defer os.Remove(tmpWAV.Name())

	if err := exec.Command("ffmpeg", "-i", tmpRaw.Name(), "-ar", "44100", "-ac", "1", "-y", tmpWAV.Name()).Run(); err != nil {
		writeJSON(w, identifyResponse{Error: "audio_processing_failed"})
		return
	}

	// Identify: AcoustID first, AudD fallback
	result, err := identify(srv.acrClient, srv.acoustidClient, srv.auddClient, tmpWAV.Name())
	if err != nil {
		log.Printf("identify error: %v", err)
		writeJSON(w, identifyResponse{Error: "identification_error"})
		return
	}
	if result == nil {
		writeJSON(w, identifyResponse{Error: "not_recognized"})
		return
	}

	detected := &detectedSong{Artist: result.Artist, Title: result.Title}
	cacheKey := strings.ToLower(result.Artist) + ":" + strings.ToLower(cleanTitle(result.Title))

	// Check Supabase cache
	if srv.cache != nil {
		if tab := srv.cache.Get(cacheKey); tab != nil {
			writeJSON(w, identifyResponse{Detected: detected, Tab: tab, Cached: true})
			return
		}
	}

	// Search Ultimate Guitar — try multiple query strategies
	tab, tabErr := searchAndFetch(srv.scraper, result.Title, result.Artist, srv.primaryType, srv.fallbackType)
	if tabErr != nil || tab == nil {
		writeJSON(w, identifyResponse{Detected: detected, Error: "no_tab_found"})
		return
	}

	// Store in cache
	if srv.cache != nil {
		srv.cache.Set(cacheKey, tab)
	}

	writeJSON(w, identifyResponse{Detected: detected, Tab: tab})
}

// searchAndFetch tries several query strategies against UG and returns the
// best tab found, or nil if nothing matches.
func searchAndFetch(s ultimateguitar.Scraper, title, artist string, primary, fallback ultimateguitar.TabType) (*ultimateguitar.TabResult, error) {
	clean := cleanTitle(title)
	normalized := normalizeNumbers(clean)
	queries := []string{
		clean,                      // title only
		normalized,                 // title with numbers as words (e.g. "million" not "1000000")
		clean + " " + artist,      // title + artist
		normalized + " " + artist, // normalized title + artist
		artist + " " + clean,      // artist + title
		artist,                    // artist only — last resort
	}
	// deduplicate while preserving order
	seen := map[string]bool{}
	unique := queries[:0]
	for _, q := range queries {
		if q != "" && !seen[q] {
			seen[q] = true
			unique = append(unique, q)
		}
	}
	queries = unique

	for _, q := range queries {
		for _, tabType := range []ultimateguitar.TabType{primary, fallback} {
			res, err := s.Search(ultimateguitar.SearchParams{
				Title: q,
				Type:  []ultimateguitar.TabType{tabType},
			})
			if err != nil || len(res.Tabs) == 0 {
				continue
			}
			best := selectBestTab(res.Tabs)
			tab, err := s.GetTabByID(best.ID)
			if err != nil {
				continue
			}
			return &tab, nil
		}
	}
	return nil, nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// ── Supabase cache ──────────────────────────────────────────────────────────

type supabaseCache struct {
	URL    string
	APIKey string
}

func (c *supabaseCache) Get(key string) *ultimateguitar.TabResult {
	endpoint := fmt.Sprintf("%s/rest/v1/tab_cache?cache_key=eq.%s&select=tab_data",
		c.URL, url.QueryEscape(key))
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("apikey", c.APIKey)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	var rows []struct {
		TabData json.RawMessage `json:"tab_data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil || len(rows) == 0 {
		return nil
	}

	var tab ultimateguitar.TabResult
	if err := json.Unmarshal(rows[0].TabData, &tab); err != nil {
		return nil
	}
	return &tab
}

func (c *supabaseCache) Set(key string, tab *ultimateguitar.TabResult) {
	tabData, err := json.Marshal(tab)
	if err != nil {
		return
	}

	body, _ := json.Marshal(map[string]interface{}{
		"cache_key": key,
		"tab_data":  json.RawMessage(tabData),
	})

	endpoint := fmt.Sprintf("%s/rest/v1/tab_cache", c.URL)
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("apikey", c.APIKey)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "resolution=merge-duplicates")

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
