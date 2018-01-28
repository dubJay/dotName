package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"text/template"

	"github.com/gorilla/feeds"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db *sql.DB
	tmpls map[string]*template.Template

	dbPath    = flag.String("dbPath", "db/testDB.db", "Datafile to use")
	port      = flag.String("port", ":8080", "Port for server to listen on")
	rootDir   = flag.String("rootDir", "", "Path to webdir structure")
	templates = flag.String("templates", "templates", "Templates directory")
)

// Queries for db actions.
var (
	entryQuery   = `SELECT timestamp, title, next, previous, paragraph, image FROM entry WHERE timestamp = ?`
	landingQuery = `SELECT timestamp, title, next, previous, paragraph, image FROM entry ORDER BY timestamp DESC LIMIT ?`
	historyQuery = `SELECT timestamp, title FROM entry ORDER BY timestamp DESC`
	oneoffQuery  = `SELECT uid, paragraph, image from oneoff WHERE uid = ?`
)

const (
	entryPage   = "entry.html"
	historyPage = "history.html"
	landingPage = "index.html"
)

type entry struct {
	// Timestamp, UID.
	entry_id  int
	title     string
	next      int
	previous  int
	content   string
	image     string
}

type oneoff struct {
	uid       string
	paragraph string
	image     string
}

type entryServing struct {
	Title     string
	NextPath string
	PrevPath string
	Month     string
	Day       string
	Year      string
	Content   []string
	Image     []string
}

type history struct {
	entry_id int
	title    string
}

type historyMeta struct {
	Title string
	Path  string
}

type historyEntry struct {
	Year     int
	Metadata []historyMeta
}

type historyServing []historyEntry


func initDB() {
	var err error
	db, err = sql.Open("sqlite3", filepath.Join(*rootDir, *dbPath))
	if err != nil {
		log.Fatalf("Failed to init db: %v", err)
	}
	if db == nil {
		log.Fatalf("Failed to init db. Database object is empty with path %s", *dbPath)
	}
	log.Print("Database successfully initialized");
}

func initTmpls() {
	var err error
	tmpls = make(map[string]*template.Template)
	tmpls[landingPage], err = template.New(
		landingPage).ParseFiles(filepath.Join(*rootDir, *templates, landingPage))
	if err != nil {
		log.Fatalf("error parsing template %s: %v", landingPage, err)
	}
	tmpls[entryPage], err = template.New(
		entryPage).ParseFiles(filepath.Join(*rootDir, *templates, entryPage))
	if err != nil {
		log.Fatalf("error parsing template %s: %v", entryPage, err)
	}
	tmpls[historyPage], err = template.New(
		historyPage).ParseFiles(filepath.Join(*rootDir, *templates, historyPage))
	if err != nil {
		log.Fatalf("error parsing template %s: %v", historyPage, err)
	}
	log.Print("Templates successfully initialized");
}

func splitTextBlob(s string) []string {
	return strings.Split(s, `\n`)
}

func fromEntry(e entry) entryServing {
	t := time.Unix(int64(e.entry_id), 0)
	nextStr, prevStr := "", ""
	if e.next != 0 {
		nextStr = filepath.Join("/entry", strconv.Itoa(e.next))
	}
	if e.previous != 0 {
		prevStr = filepath.Join("/entry", strconv.Itoa(e.previous))
	}

	return entryServing{
		Title: e.title,
		NextPath: nextStr,
		PrevPath: prevStr,
		Month: t.Month().String(),
		Day: strconv.Itoa(t.Day()),
		Year: strconv.Itoa(t.Year()),
		Content: splitTextBlob(e.content),
		Image: splitTextBlob(e.image),
	}
}

func fromOneOff(o oneoff) entryServing {
	return entryServing{
		Title: o.uid,
		Content: splitTextBlob(o.paragraph),
		Image: splitTextBlob(o.image),
	}
}

// This is a mess. Need to revist. 
func fromHistory(h []history) historyServing {
	m := make(map[int]map[int]string)
	sk := make(map[int][]int)
	for _, entry := range h {
		t := time.Unix(int64(entry.entry_id), 0)
		if _, ok := m[t.Year()]; !ok {
			m[t.Year()] = make(map[int]string)
			sk[t.Year()] = []int{}
		}
		m[t.Year()][entry.entry_id] = entry.title
		sk[t.Year()] = append(sk[t.Year()], entry.entry_id)
	}

	var histServe historyServing
	for key := range m {
		history := historyEntry{}
		history.Year = key
		sort.Sort(sort.Reverse(sort.IntSlice(sk[key])))
		for _, entryId := range sk[key] {
			history.Metadata = append(history.Metadata,
				historyMeta{Title: m[key][entryId], Path: filepath.Join("/entry", strconv.Itoa(entryId))})
		}
		histServe = append(histServe, history)
	}
	return histServe
}

func buildOneOff(w http.ResponseWriter, uid string) error {
	oneoff, err := getOneOff(uid)
	if err != nil {
		return fmt.Errorf("unable to find oneoff entry: %v", err)
	}
	serving := fromOneOff(oneoff)
	
	return tmpls[entryPage].Execute(w, serving)
}

func buildLandingPage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if vars["id"] != "" {
		err := buildOneOff(w, vars["id"])
		if err != nil {
			log.Printf("failed to build oneoff page: %v", err)
		} else {
			// If oneoff build was successful we don't need the landing page.
			return
		}
	} 
	entry, err := getEntry(0)
	if err != nil {
		log.Printf("failed to get entry: %v", err)
		return
	}
	serving := fromEntry(entry)

	if err := tmpls[landingPage].Execute(w, serving); err != nil {
		log.Printf("error executing template %s: %v", landingPage, err)
	}
}

func buildPage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if len(vars["id"]) == 0 {
		buildLandingPage(w, r)
	}
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		log.Printf("invalid id: %v", err)
		return
	}
	entry, err := getEntry(id)
	if err != nil {
		log.Printf("failed to get entry: %v", err)
		return
	}
	serving := fromEntry(entry)

	if err := tmpls[entryPage].Execute(w, serving); err != nil {
		log.Printf("error executing template %s: %v", entryPage, err)
	}
}

func buildNavPage(w http.ResponseWriter, r *http.Request) {
	entries, err := getHistory()
	if err != nil {
		log.Printf("unable to retrieve history entries: %v", err)
		return
	}
	serving := fromHistory(entries)

	if err := tmpls[historyPage].Execute(w, serving); err != nil {
		log.Printf("error executing template %s: %v", historyPage, err)
	}
}

func buildFeedPage(w http.ResponseWriter, r *http.Request) {
        vars := mux.Vars(r)
	if len(vars["type"]) == 0 {
		log.Print("no type requested by user.")
		http.Error(w, "no feed type specified by user.", http.StatusPreconditionRequired)
		return
	}
	contains := func(s []string, e string) bool {
		for _, a := range s {
			if a == e {
				return true
			}
		}
		return false
	}
	if !contains([]string{"atom.xml", "rss.xml", "jsonfeed.json"}, vars["type"]) {
		log.Print("invalid type requested by user.")
		http.Error(w, "invalid type requested by user.", http.StatusPreconditionFailed)
		return
	}
	
	entries, err := getRecentEntries(15)
	if err != nil {
		log.Printf("unable to retrieve history entries: %v", err)
		http.Error(w, "failed to retrieve recent entries.", http.StatusInternalServerError)
		return
	}

	feed := &feeds.Feed{
		Title:       "Christopher Cawdrey's Blog",
		Link:        &feeds.Link{Href: "https://christopher.cawdrey.name"},
		Description: "Chris' musings, projects, and dispositions.",
		Author:      &feeds.Author{Name: "Christopher Cawdrey", Email: "chris@cawdrey.name"},
		Created:     time.Unix(1489554739, 0),
	}
	for _, entry := range entries {
		descriptionCutOff := 50
		if len(entry.content) < descriptionCutOff {
			descriptionCutOff = len(entry.content)
		}

		feed.Items = append(feed.Items,
			&feeds.Item{
				Title:       entry.title,
				Id:          strconv.Itoa(entry.entry_id),
				Link:        &feeds.Link{Href: strings.Join([]string{"https://christopher.cawdrey.name/entry/", strconv.Itoa(entry.entry_id)}, "")},
				Description: entry.content[:descriptionCutOff] + "...",
				Created:     time.Unix(int64(entry.entry_id), 0),
			})
	}

	switch vars["type"] {
	case "atom.xml":
		atom, err := feed.ToAtom()
		if err != nil {
			log.Printf("failed to create atom feed %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write([]byte(atom))
	case "rss.xml":
		rss, err := feed.ToRss()
		if err != nil {
			log.Printf("failed to create rss feed %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return

		}
		w.Write([]byte(rss))
	default:
		json, err := feed.ToJSON()
		if err != nil {
			log.Printf("failed to create json feed %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write([]byte(json))
	}
}

func getRecentEntries(limit int) ([]entry, error) {
	rows, err := db.Query(landingQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []entry
	for rows.Next() {
		entry := entry{}
		err := rows.Scan(&entry.entry_id, &entry.title, &entry.next, &entry.previous, &entry.content, &entry.image)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func getOneOff(id string) (oneoff, error) {
	oneoff := oneoff{}
	err := db.QueryRow(oneoffQuery, id).Scan(&oneoff.uid, &oneoff.paragraph, &oneoff.image)
	return oneoff, err
}

func getEntry(id int) (entry, error) {
	// Get entry at id. If id is empty get most recent entry.
	page := entry{}
	if id == 0 {
		rows, err := db.Query(landingQuery, 1)
		if err != nil {
			return page, err
		}
		defer rows.Close()

		for rows.Next() {
			err := rows.Scan(
				&page.entry_id, &page.title, &page.next, &page.previous, &page.content, &page.image)
			if err != nil {
				return page, err
			}
			break
		}
	} else {
		err := db.QueryRow(entryQuery, id).Scan(
			&page.entry_id, &page.title, &page.next, &page.previous, &page.content, &page.image)
		if err != nil {
			return page, err
		}
	}
	return page, nil
}

func getHistory() ([]history, error) {
	rows, err := db.Query(historyQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []history
	for rows.Next() {
		entry := history{}
		err := rows.Scan(&entry.entry_id, &entry.title)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func main() {
	flag.Parse()

	initDB()
	initTmpls()

	router := mux.NewRouter()
	router.HandleFunc("/", buildLandingPage).Methods("GET")
	router.HandleFunc("/entry/{id}", buildPage).Methods("GET")
	router.HandleFunc("/history", buildNavPage).Methods("GET")
	router.HandleFunc("/feeds/{type}", buildFeedPage).Methods("GET")
	router.HandleFunc("/{id}", buildLandingPage).Methods("GET")
	log.Fatal(http.ListenAndServe(*port, router))
}
