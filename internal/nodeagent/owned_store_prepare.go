package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

// OwnedStorePreparation is a human native-CLI receipt, not a renderer or node
// protocol DTO. It exposes only the location the user must authenticate on
// this machine; credentials and account state never enter the receipt.
type OwnedStorePreparation struct {
	NodeID                 string `json:"nodeId"`
	Store                  string `json:"store"`
	NativeHome             string `json:"nativeHome"`
	Prepared               bool   `json:"prepared"`
	AuthenticationRequired bool   `json:"authenticationRequired"`
}

func runPrepareOwnedCaelisStore(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("prepare-owned-caelis-store", flag.ContinueOnError)
	flags.SetOutput(out)
	directory := flags.String("directory", "", "existing private enrolled agent directory")
	nodeID := flags.String("node-id", "", "exact existing enrolled native node")
	store := flags.String("store", "", "new private Store; defaults to the enrolled agent's caelis-store")
	if e := flags.Parse(args); e != nil {
		return help(e)
	}
	if flags.NArg() != 0 || !identifier.MatchString(*nodeID) || CheckPrivateDirectory(*directory) != nil {
		return errors.New("exact existing private node enrollment required")
	}
	var identity struct {
		ID string `json:"id"`
	}
	if e := readPrivateJSON(filepath.Join(*directory, "node.json"), &identity); e != nil || identity.ID != *nodeID {
		return errors.New("exact existing node identity required")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if *store == "" {
		*store = filepath.Join(*directory, "caelis-store")
	}
	if e := caelis.PrepareOwnedStore(*nodeID, *store); e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(OwnedStorePreparation{NodeID: *nodeID, Store: *store, NativeHome: filepath.Join(*store, ".native-home"), Prepared: true, AuthenticationRequired: true})
}
