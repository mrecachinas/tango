package documents

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/metrics"
	"tango/internal/state"
)

func LoadDatabases() {
	s := state.S
	state.ShowLoading("Loading databases")

	go func() {
		metricsText := metrics.CollectMetricsData()

		ctx, cancel := state.NewCtx()
		defer cancel()

		dbNames, err := s.Client.ListDatabaseNames(ctx, bson.D{})
		if err != nil {
			log.Fatal(err)
		}

		var allDBItems []string
		totalColls := 0
		for _, db := range dbNames {
			collNames, err := s.Client.Database(db).ListCollectionNames(ctx, bson.D{})
			if err != nil {
				continue
			}
			totalColls += len(collNames)
			item := fmt.Sprintf("%s [gray](%d)", db, len(collNames))
			allDBItems = append(allDBItems, item)
		}

		dbCount := len(dbNames)

		s.App.QueueUpdateDraw(func() {
			state.DoneLoading()
			s.MetricsBar.SetText(metricsText)
			s.AllDBItems = nil
			s.DbFilterText = ""
			for _, item := range allDBItems {
				s.AllDBItems = append(s.AllDBItems, item)
				s.DbList.AddItem(item, "", 0, nil)
			}
			s.CachedDBCount = dbCount
			s.CachedCollCount = totalColls
		})
	}()
}

func SelectDatabase(dbName string) {
	s := state.S
	StopLiveMode()
	state.ShowLoading("Loading collections")

	s.CurrentDB = dbName
	s.CurrentColl = ""
	s.CurrentPage = 0
	s.CurrentFilter = nil
	s.SnapshotID = nil
	s.CountDirty = true
	state.ClearDocPanel()
	s.SearchBar.SetText("")

	go func() {
		ctx, cancel := state.NewCtx()
		defer cancel()

		collNames, err := s.Client.Database(dbName).ListCollectionNames(ctx, bson.D{})
		if err != nil {
			s.App.QueueUpdateDraw(func() {
				state.DoneLoading()
				s.DocDetail.SetText(fmt.Sprintf("[red]Error listing collections: %s", err))
				s.DocPages.SwitchToPage("detail")
			})
			return
		}

		var allCollItems []string
		for _, coll := range collNames {
			count, err := s.Client.Database(dbName).Collection(coll).EstimatedDocumentCount(ctx)
			if err != nil {
				count = -1
			}
			item := fmt.Sprintf("%s [gray](%d)", coll, count)
			allCollItems = append(allCollItems, item)
		}

		s.App.QueueUpdateDraw(func() {
			state.DoneLoading()
			s.CachedCollNames = collNames
			s.AllCollItems = nil
			s.CollFilterText = ""
			for _, item := range allCollItems {
				s.AllCollItems = append(s.AllCollItems, item)
				s.CollList.AddItem(item, "", 0, nil)
			}
			s.CollList.SetTitle(fmt.Sprintf(" Collections [yellow](%s)[white] ", dbName))
			if s.WideMode {
				s.DocPages.SwitchToPage("collections")
			}
			s.App.SetFocus(s.CollList)
			state.RefreshPanelBorders()
		})
	}()
}

func SelectCollection(collName string) {
	s := state.S
	StopLiveMode()
	s.CurrentColl = collName
	s.CurrentPage = 0
	s.CurrentFilter = nil
	s.SnapshotID = nil
	s.CurrentSortField = ""
	s.CurrentSortOrder = 0
	s.CountDirty = true
	ClearSelection()
	s.SearchBar.SetText("")
	LoadDocuments()
}

func LoadDocuments() {
	s := state.S
	state.ShowLoading("Loading documents")
	ClearSelection()

	if s.CurrentDB == "" || s.CurrentColl == "" {
		state.DoneLoading()
		return
	}

	// Capture state before goroutine to avoid races.
	currentDB := s.CurrentDB
	currentColl := s.CurrentColl
	currentPage := s.CurrentPage
	currentFilter := s.CurrentFilter
	currentSortField := s.CurrentSortField
	currentSortOrder := s.CurrentSortOrder
	countDirty := s.CountDirty
	snapshotID := s.SnapshotID

	go func() {
		ctx, cancel := state.NewCtx()
		defer cancel()

		coll := s.Client.Database(currentDB).Collection(currentColl)

		filter := bson.M{}
		if currentFilter != nil {
			filter = currentFilter
		}

		// Capture snapshot on first page load for consistent pagination.
		if currentPage == 0 && snapshotID == nil {
			var maxDoc bson.M
			findOpts := options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})
			if err := coll.FindOne(ctx, filter, findOpts).Decode(&maxDoc); err == nil {
				snapshotID = maxDoc["_id"]
				s.SnapshotID = snapshotID
			}
		}

		// Merge snapshot constraint into the filter.
		effectiveFilter := bson.M{}
		for k, v := range filter {
			effectiveFilter[k] = v
		}
		if snapshotID != nil {
			effectiveFilter["_id"] = bson.M{"$lte": snapshotID}
		}

		var totalDocs int64
		var newCountDirty bool
		var err error
		if len(effectiveFilter) == 0 {
			totalDocs, err = coll.EstimatedDocumentCount(ctx)
			if err != nil {
				s.App.QueueUpdateDraw(func() {
					state.DoneLoading()
					s.DocDetail.SetText(fmt.Sprintf("[red]Error counting documents: %s", err))
					s.DocPages.SwitchToPage("detail")
				})
				return
			}
			newCountDirty = countDirty
		} else if countDirty {
			totalDocs, err = coll.CountDocuments(ctx, effectiveFilter)
			if err != nil {
				s.App.QueueUpdateDraw(func() {
					state.DoneLoading()
					s.DocDetail.SetText(fmt.Sprintf("[red]Error counting documents: %s", err))
					s.DocPages.SwitchToPage("detail")
				})
				return
			}
			newCountDirty = false
		} else {
			totalDocs = s.TotalDocs
			newCountDirty = false
		}

		skip := int64(currentPage * state.PageSize)
		limit := int64(state.PageSize)
		opts := options.Find().SetSkip(skip).SetLimit(limit)
		if currentSortField != "" {
			opts.SetSort(bson.D{{Key: currentSortField, Value: currentSortOrder}})
		}

		cursor, err := coll.Find(ctx, effectiveFilter, opts)
		if err != nil {
			s.App.QueueUpdateDraw(func() {
				state.DoneLoading()
				s.DocDetail.SetText(fmt.Sprintf("[red]Error querying: %s", err))
				s.DocPages.SwitchToPage("detail")
			})
			return
		}
		defer func() { _ = cursor.Close(ctx) }()

		var docs []bson.M
		if err := cursor.All(ctx, &docs); err != nil {
			s.App.QueueUpdateDraw(func() {
				state.DoneLoading()
				s.DocDetail.SetText(fmt.Sprintf("[red]Error decoding results: %s", err))
				s.DocPages.SwitchToPage("detail")
			})
			return
		}

		totalPages := (totalDocs + int64(state.PageSize) - 1) / int64(state.PageSize)
		if totalPages == 0 {
			totalPages = 1
		}

		// Pre-format list items outside the UI thread.
		var listItems []string
		for i, doc := range docs {
			jsonBytes, err := json.Marshal(doc)
			if err != nil {
				listItems = append(listItems, fmt.Sprintf("[red]Error marshalling record %d", i))
				continue
			}
			listItems = append(listItems, string(jsonBytes))
		}

		title := fmt.Sprintf(" Documents [yellow](%s.%s)[white] [green][%d/%d][white] %d total",
			currentDB, currentColl, currentPage+1, totalPages, totalDocs)
		if currentSortField != "" {
			arrow := "↑"
			if currentSortOrder == -1 {
				arrow = "↓"
			}
			title += fmt.Sprintf(" [aqua]⇅ %s %s[white]", currentSortField, arrow)
		}
		if skip > 100000 {
			title += " [red]⚠ deep page — may be slow[white]"
		}
		if snapshotID != nil && currentPage > 0 {
			title += " 📌"
		}
		title += " "

		s.App.QueueUpdateDraw(func() {
			state.DoneLoading()
			s.TotalDocs = totalDocs
			s.CurrentDocs = docs
			s.CountDirty = newCountDirty

			s.DocList.Clear()
			for _, item := range listItems {
				s.DocList.AddItem(item, "", 0, nil)
			}

			s.DocPages.SwitchToPage("list")
			s.DocList.SetTitle(title)
			s.App.SetFocus(s.DocList)
			state.RefreshPanelBorders()
		})
	}()
}

func NextPage() {
	s := state.S
	totalPages := int((s.TotalDocs + int64(state.PageSize) - 1) / int64(state.PageSize))
	if s.CurrentPage < totalPages-1 {
		s.CurrentPage++
		LoadDocuments()
	}
}

func PrevPage() {
	s := state.S
	if s.CurrentPage > 0 {
		s.CurrentPage--
		LoadDocuments()
	}
}

func Refresh() {
	s := state.S
	if s.CurrentDB != "" && s.CurrentColl != "" {
		s.SnapshotID = nil
		s.CountDirty = true
		LoadDocuments()
	} else if s.CurrentDB != "" {
		SelectDatabase(s.CurrentDB)
	} else {
		s.DbList.Clear()
		s.AllDBItems = nil
		s.DbFilterText = ""
		LoadDatabases()
	}
}

func FullRefresh() {
	s := state.S
	s.DbList.Clear()
	s.CollList.Clear()
	s.AllDBItems = nil
	s.AllCollItems = nil
	s.DbFilterText = ""
	s.CollFilterText = ""
	state.ClearDocPanel()
	savedDB := s.CurrentDB
	savedColl := s.CurrentColl
	s.CurrentDB = ""
	s.CurrentColl = ""
	LoadDatabases()
	if savedDB != "" {
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.App.QueueUpdateDraw(func() {
				s.CurrentDB = savedDB
				SelectDatabase(savedDB)
				if savedColl != "" {
					go func() {
						time.Sleep(500 * time.Millisecond)
						s.App.QueueUpdateDraw(func() {
							SelectCollection(savedColl)
						})
					}()
				}
			})
		}()
	}
}

func StartLiveMode() {
	s := state.S
	if s.LiveMode {
		return
	}
	if s.CurrentDB == "" || s.CurrentColl == "" {
		state.ShowError("Select a collection first")
		return
	}

	s.LiveMode = true
	interval := s.LiveInterval
	if interval == 0 {
		interval = 2 * time.Second
	}
	s.LiveTicker = time.NewTicker(interval)
	s.LiveStopCh = make(chan struct{})

	// Refresh title immediately to show live indicator
	s.DocList.SetTitle(state.FormatDocListTitle())
	state.UpdateStatusHints()

	go func() {
		for {
			select {
			case <-s.LiveTicker.C:
				s.App.QueueUpdateDraw(func() {
					if s.LiveMode {
						s.CurrentPage = 0
						s.CountDirty = true
						LoadDocuments()
					}
				})
			case <-s.LiveStopCh:
				return
			}
		}
	}()
}

func StopLiveMode() {
	s := state.S
	if !s.LiveMode {
		return
	}
	s.LiveMode = false
	if s.LiveTicker != nil {
		s.LiveTicker.Stop()
	}
	if s.LiveStopCh != nil {
		close(s.LiveStopCh)
	}
	// Refresh title to remove live indicator
	if s.CurrentDB != "" && s.CurrentColl != "" {
		s.DocList.SetTitle(state.FormatDocListTitle())
	}
	state.UpdateStatusHints()
}

func SelectCollectionInList(name string) {
	s := state.S
	for i := 0; i < s.CollList.GetItemCount(); i++ {
		main, _ := s.CollList.GetItemText(i)
		if idx := strings.Index(main, " [gray]"); idx != -1 {
			main = main[:idx]
		}
		if main == name {
			s.CollList.SetCurrentItem(i)
			return
		}
	}
}
