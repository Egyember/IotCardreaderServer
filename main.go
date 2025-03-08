package main

import (
	"bytes"
	"context"
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

	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var embedFs embed.FS

var (
	dbpath   = flag.String("dbpath", "./database.db", "path to the db file")
	database *sql.DB
	htmltmpl *htmltemplate.Template
	sqltmpl  *template.Template
)

type (
	username   string
	headerdata struct {
		Uname    username
		Loggedin bool
		Title    string
	}
	card struct {
		SerialNumber string
		WriteKey     string
		ReadKey      string
		Owner        int
	}
	carddata struct {
		Status headerdata
		Cards  []card
	}
	cardReader struct {
		Id        int
		ApiKey    string
		AddCard   bool
		WriteCard bool
	}
	readerData struct {
		Reader []cardReader
		Status headerdata
	}
)

func rootHandler(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, "./admin", http.StatusSeeOther)
}

func loginNeeded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := r.Cookie("AUTH")
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}
		}
		// todo: verifie the cookie
		uname := username("user")
		cont := r.Context()
		cont = context.WithValue(cont, username("uname"), uname)
		next.ServeHTTP(w, r.WithContext(cont))
	})
}

func login(w http.ResponseWriter, r *http.Request) {
	status := headerdata{Loggedin: false, Title: "login"}
	fmt.Println("render login")
	err := htmltmpl.ExecuteTemplate(w, "login.html", status)
	if err != nil {
		fmt.Println(err)
	}
}

func admin(w http.ResponseWriter, r *http.Request) {
	cont := r.Context()
	uname := cont.Value(username("uname")).(username)
	status := headerdata{Loggedin: true, Title: "main", Uname: username(uname)}
	fmt.Println("render main")
	err := htmltmpl.ExecuteTemplate(w, "home.html", status)
	if err != nil {
		fmt.Println(err)
	}
}

func reader(w http.ResponseWriter, r *http.Request) {
	cont := r.Context()
	status := headerdata{Loggedin: true, Title: "readers", Uname: username(cont.Value(username("uname")).(username))}
	tx, err := database.Begin()
	if err != nil {
		fmt.Println(err)
		return
	}
	rows, err := tx.Query("Select id, apiKey, addCard, writeCard from reader")
	if err != nil {
		fmt.Println(err)
		tx.Rollback()
		return
	}
	defer rows.Close()
	var data readerData
	data.Status = status
	data.Reader = make([]cardReader, 0)
	for rows.Next() {
		var c cardReader
		err := rows.Scan(&c.Id, &c.ApiKey, &c.AddCard, &c.WriteCard)
		if err != nil {
			fmt.Println("error: " + err.Error())
			tx.Rollback()
			return
		}
		data.Reader = append(data.Reader, c)
	}
	tx.Commit()
	fmt.Println("render reader")
	err = htmltmpl.ExecuteTemplate(w, "reader.html", data)
	if err != nil {
		fmt.Println(err)
	}
}

func cards(w http.ResponseWriter, r *http.Request) {
	cont := r.Context()
	status := headerdata{Loggedin: true, Title: "cards", Uname: username(cont.Value(username("uname")).(username))}
	tx, err := database.Begin()
	if err != nil {
		fmt.Println(err)
		return
	}
	rows, err := tx.Query("Select serialNumber, writeKey, readKey, owner from cards")
	if err != nil {
		fmt.Println(err)
		tx.Rollback()
		return
	}
	defer rows.Close()
	var data carddata
	data.Status = status
	data.Cards = make([]card, 0)
	for rows.Next() {
		var c card
		err := rows.Scan(&c.SerialNumber, &c.WriteKey, &c.ReadKey, &c.Owner)
		if err != nil {
			fmt.Println("error: " + err.Error())
			tx.Rollback()
			return
		}
		data.Cards = append(data.Cards, c)
	}
	tx.Commit()
	fmt.Println("render cards")
	err = htmltmpl.ExecuteTemplate(w, "cards.html", data)
	if err != nil {
		fmt.Println(err)
	}
}

func main() {
	flag.Parse()
	// init templates

	htmlfs, err := fs.Sub(embedFs, "templates/htmltemplates")
	if err != nil {
		panic(err)
	}
	htmltmpl = htmltemplate.Must(htmltmpl.ParseFS(htmlfs, "*html"))
	sqlfs, err := fs.Sub(embedFs, "templates/sqltemplates")
	if err != nil {
		panic(err)
	}
	sqltmpl = template.Must(template.ParseFS(sqlfs, "*sql"))
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
		err = sqltmpl.ExecuteTemplate(create, "create.sql", nil)
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
	defer database.Close()
	http.HandleFunc("GET /{$}", rootHandler)
	http.Handle("/admin", loginNeeded(http.HandlerFunc(admin)))
	http.Handle("/admin/cards", loginNeeded(http.HandlerFunc(cards)))
	http.Handle("/admin/readers", loginNeeded(http.HandlerFunc(reader)))
	http.HandleFunc("/admin/login", login)
	http.ListenAndServe(":8090", nil)
}
