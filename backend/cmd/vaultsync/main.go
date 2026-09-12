// vaultsync materializes the secrets vault into one running container.
//
// The vault pushes to containers when it is edited through the service. An
// operator who edited DATA_DIR by hand — or restored it from a backup — has
// no such edit to ride on, and `lxc start` goes around the platform entirely.
// This is the one-shot that converges a container without a vault write.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/config"
	"github.com/futrx-com/remote.futrx.com/internal/integration/lxc"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	"github.com/futrx-com/remote.futrx.com/internal/stores"
)

func main() {
	projectID := flag.String("project", "", "project id to converge")
	container := flag.String("container", "", "its container name")
	flag.Parse()
	if *projectID == "" || *container == "" {
		log.Fatal("both -project and -container are required")
	}

	cfg := config.Load()
	storeSet, err := stores.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("init stores: %v", err)
	}
	agentModules, err := config.NewAgentModules()
	if err != nil {
		log.Fatalf("configure agent modules: %v", err)
	}
	stack := config.NewContainerStack(lxc.New(), agentModules.Profiles(), config.ContainerStackOptions{})

	vault := serviceglobalsecrets.New(
		storeSet.GlobalSecrets,
		serviceglobalsecrets.WithContainers(stack.Environment, stack.Secrets),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// nil own-keys: the project's own secrets are not being changed here, so
	// nothing of the project's shadows the vault for this pass.
	if err := vault.SyncContainer(ctx, *projectID, *container, nil); err != nil {
		log.Fatalf("sync %s: %v", *container, err)
	}
	log.Printf("vault converged into %s", *container)
}
