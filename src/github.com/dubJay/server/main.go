package main

import (
	"database/sql"
	"flag"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"text/template"

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
	landingQuery = `SELECT timestamp, title, next, previous, paragraph, image FROM entry ORDER BY timestamp DESC LIMIT 1`
	historyQuery = `SELECT timestamp, title FROM entry ORDER BY timestamp DESC`
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

type entry_serving struct {
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

type history_meta struct {
	Title string
	Path  string
}

type history_entry struct {
	Year     int
	Metadata []history_meta
}

type history_serving []history_entry


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

func fromEntry(e entry) entry_serving {
	t := time.Unix(int64(e.entry_id), 0)
	nextStr, prevStr := "", ""
	if e.next != 0 {
		nextStr = filepath.Join("/entry", strconv.Itoa(e.next))
	}
	if e.previous != 0 {
		prevStr = filepath.Join("/entry", strconv.Itoa(e.previous))
	}

	return entry_serving{
		Title: e.title,
		NextPath: nextStr,
		PrevPath: prevStr,
		Month: t.Month().String(),
		Day: strconv.Itoa(t.Day()),
		Year: strconv.Itoa(t.Year()),
		Content: strings.Split(e.content, `\n`),
		Image: strings.Split(e.image, `\n`),
	}
}

func fromHistory(h []history) history_serving {
	m := make(map[int]map[int]string)
	for _, entry := range h {
		t := time.Unix(int64(entry.entry_id), 0)
		if _, ok := m[t.Year()]; !ok {
			m[t.Year()] = make(map[int]string)
		}
		m[t.Year()][entry.entry_id] = entry.title
	}

	var histServe history_serving
	for key, value := range m {
		history := history_entry{}
		history.Year = key
		for ik, iv := range value {
			history.Metadata = append(history.Metadata,
				history_meta{Title: iv, Path: filepath.Join("/entry", strconv.Itoa(ik))})
		}
		histServe = append(histServe, history)
	}

	return histServe
}

func buildLandingPage(w http.ResponseWriter, req *http.Request) {
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

func getEntry(id int) (entry, error) {
	// Get entry at id. If id is empty get most recent entry.
	page := entry{}
	if id == 0 {
		rows, err := db.Query(landingQuery)
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
	log.Fatal(http.ListenAndServe(*port, router))
}
