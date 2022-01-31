package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	glog "github.com/gravwell/gravwell/v3/ingest/log"
)

var (
	fHost   = flag.String("host", os.Getenv("HOST"), "vsphere host")
	fUser   = flag.String("username", os.Getenv("USERNAME"), "vsphere username")
	fPass   = flag.String("password", os.Getenv("PASSWORD"), "vsphere password")
	fTarget = flag.String("cleartext-target", os.Getenv("CLEARTEXT_TARGET"), "Gravwell cleartext target")
	fSecret = flag.String("ingest-secret", os.Getenv("INGEST_SECRET"), "Gravwell ingest secret")
	fTag    = flag.String("ingest-tag", os.Getenv("INGEST_TAG"), "Gravwell ingest tag")
)

var (
	datastoreSampleInterval time.Duration = time.Minute
	runtimeSampleInterval   time.Duration = 5 * time.Second
	apiTimeout                            = 5 * time.Second
)

func main() {
	flag.Parse()
	if *fHost == `` {
		log.Fatal("missing host")
	} else if *fUser == `` || *fPass == `` {
		log.Fatal("missing username or password")
	} else if *fTarget == `` {
		log.Fatal("missing ingest target")
	} else if *fSecret == `` {
		log.Fatal("missing ingest secret")
	} else if *fTag == `` {
		log.Fatal("missing ingest tag")
	}

	// Configure the ingester
	ingestConfig := ingest.UniformMuxerConfig{
		Auth:         *fSecret,
		Destinations: []string{"tcp://" + *fTarget},
		Tags:         []string{*fTag},
		IngesterName: "Vmware Stats",
	}

	// Start the ingester
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		log.Fatalf("Failed build our ingest system: %v\n", err)
	}
	defer igst.Close()
	if err := igst.Start(); err != nil {
		log.Fatalf("Failed start our ingest system: %v\n", err)
	}

	// Wait for connection to indexers
	if err := igst.WaitForHot(0); err != nil {
		log.Fatalf("Timedout waiting for backend connections: %v\n", err)
	}

	// Generate and send some entries
	tag, err := igst.GetTag(*fTag)
	if err != nil {
		log.Fatalf("Failed to get tag: %v", err)
	}
	ctx := context.Background()
	cli, err := NewClient(ctx, *fHost, *fUser, *fPass)
	if err != nil {
		log.Fatalf("Failed to get client: %v\n", err)
	}
	defer cli.Logout(ctx)

	dsTckr := time.NewTicker(datastoreSampleInterval)
	defer dsTckr.Stop()
	statsTckr := time.NewTicker(runtimeSampleInterval)
	defer statsTckr.Stop()

	for {
		select {
		case <-dsTckr.C:
			lctx, cf := context.WithTimeout(ctx, apiTimeout)
			if err = sampleDatastores(lctx, igst, cli, tag); err != nil {
				igst.Errorf("failed to sample datastores", glog.KVErr(err))
			}
			cf()
		case <-statsTckr.C:
			lctx, cf := context.WithTimeout(ctx, apiTimeout)
			if err = sampleHostAndVMs(lctx, igst, cli, tag); err != nil {
				igst.Error("failed to sample CPU/Memory", glog.KVErr(err))
			}
			cf()
		}
	}
}

func sampleDatastores(ctx context.Context, igst *ingest.IngestMuxer, cli *client, tag entry.EntryTag) (err error) {
	var datastores map[string]Datastore
	if datastores, err = cli.HostDsMetrics(ctx); err != nil {
		return
	}
	//API is so damn slow, no point in having precision here
	ts := time.Now().Truncate(time.Second).UTC()
	for k, v := range datastores {
		var data []byte
		h := struct {
			Type string
			Name string
			Datastore
		}{
			Type:      `datastore`,
			Name:      k,
			Datastore: v,
		}
		if data, err = json.Marshal(h); err != nil {
			return fmt.Errorf("failed to marshal host %w", err)
		}
		if err = igst.WriteContext(ctx, entry.FromStandard(ts), tag, data); err != nil {
			return
		}
	}

	return
}

func sampleHostAndVMs(ctx context.Context, igst *ingest.IngestMuxer, cli *client, tag entry.EntryTag) (err error) {
	var hosts map[string]Host
	var vms map[string]VM
	if hosts, err = cli.HostCpuMemoryMetrics(ctx); err != nil {
		return fmt.Errorf("failed to sample hosts %w", err)
	}
	//API is so damn slow, no point in having precision here
	ts := time.Now().Truncate(time.Second).UTC()
	//send the host data
	for k, v := range hosts {
		var data []byte
		h := struct {
			Type string
			Name string
			Host
		}{
			Type: `host`,
			Name: k,
			Host: v,
		}
		if data, err = json.Marshal(h); err != nil {
			return fmt.Errorf("failed to marshal host %w", err)
		}
		if err = igst.WriteContext(ctx, entry.FromStandard(ts), tag, data); err != nil {
			return
		}
	}

	//now do it with VM metrics
	if vms, err = cli.VMMetrics(ctx, hosts); err != nil {
		return fmt.Errorf("Failed to sample VMs %w", err)
	}
	//API is so damn slow, no point in having precision here
	ts = time.Now().Truncate(time.Second).UTC()
	//send the host data
	for k, v := range vms {
		var data []byte
		h := struct {
			Type string
			Name string
			VM
		}{
			Type: `guest`,
			Name: k,
			VM:   v,
		}
		if data, err = json.Marshal(h); err != nil {
			return fmt.Errorf("failed to marshal VM %w", err)
		}
		if err = igst.WriteContext(ctx, entry.FromStandard(ts), tag, data); err != nil {
			return
		}
	}

	return
}
