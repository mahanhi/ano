// Command anosim plays a lab household into a new database and prints the cards.
// It does not open the live Ano database and it does not start the gateway.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ano/ano/internal/sim"
	"github.com/ano/ano/internal/store"
)

func main() {
	path := flag.String("db", "", "database file. The default is a new file under the temp directory.")
	flag.Parse()
	db := *path
	if db == "" {
		db = filepath.Join(os.TempDir(), "ano-sim.db")
		_ = os.Remove(db)
	}
	st, err := store.Open(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer st.Close()
	result, err := sim.Play(st)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Lab household in %s. Mode is %s. Streaming stayed direct: %t.\n", db, result.Mode, result.StreamStays)
	for _, card := range result.Cards {
		fmt.Printf("%s  %s  %s %s  left %d  kept %d\n", card.DeviceName, card.Destination, card.Status, card.Action, card.Metrics.BeforeBytes, card.Metrics.AfterBytes)
	}
}
