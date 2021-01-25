package main

import (
	"context"
	"encoding/json"
	"fmt"
	flag "github.com/spf13/pflag"
	"log"
	"time"

	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	MongoURL        string  `json:"mongo_url"`
	MongoPort       int     `json:"mongo_port"`
	MongoUser       string  `json:"mongo_user"`
	MongoPassword   string  `json:"mongo_password"`
	MongoDB         string  `json:"mongo_db"`
	MongoCollection string  `json:"mongo_collection"`
	RefreshRate     float64 `json:"refresh_rate"`
}

const (
	finderPage = "*finder*" // The name of the Finder page.
)

var (
	app         *tview.Application // The tview application.
	pages       *tview.Pages       // The application pages.
	finderFocus tview.Primitive    // The primitive in the Finder that last had focus.
)

func main() {
	var cfg Config
	flag.StringVar(&cfg.MongoURL, "mongo-host", "localhost", "")
	flag.IntVar(&cfg.MongoPort, "mongo-port", 27017, "")
	flag.StringVarP(&cfg.MongoDB, "mongo-db", "d", "", "")
	flag.StringVarP(&cfg.MongoUser, "mongo-user", "u", "", "")
	flag.StringVarP(&cfg.MongoPassword, "mongo-password", "p", "", "")
	flag.Parse()

	// Start the application.
	app = tview.NewApplication()
	finder(cfg)
	if err := app.Run(); err != nil {
		fmt.Printf("Error running application: %s\n", err)
	}
}

func MongoConnect(cfg Config, ctx context.Context) *mongo.Client {
	credentials := ""
	if cfg.MongoUser != "" && cfg.MongoPassword != "" {
		credentials = fmt.Sprintf("%s:%s", cfg.MongoUser, cfg.MongoPassword)
	}
	uri := fmt.Sprintf("mongodb://%s%s:%d", credentials, cfg.MongoURL, cfg.MongoPort)
	if cfg.MongoDB != "" {
		uri = fmt.Sprintf("%s/%s", uri, cfg.MongoDB)
	}
	if cfg.MongoCollection != "" {
		uri = fmt.Sprintf("%s/%s", uri, cfg.MongoCollection)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}

	return client
}

func MongoClose(client *mongo.Client, ctx context.Context) {
	if err := client.Disconnect(ctx); err != nil {
		panic(err)
	}
}

func finder(cfg Config) {
	// Create the basic objects.
	databases := tview.NewList().ShowSecondaryText(true)
	databases.SetBorder(true).SetTitle("Databases")
	//documents := tview.NewTable().SetBorders(true)
	documents := tview.NewList().ShowSecondaryText(false)
	documents.SetBorder(true).SetTitle("Documents")
	collections := tview.NewList()
	collections.ShowSecondaryText(true).
		SetDoneFunc(func() {
			collections.Clear()
			documents.Clear()
			app.SetFocus(databases)
		})
	collections.SetBorder(true).SetTitle("Collections")

	// Create the layout.
	flex := tview.NewFlex().
		AddItem(databases, 0, 1, true).
		AddItem(collections, 0, 1, false).
		AddItem(documents, 0, 3, false)

	// Connect to MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := MongoConnect(cfg, ctx)
	defer MongoClose(client, ctx)

	// Get a list of databases
	databaseNames, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		log.Fatal(err)
	}

	// Add database names to left panel
	for _, db := range databaseNames {
		collNames, err := client.Database(db).ListCollectionNames(ctx, bson.D{})
		if err != nil {
			panic(err)
		}
		numCollections := len(collNames)
		secondaryDBText := fmt.Sprintf("(%d)", numCollections)
		databases.AddItem(db, secondaryDBText, 0, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// A database was selected. Show all of its collections.
			documents.Clear()
			collections.Clear()
			client := MongoConnect(cfg, ctx)
			defer MongoClose(client, ctx)
			for _, collectionName := range collNames {
				numDocs, err := client.Database(db).Collection(collectionName).EstimatedDocumentCount(ctx)
				if err != nil {
					panic(err)
				}
				countText := fmt.Sprintf("(%d)", numDocs)
				collections.AddItem(collectionName, countText, 0, nil)
			}
			app.SetFocus(collections)
			collections.SetChangedFunc(func(i int, collectionName string, t string, s rune) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				// A collection was selected. Show its documents.
				documents.Clear()
				client := MongoConnect(cfg, ctx)
				defer MongoClose(client, ctx)
				cursor, err := client.Database(db).Collection(collectionName).Find(ctx, bson.D{})
				if err != nil {
					panic(err)
				}
				for cursor.Next(ctx) {
					// A new result variable should be declared for each document.
					var result bson.M
					if err := cursor.Decode(&result); err != nil {
						log.Fatal(err)
					}
					jsonDoc, err := json.Marshal(result)
					if err != nil {
						panic(err)
					}
					documents.AddItem(string(jsonDoc), "", 0, nil)
					//collections.SetCurrentItem(0) // Trigger the initial selection.
					//tables.SetSelectedFunc(func(i int, tableName string, t string, s rune) {
					//	content(db, dbName, tableName)
					//})
				}
			})
		})
	}

	// Set up the pages and show the Finder.
	pages = tview.NewPages().
		AddPage(finderPage, flex, true, true)
	app.SetRoot(pages, true)
}
