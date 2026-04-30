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
	Description: "Listen to audio, detect the song, and display its chords. Requires ffmpeg and an AudD API key (https://audd.io, free tier: 100/month). Set AUDD_API_KEY env var or use --api-key.",
	Flags: []cli.Flag{
		cli.IntFlag{
			Name:  "duration,d",
			Value: 5,
			Usage: "Recording duration in seconds (1-10)",
		},
		cli.StringFlag{
			Name:  "api-key",
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

	apiKey := c.String("api-key")
	if apiKey == "" {
		apiKey = os.Getenv("AUDD_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("AudD API key required. Set AUDD_API_KEY env var or use --api-key.\nGet a free key at https://audd.io (100 recognitions/month free).")
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

	client := &audd.Client{APIKey: apiKey}
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
			result, err := client.Recognize(tmp.Name())
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

func ffmpegArgs(duration int, outPath string) []string {
	var inputArgs []string
	switch runtime.GOOS {
	case "darwin":
		inputArgs = []string{"-f", "avfoundation", "-i", ":0"}
	case "windows":
		inputArgs = []string{"-f", "dshow", "-i", "audio=Microphone"}
	default:
		// Linux: try alsa, pulse is common too
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

	// All zero-vote tabs: fall back to highest rating
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
