package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/acrcloud"
	"github.com/Pilfer/ultimate-guitar-scraper/pkg/acoustid"
	"github.com/Pilfer/ultimate-guitar-scraper/pkg/audd"
	"github.com/Pilfer/ultimate-guitar-scraper/pkg/ultimateguitar"
	"github.com/cheggaaa/pb/v3"
	"github.com/fatih/color"
	"github.com/urfave/cli"
)

var ListenCommand = cli.Command{
	Name:        "listen",
	Aliases:     []string{"l"},
	Usage:       "ug listen",
	Description: "Listen to audio, detect the song, and display its chords.\n\nUses AcoustID (free, unlimited) first, falls back to AudD (100/month free).\nRequires ffmpeg. AcoustID also requires fpcalc (chromaprint).\n\nKeys via env vars: ACOUSTID_API_KEY, AUDD_API_KEY\nGet AcoustID key (free): https://acoustid.org/login\nGet AudD key (free tier): https://audd.io",
	Flags: []cli.Flag{
		cli.IntFlag{
			Name:  "duration,d",
			Value: 5,
			Usage: "Recording duration in seconds (1-10)",
		},
		cli.StringFlag{
			Name:  "acrcloud-key",
			Usage: "ACRCloud access key (overrides ACRCLOUD_ACCESS_KEY env var)",
		},
		cli.StringFlag{
			Name:  "acrcloud-secret",
			Usage: "ACRCloud access secret (overrides ACRCLOUD_ACCESS_SECRET env var)",
		},
		cli.StringFlag{
			Name:  "acrcloud-host",
			Usage: "ACRCloud host (overrides ACRCLOUD_HOST env var)",
		},
		cli.StringFlag{
			Name:  "acoustid-key",
			Usage: "AcoustID client key (overrides ACOUSTID_API_KEY env var)",
		},
		cli.StringFlag{
			Name:  "audd-key,api-key",
			Usage: "AudD API key (overrides AUDD_API_KEY env var)",
		},
		cli.StringFlag{
			Name:  "type",
			Value: "chords",
			Usage: "Preferred tab type: chords or tabs",
		},
		cli.BoolFlag{
			Name:  "no-chords",
			Usage: "Skip the chord diagram summary",
		},
	},
	Action: listenAction,
}

func listenAction(c *cli.Context) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Fatal("ffmpeg not found. Install it first:\n  Linux:  apt install ffmpeg\n  macOS:  brew install ffmpeg\n  Windows: https://ffmpeg.org/download.html")
	}

	acrKey := c.String("acrcloud-key")
	if acrKey == "" {
		acrKey = os.Getenv("ACRCLOUD_ACCESS_KEY")
	}
	acrSecret := c.String("acrcloud-secret")
	if acrSecret == "" {
		acrSecret = os.Getenv("ACRCLOUD_ACCESS_SECRET")
	}
	acrHost := c.String("acrcloud-host")
	if acrHost == "" {
		acrHost = os.Getenv("ACRCLOUD_HOST")
	}

	acoustidKey := c.String("acoustid-key")
	if acoustidKey == "" {
		acoustidKey = os.Getenv("ACOUSTID_API_KEY")
	}

	auddKey := c.String("audd-key")
	if auddKey == "" {
		auddKey = os.Getenv("AUDD_API_KEY")
	}

	if acrKey == "" && acoustidKey == "" && auddKey == "" {
		log.Fatal("At least one recognition API key is required.\n\n" +
			"ACRCloud (2000/month free) — https://acrcloud.com\n" +
			"  Set: ACRCLOUD_ACCESS_KEY, ACRCLOUD_ACCESS_SECRET, ACRCLOUD_HOST\n\n" +
			"AcoustID (free, unlimited) — https://acoustid.org/login\n" +
			"  Set: ACOUSTID_API_KEY\n\n" +
			"AudD (100/month free) — https://audd.io\n" +
			"  Set: AUDD_API_KEY")
	}

	var acrClient *acrcloud.Client
	if acrKey != "" && acrSecret != "" && acrHost != "" {
		acrClient = &acrcloud.Client{Host: acrHost, AccessKey: acrKey, AccessSecret: acrSecret}
	} else if acrKey != "" {
		color.New(color.FgYellow).Fprintln(os.Stderr,
			"Warning: ACRCloud needs ACRCLOUD_ACCESS_KEY, ACRCLOUD_ACCESS_SECRET and ACRCLOUD_HOST — skipping")
	}

	var acoustidClient *acoustid.Client
	if acoustidKey != "" {
		if _, err := exec.LookPath("fpcalc"); err != nil {
			color.New(color.FgYellow).Fprintln(os.Stderr,
				"Warning: fpcalc not found — AcoustID disabled.")
		} else {
			acoustidClient = &acoustid.Client{APIKey: acoustidKey}
		}
	}

	var auddClient *audd.Client
	if auddKey != "" {
		auddClient = &audd.Client{APIKey: auddKey}
	}

	if acrClient == nil && acoustidClient == nil && auddClient == nil {
		log.Fatal("No recognition service available. Check your API keys.")
	}

	duration := c.Int("duration")
	if duration < 1 {
		duration = 1
	}
	if duration > 10 {
		duration = 10
	}

	tabTypePref := c.String("type")
	noChords := c.Bool("no-chords")

	tmp, err := os.CreateTemp("", "ug-listen-*.wav")
	if err != nil {
		log.Fatal("Failed to create temp file: ", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	s := ultimateguitar.New()
	preferChords := tabTypePref != "tabs"
	primaryType := ultimateguitar.TabTypeChords
	fallbackType := ultimateguitar.TabTypeTabs
	if !preferChords {
		primaryType = ultimateguitar.TabTypeTabs
		fallbackType = ultimateguitar.TabTypeChords
	}

	gray := color.New(color.FgHiBlack)

	for {
		color.New(color.FgYellow).Printf("Listening for %d seconds...\n", duration)

		bar := pb.New(duration * 10)
		bar.SetRefreshRate(100 * time.Millisecond)
		bar.Start()
		done := make(chan struct{})
		go func() {
			for i := 0; i < duration*10; i++ {
				select {
				case <-done:
					return
				case <-time.After(100 * time.Millisecond):
					bar.Increment()
				}
			}
		}()

		ffCmd := exec.Command("ffmpeg", ffmpegArgs(duration, tmp.Name())...)
		ffCmd.Stderr = nil
		if err := ffCmd.Run(); err != nil {
			close(done)
			bar.Finish()
			fmt.Fprintf(os.Stderr, "ffmpeg recording failed: %v\n", err)
			goto prompt
		}
		close(done)
		bar.Finish()

		{
			fmt.Println("Identifying song...")
			result, err := identify(acrClient, acoustidClient, auddClient, tmp.Name())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Song identification error: %v\n", err)
				goto prompt
			}
			if result == nil {
				fmt.Println("Song not recognized. Try a clearer source or --duration 8.")
				goto prompt
			}

			color.New(color.FgGreen, color.Bold).Printf("Detected: %s — %s\n\n", result.Title, result.Artist)

			searchTitle := cleanTitle(result.Title) + " " + result.Artist
			searchResult, err := s.Search(ultimateguitar.SearchParams{
				Title: searchTitle,
				Type:  []ultimateguitar.TabType{primaryType},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Search error: %v\n", err)
				goto prompt
			}
			if len(searchResult.Tabs) == 0 {
				searchResult, err = s.Search(ultimateguitar.SearchParams{
					Title: searchTitle,
					Type:  []ultimateguitar.TabType{fallbackType},
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Search error: %v\n", err)
					goto prompt
				}
			}
			if len(searchResult.Tabs) == 0 {
				fmt.Printf("No tabs found for \"%s\" by %s.\n", result.Title, result.Artist)
				goto prompt
			}

			best := selectBestTab(searchResult.Tabs)
			tab, err := s.GetTabByID(best.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to fetch tab: %v\n", err)
				goto prompt
			}

			printTab(tab, noChords)
		}

	prompt:
		fmt.Println()
		gray.Print("Press Enter to listen again, or Ctrl+C to quit...")
		bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Println()
	}
}

// identify tries ACRCloud → AcoustID → AudD in order, returning the first hit.
// Returns nil, nil when no service recognizes the song.
func identify(acr *acrcloud.Client, ac *acoustid.Client, ad *audd.Client, audioPath string) (*audd.Result, error) {
	if acr != nil {
		r, err := acr.Recognize(audioPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ACRCloud: %v — trying next...\n", err)
		} else if r != nil {
			return r, nil
		}
	}
	if ac != nil {
		r, err := ac.Recognize(audioPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "AcoustID: %v — trying next...\n", err)
		} else if r != nil {
			return r, nil
		}
	}
	if ad != nil {
		return ad.Recognize(audioPath)
	}
	return nil, nil
}

func ffmpegArgs(duration int, outPath string) []string {
	var inputArgs []string
	switch runtime.GOOS {
	case "darwin":
		inputArgs = []string{"-f", "avfoundation", "-i", ":0"}
	case "windows":
		inputArgs = []string{"-f", "dshow", "-i", "audio=Microphone"}
	default:
		inputArgs = []string{"-f", "alsa", "-i", "default"}
	}
	return append(inputArgs,
		"-t", fmt.Sprintf("%d", duration),
		"-ar", "44100",
		"-ac", "1",
		"-y",
		outPath,
	)
}

func selectBestTab(tabs []ultimateguitar.Tab) *ultimateguitar.Tab {
	var best *ultimateguitar.Tab
	bestScore := -1.0

	for i := range tabs {
		t := &tabs[i]
		score := t.Rating * math.Log(float64(t.Votes)+1)
		if score > bestScore {
			bestScore = score
			best = t
		}
	}

	if bestScore == 0 {
		for i := range tabs {
			t := &tabs[i]
			if best == nil || t.Rating > best.Rating {
				best = t
			}
		}
	}

	return best
}

var (
	reFeat        = regexp.MustCompile(`(?i)\s*[\(\[]feat\.?.*?[\)\]]`)
	reFt          = regexp.MustCompile(`(?i)\s*[\(\[]ft\.?.*?[\)\]]`)
	reNoiseSuffix = regexp.MustCompile(`(?i)\s*[-–]\s*(single version|radio edit|live|remastered.*|acoustic.*|official.*|original.*)\s*$`)
	reParenNoise  = regexp.MustCompile(`(?i)\s*\((live|remastered.*|acoustic.*)\)`)
)

func cleanTitle(s string) string {
	s = reFeat.ReplaceAllString(s, "")
	s = reFt.ReplaceAllString(s, "")
	s = reNoiseSuffix.ReplaceAllString(s, "")
	s = reParenNoise.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var (
	reChTag  = regexp.MustCompile(`\[ch\](.*?)\[/ch\]`)
	reTabTag = regexp.MustCompile(`\[/?tab\]`)
)

func printTab(tab ultimateguitar.TabResult, noChords bool) {
	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan, color.Bold)
	white := color.New(color.FgWhite, color.Bold)
	gray := color.New(color.FgHiBlack)
	yellow := color.New(color.FgYellow, color.Bold)
	divider := strings.Repeat("═", 60)

	fmt.Println(divider)
	fmt.Printf("  ")
	cyan.Printf("%s", tab.SongName)
	fmt.Printf("  by  ")
	white.Printf("%s\n", tab.ArtistName)

	meta := fmt.Sprintf("  %s", tab.Type)
	if tab.Tuning != "" {
		meta += fmt.Sprintf("  |  Tuning: %s", tab.Tuning)
	}
	if tab.Capo > 0 {
		meta += fmt.Sprintf("  |  Capo: %d", tab.Capo)
	}
	if tab.Rating > 0 {
		meta += fmt.Sprintf("  |  ★ %.1f (%d votes)", tab.Rating, tab.Votes)
	}
	gray.Println(meta)
	fmt.Println(divider)

	if !noChords && len(tab.Applicature) > 0 {
		fmt.Println()
		bold.Println("Chords used:")
		const perRow = 4
		for i, a := range tab.Applicature {
			if i > 0 && i%perRow == 0 {
				fmt.Println()
			}
			frets := ""
			if len(a.Variations) > 0 {
				frets = a.Variations[0].ID
			}
			yellow.Printf("  %-6s", a.Chord)
			gray.Printf("%-12s", frets)
		}
		fmt.Println()
	}

	fmt.Println()

	content := reChTag.ReplaceAllStringFunc(tab.Content, func(m string) string {
		chord := reChTag.FindStringSubmatch(m)[1]
		return yellow.Sprint(chord)
	})
	content = reTabTag.ReplaceAllString(content, "")

	fmt.Println(content)

	if tab.URLWeb != "" {
		fmt.Println()
		gray.Printf("── Source: %s\n", tab.URLWeb)
	}
}
