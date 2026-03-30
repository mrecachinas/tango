package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rivo/tview"
	flag "github.com/spf13/pflag"

	"tango/internal/config"
	"tango/internal/db"
	"tango/internal/documents"
	"tango/internal/metrics"
	"tango/internal/state"
	"tango/internal/ui"
)

func main() {
	var cfg config.Config
	flag.StringVar(&cfg.MongoURL, "mongo-host", "localhost", "")
	flag.IntVar(&cfg.MongoPort, "mongo-port", 27017, "")
	flag.StringVarP(&cfg.MongoDB, "mongo-db", "d", "", "")
	flag.StringVarP(&cfg.MongoUser, "mongo-user", "u", "", "")
	flag.StringVarP(&cfg.MongoPassword, "mongo-password", "p", "", "")
	var profileName string
	flag.StringVar(&profileName, "profile", "", "Connection profile name from ~/.tango.yaml")
	var timeoutSecs int
	flag.IntVar(&timeoutSecs, "timeout", 10, "Query timeout in seconds")
	flag.Parse()

	if cfg.MongoPassword == "" {
		if envPass := os.Getenv("TANGO_MONGO_PASSWORD"); envPass != "" {
			cfg.MongoPassword = envPass
		}
	}

	if profileName != "" {
		profile, err := config.LoadProfile(profileName)
		if err != nil {
			log.Fatalf("Error loading profile: %s", err)
		}
		// Profile provides base values; CLI flags override them.
		if !flag.CommandLine.Changed("mongo-host") && profile.MongoURL != "" {
			cfg.MongoURL = profile.MongoURL
		}
		if !flag.CommandLine.Changed("mongo-port") && profile.MongoPort != 0 {
			cfg.MongoPort = profile.MongoPort
		}
		if !flag.CommandLine.Changed("mongo-db") && profile.MongoDB != "" {
			cfg.MongoDB = profile.MongoDB
		}
		if !flag.CommandLine.Changed("mongo-user") && profile.MongoUser != "" {
			cfg.MongoUser = profile.MongoUser
		}
		if !flag.CommandLine.Changed("mongo-password") && profile.MongoPassword != "" {
			cfg.MongoPassword = profile.MongoPassword
		}
		if profile.RefreshRate != 0 {
			cfg.RefreshRate = profile.RefreshRate
		}
	}

	state.S = &state.AppState{
		GlobalCfg:       cfg,
		QueryTimeout:    time.Duration(timeoutSecs) * time.Second,
		SelectedDocs:    make(map[int]bool),
		QueryHistoryIdx: -1,
		OpsHistory:      state.NewRollingBuffer(metrics.SparkWidth),
		ConnHistory:     state.NewRollingBuffer(metrics.SparkWidth),
		MemHistory:      state.NewRollingBuffer(metrics.SparkWidth),
		PingHistory:     state.NewRollingBuffer(metrics.SparkWidth),
		QueueHistory:    state.NewRollingBuffer(metrics.SparkWidth),
		NetInHistory:    state.NewRollingBuffer(metrics.SparkWidth),
		CursorsHistory:  state.NewRollingBuffer(metrics.SparkWidth),
		FaultsHistory:   state.NewRollingBuffer(metrics.SparkWidth),
	}

	state.LoadState()

	ctx, cancel := state.NewCtx()
	defer cancel()
	state.S.Client = db.MongoConnect(cfg, ctx)
	db.DetectReadOnly()

	state.S.App = tview.NewApplication()
	ui.BuildUI()
	documents.LoadDatabases()
	metrics.StartMetricsPoller()

	// Restore last session
	if state.S.LastDB != "" {
		go func() {
			time.Sleep(500 * time.Millisecond)
			state.S.App.QueueUpdateDraw(func() {
				documents.SelectDatabase(state.S.LastDB)
				if state.S.LastCollection != "" {
					go func() {
						time.Sleep(500 * time.Millisecond)
						state.S.App.QueueUpdateDraw(func() {
							documents.SelectCollection(state.S.LastCollection)
						})
					}()
				}
			})
		}()
	}

	if err := state.S.App.Run(); err != nil {
		fmt.Printf("Error running application: %s\n", err)
	}

	state.SaveState()

	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer disconnectCancel()
	db.MongoClose(state.S.Client, disconnectCtx)
}
