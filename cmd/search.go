package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"text/tabwriter"
	"os"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/ultimateguitar"
	"github.com/urfave/cli"
)

var SearchCmd = cli.Command{
	Name:        "search",
	Usage:       "ug search -q 'wonderwall' [-type 300] [-page 1] [-json]",
	Description: "Search for tabs/chords on Ultimate Guitar",
	Aliases:     []string{"s"},
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "q",
			Usage: "Search query (song title or artist + title)",
		},
		cli.IntFlag{
			Name:  "type",
			Value: 300,
			Usage: "Tab type: 300=Chords, 200=Tab, 400=Bass, 700=Drums",
		},
		cli.IntFlag{
			Name:  "page",
			Value: 1,
			Usage: "Result page number",
		},
		cli.BoolFlag{
			Name:  "json",
			Usage: "Output raw JSON instead of formatted table",
		},
	},
	Action: searchTabs,
}

func searchTabs(c *cli.Context) {
	query := c.String("q")
	if query == "" {
		log.Fatal("Error: -q flag is required")
	}

	tabType := int32(c.Int("type"))
	page := int32(c.Int("page"))
	asJSON := c.Bool("json")

	s := ultimateguitar.New()
	results, err := s.Search(ultimateguitar.SearchParams{
		Title: query,
		Type:  []ultimateguitar.TabType{ultimateguitar.TabType(tabType)},
		Page:  page,
	})
	if err != nil {
		log.Fatal(err)
	}

	if asJSON {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(out))
		return
	}

	if len(results.Tabs) == 0 {
		fmt.Println("No results found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tARTIST\tSONG\tTYPE\tVERSION\tRATING\tDIFFICULTY")
	fmt.Fprintln(w, "--\t------\t----\t----\t-------\t------\t----------")
	for _, tab := range results.Tabs {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%.2f\t%s\n",
			tab.ID,
			tab.ArtistName,
			tab.SongName,
			tab.TypeName,
			tab.Version,
			tab.Rating,
			tab.Difficulty,
		)
	}
	w.Flush()
}
