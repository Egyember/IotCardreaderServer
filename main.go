package main

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"text/template"
	"time"

	"server/frontend"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var embedFs embed.FS

var (
	database *sql.DB
	htmltmpl *htmltemplate.Template
	txttmpl  *template.Template
)

var (
	dbpath   = flag.String("dbpath", "./database.db", "path to the db file")
	addUser  = flag.Bool("add", false, "add admin to database")
	adminTab = flag.Bool("A", false, "add admin with adminTab permission")
	username = flag.String("u", "", "username when adding user to db")
	password = flag.String("p", "", "username when adding user to db")
)

type (
	card struct {
		SerialNumber string `json:"serialnumber"`
		Authtoken    string `json:"authtoken"`
		WriteKey     string `json:"writekey"`
		ReadKey      string `json:"readkey"`
		Owner        int    `json:"owner"`
	}
	cardReader struct {
		Id        int
		ApiKey    string
		AddCard   bool
		WriteCard bool
	}
	people struct {
		Id         int    `json:"ok"`
		Name       string `json:"name"`
		Permission string `json:"perm"`
	}
	keyRequest struct {
		ApiKey       string `json:"apikey"`
		SerialNumber string `json:"serialnumber"`
		Write        bool   `json:"write"`
	}
	keyAns struct {
		Ok  bool   `json:"ok"`
		Key string `json:"key"`
	}
	verifyRequest struct {
		ApiKey       string `json:"apikey"`
		Authtoken    string `json:"authtoken"`
		SerialNumber string `json:"serialnumber"`
	}
	verifyAns struct {
		Ok         bool   `json:"ok"`
		Name       string `json:"name"`
		Permission string `json:"perm"`
	}
)

func verifyRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-type") != "application/json" {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var request verifyRequest
	err = json.Unmarshal(body, &request)
	if err != nil {
		ans := verifyAns{
			Ok:         false,
			Name:       "",
			Permission: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		return
	}
	tx, err := database.Begin()
	defer tx.Rollback()
	if err != nil {
		panic(err)
	}
	row := tx.QueryRow("SELECT apiKey FROM reader WHERE apiKey = ? LIMIT 1", request.ApiKey)
	err = row.Err()
	if err != nil {
		ans := verifyAns{
			Ok:         false,
			Name:       "",
			Permission: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad api key")
		return
	}
	row = tx.QueryRow("SELECT people.name, people.permission FROM cards WHERE cards.authtoken = ? and cards.serialNumber = ? INNER JOIN people LIMIT 1", request.Authtoken, request.SerialNumber)
	Name := ""
	Perm := ""
	err = row.Scan(&Name, &Perm)
	if err != nil {
		ans := verifyAns{
			Ok:         false,
			Name:       "",
			Permission: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad serial number & auth key")
		return
	}
	ans := verifyAns{
		Ok:         true,
		Name:       Name,
		Permission: Perm,
	}
	js, err := json.Marshal(ans)
	if err != nil {
		panic(err)
	}
	w.Write(js)
}

func main() {
	flag.Parse()
	// init templates

	htmlfs, err := fs.Sub(embedFs, "templates/htmltemplates")
	if err != nil {
		panic(err)
	}
	frontend.Htmltmpl = htmltemplate.Must(htmltmpl.ParseFS(htmlfs, "*html"))
	txtfs, err := fs.Sub(embedFs, "templates/txttemplates")
	if err != nil {
		panic(err)
	}
	txttmpl = template.Must(template.ParseFS(txtfs, "*tmpl"))
	frontend.Txttmpl = txttmpl
	// init a new db if file dosn't exist
	if _, err := os.Stat(*dbpath); errors.Is(err, os.ErrNotExist) {
		fd, err := os.Create(*dbpath)
		if err != nil {
			panic(err)
		}
		fd.Close()
		database, err := sql.Open("sqlite3", *dbpath)
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		tx, err := database.Begin()
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		create := new(bytes.Buffer)
		err = txttmpl.ExecuteTemplate(create, "create.sql.tmpl", nil)
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		_, err = tx.Exec(create.String())
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		err = tx.Commit()
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		database.Close()
	}
	// open db connection
	database, err = sql.Open("sqlite3", *dbpath)
	if err != nil {
		panic(err)
	}
	database.SetMaxOpenConns(2)
	defer database.Close()
	if *addUser {
		tx, _ := database.Begin()
		_, err := tx.Exec("INSERT INTO admins (username, pwhash, adminTab) VALUES (?, ?, ?)", *username, frontend.ComputepwHash([]byte(*password)), *adminTab)
		if err != nil {
			fmt.Println("failed: ", err.Error())
			tx.Rollback()
		} else {
			tx.Commit()
		}
		return
	}

	frontend.Database = database

	// frontend cooki store init
	frontend.Authstore.Cookies = make([]frontend.Authcookie, 0)
	frontend.Authstore.Ticker = *time.NewTicker(1 * time.Hour)
	frontend.Authstore.Done = make(chan bool)
	go frontend.Authstore.Clean()
	frontend.AddEndpoints()

	http.HandleFunc("POST /api/request/verify", verifyRequestHandler)
	http.ListenAndServe(":8090", nil)

	frontend.Authstore.Done <- true
}
