package main

import (
	"bytes"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	htmltemplate "html/template"
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

	// cooki store init
	frontend.Authstore.Cookies = make([]frontend.Authcookie, 0)
	frontend.Authstore.Ticker = *time.NewTicker(1 * time.Hour)
	frontend.Authstore.Done = make(chan bool)
	go frontend.Authstore.Clean()

	http.HandleFunc("GET /{$}", frontend.RootHandler)
	http.Handle("/admin", frontend.LoginNeeded(http.HandlerFunc(frontend.Admin), false))
	cardsHandler := frontend.TableFactory("cards", []string{"serialNumber", "writeKey", "readKey", "owner"}, "cards")
	cardsAdd := frontend.AddFactory("cards", []string{"serialNumber", "writeKey", "readKey", "owner"}, []string{"text", "text", "text", "number"}, "cards")
	http.Handle("/admin/cards", frontend.LoginNeeded(http.HandlerFunc(cardsHandler), false))
	http.Handle("/admin/cards/add", frontend.LoginNeeded(http.HandlerFunc(cardsAdd), false))
	readerHandler := frontend.TableFactory("readers", []string{"id", "apiKey", "addCard", "writeCard"}, "reader")
	readerAdd := frontend.AddFactory("readers", []string{"id", "apiKey", "addCard", "writeCard"}, []string{"number", "text", "number", "number"}, "reader")
	http.Handle("/admin/readers", frontend.LoginNeeded(http.HandlerFunc(readerHandler), false))
	http.Handle("/admin/readers/add", frontend.LoginNeeded(http.HandlerFunc(readerAdd), false))
	peopleHandler := frontend.TableFactory("people", []string{"id", "authtoken", "name", "permission"}, "people")
	peopleAdd := frontend.AddFactory("people", []string{"id", "authtoken", "name", "permission"}, []string{"number", "text", "text", "text"}, "people")
	http.Handle("/admin/people", frontend.LoginNeeded(http.HandlerFunc(peopleHandler), false))
	http.Handle("/admin/people/add", frontend.LoginNeeded(http.HandlerFunc(peopleAdd), false))
	logHandler := frontend.TableFactory("logs", []string{"id", "card", "reader", "people", "allowed", "direction", "comment"}, "accessLog")
	http.Handle("/admin/logs", frontend.LoginNeeded(http.HandlerFunc(logHandler), false))
	adminsHandler := frontend.TableFactory("admins", []string{"id", "username", "pwhash", "adminTab"}, "admins")
	http.Handle("/admin/admins", frontend.LoginNeeded(http.HandlerFunc(adminsHandler), true))
	http.Handle("/admin/logout", frontend.LoginNeeded(http.HandlerFunc(frontend.Logout), false))
	http.HandleFunc("/admin/login", frontend.Login)
	http.ListenAndServe(":8090", nil)

	frontend.Authstore.Done <- true
}
