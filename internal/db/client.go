package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/config"
	"tango/internal/state"
)

func MongoConnect(cfg config.Config, ctx context.Context) *mongo.Client {
	uri := fmt.Sprintf("mongodb://%s:%d", cfg.MongoURL, cfg.MongoPort)
	if cfg.MongoDB != "" {
		uri += "/" + cfg.MongoDB
	}
	clientOpts := options.Client().ApplyURI(uri)
	if cfg.MongoUser != "" {
		clientOpts.SetAuth(options.Credential{
			Username: cfg.MongoUser,
			Password: cfg.MongoPassword,
		})
	}

	c, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %s", err)
	}
	return c
}

func DetectReadOnly() {
	s := state.S
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var status bson.M
	err := s.Client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "connectionStatus", Value: 1},
		{Key: "showPrivileges", Value: false},
	}).Decode(&status)
	if err != nil {
		// Can't determine privileges — fail closed
		s.ReadOnly = true
		return
	}

	authInfo, ok := status["authInfo"].(bson.M)
	if !ok {
		s.ReadOnly = true
		return
	}

	roles, ok := authInfo["authenticatedUserRoles"].(bson.A)
	if !ok || len(roles) == 0 {
		// No auth = no restrictions (local dev)
		s.ReadOnly = false
		return
	}

	writeRoles := map[string]bool{
		"readWrite": true, "readWriteAnyDatabase": true,
		"dbAdmin": true, "dbAdminAnyDatabase": true,
		"userAdmin": true, "userAdminAnyDatabase": true,
		"clusterAdmin": true, "root": true,
		"dbOwner": true, "__system": true,
	}

	for _, r := range roles {
		role, ok := r.(bson.M)
		if !ok {
			continue
		}
		roleName, _ := role["role"].(string)
		if writeRoles[roleName] {
			s.ReadOnly = false
			return
		}
	}

	s.ReadOnly = true
}

func MongoClose(c *mongo.Client, ctx context.Context) {
	if err := c.Disconnect(ctx); err != nil {
		log.Printf("Error disconnecting from MongoDB: %s", err)
	}
}
