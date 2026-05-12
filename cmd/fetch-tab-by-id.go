package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/Pilfer/ultimate-guitar-scraper/pkg/ultimateguitar"
	"github.com/urfave/cli"
)

var FetchTab = cli.Command{
	Name:        "fetch",
	Usage:       "ug f -id {tabId} [-json]",
	Description: "Fetch a tab from ultimate-guitar.com by id",
	Aliases:     []string{"f"},
	Flags: []cli.Flag{
		cli.Int64Flag{
			Name:  "id",
			Value: 1947141,
			Usage: "",
		},
		cli.BoolFlag{
			Name:  "json",
			Usage: "Output raw JSON instead of formatted text",
		},
	},
	Action: fetchTabByID,
}

func fetchTabByID(c *cli.Context) {
	var tabID int64

	if c.IsSet("id") {
		tabID = c.Int64("id")
	}

	s := ultimateguitar.New()
	tab, err := s.GetTabByID(tabID)

	if err != nil {
		log.Fatal(err)
	}

	if c.Bool("json") {
		out, err := json.MarshalIndent(tab, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(out))
		return
	}

	fmt.Println("----------------------------------------------------------------------")
	fmt.Println("Song name:", tab.SongName, " by ", tab.ArtistName)
	fmt.Println("----------------------------------------------------------------------")

	tabOut := strings.ReplaceAll(tab.Content, "[tab]", "")
	tabOut = strings.ReplaceAll(tabOut, "[/tab]", "")
	tabOut = strings.ReplaceAll(tabOut, "[ch]", "")
	tabOut = strings.ReplaceAll(tabOut, "[/ch]", "")
	fmt.Println(tabOut)
}
