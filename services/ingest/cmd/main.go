package main

import (
	"flag"
	"fmt"
	"os"

	"kinga/services/ingest/pkg/ingest"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: kinga-ingest <source.csv> <dest.jsonl>\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	src := args[0]
	dst := args[1]

	count, err := ingest.ConvertCSVFile(src, dst)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ingestion error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully ingested %d canonical events into %s\n", count, dst)
}
