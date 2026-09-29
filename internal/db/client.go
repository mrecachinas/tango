package db

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"tango/internal/config"
	"tango/internal/state"
)

// ClientOptions builds driver options from cfg. A URI, when set, takes
// precedence over host/port/db; explicit user/password/auth source are
// applied on top of whatever the URI specifies.
func ClientOptions(cfg config.Config) (*options.ClientOptions, string) {
	opts := options.Client().SetAppName("tango")
	var label string
	if cfg.URI != "" {
		opts.ApplyURI(cfg.URI)
		label = redactedHosts(cfg.URI)
	} else {
		hostPort := net.JoinHostPort(cfg.MongoURL, strconv.Itoa(cfg.MongoPort))
		uri := "mongodb://" + hostPort
		if cfg.MongoDB != "" {
			uri += "/" + url.PathEscape(cfg.MongoDB)
		}
		opts.ApplyURI(uri)
		label = hostPort
	}
	if cfg.MongoUser != "" || cfg.AuthSource != "" {
		var cred options.Credential
		if opts.Auth != nil {
			cred = *opts.Auth
		}
		if cfg.MongoUser != "" {
			cred.Username = cfg.MongoUser
			cred.Password = cfg.MongoPassword
			cred.PasswordSet = cfg.MongoPassword != ""
		}
		if cfg.AuthSource != "" {
			cred.AuthSource = cfg.AuthSource
		}
		if cred.Username != "" || cred.AuthMechanism != "" {
			opts.SetAuth(cred)
		}
	}
	return opts, label
}

// redactedHosts returns the host part of a connection string without credentials.
func redactedHosts(uri string) string {
	rest := uri
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	return rest
}

// Connect connects and verifies the server is reachable within timeout.
func Connect(cfg config.Config, timeout time.Duration) (*mongo.Client, string, error) {
	opts, label := ClientOptions(cfg)
	opts.SetServerSelectionTimeout(timeout)
	if err := opts.Validate(); err != nil {
		return nil, label, fmt.Errorf("invalid connection settings: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, label, err
	}
	if err := client.Ping(ctx, readpref.Nearest()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, label, fmt.Errorf("cannot reach %s: %w", label, err)
	}
	return client, label, nil
}

// DetectPrivileges reads the connected user's privileges. On failure it
// fails closed (no write privileges) and returns the error.
func DetectPrivileges(client *mongo.Client) (*state.Privileges, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var status bson.M
	err := client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "connectionStatus", Value: 1},
		{Key: "showPrivileges", Value: true},
	}).Decode(&status)
	if err != nil {
		return &state.Privileges{}, err
	}
	return state.ParsePrivileges(status), nil
}
