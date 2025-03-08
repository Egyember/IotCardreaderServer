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
	txttmpl  *template.Template
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
	cardReader struct {
		Id        int
		ApiKey    string
		AddCard   bool
		WriteCard bool
	}
	people struct {
		id         int
		authtoken  string
		name       string
		permission string
	}
	renderData struct {
		Status headerdata
		Filds  []map[string]any
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

func tableFactory(title string, fildNames []string) http.HandlerFunc {
	sqlfilds := ""
	for _, v := range fildNames {
		sqlfilds += v
		sqlfilds += ", "
	}
	sqlfilds = sqlfilds[0:(len(sqlfilds) - 2)]
	query := fmt.Sprintf("Select %s from people", sqlfilds)
	args := struct {
		FildNames []string
		Url       string
	}{fildNames, title}
	buff := new(bytes.Buffer)
	txttmpl.ExecuteTemplate(buff, "magic.html.tmpl", args)
	templ, err := htmltmpl.Clone()
	if err != nil {
		panic(err)
	}
	templ, err = templ.Parse(buff.String())
	if err != nil {
		fmt.Println("fuck this shit")
		panic(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		cont := r.Context()
		status := headerdata{Loggedin: true, Title: title, Uname: username(cont.Value(username("uname")).(username))}
		tx, err := database.Begin()
		if err != nil {
			fmt.Println(err)
			return
		}
		rows, err := tx.Query(query)
		if err != nil {
			fmt.Println(err)
			tx.Rollback()
			return
		}
		defer rows.Close()

		var data renderData
		data.Status = status
		data.Filds = make([]map[string]any, 0)
		cols, _ := rows.Columns()
		for rows.Next() {
			// Create a slice of interface{}'s to represent each column,
			// and a second slice to contain pointers to each item in the columns slice.
			columns := make([]interface{}, len(cols))
			columnPointers := make([]interface{}, len(cols))
			for i := range columns {
				columnPointers[i] = &columns[i]
			}

			err := rows.Scan(columnPointers...)
			if err != nil {
				fmt.Println("error: " + err.Error())
				tx.Rollback()
				return
			}
			m := make(map[string]interface{})
			for i, colName := range cols {
				val := columnPointers[i].(*interface{})
				m[colName] = *val
			}
			data.Filds = append(data.Filds, m)
		}
		fmt.Println(data.Filds)
		tx.Commit()
		fmt.Println("render people")
		err = templ.ExecuteTemplate(w, "magic", data)
		if err != nil {
			fmt.Println(err)
		}
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
	txtfs, err := fs.Sub(embedFs, "templates/txttemplates")
	if err != nil {
		panic(err)
	}
	txttmpl = template.Must(template.ParseFS(txtfs, "*tmpl"))
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
	defer database.Close()
	http.HandleFunc("GET /{$}", rootHandler)
	http.Handle("/admin", loginNeeded(http.HandlerFunc(admin)))
	cardsHandler := tableFactory("cards", []string{"serialNumber", "writeKey", "readKey", "owner"})
	http.Handle("/admin/cards", loginNeeded(http.HandlerFunc(cardsHandler)))
	readerHandler := tableFactory("readers", []string{"id", "apiKey", "addCard", "writeCard"})
	http.Handle("/admin/readers", loginNeeded(http.HandlerFunc(readerHandler)))
	peopleHandler := tableFactory("people", []string{"id", "authtoken", "name", "permission"})
	http.Handle("/admin/people", loginNeeded(http.HandlerFunc(peopleHandler)))
	http.HandleFunc("/admin/login", login)
	http.ListenAndServe(":8090", nil)
}
