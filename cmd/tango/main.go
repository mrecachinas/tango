package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rivo/tview"
	flag "github.com/spf13/pflag"
	"golang.org/x/term"

	"tango/internal/config"
	"tango/internal/db"
	"tango/internal/documents"
	"tango/internal/metrics"
	"tango/internal/state"
	"tango/internal/ui"
	"tango/internal/version"
)

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "tango: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")

	var cfg config.Config
	flag.StringVar(&cfg.URI, "uri", "", "MongoDB connection string (mongodb:// or mongodb+srv://); env TANGO_MONGO_URI")
	flag.StringVar(&cfg.MongoURL, "mongo-host", "localhost", "MongoDB host (ignored with --uri)")
	flag.IntVar(&cfg.MongoPort, "mongo-port", 27017, "MongoDB port (ignored with --uri)")
	flag.StringVarP(&cfg.MongoDB, "mongo-db", "d", "", "Default database (also the default auth source)")
	flag.StringVarP(&cfg.MongoUser, "mongo-user", "u", "", "Username")
	flag.StringVarP(&cfg.MongoPassword, "mongo-password", "p", "", "Password (prefer TANGO_MONGO_PASSWORD or the interactive prompt; flags are visible in ps)")
	flag.StringVar(&cfg.AuthSource, "auth-source", "", "Authentication database")
	flag.Float64Var(&cfg.RefreshRate, "refresh-rate", 2, "Seconds between metrics refreshes")
	var profileName string
	flag.StringVar(&profileName, "profile", "", "Connection profile name from ~/.tango.yaml")
	var timeoutSecs int
	flag.IntVar(&timeoutSecs, "timeout", 10, "Query timeout in seconds")
	flag.Parse()

	if showVersion {
		fmt.Printf("tango %s (%s)\n", version.Version, version.Commit)
		os.Exit(0)
	}

	// Precedence: command-line flag > environment > profile > default.
	changed := flag.CommandLine.Changed
	if profileName != "" {
		profile, err := config.LoadProfile(profileName)
		if err != nil {
			fatalf("loading profile: %s", err)
		}
		setStr := func(flagName string, dst *string, v string) {
			if !changed(flagName) && v != "" {
				*dst = v
			}
		}
		setStr("uri", &cfg.URI, profile.URI)
		setStr("mongo-host", &cfg.MongoURL, profile.MongoURL)
		setStr("mongo-db", &cfg.MongoDB, profile.MongoDB)
		setStr("mongo-user", &cfg.MongoUser, profile.MongoUser)
		setStr("mongo-password", &cfg.MongoPassword, profile.MongoPassword)
		setStr("auth-source", &cfg.AuthSource, profile.AuthSource)
		if !changed("mongo-port") && profile.MongoPort != 0 {
			cfg.MongoPort = profile.MongoPort
		}
		if !changed("refresh-rate") && profile.RefreshRate != 0 {
			cfg.RefreshRate = profile.RefreshRate
		}
	}
	if v := os.Getenv("TANGO_MONGO_URI"); v != "" && !changed("uri") {
		cfg.URI = v
	}
	if v := os.Getenv("TANGO_MONGO_PASSWORD"); v != "" && !changed("mongo-password") {
		cfg.MongoPassword = v
	}
	if cfg.MongoUser != "" && cfg.MongoPassword == "" && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "Password for %s: ", cfg.MongoUser)
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fatalf("reading password: %s", err)
		}
		cfg.MongoPassword = string(pw)
	}
	if timeoutSecs < 1 {
		fatalf("--timeout must be at least 1 second")
	}
	refresh := time.Duration(cfg.RefreshRate * float64(time.Second))
	if refresh < 500*time.Millisecond {
		refresh = 500 * time.Millisecond
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	client, hostLabel, err := db.Connect(cfg, timeout)
	if err != nil {
		fatalf("%s", err)
	}
	privs, privErr := db.DetectPrivileges(client)

	state.S = &state.AppState{
		Client:          client,
		GlobalCfg:       cfg,
		HostLabel:       hostLabel,
		QueryTimeout:    timeout,
		RefreshInterval: refresh,
		SelectedDocs:    make(map[int]bool),
		QueryHistoryIdx: -1,
		Privileges:      privs,
		ReadOnly:        !privs.AnyWrite(),
		NoKeyset:        map[state.Namespace]bool{},
	}
	s := state.S
	state.LoadState()

	s.App = tview.NewApplication()
	ui.BuildUI()
	if privErr != nil {
		state.FlashError("Couldn't read privileges (" + privErr.Error() + "); write operations disabled")
	}
	documents.LoadDatabases(func() { documents.RestoreSession(s.LastDB, s.LastCollection) })
	metrics.Start(refresh)

	if err := s.App.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %s\n", err)
	}

	state.SaveState()

	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer disconnectCancel()
	_ = client.Disconnect(disconnectCtx)
}
