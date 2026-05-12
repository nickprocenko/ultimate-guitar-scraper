package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/ultimateguitar"
	"github.com/urfave/cli"
)

var FetchTabURL = cli.Command{
	Name:        "fetch-url",
	Usage:       "ug fetch-url -url 'https://tabs.ultimate-guitar.com/tab/...' [-json]",
	Description: "Fetch a tab from Ultimate Guitar by its URL",
	Aliases:     []string{"fu"},
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "url",
			Usage: "Full Ultimate Guitar tab URL",
		},
		cli.BoolFlag{
			Name:  "json",
			Usage: "Output raw JSON",
		},
	},
	Action: fetchTabByURL,
}

func fetchTabByURL(c *cli.Context) {
	ugURL := c.String("url")
	if ugURL == "" {
		log.Fatal("Error: -url flag is required")
	}

	asJSON := c.Bool("json")

	s := ultimateguitar.New()

	// Resolve URL to tab ID
	urlResult, err := s.TabByURL(ugURL)
	if err != nil {
		log.Fatal("Could not resolve URL: ", err)
	}

	// Fetch full tab content by ID
	tab, err := s.GetTabByID(int64(urlResult.ID))
	if err != nil {
		log.Fatal(err)
	}

	if asJSON {
		out, err := json.MarshalIndent(tab, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(out))
		return
	}

	fmt.Println("----------------------------------------------------------------------")
	fmt.Printf("Song: %s by %s\n", tab.SongName, tab.ArtistName)
	if tab.TonalityName != "" {
		fmt.Printf("Key: %s\n", tab.TonalityName)
	}
	if tab.Capo > 0 {
		fmt.Printf("Capo: %d\n", tab.Capo)
	}
	if tab.Tuning.Value != "" {
		fmt.Printf("Tuning: %s\n", tab.Tuning.Value)
	}
	fmt.Println("----------------------------------------------------------------------")

	tabOut := strings.ReplaceAll(tab.Content, "[tab]", "")
	tabOut = strings.ReplaceAll(tabOut, "[/tab]", "")
	tabOut = strings.ReplaceAll(tabOut, "[ch]", "")
	tabOut = strings.ReplaceAll(tabOut, "[/ch]", "")
	fmt.Println(tabOut)
}
