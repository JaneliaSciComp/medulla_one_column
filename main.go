// Copyright 2013 HHMI.  All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//     * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//     * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//     * Neither the name of HHMI nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
// Author: katzw@janelia.hhmi.org (Bill Katz)
//  Written as part of the FlyEM Project at Janelia Farm Research Center.

// This program serves a simple web page that allows visitors to query
// connection information between two cell names, possibly using wild-cards.
package main

import (
	//	"bufio"
	//	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const helpMessage = `
web_connectome serves a simple web page that allows visitors to
  query connections between named cell types, possibly using wild-cards.

Usage: web_connectome [options]

      -names      =string   File name of cell names CSV (default: %s)
      -connect    =string   File name of connectivity CSV (default: %s)
      -http       =string   Address for HTTP communication
      -debug      (flag)    Run in debug mode.  Verbose.
  -h, -help       (flag)    Show help message
`

const htmlTemplate = `
<!DOCTYPE html>
<html>
  <head>
    <title>Search Results</title>
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
		th, td { text-align: left; padding: 0.3em; }
	</style>
  </head>
  <body>
  	<div align="center">
  		<div align="left"  style="width:80%%">
%s
		</div>
	</div>
  </body>
</html>
`

const (
	DefaultCellsFilename = "cell_names.csv"
	DefaultConnectivityFilename = "connectivity_mat_379.csv"
	DefaultWebAddress = "localhost:8000"

	// The relative URL path to our API
	WebAPIPath = "/api/"
)

var (
	connectivity NamedConnectome 
	cellList CellList 

	cellsFilename = flag.String("names", DefaultCellsFilename, "")
	connectivityFilename = flag.String("connect", DefaultConnectivityFilename, "")
	httpAddress = flag.String("http", DefaultWebAddress, "")

	webPagesDir = filepath.Join(currentDir(), "web_pages")

	showHelp = flag.Bool("help", false, "")
	runDebug = flag.Bool("debug", false, "")
)

func currentDir() string {
	currentDir, err := os.Getwd()
	if err != nil {
		log.Fatalln("Could not get current directory:", err)
	}
	return currentDir
}

// Slice of cell names whose order is important since it matches the
// connectivity matrix.
type CellList []string

func (list CellList) Len() int           { return len(list) }
func (list CellList) Swap(i, j int)      { list[i], list[j] = list[j], list[i] }
func (list CellList) Less(i, j int) bool { return list[i] > list[j] }


type Connection struct {
	pre      string
	post     string
	strength int
}

type ConnectionList []Connection

func (list ConnectionList) Len() int           { return len(list) }
func (list ConnectionList) Swap(i, j int)      { list[i], list[j] = list[j], list[i] }
func (list ConnectionList) Less(i, j int) bool { return list[i].strength > list[j].strength }

func (list ConnectionList) SortByStrength() {
	sort.Sort(list)
}

// NamedConnectome holds strength of connections between two bodies
// that are identified using names (strings) instead of body ids as
// in the Connectome type.
type NamedConnectome map[string](map[string]int)

// GetConnection returns a (pre, post) strength and 'found' bool.
func (nc NamedConnectome) ConnectionStrength(pre, post string) (strength int, found bool) {
	connections, found := nc[pre]
	if found {
		_, found = connections[post]
		if found {
			strength = nc[pre][post]
			if strength == 0 {
				found = false
			}
		}
	}
	return
}

// AddConnection adds a (pre, post) connection of given strength
// to a connectome.
func (nc *NamedConnectome) AddConnection(pre, post string, strength int) {
	if len(*nc) == 0 {
		*nc = make(NamedConnectome)
	}
	connections, preFound := (*nc)[pre]
	if preFound {
		_, postFound := connections[post]
		if postFound {
			(*nc)[pre][post] += strength
		} else {
			(*nc)[pre][post] = strength
		}
	} else {
		(*nc)[pre] = make(map[string]int)
		(*nc)[pre][post] = strength
	}
}

// MatchingNames returns a slice of body names that have prefixes matching
// the given slice of patterns
func (nc NamedConnectome) MatchingNames(patterns []string) (matches []string) {
	matches = make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern[len(pattern)-1:] == "*" {
			// Use as prefix
			pattern = pattern[:len(pattern)-1]
			for name, _ := range nc {
				if strings.HasPrefix(name, pattern) {
					matches = append(matches, name)
				}
			}
		} else {
			// Require exact matching
			_, found := nc[pattern]
			if found {
				matches = append(matches, pattern)
			}
		}
	}
	return
}

// Handler for all web page requests except for API
func mainHandler(w http.ResponseWriter, r *http.Request) {
	path := "index.html"
	if r.URL.Path != "/" {
		path = r.URL.Path
	}
	filename := filepath.Join(webPagesDir, path)
	log.Printf("Serving %s -> %s\n", r.URL.Path, filename)
	http.ServeFile(w, r, filename)
}

// Handler for all search requests, i.e., POST of two cell search patterns.
func searchHandler(w http.ResponseWriter, r *http.Request) {
	action := strings.ToLower(r.Method)
	if action == "post" {
		preNames := r.FormValue("pre")
		postNames := r.FormValue("post")
		results := getSearchHTML(preNames, postNames)
		fmt.Fprintf(w, htmlTemplate, results)
	} else {
		http.Error(w, "Illegal search request.  Requires POST.", http.StatusBadRequest)
	}
}

func getSearchHTML(preNames, postNames string) (text string) {
	pre := strings.Split(preNames, ",")
	post := strings.Split(postNames, ",")
	for i, _ := range pre {
		pre[i] = strings.TrimSpace(pre[i])
	}
	for i, _ := range post {
		post[i] = strings.TrimSpace(post[i])
	}
	connections := make(ConnectionList, 0, len(pre))
	for _, preName := range connectivity.MatchingNames(pre) {
		for _, postName := range connectivity.MatchingNames(post) {
			strength, found := connectivity.ConnectionStrength(preName, postName)
			if found {
				connection := Connection{preName, postName, strength}
				connections = append(connections, connection)
			}
		}
	}
	if len(connections) > 0 {
		connections.SortByStrength()
		text = "<h3>Connections in order of strength:</h3>\n"
		text += "<p>Presynaptic cells in search: " + preNames + "<br />\n"
		text += "Postsynaptic cells in search: " + postNames + "</p>\n"
		text += "<table><tr><th># Synapses</th><th>Presynaptic cell</th><th>Postsynaptic cell</th></tr>\n"
		for _, connection := range connections {
			text += fmt.Sprintf("<tr><td>%d</td><td>%s</td><td>%s</td></tr>", 
				connection.strength, connection.pre, connection.post)
		}
		text += "</table>\n"
	} else {
		text = "<p><strong>No connections found.</strong></p>"
	}
	return
}

func ReadCellsCSV(filename string) (names CellList) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("ERROR: Failed to open cell names csv file: %s [%s]\n",
			filename, err)
	}
	defer file.Close()

	// Reserve enough for the nature paper # of cells
	names = make(CellList, 0, 390)
	csvReader := csv.NewReader(file)

	// Read all connectivity matrix
	for {
		items, err := csvReader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("Error on reading cell list name file (%s): %s", filename, err)
		} else if items[0] == "" {
			continue
		} else {
			names = append(names, items[0])
		}
	}
	log.Printf("Read in %d cell names from %s.\n", len(names), filename)
	return
}


func ReadConnectionsCSV(names CellList, filename string) (connects NamedConnectome) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("ERROR: Failed to open connectome csv file: %s [%s]\n",
			filename, err)
	}
	defer file.Close()

	connects = make(NamedConnectome)
	csvReader := csv.NewReader(file)

	bodyNum := 0
	// Read all connectivity matrix
	for {
		items, err := csvReader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Println("Warning:", err)
		} else if items[0] == "" {
			continue
		} else if len(items) != len(names) {
			log.Fatalf("ERROR: CSV has inconsistent # of columns (%d) vs cell names supplied (%d)!",
				len(items), len(names))
		} else {
			preName := names[bodyNum]
			for i := 0; i < len(items); i++ {
				postName := names[i]
				strength, err := strconv.Atoi(items[i])
				if err != nil {
					log.Fatalln("ERROR: Could not parse CSV line:",
						items, "\nError:", err)
				}
				if strength > 0 {
					connects.AddConnection(preName, postName, strength)
				}
			}
		}
		bodyNum++
	}
	return
}



func main() {
	flag.BoolVar(showHelp, "h", false, "Show help message")
	flag.Usage = func() { 
		fmt.Printf(helpMessage, DefaultCellsFilename, DefaultConnectivityFilename) 
	}
	flag.Parse()

	if flag.NArg() >= 1 && strings.ToLower(flag.Args()[0]) == "help" {
		*showHelp = true
	}

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}
	if *runDebug {
		fmt.Println("Running in Debug mode...")
	}

	// Read the named bodies
	cells := ReadCellsCSV(*cellsFilename)

	// Read the connections
	connectivity = ReadConnectionsCSV(cells, *connectivityFilename)

	fmt.Printf("Ready to serve connections between %d neurons...\n", len(connectivity))

	// Listen and serve HTTP requests using address and don't let stay-alive
	// connections hog goroutines for more than an hour.
	// See for discussion:
	// http://stackoverflow.com/questions/10971800/golang-http-server-leaving-open-goroutines
	fmt.Printf("Web server listening at %s ...\n", *httpAddress)

	src := &http.Server{
		Addr:        *httpAddress,
		ReadTimeout: 1 * time.Hour,
	}

	http.HandleFunc("/search", searchHandler)
	http.HandleFunc("/", mainHandler)

	// Serve it up!
	src.ListenAndServe()
}
